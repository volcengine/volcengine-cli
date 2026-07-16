#!/usr/bin/env node

"use strict";

const assert = require("assert");
const { spawnSync } = require("child_process");
const path = require("path");

const {
  BINARIES,
  SKILL_REPOS,
  DEFAULT_AGENTS,
  USAGE,
  normalizeList,
  validateValue,
  validateRepoSlug,
  validatePassthru,
  parseArgs,
  buildNpmInstallArgs,
  buildSkillsAddArgs,
  npxArgvForRepo,
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

// --- validateRepoSlug ------------------------------------------------------
check("validateRepoSlug", () => {
  assert.strictEqual(
    validateRepoSlug("volcengine/ark-cli"),
    "volcengine/ark-cli"
  );
  assert.strictEqual(
    validateRepoSlug("volcengine/volcengine-skills"),
    "volcengine/volcengine-skills"
  );
  assert.throws(() => validateRepoSlug("evil/repo"), /Unknown repo/);
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
  // Windows quoted PATH segment: quotes stripped so path.join resolves.
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
  assert.deepStrictEqual(o.agents, []);
  assert.deepStrictEqual(o.skills, []);
  assert.deepStrictEqual(o.repos, SKILL_REPOS);
  assert.deepStrictEqual(o.passthrough, []);
  assert.strictEqual(o.help, false);
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
check("parseArgs passthrough after --", () => {
  const o = parseArgs(["--", "--full-depth", "reviewer"]);
  assert.deepStrictEqual(o.passthrough, ["--full-depth", "reviewer"]);
});
check("parseArgs repo accumulates (not replaces to one)", () => {
  const o = parseArgs([
    "--repo",
    "volcengine/ark-cli",
    "--repo",
    "volcengine/volcengine-skills",
  ]);
  assert.deepStrictEqual(o.repos, [
    "volcengine/ark-cli",
    "volcengine/volcengine-skills",
  ]);
});
check("parseArgs errors", () => {
  assert.throws(() => parseArgs(["--unknown"]), /Unknown option/);
  assert.throws(() => parseArgs(["--repo", "evil/repo"]), /Unknown repo/);
  assert.throws(() => parseArgs(["--agent"]), /Missing value for --agent/);
  assert.throws(() => parseArgs(["--agent", "--local"]), /Missing value for --agent/);
  // empty / comma-only values must fail loudly (not silently default to '*' or
  // drop the repo list to a no-op download)
  assert.throws(() => parseArgs(["--repo", ""]), /Missing value for --repo/);
  assert.throws(() => parseArgs(["--repo", ","]), /Missing value for --repo/);
  assert.throws(() => parseArgs(["--agent", ""]), /Missing value for --agent/);
  assert.throws(() => parseArgs(["--skill", "  "]), /Missing value for --skill/);
  // invalid tokens must be rejected inside parseArgs (drives validateValue /
  // validatePassthru rejection paths, not just their isolated unit tests)
  assert.throws(() => parseArgs(["--agent", "a;b"]), /Invalid agent value/);
  assert.throws(() => parseArgs(["--", "$(x)"]), /Invalid passthrough token/);
});
check("parseArgs --repo empty does not clobber default list on later valid repo", () => {
  // sanity: a valid --repo still overrides cleanly
  assert.deepStrictEqual(parseArgs(["--repo", "volcengine/ark-cli"]).repos, [
    "volcengine/ark-cli",
  ]);
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

// --- buildSkillsAddArgs ----------------------------------------------------
check("buildSkillsAddArgs defaults (all skills / built-in agent list)", () => {
  assert.deepStrictEqual(
    buildSkillsAddArgs("volcengine/ark-cli", {
      agents: [],
      skills: [],
      yes: true,
      passthrough: [],
    }),
    ["skills", "add", "volcengine/ark-cli", "-s", "*"]
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
check("buildSkillsAddArgs agent override drops '*' agent, keeps '*' skill", () => {
  const args = buildSkillsAddArgs("volcengine/volcengine-skills", {
    agents: ["claude-code"],
    skills: [],
    yes: true,
    passthrough: [],
  });
  assert.deepStrictEqual(args, [
    "skills",
    "add",
    "volcengine/volcengine-skills",
    "-s",
    "*",
    "-a",
    "claude-code",
    "-y",
  ]);
});
check("buildSkillsAddArgs repeated agents => repeated -a", () => {
  const args = buildSkillsAddArgs("volcengine/ark-cli", {
    agents: ["claude-code", "codex"],
    skills: [],
    yes: true,
    passthrough: [],
  });
  // one -a per value (verified skills CLI accumulates variadic tokens)
  assert.deepStrictEqual(
    args.filter((_, i) => args[i - 1] === "-a"),
    ["claude-code", "codex"]
  );
});
check("buildSkillsAddArgs flags & passthrough", () => {
  const args = buildSkillsAddArgs("volcengine/ark-cli", {
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
    ["skills", "add", "volcengine/ark-cli", "-s", "sign", "-a"]
  );
});
check("buildSkillsAddArgs rejects bad repo", () => {
  assert.throws(() => buildSkillsAddArgs("evil/repo", {}), /Unknown repo/);
});

// --- npxArgvForRepo --------------------------------------------------------
check("npxArgvForRepo defaults", () => {
  assert.deepStrictEqual(
    npxArgvForRepo("volcengine/ark-cli", {
      agents: [],
      skills: [],
      yes: true,
      passthrough: [],
    }),
    ["--yes", "skills", "add", "volcengine/ark-cli", "-s", "*"]
      .concat(DEFAULT_AGENTS.flatMap((a) => ["-a", a]))
      .concat(["-y"])
  );
});

// --- formatCommand (display quoting; execution unaffected) -----------------
check("formatCommand quotes shell-unsafe args only", () => {
  assert.strictEqual(formatArg("volcengine/ark-cli"), "volcengine/ark-cli");
  assert.strictEqual(formatArg("@volcengine/cli"), "@volcengine/cli");
  assert.strictEqual(formatArg("*"), "'*'");
  assert.strictEqual(
    formatCommand("npx", ["--yes", "skills", "add", "volcengine/ark-cli", "-a", "*"]),
    "npx --yes skills add volcengine/ark-cli -a '*'"
  );
});

// --- planSetup -------------------------------------------------------------
function detectDeps(present) {
  return {
    platform: "linux",
    env: UNIX_ENV,
    isExecutable: fakeIsExecutable(present),
  };
}
check("planSetup all present -> no installs, 2 skill steps", () => {
  const plan = planSetup(parseArgs([]), detectDeps(["ve", "arkcli"]));
  assert.deepStrictEqual(plan.installs, []);
  assert.strictEqual(plan.skills.length, 2);
  assert.deepStrictEqual(
    plan.skills[0].args,
    ["--yes", "skills", "add", "volcengine/volcengine-skills", "-s", "*"]
      .concat(DEFAULT_AGENTS.flatMap((a) => ["-a", a]))
      .concat(["-y"])
  );
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
  assert.deepStrictEqual(skipSkills.skills, []);

  const force = planSetup(parseArgs(["--force"]), detectDeps(["ve", "arkcli"]));
  assert.strictEqual(force.installs.length, 2);
  assert.strictEqual(force.installs[0].reason, "forced");
  assert.strictEqual(force.installs[1].reason, "forced");
});
check("executePlan --force reinstalls present binaries + says 'reinstalled'", () => {
  const plan = planSetup(parseArgs(["--force"]), detectDeps(["ve", "arkcli"]));
  const exec = fakeExec();
  const log = captureLog();
  const res = executePlan(plan, { exec, log, platform: "linux" });
  assert.strictEqual(res.code, 0);
  assert.deepStrictEqual(exec.calls.map((c) => c.cmd), ["npm", "npm", "npx", "npx"]);
  assert.ok(log.text().indexOf("reinstalled") !== -1);
});
check("planSetup local scope + repo filter", () => {
  const plan = planSetup(
    parseArgs(["--local", "--repo", "volcengine/ark-cli"]),
    detectDeps([])
  );
  assert.deepStrictEqual(plan.installs[0].args, ["install", "@volcengine/cli"]);
  assert.strictEqual(plan.skills.length, 1);
  assert.strictEqual(plan.skills[0].repo, "volcengine/ark-cli");
});

// --- aggregateExitCode -----------------------------------------------------
check("aggregateExitCode", () => {
  assert.strictEqual(aggregateExitCode([]), 0);
  assert.strictEqual(aggregateExitCode([{ ok: true }, { ok: true }]), 0);
  assert.strictEqual(aggregateExitCode([{ ok: true }, { ok: false }]), 2);
  assert.strictEqual(aggregateExitCode([{ ok: false }, { ok: false }]), 3);
});

// --- executePlan (fake exec, records ordered calls) ------------------------
check("executePlan order + success", () => {
  const plan = planSetup(parseArgs([]), detectDeps(["ve"])); // arkcli missing -> 1 install
  const exec = fakeExec();
  const log = captureLog();
  const res = executePlan(plan, { exec, log, platform: "linux" });
  assert.strictEqual(res.code, 0);
  assert.deepStrictEqual(
    exec.calls.map((c) => c.cmd),
    ["npm", "npx", "npx"]
  );
  assert.ok(log.text().indexOf("Result: OK") !== -1);
});
check("executePlan partial failure -> exit 2, non-fail-fast", () => {
  const plan = planSetup(parseArgs([]), detectDeps(["ve", "arkcli"])); // no installs, 2 skills
  const exec = fakeExec({ fail: ["volcengine/ark-cli"] });
  const log = captureLog();
  const res = executePlan(plan, { exec, log, platform: "linux" });
  assert.strictEqual(res.code, 2);
  // both repos still attempted despite the first-run failure of one
  assert.strictEqual(exec.calls.length, 2);
});
check("executePlan all-present + both skills fail -> exit 3 (skips excluded)", () => {
  const plan = planSetup(parseArgs([]), detectDeps(["ve", "arkcli"]));
  const exec = fakeExec({ fail: ["skills", "add"] });
  const log = captureLog();
  const res = executePlan(plan, { exec, log, platform: "linux" });
  assert.strictEqual(res.code, 3);
});
check("executePlan ENOENT message", () => {
  const plan = planSetup(parseArgs(["--skip-install"]), detectDeps([]));
  const exec = fakeExec({ enoent: ["npx"] });
  const log = captureLog();
  const res = executePlan(plan, { exec, log, platform: "linux" });
  assert.strictEqual(res.code, 3);
  assert.ok(log.text().indexOf("not found") !== -1);
  // --skip-install => detections are found:null => the "[skip]" summary line
  assert.ok(log.text().indexOf("[skip] tool ve (install skipped)") !== -1);
});

// --- renderSummary ---------------------------------------------------------
check("renderSummary is a string with a Result line", () => {
  const plan = planSetup(parseArgs([]), detectDeps(["ve", "arkcli"]));
  const summary = renderSummary(plan, [
    { label: "skills volcengine/volcengine-skills", cmd: "npx", ok: true, status: 0 },
    { label: "skills volcengine/ark-cli", cmd: "npx", ok: true, status: 0 },
  ]);
  assert.ok(typeof summary === "string");
  assert.ok(summary.indexOf("Result:") !== -1);
  assert.ok(summary.indexOf("already present") !== -1);
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
  const noInstall = planSetup(parseArgs([]), detectDeps(["ve", "arkcli"]));
  assert.strictEqual(
    renderSummary(noInstall, []).indexOf("Note: the npm global bin dir"),
    -1
  );
});

// --- main (async) ----------------------------------------------------------
(async () => {
  // dry-run
  {
    const exec = fakeExec();
    const log = captureLog();
    const code = await main(["--dry-run"], {
      exec,
      log,
      platform: "linux",
      env: UNIX_ENV,
      isExecutable: fakeIsExecutable(["ve", "arkcli"]),
    });
    assert.strictEqual(code, 0);
    assert.strictEqual(exec.calls.length, 0);
    assert.ok(log.text().indexOf("volcengine/volcengine-skills") !== -1);
    assert.ok(log.text().indexOf("volcengine/ark-cli") !== -1);
    passed += 1;
  }

  // dry-run with nothing to do (both skip flags) -> "(nothing to do)" branch
  {
    const exec = fakeExec();
    const log = captureLog();
    const code = await main(["--skip-install", "--skip-skills", "--dry-run"], {
      exec,
      log,
      platform: "linux",
      env: UNIX_ENV,
      isExecutable: fakeIsExecutable(["ve", "arkcli"]),
    });
    assert.strictEqual(code, 0);
    assert.strictEqual(exec.calls.length, 0);
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

  // full run via main with fake exec
  {
    const exec = fakeExec();
    const log = captureLog();
    const code = await main([], {
      exec,
      log,
      platform: "linux",
      env: UNIX_ENV,
      isExecutable: fakeIsExecutable(["ve", "arkcli"]),
    });
    assert.strictEqual(code, 0);
    assert.deepStrictEqual(exec.calls.map((c) => c.cmd), ["npx", "npx"]);
    passed += 1;
  }

  // --- package.json / bin shim (mirror install_test.js style) --------------
  assert.strictEqual(pkg.bin["skills-setup"], "bin/skills-setup");
  assert.strictEqual(pkg.name, "@volcengine/skills-setup");
  assert.strictEqual(pkg.engines.node, ">=16");
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

  // --- end-to-end: real subprocess, --dry-run with skip-install ------------
  {
    const r = spawnSync(
      process.execPath,
      [path.join(__dirname, "setup.js"), "--skip-install", "--dry-run"],
      { encoding: "utf8" }
    );
    assert.strictEqual(r.status, 0);
    assert.ok(r.stdout.indexOf("npx --yes skills add volcengine/volcengine-skills") !== -1);
    passed += 1;
  }

  console.log("skills-setup tests passed (" + passed + " checks)");
})().catch((err) => {
  console.error("TEST FAILURE:", err && err.stack ? err.stack : err);
  process.exit(1);
});
