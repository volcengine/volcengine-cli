#!/usr/bin/env node

const fs = require("fs");
const path = require("path");

const version = process.argv[2];
const downloadBaseURL = normalizeBaseURL(process.argv[3]);

if (!version || !/^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$/.test(version)) {
  throw new Error(`invalid npm version: ${version}`);
}

if (!downloadBaseURL) {
  throw new Error("download base URL is required");
}

try {
  const parsed = new URL(downloadBaseURL);
  if (parsed.protocol !== "https:") {
    throw new Error("download base URL must use https");
  }
} catch (err) {
  throw new Error(`invalid download base URL: ${downloadBaseURL}`);
}

const root = path.resolve(__dirname, "..");
const packagePath = path.join(root, "npm", "package.json");
const installPath = path.join(root, "npm", "install.js");
const checksumSourcePath = path.join(root, "dist", `volcengine-cli_${version}_SHA256SUMS`);
const checksumPackagePath = path.join(root, "npm", "checksum");

const pkg = JSON.parse(fs.readFileSync(packagePath, "utf8"));
pkg.version = version;
fs.writeFileSync(packagePath, `${JSON.stringify(pkg, null, 2)}\n`);

let install = fs.readFileSync(installPath, "utf8");
install = replaceOne(
  install,
  /const DEFAULT_DOWNLOAD_BASE_URL = "([^"]+)";/,
  `const DEFAULT_DOWNLOAD_BASE_URL = "${downloadBaseURL}";`,
  "install.js DEFAULT_DOWNLOAD_BASE_URL"
);
fs.writeFileSync(installPath, install);

if (!fs.existsSync(checksumSourcePath)) {
  throw new Error(`missing checksum file: ${checksumSourcePath}`);
}
fs.copyFileSync(checksumSourcePath, checksumPackagePath);

function normalizeBaseURL(url) {
  return String(url || "").replace(/\/+$/, "");
}

function replaceOne(content, pattern, replacement, label) {
  const matches = content.match(pattern);
  if (!matches) {
    throw new Error(`missing ${label}`);
  }
  return content.replace(pattern, replacement);
}
