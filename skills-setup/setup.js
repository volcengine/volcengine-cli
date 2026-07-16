#!/usr/bin/env node

"use strict";

const { spawnSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// The two CLI binaries this tool bootstraps. `arkcli` is the real bin name of
// @volcengine/ark-cli; `ark-cli` is accepted as an alias because users often
// spell it that way.
const BINARIES = [
  { name: "ve", aliases: ["ve"], pkg: "@volcengine/cli" },
  { name: "arkcli", aliases: ["arkcli", "ark-cli"], pkg: "@volcengine/ark-cli" },
];

// The repo the skills bundle is built from (informational; the bundle itself
// is downloaded pre-packaged, so this tool never clones it at runtime). Only
// `volcengine/volcengine-skills` is bundled — arkcli ships its own skills when
// `@volcengine/ark-cli` is installed, so re-bundling them here is redundant.
const SKILL_REPOS = ["volcengine/volcengine-skills"];

// Default location of the pre-packaged skills bundle (a .zip containing skill
// directories at its root). Downloaded at runtime, extracted, then handed to
// `skills add <dir>`. Override with --bundle-url or the SKILLS_BUNDLE_URL env
// var; use a local zip with --bundle-file.
const DEFAULT_BUNDLE_URL =
  "https://cloudcache.volccdn.com/ve/skills/v1.0.0/volcengine-skills-bundle.zip";

// Default agents targeted by `skills add` when the caller does not pass
// `--agent`. We enumerate the supported agents explicitly instead of using the
// `*` wildcard because `*` pulls in `promptscript`, which fails during a global
// (`-g`) install. Agents that read from the shared `~/.agent/skills` directory
// are covered transitively and do not need to be listed here; use `--agent` to
// target any other agent not in this list.
const DEFAULT_AGENTS = [
  "claude-code", "codex", "deepagents", "cursor", "antigravity",
  "antigravity-cli", "openclaw", "hermes-agent", "opencode", "trae", "pi",
];

// Agent / skill values: alphanumerics plus a safe punctuation set (and the '*'
// wildcard). A leading '-' is forbidden so a value can never masquerade as an
// option (option-injection), and shell metacharacters are excluded.
const VALUE_RE = /^[A-Za-z0-9_.*@][A-Za-z0-9_.*@\-/]*$/;

// Passthrough tokens after `--`: either a safe flag (-x / --xyz) or a safe value.
const PASSTHRU_FLAG_RE = /^--?[A-Za-z0-9][A-Za-z0-9-]*$/;

// Flags that consume a following value parsed as a comma-separated list.
const VALUE_FLAGS = { "--agent": "agent", "--skill": "skill" };

const USAGE = [
  "Usage: skills-setup [options] [-- <extra args forwarded to `skills add`>]",
  "",
  "Ensure `ve` and `arkcli` are installed, then install volcengine agent",
  "skills from a pre-packaged bundle: download the bundle zip, extract it",
  "(via tar or unzip), and run `npx skills add <dir>`.",
  "",
  "Options:",
  "  --agent <name>    Target agent(s) for skills (repeatable or comma-separated).",
  "                    Default: the built-in supported agent list (excludes",
  "                    promptscript, which fails on global install).",
  "  --skill <name>    Skill name(s) to install (repeatable or comma-separated).",
  "                    Default: all skills (*).",
  "  --bundle-url <url>  Download the skills bundle zip from this URL.",
  "                    Default: $SKILLS_BUNDLE_URL, else the built-in URL.",
  "  --bundle-file <path>  Use a local bundle zip instead of downloading.",
  "  --local           Install ve/arkcli into the local project (npm i) rather",
  "                    than globally (npm i -g, the default).",
  "  --skills-project  Install skills into the current project instead of the",
  "                    user-global scope (skills install globally by default).",
  "  --copy            Pass --copy to `skills add` (copy instead of symlink).",
  "  --full-depth      Pass --full-depth to `skills add`.",
  "  --no-yes          Do not pass -y to `skills add`.",
  "  --skip-install    Skip ve/arkcli detection & install (skills only).",
  "  --skip-skills     Skip skills install (binaries only).",
  "  --force           Reinstall ve/arkcli even if already present.",
  "  --dry-run         Print the planned commands without executing.",
  "  -h, --help        Show this help.",
  "",
  "Bundle sources: " + SKILL_REPOS.join(", "),
].join("\n");

// ---------------------------------------------------------------------------
// Pure helpers: parsing & validation
// ---------------------------------------------------------------------------

function normalizeList(prev, value) {
  const parts = String(value)
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  return prev.concat(parts);
}

function validateValue(value, label) {
  if (typeof value !== "string" || !VALUE_RE.test(value)) {
    throw new Error("Invalid " + label + " value: " + value);
  }
  return value;
}

function validateBundleUrl(url) {
  if (typeof url !== "string" || !/^https?:\/\/[^\s]+$/.test(url)) {
    throw new Error("Invalid --bundle-url (expected http(s) URL): " + url);
  }
  return url;
}

function validatePassthru(token) {
  if (
    typeof token !== "string" ||
    !(PASSTHRU_FLAG_RE.test(token) || VALUE_RE.test(token))
  ) {
    throw new Error("Invalid passthrough token: " + token);
  }
  return token;
}

// Read the single value that follows a flag, rejecting a missing value or a
// value that looks like another option.
function takeFlagValue(argv, i, tok) {
  const next = argv[i + 1];
  if (next === undefined || next === "--" || next.charAt(0) === "-") {
    throw new Error("Missing value for " + tok);
  }
  return next;
}

function parseArgs(argv) {
  const options = {
    agents: [],
    skills: [],
    bundleUrl: null,
    bundleFile: null,
    scope: "global",
    yes: true,
    skillsGlobal: true,
    copy: false,
    fullDepth: false,
    skipInstall: false,
    skipSkills: false,
    force: false,
    dryRun: false,
    help: false,
    passthrough: [],
  };
  let i = 0;
  for (; i < argv.length; i++) {
    const tok = argv[i];
    if (tok === "--") {
      i++;
      break;
    }
    if (tok === "-h" || tok === "--help") {
      options.help = true;
      continue;
    }
    if (tok === "--local") {
      options.scope = "local";
      continue;
    }
    if (tok === "--skills-project") {
      options.skillsGlobal = false;
      continue;
    }
    if (tok === "--copy") {
      options.copy = true;
      continue;
    }
    if (tok === "--full-depth") {
      options.fullDepth = true;
      continue;
    }
    if (tok === "--no-yes") {
      options.yes = false;
      continue;
    }
    if (tok === "--skip-install") {
      options.skipInstall = true;
      continue;
    }
    if (tok === "--skip-skills") {
      options.skipSkills = true;
      continue;
    }
    if (tok === "--force") {
      options.force = true;
      continue;
    }
    if (tok === "--dry-run") {
      options.dryRun = true;
      continue;
    }
    if (tok === "--bundle-url") {
      options.bundleUrl = validateBundleUrl(takeFlagValue(argv, i, tok));
      i++;
      continue;
    }
    if (tok === "--bundle-file") {
      // A local path; existence is checked at execution time.
      options.bundleFile = takeFlagValue(argv, i, tok);
      i++;
      continue;
    }
    if (Object.prototype.hasOwnProperty.call(VALUE_FLAGS, tok)) {
      const next = takeFlagValue(argv, i, tok);
      i++;
      const kind = VALUE_FLAGS[tok];
      // A blank / comma-only value (e.g. `--agent ,`) normalizes to an empty
      // list. Reject it loudly instead of silently defaulting to the wildcard.
      const parts = normalizeList([], next);
      if (parts.length === 0) {
        throw new Error("Missing value for " + tok);
      }
      if (kind === "agent") {
        parts.forEach((v) => options.agents.push(validateValue(v, "agent")));
      } else {
        parts.forEach((v) => options.skills.push(validateValue(v, "skill")));
      }
      continue;
    }
    throw new Error("Unknown option: " + tok);
  }
  for (; i < argv.length; i++) {
    options.passthrough.push(validatePassthru(argv[i]));
  }
  return options;
}

// ---------------------------------------------------------------------------
// Pure helpers: command / argv construction
// ---------------------------------------------------------------------------

function buildNpmInstallArgs(pkg, options) {
  return options.scope === "local"
    ? ["install", pkg]
    : ["install", "-g", pkg];
}

// Build `skills add <source> ...` where source is a LOCAL directory (the
// extracted bundle). `skills add` accepts a local path and discovers the skill
// directories inside it.
function buildSkillsAddArgs(source, options) {
  const args = ["skills", "add", source];
  // `skills add` parses -s/-a as space-separated variadic lists that accumulate
  // across repeats (verified against the CLI source). One flag per value is the
  // safe, unambiguous form; comma-joined values would NOT work.
  const skills = options.skills && options.skills.length ? options.skills : ["*"];
  for (const s of skills) args.push("-s", s);
  const agents =
    options.agents && options.agents.length ? options.agents : DEFAULT_AGENTS;
  for (const a of agents) args.push("-a", a);
  if (options.yes !== false) args.push("-y");
  if (options.skillsGlobal) args.push("-g");
  if (options.copy) args.push("--copy");
  if (options.fullDepth) args.push("--full-depth");
  if (options.passthrough && options.passthrough.length) {
    for (const t of options.passthrough) args.push(t);
  }
  return args;
}

function npxArgvForSource(source, options) {
  // Leading --yes auto-confirms npx fetching the `skills` package itself.
  return ["--yes"].concat(buildSkillsAddArgs(source, options));
}

// Resolve the bundle download URL from (in priority order): --bundle-url, the
// SKILLS_BUNDLE_URL env var, then the built-in default. Returns "" when none.
function resolveBundleUrl(options, env) {
  env = env || {};
  return options.bundleUrl || env.SKILLS_BUNDLE_URL || DEFAULT_BUNDLE_URL || "";
}

// Ordered extraction attempts for a zip. `tar -xf` works with bsdtar/libarchive
// (macOS, Windows 10+); `unzip` is the fallback. If a tool is missing OR cannot
// read the zip (e.g. GNU tar), we fall through to the next candidate.
function extractCandidates(zipPath, destDir) {
  return [
    { cmd: "tar", args: ["-xf", zipPath, "-C", destDir] },
    { cmd: "unzip", args: ["-oq", zipPath, "-d", destDir] },
  ];
}

// ---------------------------------------------------------------------------
// Detection: pure-JS PATH scan (no `which`/`where` dependency, CWD-safe)
// ---------------------------------------------------------------------------

function splitPath(pathEnv, platform) {
  const sep = platform === "win32" ? ";" : ":";
  return String(pathEnv || "")
    .split(sep)
    .map((seg) => (platform === "win32" ? seg.replace(/^"(.*)"$/, "$1") : seg))
    .filter(Boolean);
}

function windowsExtensions(pathextEnv) {
  const raw = String(pathextEnv || ".COM;.EXE;.BAT;.CMD");
  return raw
    .split(";")
    .map((s) => s.trim())
    .filter(Boolean)
    .map((s) => s.toLowerCase());
}

function commandCandidates(command, platform, pathEnv, pathextEnv) {
  const dirs = splitPath(pathEnv, platform);
  const candidates = [];
  if (platform === "win32") {
    const exts = windowsExtensions(pathextEnv);
    for (const dir of dirs) {
      // No bare, extension-less candidate on Windows: cmd.exe/PowerShell cannot
      // run one, and a real `npm i -g` always drops a .cmd/.exe shim, so require
      // a PATHEXT match to avoid false-positive "already present" detections.
      for (const ext of exts) {
        candidates.push(path.join(dir, command + ext));
      }
    }
  } else {
    for (const dir of dirs) {
      candidates.push(path.join(dir, command));
    }
  }
  return candidates;
}

function defaultIsExecutable(file, platform) {
  try {
    const st = fs.statSync(file);
    if (!st.isFile()) return false;
    if (platform === "win32") return true; // extension already implies runnable
    return (st.mode & 0o111) !== 0; // any execute bit
  } catch (_) {
    return false;
  }
}

function commandExists(command, deps) {
  deps = deps || {};
  const platform = deps.platform || process.platform;
  const env = deps.env || process.env;
  const isExecutable = deps.isExecutable || defaultIsExecutable;
  const candidates = commandCandidates(
    command,
    platform,
    env.PATH || env.Path || "",
    env.PATHEXT || ""
  );
  for (const c of candidates) {
    if (isExecutable(c, platform)) return true;
  }
  return false;
}

function detectBinary(bin, deps) {
  for (const alias of bin.aliases) {
    if (commandExists(alias, deps)) {
      return { name: bin.name, pkg: bin.pkg, found: true, foundVia: alias };
    }
  }
  return { name: bin.name, pkg: bin.pkg, found: false, foundVia: null };
}

// ---------------------------------------------------------------------------
// Planning
// ---------------------------------------------------------------------------

function planSetup(options, deps) {
  deps = deps || {};
  const detections = [];
  const installs = [];
  for (const bin of BINARIES) {
    let detection;
    if (options.skipInstall) {
      detection = { name: bin.name, pkg: bin.pkg, found: null, foundVia: null };
    } else {
      detection = detectBinary(bin, deps);
    }
    detections.push(detection);
    if (!options.skipInstall && (options.force || !detection.found)) {
      installs.push({
        pkg: bin.pkg,
        cmd: "npm",
        args: buildNpmInstallArgs(bin.pkg, options),
        reason: options.force ? "forced" : "missing",
        label: "install " + bin.pkg,
      });
    }
  }
  let bundle = null;
  if (!options.skipSkills) {
    bundle = {
      file: options.bundleFile || null,
      url: options.bundleFile ? "" : resolveBundleUrl(options, deps.env),
      label: "skills bundle",
    };
  }
  return { detections: detections, installs: installs, bundle: bundle };
}

// ---------------------------------------------------------------------------
// Execution (the only side-effecting layer, funneled through `exec`)
// ---------------------------------------------------------------------------

function defaultExec(cmd, args, opts) {
  opts = opts || {};
  const platform = opts.platform || process.platform;
  // On Windows, npm/npx are `.cmd` batch shims that Node refuses to launch with
  // shell:false; run them through the shell there. All args are pre-validated
  // (no spaces / shell metacharacters), so the shell surface is safe. On unix
  // we spawn directly with shell:false — no shell surface at all.
  const useShell = platform === "win32";
  return spawnSync(cmd, args, { stdio: "inherit", shell: useShell });
}

// Download `url` to `destPath`. Uses the global fetch (Node >= 18).
async function defaultDownload(url, destPath) {
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error("HTTP " + res.status + " " + res.statusText);
  }
  const buf = Buffer.from(await res.arrayBuffer());
  fs.writeFileSync(destPath, buf);
}

// Render a command for HUMAN DISPLAY only (dry-run + echo). Execution always
// uses the raw argv array, so this quoting is purely so a copy-pasted line is
// shell-safe (e.g. `*` is not glob-expanded).
function formatArg(arg) {
  if (/^[A-Za-z0-9_@/.:=-]+$/.test(arg)) return arg;
  return "'" + String(arg).replace(/'/g, "'\\''") + "'";
}

function formatCommand(cmd, args) {
  return [cmd].concat(args.map(formatArg)).join(" ");
}

function mark(ok) {
  return ok ? "[ok]" : "[FAIL]";
}

// Exit code is based on steps that were ACTUALLY executed. Pure skips
// (already-present binaries, --skip-*) never turn a total failure into a
// partial one.
function aggregateExitCode(steps) {
  const ran = steps.length;
  if (ran === 0) return 0;
  const failed = steps.filter((s) => !s.ok).length;
  if (failed === 0) return 0;
  if (failed === ran) return 3;
  return 2;
}

function renderSummary(plan, steps) {
  const lines = ["Setup summary:"];
  const installStepFor = (pkg) =>
    steps.find((s) => s.label === "install " + pkg);
  for (const d of plan.detections) {
    if (d.found === null) {
      lines.push("  [skip] tool " + d.name + " (install skipped)");
      continue;
    }
    const step = installStepFor(d.pkg);
    if (step) {
      const verb = d.found ? "reinstalled" : "installed";
      lines.push(
        "  " +
          mark(step.ok) +
          " tool " +
          d.name +
          " " +
          verb +
          " (" +
          d.pkg +
          ")" +
          (step.ok ? "" : " — " + (step.error || "exit " + step.status))
      );
    } else if (d.found) {
      lines.push(
        "  [ok] tool " + d.name + " already present (via " + d.foundVia + ")"
      );
    }
  }
  for (const s of steps.filter((x) => !/^install /.test(x.label))) {
    lines.push(
      "  " +
        mark(s.ok) +
        " " +
        s.label +
        (s.ok ? "" : " — " + (s.error || "exit " + s.status))
    );
  }
  const failed = steps.filter((s) => !s.ok).length;
  const code = aggregateExitCode(steps);
  const verdict = code === 0 ? "OK" : code === 2 ? "PARTIAL" : "FAILED";
  lines.push(
    "Result: " +
      verdict +
      " (" +
      (steps.length - failed) +
      " ok, " +
      failed +
      " failed) — exit " +
      code
  );
  if (plan.installs.some((s) => s.args.indexOf("-g") !== -1)) {
    lines.push(
      "Note: the npm global bin dir must be on your PATH for ve/arkcli to be callable."
    );
  }
  return lines.join("\n");
}

// Turn a spawnSync-style result into a recorded step.
function recordStep(steps, label, cmd, res, platform) {
  res = res || {};
  const ok = !res.error && res.status === 0;
  const notFound =
    (res.error && res.error.code === "ENOENT") ||
    (platform === "win32" && res.status === 9009);
  steps.push({
    label: label,
    cmd: cmd,
    ok: ok,
    status: typeof res.status === "number" ? res.status : null,
    notFound: notFound,
    error: ok
      ? null
      : notFound
      ? cmd + " not found"
      : res.error
      ? res.error.message
      : null,
  });
  return ok;
}

// Download (or locate) the bundle zip, extract it, and `skills add` the result.
// Records one or more steps into `steps`.
async function runSkillsBundle(bundle, steps, deps) {
  const exec = deps.exec || defaultExec;
  const download = deps.download || defaultDownload;
  const log = deps.log || console;
  const platform = deps.platform || process.platform;
  const mkdtemp = deps.mkdtemp || ((p) => fs.mkdtempSync(p));
  const existsSync = deps.existsSync || fs.existsSync;
  // Recursive, force removal so cleanup never throws (e.g. already gone).
  const rmdir =
    deps.rmdir || ((p) => fs.rmSync(p, { recursive: true, force: true }));

  const work = mkdtemp(path.join(os.tmpdir(), "skills-bundle-"));
  const extractDir = path.join(work, "extracted");
  fs.mkdirSync(extractDir, { recursive: true });

  try {
    // 1. Obtain the zip: local file or download.
    let zipPath;
    if (bundle.file) {
      zipPath = path.resolve(bundle.file);
      if (!existsSync(zipPath)) {
        steps.push({
          label: "skills bundle",
          cmd: "bundle",
          ok: false,
          status: null,
          error: "bundle file not found: " + zipPath,
        });
        return;
      }
    } else if (bundle.url) {
      zipPath = path.join(work, "bundle.zip");
      log.log("\n$ download " + bundle.url);
      try {
        await download(bundle.url, zipPath);
      } catch (err) {
        steps.push({
          label: "download bundle",
          cmd: "download",
          ok: false,
          status: null,
          error: "download failed: " + ((err && err.message) || String(err)),
        });
        return;
      }
      steps.push({
        label: "download bundle",
        cmd: "download",
        ok: true,
        status: 0,
        error: null,
      });
    } else {
      steps.push({
        label: "skills bundle",
        cmd: "bundle",
        ok: false,
        status: null,
        error:
          "no bundle source — set --bundle-url, $SKILLS_BUNDLE_URL, or use --bundle-file",
      });
      return;
    }

    // 2. Extract: try tar, then unzip. Fall through on any failure (missing
    // tool or unreadable archive, e.g. GNU tar cannot read zip).
    const candidates = extractCandidates(zipPath, extractDir);
    let extracted = false;
    let allNotFound = true;
    for (const c of candidates) {
      log.log("\n$ " + formatCommand(c.cmd, c.args));
      const res = exec(c.cmd, c.args, { platform: platform }) || {};
      const ok = !res.error && res.status === 0;
      const notFound =
        (res.error && res.error.code === "ENOENT") ||
        (platform === "win32" && res.status === 9009);
      if (!notFound) allNotFound = false;
      if (ok) {
        extracted = true;
        break;
      }
    }
    if (!extracted) {
      steps.push({
        label: "extract bundle",
        cmd: "extract",
        ok: false,
        status: null,
        error: allNotFound
          ? "skill install failed: neither `tar` nor `unzip` is available — please install tar or unzip"
          : "skill install failed: could not extract the bundle zip with tar or unzip",
      });
      return;
    }
    steps.push({
      label: "extract bundle",
      cmd: "extract",
      ok: true,
      status: 0,
      error: null,
    });

    // 3. Install skills from the extracted directory. `skills add` COPIES the
    // skill files into each agent's skills dir, so removing the temp dir
    // afterwards is safe.
    const argv = npxArgvForSource(extractDir, deps.addOptions);
    log.log("\n$ " + formatCommand("npx", argv));
    const res = exec("npx", argv, { platform: platform }) || {};
    recordStep(steps, "skills add (bundle)", "npx", res, platform);
  } finally {
    // Always reclaim the temp dir (download + extraction scratch space).
    try {
      rmdir(work);
    } catch (_) {
      /* best-effort cleanup */
    }
  }
}

async function executePlan(plan, deps) {
  deps = deps || {};
  const exec = deps.exec || defaultExec;
  const log = deps.log || console;
  const platform = deps.platform || process.platform;
  const steps = [];
  for (const step of plan.installs) {
    log.log("\n$ " + formatCommand(step.cmd, step.args));
    const res = exec(step.cmd, step.args, { platform: platform }) || {};
    recordStep(steps, step.label, step.cmd, res, platform);
  }
  if (plan.bundle) {
    await runSkillsBundle(plan.bundle, steps, {
      exec: exec,
      download: deps.download,
      log: log,
      platform: platform,
      mkdtemp: deps.mkdtemp,
      existsSync: deps.existsSync,
      rmdir: deps.rmdir,
      addOptions: deps.addOptions,
    });
  }
  log.log("\n" + renderSummary(plan, steps));
  return { code: aggregateExitCode(steps), steps: steps };
}

// ---------------------------------------------------------------------------
// Entrypoint
// ---------------------------------------------------------------------------

// For dry-run: describe the bundle actions without touching the filesystem.
function bundleDryRunLines(bundle, options) {
  const lines = [];
  if (bundle.file) {
    lines.push("  extract " + path.resolve(bundle.file) + " -> <tmp-dir>");
  } else if (bundle.url) {
    lines.push("  download " + bundle.url + " -> <tmp-dir>/bundle.zip");
    lines.push("  extract <tmp-dir>/bundle.zip -> <tmp-dir> (tar -xf || unzip)");
  } else {
    lines.push(
      "  (no bundle source — set --bundle-url, $SKILLS_BUNDLE_URL, or --bundle-file)"
    );
  }
  lines.push("  " + formatCommand("npx", npxArgvForSource("<tmp-dir>", options)));
  return lines;
}

async function main(argv, deps) {
  deps = deps || {};
  const log = deps.log || console;
  let options;
  try {
    options = parseArgs(argv);
  } catch (err) {
    log.error((err && err.message) || String(err));
    log.error("\n" + USAGE);
    return 1;
  }
  if (options.help) {
    log.log(USAGE);
    return 0;
  }
  const detectDeps = {
    platform: deps.platform || process.platform,
    env: deps.env || process.env,
    isExecutable: deps.isExecutable,
  };
  const plan = planSetup(options, detectDeps);
  if (options.dryRun) {
    log.log("Dry run — planned commands:");
    const hasWork = plan.installs.length > 0 || plan.bundle;
    if (!hasWork) {
      log.log("  (nothing to do)");
    }
    for (const step of plan.installs) {
      log.log("  " + formatCommand(step.cmd, step.args));
    }
    if (plan.bundle) {
      for (const line of bundleDryRunLines(plan.bundle, options)) {
        log.log(line);
      }
    }
    return 0;
  }
  const result = await executePlan(plan, {
    exec: deps.exec,
    download: deps.download,
    log: log,
    platform: detectDeps.platform,
    mkdtemp: deps.mkdtemp,
    existsSync: deps.existsSync,
    addOptions: options,
  });
  return result.code;
}

if (require.main === module) {
  main(process.argv.slice(2))
    .then((code) => process.exit(code))
    .catch((err) => {
      console.error("skills-setup failed:", err && err.message);
      process.exit(1);
    });
}

module.exports = {
  BINARIES,
  SKILL_REPOS,
  DEFAULT_BUNDLE_URL,
  DEFAULT_AGENTS,
  VALUE_RE,
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
  defaultIsExecutable,
  commandExists,
  detectBinary,
  planSetup,
  defaultExec,
  defaultDownload,
  aggregateExitCode,
  renderSummary,
  recordStep,
  runSkillsBundle,
  executePlan,
  bundleDryRunLines,
  main,
};
