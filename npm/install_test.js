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
  checksumForArchive,
  createWindowsVeShim,
  downloadErrorMessage,
  normalizeBaseURL,
  parseChecksum,
  sha256,
  targetForPlatform,
  verifyArchiveChecksum,
  version,
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
assert.strictEqual(pkg.name, "@volcengine/cli");
assert.strictEqual(version, pkg.version);
assert.strictEqual(pkg.repository.url, "https://github.com/volcengine/volcengine-cli");
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
assert.match(
  downloadErrorMessage(404, "https://example.com/missing.zip"),
  /\nPlease download Volcengine CLI from the official releases page: https:\/\/github\.com\/volcengine\/volcengine-cli\/releases/
);

const checksumContent = `
696221f4d866a9f194806057b234a9d1609aeaec347b4a6b315cbaa8592640eb  volcengine-cli_1.0.47_darwin_amd64.zip
fe04ac6269206520c97197601e8174917e66eaea34ddafdbd1565e81de62e54a  ./volcengine-cli_1.0.47_darwin_arm64.zip
`;
const checksumEntries = parseChecksum(checksumContent);
assert.strictEqual(checksumEntries.length, 2);
assert.deepStrictEqual(checksumEntries[0], {
  hash: "696221f4d866a9f194806057b234a9d1609aeaec347b4a6b315cbaa8592640eb",
  filename: "volcengine-cli_1.0.47_darwin_amd64.zip",
});
assert.strictEqual(
  checksumForArchive(checksumContent, "volcengine-cli_1.0.47_darwin_amd64.zip"),
  "696221f4d866a9f194806057b234a9d1609aeaec347b4a6b315cbaa8592640eb"
);
assert.strictEqual(
  checksumForArchive(checksumContent, "volcengine-cli_1.0.47_darwin_arm64.zip"),
  "fe04ac6269206520c97197601e8174917e66eaea34ddafdbd1565e81de62e54a"
);
assert.throws(
  () => parseChecksum("not-a-checksum  volcengine-cli_1.0.47_linux_amd64.zip"),
  /Invalid checksum line 1/
);
assert.throws(
  () => checksumForArchive(checksumContent, "volcengine-cli_1.0.47_linux_amd64.zip"),
  /Checksum entry not found/
);
assert.strictEqual(
  sha256(Buffer.from("abc")),
  "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
);

withTempDir((dir) => {
  const checksumPath = path.join(dir, "checksum");
  const archiveData = Buffer.from("archive-data");
  const archiveName = "volcengine-cli_1.2.3_linux_amd64.zip";
  const expected = sha256(archiveData);

  fs.writeFileSync(checksumPath, `${expected}  ${archiveName}\n`);
  assert.strictEqual(verifyArchiveChecksum(archiveData, archiveName, checksumPath), expected);

  fs.writeFileSync(checksumPath, `${"0".repeat(64)}  ${archiveName}\n`);
  assert.throws(
    () => verifyArchiveChecksum(archiveData, archiveName, checksumPath),
    /official releases page: https:\/\/github\.com\/volcengine\/volcengine-cli\/releases/
  );
});

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
