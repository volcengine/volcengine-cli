#!/usr/bin/env node

const assert = require("assert");
const crypto = require("crypto");
const fs = require("fs");
const os = require("os");
const path = require("path");

const {
  prepareFromBuffers,
  validateManifest,
} = require("./prepare_npm_skills");

const skills = [
  "volcengine-cli",
  "volcengine-find-skills",
  "volcengine-knowledge-search",
  "volcengine-troubleshooting",
];
const bundle = Buffer.from("test bundle");
const sha256 = crypto.createHash("sha256").update(bundle).digest("hex");
const manifest = {
  schemaVersion: 1,
  version: "1.2.3",
  bundle: {
    file: "volcengine-skill-bundle.zip",
    sha256,
    size: bundle.length,
    cdnUrl:
      "https://cloudcache.volccdn.com/ve/skills/v1.2.3/volcengine-skill-bundle.zip",
  },
  skills: skills.map((name) => ({ name, sha256: "0".repeat(64) })),
};

assert.deepStrictEqual(validateManifest(manifest), manifest);
assert.throws(
  () => validateManifest({ ...manifest, skills: manifest.skills.slice(1) }),
  /exactly the four core Skills/
);
assert.throws(
  () => validateManifest({ ...manifest, bundle: { ...manifest.bundle, sha256: "bad" } }),
  /bundle metadata/
);

const output = fs.mkdtempSync(path.join(os.tmpdir(), "ve-npm-skills-"));
try {
  prepareFromBuffers(Buffer.from(JSON.stringify(manifest)), bundle, output);
  assert.deepStrictEqual(
    JSON.parse(fs.readFileSync(path.join(output, "manifest.json"), "utf8")),
    manifest
  );
  assert.deepStrictEqual(
    fs.readFileSync(path.join(output, "volcengine-skill-bundle.zip")),
    bundle
  );
  assert.throws(
    () => prepareFromBuffers(Buffer.from(JSON.stringify(manifest)), Buffer.from("wrong"), output),
    /size mismatch/
  );
} finally {
  fs.rmSync(output, { recursive: true, force: true });
}

console.log("prepare npm skills tests passed");
