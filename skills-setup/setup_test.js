#!/usr/bin/env node

"use strict";

const assert = require("assert");
const { spawnSync } = require("child_process");
const path = require("path");

const {
  BINARIES,
  SKILL_REPOS,
  DEFAULT_BUNDLE_URL,
  DEFAULT_AGENTS,
  USAGE,
  normalizeList,
  validateValue,
  validateBundleUrl,
  validatePassthru,
  parseArgs,
  buildNpmInstallArgs,
  buildSkillsAddArgs,
  npxArgvForSource,
  resolveBundleUrl,
  extractCandidates,
  formatArg,
  formatCommand,
  splitPath,
  windowsExtensions,
  commandCandidates,
  commandExists,
  detectBinary,
  planSetup,
  aggregateExitCode,
  renderSummary,
  executePlan,
  main,
} = require("./setup");
const pkg = require("./package.json");

const BUNDLE_URL = "https://example.test/skills-bundle.zip";

// A capturing logger so main()/executePlan() never write to the real console.
function captureLog() {
  const out = [];
  const err = [];
  return {
    out,
    err,
    log: (...a) => out.push(a.join(" ")),
    error: (...a) => err.push(a.join(" ")),
    text: () => out.join("\n"),
    errText: () => err.join("\n"),
  };
}

// A fake exec that records calls and never spawns a real process.
function fakeExec(opts) {
  opts = opts || {};
  const failMatchers = opts.fail || []; // substrings of "cmd args" that should fail
  const enoent = opts.enoent || []; // commands that simulate ENOENT
  const calls = [];
  const exec = (cmd, args) => {
    calls.push({ cmd, args });
    const line = [cmd].concat(args).join(" ");
    if (enoent.indexOf(cmd) !== -1) {
      return { error: Object.assign(new Error("spawn ENOENT"), { code: "ENOENT" }), status: null };
    }
    if (failMatchers.some((m) => line.indexOf(m) !== -1)) {
      return { status: 1 };
    }
    return { status: 0 };
  };
  exec.calls = calls;
  return exec;
}

// A fake download that records calls and resolves (or rejects) without network.
function fakeDownload(opts) {
  opts = opts || {};
  const calls = [];
  const fn = async (url, dest) => {
    calls.push({ url, dest });
    if (opts.fail) throw new Error(opts.fail === true ? "boom" : opts.fail);
  };
  fn.calls = calls;
  return fn;
}

// A fake isExecutable that reports a fixed set of "present" command names,
// derived from each candidate path's basename (extension stripped).
function fakeIsExecutable(presentNames) {
  const set = new Set(presentNames);
  return (file) => {
    const base = path.basename(file).replace(/\.[^.]+$/, "");
    return set.has(base) || set.has(path.basename(file));
  };
}

const UNIX_ENV = { PATH: "/usr/local/bin:/usr/bin" };

let passed = 0;
function check(label, fn) {
  fn();
  passed += 1;
}

// --- normalizeList ---------------------------------------------------------
check("normalizeList comma", () => {
  assert.deepStrictEqual(normalizeList([], "a,b"), ["a", "b"]);
  assert.deepStrictEqual(normalizeList(["a"], "b"), ["a", "b"]);
  assert.deepStrictEqual(normalizeList([], " a , , b "), ["a", "b"]);
});

// --- validateValue ---------------------------------------------------------
check("validateValue", () => {
  assert.strictEqual(validateValue("*", "agent"), "*");
  assert.strictEqual(validateValue("claude-code", "agent"), "claude-code");
  assert.strictEqual(validateValue("a", "skill"), "a");
  assert.throws(() => validateValue("--copy", "agent"), /Invalid agent value/);
  assert.throws(() => validateValue("a b", "agent"), /Invalid agent value/);
  assert.throws(() => validateValue("a;b", "agent"), /Invalid agent value/);
  assert.throws(() => validateValue("-x", "agent"), /Invalid agent value/);
  assert.throws(() => validateValue("$(x)", "agent"), /Invalid agent value/);
});

// --- validateBundleUrl -----------------------------------------------------
check("validateBundleUrl", () => {
  assert.strictEqual(validateBundleUrl(BUNDLE_URL), BUNDLE_URL);
  assert.strictEqual(
    validateBundleUrl("http://a.b/c.zip"),
    "http://a.b/c.zip"
  );
  assert.throws(() => validateBundleUrl("ftp://x/y"), /Invalid --bundle-url/);
  assert.throws(() => validateBundleUrl("not a url"), /Invalid --bundle-url/);
  assert.throws(() => validateBundleUrl(""), /Invalid --bundle-url/);
});

// --- validatePassthru ------------------------------------------------------
check("validatePassthru", () => {
  assert.strictEqual(validatePassthru("--full-depth"), "--full-depth");
  assert.strictEqual(validatePassthru("-y"), "-y");
  assert.strictEqual(validatePassthru("reviewer"), "reviewer");
  assert.throws(() => validatePassthru("$(x)"), /Invalid passthrough token/);
  assert.throws(() => validatePassthru("a b"), /Invalid passthrough token/);
});

// --- commandCandidates -----------------------------------------------------
check("commandCandidates unix", () => {
  assert.deepStrictEqual(
    commandCandidates("ve", "linux", "/usr/bin:/bin", ""),
    [path.join("/usr/bin", "ve"), path.join("/bin", "ve")]
  );
});
check("commandCandidates win32 (extension required, no bare name)", () => {
  const c = commandCandidates("ve", "win32", "C:\\bin", ".EXE;.CMD");
  assert.strictEqual(c.indexOf(path.join("C:\\bin", "ve")), -1);
  assert.ok(c.indexOf(path.join("C:\\bin", "ve.exe")) !== -1);
  assert.ok(c.indexOf(path.join("C:\\bin", "ve.cmd")) !== -1);
});
check("splitPath separator, empty-segment filter, win32 quote strip", () => {
  assert.deepStrictEqual(splitPath("a;;b", "win32"), ["a", "b"]);
  assert.deepStrictEqual(splitPath("a:/x:", "linux"), ["a", "/x"]);
  assert.deepStrictEqual(splitPath("", "linux"), []);
  assert.deepStrictEqual(
    splitPath('"C:\\Program Files\\nodejs";C:\\Windows', "win32"),
    ["C:\\Program Files\\nodejs", "C:\\Windows"]
  );
});
check("windowsExtensions default fallback + normalization", () => {
  assert.deepStrictEqual(windowsExtensions(""), [".com", ".exe", ".bat", ".cmd"]);
  assert.deepStrictEqual(windowsExtensions(" .EXE ; ; .CMD "), [".exe", ".cmd"]);
});

// --- commandExists / detectBinary (PATH scan, no spawn) --------------------
check("commandExists via fake isExecutable", () => {
  const deps = {
    platform: "linux",
    env: UNIX_ENV,
    isExecutable: fakeIsExecutable(["ve"]),
  };
  assert.strictEqual(commandExists("ve", deps), true);
  assert.strictEqual(commandExists("arkcli", deps), false);
});
check("detectBinary found", () => {
  const deps = {
    platform: "linux",
    env: UNIX_ENV,
    isExecutable: fakeIsExecutable(["ve"]),
  };
  assert.deepStrictEqual(detectBinary(BINARIES[0], deps), {
    name: "ve",
    pkg: "@volcengine/cli",
    found: true,
    foundVia: "ve",
  });
});
check("detectBinary arkcli alias fallback (ark-cli)", () => {
  const deps = {
    platform: "linux",
    env: UNIX_ENV,
    isExecutable: fakeIsExecutable(["ark-cli"]),
  };
  const r = detectBinary(BINARIES[1], deps);
  assert.strictEqual(r.found, true);
  assert.strictEqual(r.foundVia, "ark-cli");
});
check("detectBinary not found", () => {
  const deps = {
    platform: "linux",
    env: UNIX_ENV,
    isExecutable: fakeIsExecutable([]),
  };
  assert.deepStrictEqual(detectBinary(BINARIES[0], deps).found, false);
});

// --- parseArgs -------------------------------------------------------------
check("parseArgs defaults", () => {
  const o = parseArgs([]);
  assert.strictEqual(o.scope, "global");
  assert.strictEqual(o.yes, true);
  assert.strictEqual(o.skillsGlobal, true); // skills install globally by default
  assert.deepStrictEqual(o.agents, []);
  assert.deepStrictEqual(o.skills, []);
  assert.strictEqual(o.bundleUrl, null);
  assert.strictEqual(o.bundleFile, null);
  assert.deepStrictEqual(o.passthrough, []);
  assert.strictEqual(o.help, false);
});
check("parseArgs --skills-project opts out of global skills", () => {
  assert.strictEqual(parseArgs(["--skills-project"]).skillsGlobal, false);
});
check("default parsed options produce a global (-g) skills add", () => {
  const args = buildSkillsAddArgs("/tmp/bundle", parseArgs([]));
  assert.ok(args.indexOf("-g") !== -1);
  assert.strictEqual(buildSkillsAddArgs("/tmp/bundle", parseArgs(["--skills-project"])).indexOf("-g"), -1);
});
check("parseArgs repeated agent + local", () => {
  const o = parseArgs(["--agent", "claude-code", "--agent", "codex", "--local"]);
  assert.deepStrictEqual(o.agents, ["claude-code", "codex"]);
  assert.strictEqual(o.scope, "local");
});
check("parseArgs comma agent + no-yes + help", () => {
  assert.deepStrictEqual(parseArgs(["--agent", "a,b"]).agents, ["a", "b"]);
  assert.strictEqual(parseArgs(["--no-yes"]).yes, false);
  assert.strictEqual(parseArgs(["-h"]).help, true);
});
check("parseArgs bundle-url / bundle-file", () => {
  assert.strictEqual(parseArgs(["--bundle-url", BUNDLE_URL]).bundleUrl, BUNDLE_URL);
  assert.strictEqual(parseArgs(["--bundle-file", "./b.zip"]).bundleFile, "./b.zip");
});
check("parseArgs passthrough after --", () => {
  const o = parseArgs(["--", "--full-depth", "reviewer"]);
  assert.deepStrictEqual(o.passthrough, ["--full-depth", "reviewer"]);
});
check("parseArgs errors", () => {
  assert.throws(() => parseArgs(["--unknown"]), /Unknown option/);
  assert.throws(() => parseArgs(["--agent"]), /Missing value for --agent/);
  assert.throws(() => parseArgs(["--agent", "--local"]), /Missing value for --agent/);
  assert.throws(() => parseArgs(["--bundle-url"]), /Missing value for --bundle-url/);
  assert.throws(() => parseArgs(["--bundle-url", "nope"]), /Invalid --bundle-url/);
  assert.throws(() => parseArgs(["--bundle-file"]), /Missing value for --bundle-file/);
  // empty / comma-only list values must fail loudly (not silently default to '*')
  assert.throws(() => parseArgs(["--agent", ""]), /Missing value for --agent/);
  assert.throws(() => parseArgs(["--skill", "  "]), /Missing value for --skill/);
  // invalid tokens must be rejected inside parseArgs
  assert.throws(() => parseArgs(["--agent", "a;b"]), /Invalid agent value/);
  assert.throws(() => parseArgs(["--", "$(x)"]), /Invalid passthrough token/);
});

// --- buildNpmInstallArgs ---------------------------------------------------
check("buildNpmInstallArgs", () => {
  assert.deepStrictEqual(buildNpmInstallArgs("@volcengine/cli", { scope: "global" }), [
    "install",
    "-g",
    "@volcengine/cli",
  ]);
  assert.deepStrictEqual(buildNpmInstallArgs("@volcengine/cli", { scope: "local" }), [
    "install",
    "@volcengine/cli",
  ]);
});

// --- buildSkillsAddArgs (local dir source) ---------------------------------
check("buildSkillsAddArgs defaults (all skills / built-in agent list)", () => {
  assert.deepStrictEqual(
    buildSkillsAddArgs("/tmp/bundle", {
      agents: [],
      skills: [],
      yes: true,
      passthrough: [],
    }),
    ["skills", "add", "/tmp/bundle", "-s", "*"]
      .concat(DEFAULT_AGENTS.flatMap((a) => ["-a", a]))
      .concat(["-y"])
  );
});
check("DEFAULT_AGENTS excludes promptscript and invalid agents", () => {
  assert.ok(DEFAULT_AGENTS.length > 0);
  assert.strictEqual(DEFAULT_AGENTS.indexOf("promptscript"), -1);
  assert.strictEqual(DEFAULT_AGENTS.indexOf("eve"), -1);
  assert.strictEqual(DEFAULT_AGENTS.indexOf("zcode"), -1);
});
check("buildSkillsAddArgs agent override drops default list, keeps '*' skill", () => {
  const args = buildSkillsAddArgs("/tmp/bundle", {
    agents: ["claude-code"],
    skills: [],
    yes: true,
    passthrough: [],
  });
  assert.deepStrictEqual(args, [
    "skills",
    "add",
    "/tmp/bundle",
    "-s",
    "*",
    "-a",
    "claude-code",
    "-y",
  ]);
});
check("buildSkillsAddArgs repeated agents => repeated -a", () => {
  const args = buildSkillsAddArgs("/tmp/bundle", {
    agents: ["claude-code", "codex"],
    skills: [],
    yes: true,
    passthrough: [],
  });
  assert.deepStrictEqual(
    args.filter((_, i) => args[i - 1] === "-a"),
    ["claude-code", "codex"]
  );
});
check("buildSkillsAddArgs flags & passthrough", () => {
  const args = buildSkillsAddArgs("/tmp/bundle", {
    agents: [],
    skills: ["sign"],
    yes: false,
    copy: true,
    fullDepth: true,
    passthrough: ["--subagent", "reviewer"],
  });
  assert.strictEqual(args.indexOf("-y"), -1); // --no-yes drops -y
  assert.ok(args.indexOf("--copy") !== -1);
  assert.ok(args.indexOf("--full-depth") !== -1);
  assert.deepStrictEqual(args.slice(-2), ["--subagent", "reviewer"]);
  assert.deepStrictEqual(
    args.slice(0, 6),
    ["skills", "add", "/tmp/bundle", "-s", "sign", "-a"]
  );
});

// --- npxArgvForSource ------------------------------------------------------
check("npxArgvForSource defaults", () => {
  assert.deepStrictEqual(
    npxArgvForSource("/tmp/bundle", {
      agents: [],
      skills: [],
      yes: true,
      passthrough: [],
    }),
    ["--yes", "skills", "add", "/tmp/bundle", "-s", "*"]
      .concat(DEFAULT_AGENTS.flatMap((a) => ["-a", a]))
      .concat(["-y"])
  );
});

// --- resolveBundleUrl ------------------------------------------------------
check("resolveBundleUrl priority: flag > env > default", () => {
  assert.strictEqual(
    resolveBundleUrl({ bundleUrl: BUNDLE_URL }, { SKILLS_BUNDLE_URL: "http://env/x.zip" }),
    BUNDLE_URL
  );
  assert.strictEqual(
    resolveBundleUrl({ bundleUrl: null }, { SKILLS_BUNDLE_URL: "http://env/x.zip" }),
    "http://env/x.zip"
  );
  assert.strictEqual(resolveBundleUrl({ bundleUrl: null }, {}), DEFAULT_BUNDLE_URL || "");
});

// --- extractCandidates -----------------------------------------------------
check("extractCandidates: tar first, unzip fallback", () => {
  const c = extractCandidates("/tmp/b.zip", "/tmp/out");
  assert.deepStrictEqual(c[0], { cmd: "tar", args: ["-xf", "/tmp/b.zip", "-C", "/tmp/out"] });
  assert.deepStrictEqual(c[1], { cmd: "unzip", args: ["-oq", "/tmp/b.zip", "-d", "/tmp/out"] });
});

// --- formatCommand ---------------------------------------------------------
check("formatCommand quotes shell-unsafe args only", () => {
  assert.strictEqual(formatArg("/tmp/bundle"), "/tmp/bundle");
  assert.strictEqual(formatArg("@volcengine/cli"), "@volcengine/cli");
  assert.strictEqual(formatArg("*"), "'*'");
  assert.strictEqual(
    formatCommand("npx", ["--yes", "skills", "add", "/tmp/bundle", "-a", "*"]),
    "npx --yes skills add /tmp/bundle -a '*'"
  );
});

// --- planSetup -------------------------------------------------------------
function detectDeps(present, env) {
  return {
    platform: "linux",
    env: env || UNIX_ENV,
    isExecutable: fakeIsExecutable(present),
  };
}
check("planSetup all present -> no installs, one bundle", () => {
  const plan = planSetup(parseArgs(["--bundle-url", BUNDLE_URL]), detectDeps(["ve", "arkcli"]));
  assert.deepStrictEqual(plan.installs, []);
  assert.ok(plan.bundle);
  assert.strictEqual(plan.bundle.url, BUNDLE_URL);
  assert.strictEqual(plan.bundle.file, null);
});
check("planSetup bundle-file wins over url", () => {
  const plan = planSetup(parseArgs(["--bundle-file", "./b.zip"]), detectDeps(["ve", "arkcli"]));
  assert.strictEqual(plan.bundle.file, "./b.zip");
  assert.strictEqual(plan.bundle.url, "");
});
check("planSetup arkcli missing -> one install (ark), ve absent from installs", () => {
  const plan = planSetup(parseArgs([]), detectDeps(["ve"]));
  assert.strictEqual(plan.installs.length, 1);
  assert.strictEqual(plan.installs[0].pkg, "@volcengine/ark-cli");
  assert.deepStrictEqual(plan.installs[0].args, [
    "install",
    "-g",
    "@volcengine/ark-cli",
  ]);
});
check("planSetup skip-install / skip-skills / force", () => {
  const skipInstall = planSetup(parseArgs(["--skip-install"]), detectDeps([]));
  assert.deepStrictEqual(skipInstall.installs, []);
  assert.strictEqual(skipInstall.detections[0].found, null);

  const skipSkills = planSetup(parseArgs(["--skip-skills"]), detectDeps(["ve", "arkcli"]));
  assert.strictEqual(skipSkills.bundle, null);

  const force = planSetup(parseArgs(["--force"]), detectDeps(["ve", "arkcli"]));
  assert.strictEqual(force.installs.length, 2);
  assert.strictEqual(force.installs[0].reason, "forced");
  assert.strictEqual(force.installs[1].reason, "forced");
});
check("planSetup local scope", () => {
  const plan = planSetup(parseArgs(["--local"]), detectDeps([]));
  assert.deepStrictEqual(plan.installs[0].args, ["install", "@volcengine/cli"]);
});

// --- aggregateExitCode -----------------------------------------------------
check("aggregateExitCode", () => {
  assert.strictEqual(aggregateExitCode([]), 0);
  assert.strictEqual(aggregateExitCode([{ ok: true }, { ok: true }]), 0);
  assert.strictEqual(aggregateExitCode([{ ok: true }, { ok: false }]), 2);
  assert.strictEqual(aggregateExitCode([{ ok: false }, { ok: false }]), 3);
});

// --- renderSummary ---------------------------------------------------------
check("renderSummary is a string with a Result line + non-install steps", () => {
  const plan = planSetup(parseArgs(["--bundle-url", BUNDLE_URL]), detectDeps(["ve", "arkcli"]));
  const summary = renderSummary(plan, [
    { label: "download bundle", cmd: "download", ok: true, status: 0 },
    { label: "extract bundle", cmd: "extract", ok: true, status: 0 },
    { label: "skills add (bundle)", cmd: "npx", ok: true, status: 0 },
  ]);
  assert.ok(typeof summary === "string");
  assert.ok(summary.indexOf("Result:") !== -1);
  assert.ok(summary.indexOf("already present") !== -1);
  assert.ok(summary.indexOf("skills add (bundle)") !== -1);
});
check("renderSummary install/reinstall + [FAIL] lines", () => {
  const plan = {
    detections: [
      { name: "ve", pkg: "@volcengine/cli", found: true, foundVia: "ve" },
      { name: "arkcli", pkg: "@volcengine/ark-cli", found: false, foundVia: null },
    ],
    installs: [
      { pkg: "@volcengine/cli", args: ["install", "-g", "@volcengine/cli"] },
      { pkg: "@volcengine/ark-cli", args: ["install", "-g", "@volcengine/ark-cli"] },
    ],
  };
  const s = renderSummary(plan, [
    { label: "install @volcengine/cli", cmd: "npm", ok: true, status: 0 },
    { label: "install @volcengine/ark-cli", cmd: "npm", ok: false, status: 1 },
  ]);
  assert.ok(s.indexOf("[ok] tool ve reinstalled (@volcengine/cli)") !== -1);
  assert.ok(
    s.indexOf("[FAIL] tool arkcli installed (@volcengine/ark-cli) — exit 1") !== -1
  );
});
check("renderSummary emits global-bin-dir Note only for global installs", () => {
  const globalPlan = planSetup(parseArgs([]), detectDeps([])); // 2 global (-g) installs
  assert.ok(
    renderSummary(globalPlan, []).indexOf("Note: the npm global bin dir") !== -1
  );
  const localPlan = planSetup(parseArgs(["--local"]), detectDeps([])); // installs w/o -g
  assert.strictEqual(
    renderSummary(localPlan, []).indexOf("Note: the npm global bin dir"),
    -1
  );
  const noInstall = planSetup(parseArgs(["--bundle-url", BUNDLE_URL]), detectDeps(["ve", "arkcli"]));
  assert.strictEqual(
    renderSummary(noInstall, []).indexOf("Note: the npm global bin dir"),
    -1
  );
});

// --- async: executePlan + main ---------------------------------------------
(async () => {
  const opts = (a) => parseArgs(a);

  // executePlan: no installs, bundle download+extract(tar)+npx all succeed
  {
    const parsed = opts(["--bundle-url", BUNDLE_URL]);
    const plan = planSetup(parsed, detectDeps(["ve", "arkcli"]));
    const exec = fakeExec();
    const download = fakeDownload();
    const log = captureLog();
    const res = await executePlan(plan, {
      exec, download, log, platform: "linux", addOptions: parsed,
    });
    assert.strictEqual(res.code, 0);
    assert.strictEqual(download.calls.length, 1);
    // tar (extract) then npx (skills add); no npm installs
    assert.deepStrictEqual(exec.calls.map((c) => c.cmd), ["tar", "npx"]);
    assert.ok(log.text().indexOf("Result: OK") !== -1);
    passed += 1;
  }

  // executePlan: install (arkcli missing) then bundle
  {
    const parsed = opts(["--bundle-url", BUNDLE_URL]);
    const plan = planSetup(parsed, detectDeps(["ve"]));
    const exec = fakeExec();
    const download = fakeDownload();
    const log = captureLog();
    const res = await executePlan(plan, {
      exec, download, log, platform: "linux", addOptions: parsed,
    });
    assert.strictEqual(res.code, 0);
    assert.deepStrictEqual(exec.calls.map((c) => c.cmd), ["npm", "tar", "npx"]);
    passed += 1;
  }

  // extract fallback: tar missing (ENOENT) -> unzip succeeds
  {
    const parsed = opts(["--bundle-url", BUNDLE_URL]);
    const plan = planSetup(parsed, detectDeps(["ve", "arkcli"]));
    const exec = fakeExec({ enoent: ["tar"] });
    const download = fakeDownload();
    const log = captureLog();
    const res = await executePlan(plan, {
      exec, download, log, platform: "linux", addOptions: parsed,
    });
    assert.strictEqual(res.code, 0);
    assert.deepStrictEqual(exec.calls.map((c) => c.cmd), ["tar", "unzip", "npx"]);
    passed += 1;
  }

  // extract: neither tar nor unzip present -> clear "install tar or unzip" error
  {
    const parsed = opts(["--bundle-url", BUNDLE_URL]);
    const plan = planSetup(parsed, detectDeps(["ve", "arkcli"]));
    const exec = fakeExec({ enoent: ["tar", "unzip"] });
    const download = fakeDownload();
    const log = captureLog();
    const res = await executePlan(plan, {
      exec, download, log, platform: "linux", addOptions: parsed,
    });
    assert.strictEqual(res.code, 2); // download ok, extract failed
    assert.ok(/neither .*tar.* nor .*unzip/i.test(log.text()));
    assert.ok(log.text().indexOf("install tar or unzip") !== -1);
    // npx skills add never runs when extraction fails
    assert.strictEqual(exec.calls.filter((c) => c.cmd === "npx").length, 0);
    passed += 1;
  }

  // extract: tar present but cannot read zip (GNU tar), unzip missing -> generic
  {
    const parsed = opts(["--bundle-url", BUNDLE_URL]);
    const plan = planSetup(parsed, detectDeps(["ve", "arkcli"]));
    const exec = fakeExec({ fail: ["tar "], enoent: ["unzip"] });
    const download = fakeDownload();
    const log = captureLog();
    const res = await executePlan(plan, {
      exec, download, log, platform: "linux", addOptions: parsed,
    });
    assert.strictEqual(res.code, 2);
    assert.ok(log.text().indexOf("could not extract") !== -1);
    passed += 1;
  }

  // download failure -> recorded, no extract/npx
  {
    const parsed = opts(["--bundle-url", BUNDLE_URL]);
    const plan = planSetup(parsed, detectDeps(["ve", "arkcli"]));
    const exec = fakeExec();
    const download = fakeDownload({ fail: "network down" });
    const log = captureLog();
    const res = await executePlan(plan, {
      exec, download, log, platform: "linux", addOptions: parsed,
    });
    assert.strictEqual(res.code, 3);
    assert.ok(log.text().indexOf("download failed") !== -1);
    assert.strictEqual(exec.calls.length, 0);
    passed += 1;
  }

  // no bundle source (neither file nor url resolvable) -> clear error
  {
    const parsed = opts([]);
    // Simulate an empty default/env by handing executePlan a bundle with no
    // file and an empty url.
    const plan = planSetup(parsed, detectDeps(["ve", "arkcli"]));
    plan.bundle = { file: null, url: "", label: "skills bundle" };
    const exec = fakeExec();
    const download = fakeDownload();
    const log = captureLog();
    const res = await executePlan(plan, {
      exec, download, log, platform: "linux", addOptions: parsed,
    });
    assert.strictEqual(res.code, 3);
    assert.ok(log.text().indexOf("no bundle source") !== -1);
    assert.strictEqual(download.calls.length, 0);
    passed += 1;
  }

  // temp dir is cleaned up on success (rmdir called once with the work dir)
  {
    const parsed = opts(["--bundle-url", BUNDLE_URL]);
    const plan = planSetup(parsed, detectDeps(["ve", "arkcli"]));
    const rmCalls = [];
    const res = await executePlan(plan, {
      exec: fakeExec(), download: fakeDownload(), log: captureLog(),
      platform: "linux", addOptions: parsed, rmdir: (p) => { rmCalls.push(p); require("fs").rmSync(p, { recursive: true, force: true }); },
    });
    assert.strictEqual(res.code, 0);
    assert.strictEqual(rmCalls.length, 1);
    assert.ok(rmCalls[0].indexOf("skills-bundle-") !== -1);
    passed += 1;
  }

  // temp dir is cleaned up even when a step fails (extract fails -> still rm'd)
  {
    const parsed = opts(["--bundle-url", BUNDLE_URL]);
    const plan = planSetup(parsed, detectDeps(["ve", "arkcli"]));
    const rmCalls = [];
    const res = await executePlan(plan, {
      exec: fakeExec({ enoent: ["tar", "unzip"] }),
      download: fakeDownload(), log: captureLog(),
      platform: "linux", addOptions: parsed, rmdir: (p) => { rmCalls.push(p); require("fs").rmSync(p, { recursive: true, force: true }); },
    });
    assert.strictEqual(res.code, 2);
    assert.strictEqual(rmCalls.length, 1);
    passed += 1;
  }

  // bundle-file: uses local zip (no download), extract + npx
  {
    const parsed = opts(["--bundle-file", "/tmp/local.zip"]);
    const plan = planSetup(parsed, detectDeps(["ve", "arkcli"]));
    const exec = fakeExec();
    const download = fakeDownload();
    const log = captureLog();
    const res = await executePlan(plan, {
      exec, download, log, platform: "linux", addOptions: parsed,
      existsSync: () => true,
    });
    assert.strictEqual(res.code, 0);
    assert.strictEqual(download.calls.length, 0);
    assert.deepStrictEqual(exec.calls.map((c) => c.cmd), ["tar", "npx"]);
    passed += 1;
  }

  // bundle-file missing on disk -> error, no extract
  {
    const parsed = opts(["--bundle-file", "/tmp/missing.zip"]);
    const plan = planSetup(parsed, detectDeps(["ve", "arkcli"]));
    const exec = fakeExec();
    const log = captureLog();
    const res = await executePlan(plan, {
      exec, log, platform: "linux", addOptions: parsed,
      existsSync: () => false,
    });
    assert.strictEqual(res.code, 3);
    assert.ok(log.text().indexOf("bundle file not found") !== -1);
    assert.strictEqual(exec.calls.length, 0);
    passed += 1;
  }

  // main dry-run: prints download + extract + npx add <tmp-dir>
  {
    const exec = fakeExec();
    const log = captureLog();
    const code = await main(["--bundle-url", BUNDLE_URL, "--dry-run"], {
      exec, log, platform: "linux", env: UNIX_ENV,
      isExecutable: fakeIsExecutable(["ve", "arkcli"]),
    });
    assert.strictEqual(code, 0);
    assert.strictEqual(exec.calls.length, 0);
    assert.ok(log.text().indexOf("download " + BUNDLE_URL) !== -1);
    assert.ok(log.text().indexOf("npx --yes skills add '<tmp-dir>'") !== -1);
    passed += 1;
  }

  // main dry-run nothing to do (both skip flags)
  {
    const exec = fakeExec();
    const log = captureLog();
    const code = await main(["--skip-install", "--skip-skills", "--dry-run"], {
      exec, log, platform: "linux", env: UNIX_ENV,
      isExecutable: fakeIsExecutable(["ve", "arkcli"]),
    });
    assert.strictEqual(code, 0);
    assert.ok(log.text().indexOf("(nothing to do)") !== -1);
    passed += 1;
  }

  // help
  {
    const log = captureLog();
    const code = await main(["--help"], { log });
    assert.strictEqual(code, 0);
    assert.strictEqual(log.text(), USAGE);
    passed += 1;
  }

  // usage error
  {
    const exec = fakeExec();
    const log = captureLog();
    const code = await main(["--unknown"], { exec, log, platform: "linux" });
    assert.strictEqual(code, 1);
    assert.strictEqual(exec.calls.length, 0);
    assert.ok(log.errText().indexOf("Unknown option") !== -1);
    assert.ok(log.errText().indexOf("Usage:") !== -1);
    passed += 1;
  }

  // full run via main with fake exec + fake download
  {
    const exec = fakeExec();
    const download = fakeDownload();
    const log = captureLog();
    const code = await main(["--bundle-url", BUNDLE_URL], {
      exec, download, log, platform: "linux", env: UNIX_ENV,
      isExecutable: fakeIsExecutable(["ve", "arkcli"]),
    });
    assert.strictEqual(code, 0);
    assert.deepStrictEqual(exec.calls.map((c) => c.cmd), ["tar", "npx"]);
    passed += 1;
  }

  // --- package.json / bin shim --------------------------------------------
  assert.strictEqual(pkg.bin["skills-setup"], "bin/skills-setup");
  assert.strictEqual(pkg.name, "@volcengine/skills-setup");
  assert.strictEqual(pkg.engines.node, ">=18");
  assert.strictEqual(pkg.dependencies, undefined);
  assert.strictEqual(
    pkg.repository.url,
    "https://github.com/volcengine/volcengine-cli"
  );
  passed += 1;

  const fs = require("fs");
  const shim = fs.readFileSync(path.join(__dirname, "bin", "skills-setup"), "utf8");
  assert.ok(shim.startsWith("#!/usr/bin/env node"));
  passed += 1;

  // --- end-to-end: real subprocess, --help only (no network) ---------------
  {
    const r = spawnSync(process.execPath, [path.join(__dirname, "setup.js"), "--help"], {
      encoding: "utf8",
    });
    assert.strictEqual(r.status, 0);
    assert.ok(r.stdout.indexOf("Usage: skills-setup") !== -1);
    passed += 1;
  }

  // --- end-to-end: real subprocess, --dry-run with bundle url (no network) --
  {
    const r = spawnSync(
      process.execPath,
      [
        path.join(__dirname, "setup.js"),
        "--skip-install",
        "--bundle-url",
        BUNDLE_URL,
        "--dry-run",
      ],
      { encoding: "utf8" }
    );
    assert.strictEqual(r.status, 0);
    assert.ok(r.stdout.indexOf("download " + BUNDLE_URL) !== -1);
    assert.ok(r.stdout.indexOf("npx --yes skills add '<tmp-dir>'") !== -1);
    passed += 1;
  }

  console.log("skills-setup tests passed (" + passed + " checks)");
})().catch((err) => {
  console.error("TEST FAILURE:", err && err.stack ? err.stack : err);
  process.exit(1);
});
