#!/usr/bin/env node

"use strict";

const { spawnSync } = require("child_process");
const fs = require("fs");
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

// The only skill repositories this tool will pull from. `--repo` may filter to
// a subset but can never inject an arbitrary source.
const SKILL_REPOS = ["volcengine/volcengine-skills", "volcengine/ark-cli"];

// Agent / skill values: alphanumerics plus a safe punctuation set (and the '*'
// wildcard). A leading '-' is forbidden so a value can never masquerade as an
// option (option-injection), and shell metacharacters are excluded.
const VALUE_RE = /^[A-Za-z0-9_.*@][A-Za-z0-9_.*@\-/]*$/;

// Passthrough tokens after `--`: either a safe flag (-x / --xyz) or a safe value.
const PASSTHRU_FLAG_RE = /^--?[A-Za-z0-9][A-Za-z0-9-]*$/;

// Flags that consume a following value.
const VALUE_FLAGS = { "--agent": "agent", "--skill": "skill", "--repo": "repo" };

const USAGE = [
  "Usage: skills-setup [options] [-- <extra args forwarded to `skills add`>]",
  "",
  "Ensure `ve` and `arkcli` are installed, then download volcengine agent",
  "skills via `npx skills add` for both official repos.",
  "",
  "Options:",
  "  --agent <name>    Target agent(s) for skills (repeatable or comma-separated).",
  "                    Default: all agents (*).",
  "  --skill <name>    Skill name(s) to install (repeatable or comma-separated).",
  "                    Default: all skills (*).",
  "  --repo <slug>     Restrict to a whitelisted repo (repeatable).",
  "                    Default: both official repos.",
  "  --local           Install ve/arkcli into the local project (npm i) rather",
  "                    than globally (npm i -g, the default).",
  "  --skills-global   Pass -g to `skills add` (user-level skills install).",
  "  --copy            Pass --copy to `skills add` (copy instead of symlink).",
  "  --full-depth      Pass --full-depth to `skills add`.",
  "  --no-yes          Do not pass -y to `skills add`.",
  "  --skip-install    Skip ve/arkcli detection & install (skills only).",
  "  --skip-skills     Skip skills download (binaries only).",
  "  --force           Reinstall ve/arkcli even if already present.",
  "  --dry-run         Print the planned npm/npx commands without executing.",
  "  -h, --help        Show this help.",
  "",
  "Repos: " + SKILL_REPOS.join(", "),
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

function validateRepoSlug(slug) {
  if (SKILL_REPOS.indexOf(slug) === -1) {
    throw new Error(
      "Unknown repo: " + slug + " (allowed: " + SKILL_REPOS.join(", ") + ")"
    );
  }
  return slug;
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

function parseArgs(argv) {
  const options = {
    agents: [],
    skills: [],
    repos: SKILL_REPOS.slice(),
    scope: "global",
    yes: true,
    skillsGlobal: false,
    copy: false,
    fullDepth: false,
    skipInstall: false,
    skipSkills: false,
    force: false,
    dryRun: false,
    help: false,
    passthrough: [],
  };
  let repoOverridden = false;
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
    if (tok === "--skills-global") {
      options.skillsGlobal = true;
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
    if (Object.prototype.hasOwnProperty.call(VALUE_FLAGS, tok)) {
      const next = argv[i + 1];
      if (next === undefined || next === "--" || next.charAt(0) === "-") {
        throw new Error("Missing value for " + tok);
      }
      i++;
      const kind = VALUE_FLAGS[tok];
      // A blank / comma-only value (e.g. `--repo ""`, `--agent ,`) normalizes to
      // an empty list. Reject it loudly instead of silently defaulting to the
      // wildcard (agent/skill) or clearing the repo list into a no-op download.
      const parts = normalizeList([], next);
      if (parts.length === 0) {
        throw new Error("Missing value for " + tok);
      }
      if (kind === "agent") {
        parts.forEach((v) => options.agents.push(validateValue(v, "agent")));
      } else if (kind === "skill") {
        parts.forEach((v) => options.skills.push(validateValue(v, "skill")));
      } else {
        // repo: only clear the default list once we have >= 1 valid slug, so a
        // degenerate value can never silently drop us to zero repos.
        if (!repoOverridden) {
          options.repos = [];
          repoOverridden = true;
        }
        parts.forEach((v) => options.repos.push(validateRepoSlug(v)));
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

function buildSkillsAddArgs(repo, options) {
  validateRepoSlug(repo);
  const args = ["skills", "add", repo];
  // `skills add` parses -s/-a as space-separated variadic lists that accumulate
  // across repeats (verified against the CLI source). One flag per value is the
  // safe, unambiguous form; comma-joined values would NOT work.
  const skills = options.skills && options.skills.length ? options.skills : ["*"];
  for (const s of skills) args.push("-s", s);
  const agents = options.agents && options.agents.length ? options.agents : ["*"];
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

function npxArgvForRepo(repo, options) {
  // Leading --yes auto-confirms npx fetching the `skills` package itself.
  return ["--yes"].concat(buildSkillsAddArgs(repo, options));
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
  const skills = [];
  if (!options.skipSkills) {
    for (const repo of options.repos) {
      skills.push({
        repo: repo,
        cmd: "npx",
        args: npxArgvForRepo(repo, options),
        label: "skills " + repo,
      });
    }
  }
  return { detections: detections, installs: installs, skills: skills };
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
  for (const s of steps.filter((x) => x.cmd === "npx")) {
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

function executePlan(plan, deps) {
  deps = deps || {};
  const exec = deps.exec || defaultExec;
  const log = deps.log || console;
  const platform = deps.platform || process.platform;
  const steps = [];
  const runStep = (step) => {
    log.log("\n$ " + formatCommand(step.cmd, step.args));
    const res = exec(step.cmd, step.args, { platform: platform }) || {};
    const ok = !res.error && res.status === 0;
    // ENOENT surfaces on unix (shell:false). On Windows (shell:true) a missing
    // npm/npx runs through cmd.exe, which returns 9009 ("is not recognized")
    // with no spawn error — treat that as not-found too so the hint still fires.
    const notFound =
      (res.error && res.error.code === "ENOENT") ||
      (platform === "win32" && res.status === 9009);
    steps.push({
      label: step.label,
      cmd: step.cmd,
      ok: ok,
      status: typeof res.status === "number" ? res.status : null,
      error: ok
        ? null
        : notFound
        ? step.cmd + " not found — install Node.js/npm and ensure it is on PATH"
        : res.error
        ? res.error.message
        : null,
    });
  };
  for (const step of plan.installs) runStep(step);
  for (const step of plan.skills) runStep(step);
  log.log("\n" + renderSummary(plan, steps));
  return { code: aggregateExitCode(steps), steps: steps };
}

// ---------------------------------------------------------------------------
// Entrypoint
// ---------------------------------------------------------------------------

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
    const all = plan.installs.concat(plan.skills);
    if (all.length === 0) {
      log.log("  (nothing to do)");
    }
    for (const step of all) {
      log.log("  " + formatCommand(step.cmd, step.args));
    }
    return 0;
  }
  const result = executePlan(plan, {
    exec: deps.exec,
    log: log,
    platform: detectDeps.platform,
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
  VALUE_RE,
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
  defaultIsExecutable,
  commandExists,
  detectBinary,
  planSetup,
  defaultExec,
  aggregateExitCode,
  renderSummary,
  executePlan,
  main,
};
