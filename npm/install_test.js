#!/usr/bin/env node

const assert = require("assert");
const { spawnSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

const {
  archiveNameForTarget,
  archiveURLForTarget,
  binaryNameForPlatform,
  createWindowsVeShim,
  normalizeBaseURL,
  targetForPlatform,
} = require("./install");
const pkg = require("./package.json");

const packagedVe = fs.readFileSync(path.join(__dirname, "bin", "ve"), "utf8");
assert.ok(packagedVe.startsWith("#!/usr/bin/env node"));

function withTempDir(fn) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ve-npm-test-"));
  try {
    fn(dir);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

assert.strictEqual(pkg.bin.ve, "bin/ve");
assert.strictEqual(pkg.name, "@sdk-liuzhaoliang/cli");
assert.strictEqual(pkg.repository.url, "https://github.com/sdk-liuzhaoliang/volcengine-cli");
assert.strictEqual(binaryNameForPlatform("win32"), "ve.exe");
assert.strictEqual(binaryNameForPlatform("linux"), "ve");
assert.strictEqual(binaryNameForPlatform("darwin"), "ve");
assert.strictEqual(normalizeBaseURL("https://example.com/base///"), "https://example.com/base");
assert.deepStrictEqual(targetForPlatform("darwin", "arm64"), {
  platform: "darwin",
  arch: "arm64",
});
assert.deepStrictEqual(targetForPlatform("linux", "arm"), {
  platform: "linux",
  arch: "arm",
});
assert.deepStrictEqual(targetForPlatform("win32", "arm64"), {
  platform: "windows",
  arch: "arm64",
});
assert.strictEqual(targetForPlatform("win32", "arm"), null);
assert.strictEqual(
  archiveNameForTarget({ platform: "darwin", arch: "arm64" }, "1.2.3"),
  "volcengine-cli_1.2.3_darwin_arm64.zip"
);
assert.strictEqual(
  archiveURLForTarget(
    { platform: "linux", arch: "amd64" },
    "1.2.3",
    "https://bucket.example.com/releases///"
  ),
  "https://bucket.example.com/releases/v1.2.3/volcengine-cli_1.2.3_linux_amd64.zip"
);

withTempDir((dir) => {
  const binDir = path.join(dir, "bin");
  fs.mkdirSync(binDir, { recursive: true });

  const exePath = path.join(binDir, "ve.exe");
  fs.writeFileSync(exePath, "#!/bin/sh\necho ve.exe \"$@\"\nexit 7\n");
  fs.chmodSync(exePath, 0o755);

  createWindowsVeShim(binDir);

  const shimPath = path.join(binDir, "ve");
  assert.ok(fs.existsSync(shimPath), "Windows npm entry bin/ve should exist");

  const result = spawnSync(process.execPath, [shimPath, "arg1", "arg2"], {
    encoding: "utf8",
  });
  assert.strictEqual(result.status, 7);
  assert.strictEqual(result.stdout.trim(), "ve.exe arg1 arg2");
});

console.log("install tests passed");
