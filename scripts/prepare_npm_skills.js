#!/usr/bin/env node

"use strict";

const crypto = require("crypto");
const fs = require("fs");
const https = require("https");
const path = require("path");

const DEFAULT_MANIFEST_URL =
  "https://cloudcache.volccdn.com/ve/skills/latest/manifest.json";
const BUNDLE_FILE = "volcengine-skill-bundle.zip";
const CORE_SKILLS = [
  "volcengine-cli",
  "volcengine-find-skills",
  "volcengine-knowledge-search",
  "volcengine-troubleshooting",
];
const MAX_MANIFEST_BYTES = 1024 * 1024;
const MAX_BUNDLE_BYTES = 20 * 1024 * 1024;

function download(url, maxBytes, redirects = 0) {
  return new Promise((resolve, reject) => {
    if (redirects > 5) {
      reject(new Error("too many redirects"));
      return;
    }
    let parsed;
    try {
      parsed = new URL(url);
    } catch (err) {
      reject(new Error(`invalid download URL: ${url}`));
      return;
    }
    if (parsed.protocol !== "https:") {
      reject(new Error(`download URL must use https: ${url}`));
      return;
    }
    const request = https.get(parsed, (response) => {
      if (
        response.statusCode >= 300 &&
        response.statusCode < 400 &&
        response.headers.location
      ) {
        response.resume();
        const nextURL = new URL(response.headers.location, parsed).toString();
        resolve(download(nextURL, maxBytes, redirects + 1));
        return;
      }
      if (response.statusCode !== 200) {
        response.resume();
        reject(new Error(`HTTP ${response.statusCode} for ${url}`));
        return;
      }
      const contentLength = Number(response.headers["content-length"] || 0);
      if (contentLength > maxBytes) {
        response.resume();
        reject(new Error(`download exceeds ${maxBytes} bytes`));
        return;
      }
      const chunks = [];
      let received = 0;
      response.on("data", (chunk) => {
        received += chunk.length;
        if (received > maxBytes) {
          request.destroy(new Error(`download exceeds ${maxBytes} bytes`));
          return;
        }
        chunks.push(chunk);
      });
      response.on("end", () => resolve(Buffer.concat(chunks)));
      response.on("error", reject);
    });
    request.setTimeout(30000, () => request.destroy(new Error("download timed out")));
    request.on("error", reject);
  });
}

function validateManifest(manifest) {
  if (!manifest || manifest.schemaVersion !== 1) {
    throw new Error("unsupported Skill manifest schema");
  }
  if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(manifest.version || "")) {
    throw new Error("invalid Skill release version");
  }
  const bundle = manifest.bundle || {};
  if (
    bundle.file !== BUNDLE_FILE ||
    !/^[0-9a-f]{64}$/i.test(bundle.sha256 || "") ||
    !Number.isSafeInteger(bundle.size) ||
    bundle.size <= 0 ||
    bundle.size > MAX_BUNDLE_BYTES ||
    typeof bundle.cdnUrl !== "string" ||
    !bundle.cdnUrl.startsWith("https://")
  ) {
    throw new Error("invalid Skill bundle metadata");
  }
  if (!Array.isArray(manifest.skills)) {
    throw new Error("Skill manifest must contain exactly the four core Skills");
  }
  const names = manifest.skills
    .map((skill) => {
      if (!skill || !/^[0-9a-f]{64}$/i.test(skill.sha256 || "")) {
        throw new Error("invalid Skill content metadata");
      }
      return skill.name;
    })
    .sort();
  if (JSON.stringify(names) !== JSON.stringify(CORE_SKILLS)) {
    throw new Error("Skill manifest must contain exactly the four core Skills");
  }
  return manifest;
}

function prepareFromBuffers(manifestData, bundleData, outputDir) {
  let manifest;
  try {
    manifest = validateManifest(JSON.parse(manifestData.toString("utf8")));
  } catch (err) {
    throw new Error(`invalid Skill manifest: ${err.message || err}`);
  }
  if (bundleData.length !== manifest.bundle.size) {
    throw new Error(
      `Skill bundle size mismatch: expected ${manifest.bundle.size}, got ${bundleData.length}`
    );
  }
  const actual = crypto.createHash("sha256").update(bundleData).digest("hex");
  if (actual !== manifest.bundle.sha256.toLowerCase()) {
    throw new Error(
      `Skill bundle sha256 mismatch: expected ${manifest.bundle.sha256}, got ${actual}`
    );
  }
  fs.mkdirSync(outputDir, { recursive: true });
  const manifestPath = path.join(outputDir, "manifest.json");
  const bundlePath = path.join(outputDir, BUNDLE_FILE);
  fs.writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
  fs.writeFileSync(bundlePath, bundleData);
  return { manifestPath, bundlePath, version: manifest.version };
}

async function main() {
  const manifestURL = process.argv[2] || DEFAULT_MANIFEST_URL;
  const outputDir = path.resolve(process.argv[3] || path.join(__dirname, "..", "npm", "skills"));
  const manifestData = await download(manifestURL, MAX_MANIFEST_BYTES);
  const manifest = validateManifest(JSON.parse(manifestData.toString("utf8")));
  const bundleData = await download(manifest.bundle.cdnUrl, MAX_BUNDLE_BYTES);
  const result = prepareFromBuffers(manifestData, bundleData, outputDir);
  console.log(`Prepared npm Skill fallback v${result.version} in ${outputDir}`);
}

if (require.main === module) {
  main().catch((err) => {
    console.error(`Prepare npm Skill fallback failed: ${err.message || err}`);
    process.exit(1);
  });
}

module.exports = {
  download,
  prepareFromBuffers,
  validateManifest,
};
