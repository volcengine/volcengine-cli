#!/usr/bin/env node

const { execSync } = require("child_process");
const https = require("https");
const fs = require("fs");
const path = require("path");

const VERSION = "1.0.46";
const DEFAULT_DOWNLOAD_BASE_URL = "https://sdk-liuzhaoliang.tos-cn-beijing.volces.com/volcengine-cli";
const DOWNLOAD_BASE_URL = normalizeBaseURL(
  process.env.VOLCENGINE_CLI_DOWNLOAD_BASE_URL || DEFAULT_DOWNLOAD_BASE_URL
);

const PLATFORM_MAP = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
  freebsd: "freebsd",
};

const ARCH_MAP = {
  x64: "amd64",
  arm64: "arm64",
  ia32: "386",
  arm: "arm",
};

const SUPPORTED_TARGETS = {
  darwin: ["amd64", "arm64"],
  linux: ["amd64", "386", "arm", "arm64"],
  freebsd: ["amd64", "386", "arm", "arm64"],
  windows: ["amd64", "386", "arm64"],
};

function normalizeBaseURL(url) {
  return String(url || "").replace(/\/+$/, "");
}

function binaryNameForPlatform(platform) {
  return platform === "win32" ? "ve.exe" : "ve";
}

function targetForPlatform(platform, arch) {
  const targetPlatform = PLATFORM_MAP[platform];
  const targetArch = ARCH_MAP[arch];

  if (!targetPlatform || !targetArch) {
    return null;
  }

  const supportedArchs = SUPPORTED_TARGETS[targetPlatform] || [];
  if (supportedArchs.indexOf(targetArch) === -1) {
    return null;
  }

  return {
    platform: targetPlatform,
    arch: targetArch,
  };
}

function archiveNameForTarget(target, version) {
  return `volcengine-cli_${version}_${target.platform}_${target.arch}.zip`;
}

function archiveURLForTarget(target, version, downloadBaseURL) {
  const baseURL = normalizeBaseURL(downloadBaseURL);
  return `${baseURL}/v${version}/${archiveNameForTarget(target, version)}`;
}

function createWindowsVeShim(binDir) {
  const shimPath = path.join(binDir, "ve");
  const shim = `#!/usr/bin/env node

const { spawnSync } = require("child_process");
const path = require("path");

const exePath = path.join(__dirname, "ve.exe");
const result = spawnSync(exePath, process.argv.slice(2), { stdio: "inherit" });

if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}

process.exit(result.status === null ? 1 : result.status);
`;

  fs.writeFileSync(shimPath, shim);
  fs.chmodSync(shimPath, 0o755);
}

function download(url) {
  return new Promise((resolve, reject) => {
    const follow = (url) => {
      https.get(url, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          follow(res.headers.location);
          return;
        }
        if (res.statusCode !== 200) {
          reject(new Error(`Download failed: HTTP ${res.statusCode} for ${url}`));
          return;
        }
        const chunks = [];
        res.on("data", (chunk) => chunks.push(chunk));
        res.on("end", () => resolve(Buffer.concat(chunks)));
        res.on("error", reject);
      }).on("error", reject);
    };
    follow(url);
  });
}

async function install() {
  const target = targetForPlatform(process.platform, process.arch);

  if (!target) {
    console.error(`Unsupported platform: ${process.platform} ${process.arch}`);
    process.exit(1);
  }

  const zipName = archiveNameForTarget(target, VERSION);
  const url = archiveURLForTarget(target, VERSION, DOWNLOAD_BASE_URL);
  const binDir = path.join(__dirname, "bin");
  const isWindows = process.platform === "win32";
  const binaryName = binaryNameForPlatform(process.platform);
  const binPath = path.join(binDir, binaryName);

  fs.mkdirSync(binDir, { recursive: true });
  console.log(`Downloading ${zipName}...`);

  const data = await download(url);
  const tmpDir = path.join(__dirname, ".tmp");
  const zipPath = path.join(tmpDir, zipName);

  fs.mkdirSync(tmpDir, { recursive: true });
  fs.writeFileSync(zipPath, data);

  try {
    if (isWindows) {
      execSync(
        `powershell -Command "Expand-Archive -Path '${zipPath}' -DestinationPath '${tmpDir}' -Force"`,
        { stdio: "pipe" }
      );
    } else {
      execSync(`unzip -o -q "${zipPath}" -d "${tmpDir}"`, { stdio: "pipe" });
    }

    const extracted = fs.readdirSync(tmpDir);
    const veBinary = extracted.find((f) => f === "ve" || f === "ve.exe");

    if (!veBinary) {
      console.error("Could not find 've' binary in zip archive. Found:", extracted);
      process.exit(1);
    }

    fs.copyFileSync(path.join(tmpDir, veBinary), binPath);

    if (isWindows) {
      createWindowsVeShim(binDir);
    } else {
      fs.chmodSync(binPath, 0o755);
    }

    if (process.platform === "darwin") {
      try {
        execSync(`xattr -d com.apple.quarantine "${binPath}"`, { stdio: "pipe" });
      } catch (_) {
        // Attribute may not exist, ignore.
      }
    }

    console.log(`Volcengine CLI v${VERSION} installed for ${target.platform}/${target.arch}`);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
}

if (require.main === module) {
  install().catch((err) => {
    console.error("Installation failed:", err.message);
    process.exit(1);
  });
}

module.exports = {
  archiveNameForTarget,
  archiveURLForTarget,
  binaryNameForPlatform,
  createWindowsVeShim,
  normalizeBaseURL,
  targetForPlatform,
};
