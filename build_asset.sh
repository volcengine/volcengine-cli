#!/bin/bash
# Build CLI bindata assets from vestack/volcengine-sdk-metadata.
#
# Usage:
#   sh build_asset.sh <metadata-git-url> [branch] [--target all|explorer|param|metadata]
#
# Targets (param generation is optional; explorer follows master: always attempt):
#   all       Generate explorer + param descriptions, then bindata (default)
#   explorer  Generate Action/service descriptions only; leave paramdescriptions unchanged
#   param     Generate param descriptions + explorer, then bindata
#   metadata  Submodule + explorer (same as master) + metadata bindata; leave paramdescriptions unchanged
#
# Layout:
#   asset/asset.go                      ← metadata + explorer_descriptions (go-bindata)
#   asset/paramdescriptions/params.json ← param source of truth
#   asset/paramdescriptions/bindata.go  ← go-bindata from params.json (package paramdescriptions)
#
# explorer_descriptions is a pre-existing master feature: always run
# generate_explorer_descriptions.go; on failure soft-fail to {} so metadata
# bindata can still proceed (same as origin/master build_asset.sh).
#
# Non-interactive:
#   --target is set, or stdin/stdout is not a TTY → no menu (default target=all)
#   BUILD_ASSET_TARGET env overrides default when --target is omitted
#
# Env:
#   SKIP_PARAM_DESCRIPTIONS=1   skip param HTTP generation; keep existing paramdescriptions
#   PARAM_DESC_DELAY            default 150ms
#   PARAM_DESC_LANG             default both
#   BUILD_ASSET_TARGET          default target when --target omitted and non-interactive

set -e

# Always run from repo root (this script's directory), regardless of caller cwd.
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

usage() {
  cat <<'USAGE'
Usage: sh build_asset.sh <metadata-git-url> [branch] [--target all|explorer|param|metadata]

  all       Explorer + param descriptions + bindata (default)
  explorer  Explorer only + asset/typeset/structset bindata (params package left as-is)
  param     Param descriptions + explorer + bindata
  metadata  Explorer (master-compatible) + metadata bindata; params package left as-is

explorer_descriptions always follows master: attempt generate; on failure write
empty {} and continue (never skip/wipe just because target is metadata).

Param product lives under asset/paramdescriptions/ (not asset/asset.go).
On param generation failure the existing params.json + bindata.go are preserved
and the build exits non-zero (no silent empty product).
Interactive menu is shown only when --target is omitted and the terminal is interactive.
CI / pipes default to "all" (or BUILD_ASSET_TARGET).

Examples:
  sh build_asset.sh https://code.byted.org/iaasng/vestack-sdk-metadata master
  sh build_asset.sh https://code.byted.org/iaasng/vestack-sdk-metadata master --target param
  BUILD_ASSET_TARGET=explorer sh build_asset.sh <url>
USAGE
}

url=""
urlBranch=""
target=""

while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help|help)
      usage
      exit 0
      ;;
    --target)
      if [ -z "${2:-}" ]; then
        echo "error: --target requires a value (all|explorer|param|metadata)" >&2
        exit 1
      fi
      target="$2"
      shift 2
      ;;
    --target=*)
      target="${1#*=}"
      shift
      ;;
    --*)
      echo "error: unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
    *)
      if [ -z "$url" ]; then
        url="$1"
      elif [ -z "$urlBranch" ]; then
        urlBranch="$1"
      else
        echo "error: unexpected argument: $1" >&2
        usage >&2
        exit 1
      fi
      shift
      ;;
  esac
done

if [ -z "$url" ]; then
  echo "error: Please set metadata repo url" >&2
  usage >&2
  exit 1
fi

# Resolve target: --target flag > BUILD_ASSET_TARGET/CI env > interactive menu > all
if [ -z "$target" ]; then
  if [ -n "${BUILD_ASSET_TARGET:-}" ]; then
    target="$BUILD_ASSET_TARGET"
  elif [ -n "${CI:-}" ] || [ -n "${GITHUB_ACTIONS:-}" ] || [ -n "${BUILDKITE:-}" ]; then
    target="all"
  elif [ -t 0 ] && [ -t 1 ]; then
    echo "Select asset build target:"
    echo "  1) all       - explorer + param descriptions + bindata  (default)"
    echo "  2) explorer  - Action/service descriptions only (keep existing params)"
    echo "  3) param     - parameter descriptions + explorer"
    echo "  4) metadata  - explorer + metadata bindata (keep params; no param HTTP)"
    printf "Choice [1-4, default 1]: "
    read -r choice || choice=""
    case "${choice:-1}" in
      1|"") target="all" ;;
      2) target="explorer" ;;
      3) target="param" ;;
      4) target="metadata" ;;
      all|explorer|param|metadata) target="$choice" ;;
      *)
        echo "error: invalid choice: $choice" >&2
        exit 1
        ;;
    esac
  else
    target="all"
  fi
fi

case "$target" in
  all|explorer|param|metadata) ;;
  *)
    echo "error: invalid --target '$target' (want all|explorer|param|metadata)" >&2
    exit 1
    ;;
esac

echo "==> build_asset target=$target url=$url branch=${urlBranch:-<default>} root=$ROOT"

if ! command -v go-bindata >/dev/null 2>&1; then
  echo "error: go-bindata not found on PATH (required to regenerate asset packages)" >&2
  exit 1
fi

# Explorer is always generated (master-compatible). Only param generation is target-gated.
do_param=0
case "$target" in
  all|param)
    do_param=1
    ;;
  explorer|metadata)
    do_param=0
    ;;
esac

if [ "${SKIP_PARAM_DESCRIPTIONS}" = "1" ]; then
  echo "==> SKIP_PARAM_DESCRIPTIONS=1 → disable param generation"
  echo "warning: asset/paramdescriptions left unchanged for this build" >&2
  do_param=0
fi

#clean git cache before build
rm -rf volcengine-sdk-metadata
rm -rf .git/modules/volcengine-sdk-metadata
git config --local --unset submodule.volcengine-sdk-metadata.url 2>/dev/null || true
git config --local --unset submodule.volcengine-sdk-metadata.active 2>/dev/null || true
git rm --cached volcengine-sdk-metadata 2>/dev/null || true

rm -rf .gitmodules
touch .gitmodules

git submodule add "$url" volcengine-sdk-metadata
if [ -n "$urlBranch" ]; then
  (
    cd volcengine-sdk-metadata
    # -B: reset/create local branch (checkout -b fails when default branch name already exists)
    git fetch origin "$urlBranch" 2>/dev/null || true
    git checkout -B "$urlBranch" "origin/$urlBranch"
  )
fi

PARAM_DESC_DIR="asset/paramdescriptions"
PARAM_DESC_JSON="${PARAM_DESC_DIR}/params.json"
PARAM_DESC_BINDATA="${PARAM_DESC_DIR}/bindata.go"

write_empty_explorer() {
  mkdir -p volcengine-sdk-metadata/explorer_descriptions
  printf '{}\n' > volcengine-sdk-metadata/explorer_descriptions/descriptions.json
}

# Regenerate package paramdescriptions from source params.json (not asset/asset.go).
# Requires a non-empty existing params.json; never invents an empty product here.
bindata_param_descriptions() {
  if [ ! -f "${PARAM_DESC_JSON}" ]; then
    echo "error: ${PARAM_DESC_JSON} missing; cannot generate paramdescriptions bindata" >&2
    return 1
  fi
  echo "==> go-bindata paramdescriptions (${PARAM_DESC_JSON} → ${PARAM_DESC_BINDATA})"
  (
    cd "${PARAM_DESC_DIR}"
    go-bindata -pkg paramdescriptions -prefix . -o bindata.go params.json
  )
}

# Action / service descriptions (lightweight: explorer/apis).
# Always run — same control flow as origin/master build_asset.sh (not a new feature).
# Soft-fail only when generation fails so metadata bindata can still proceed.
echo "==> generating explorer descriptions (always; master-compatible)"
if ! go run ./scripts/generate_explorer_descriptions.go \
  --metadata-dir volcengine-sdk-metadata/metadata \
  --out volcengine-sdk-metadata/explorer_descriptions/descriptions.json
then
  echo "skip explorer descriptions generation" >&2
  echo "warning: explorer descriptions generation failed; writing empty product" >&2
  write_empty_explorer
fi

# Parameter descriptions (heavier: explorer/api-swagger per action, rate-limited).
# Product: asset/paramdescriptions/params.json + bindata.go (separate package).
# Fail closed: never overwrite a good corpus with empty product on generator failure.
if [ "$do_param" = "1" ]; then
  echo "==> generating param descriptions → ${PARAM_DESC_JSON} (may take a long time)"
  PARAM_DELAY="${PARAM_DESC_DELAY:-150ms}"
  PARAM_LANG="${PARAM_DESC_LANG:-both}"
  mkdir -p "${PARAM_DESC_DIR}"
  if ! go run ./scripts/generate_param_descriptions.go \
    --metadata-dir volcengine-sdk-metadata/metadata \
    --out "${PARAM_DESC_JSON}" \
    --delay "${PARAM_DELAY}" \
    --lang "${PARAM_LANG}"
  then
    echo "error: param descriptions generation failed" >&2
    if [ -f "${PARAM_DESC_JSON}" ] || [ -f "${PARAM_DESC_BINDATA}" ]; then
      echo "error: keeping existing asset/paramdescriptions (params.json / bindata.go) unchanged" >&2
    fi
    exit 1
  fi
  if ! bindata_param_descriptions; then
    echo "error: paramdescriptions bindata failed; existing bindata.go left as-is if present" >&2
    exit 1
  fi
else
  echo "==> skip param descriptions generation (target=$target)"
  if [ -f "${PARAM_DESC_BINDATA}" ]; then
    echo "    keeping existing ${PARAM_DESC_BINDATA} (CLI loads embedded bindata)"
    if [ ! -f "${PARAM_DESC_JSON}" ]; then
      echo "warning: ${PARAM_DESC_JSON} missing; source JSON absent but embedded bindata still used" >&2
    fi
  elif [ -f "${PARAM_DESC_JSON}" ]; then
    echo "warning: ${PARAM_DESC_BINDATA} missing while ${PARAM_DESC_JSON} exists; run: go generate ./asset/paramdescriptions" >&2
  else
    echo "warning: no paramdescriptions product; CLI param help will be empty until generated" >&2
  fi
fi

echo "==> go-bindata (metadata + explorer only; params live in asset/paramdescriptions)"
go-bindata -pkg asset -o asset/asset.go \
  volcengine-sdk-metadata/metadata/... \
  volcengine-sdk-metadata/explorer_descriptions/...
go-bindata -pkg typeset -o typeset/typeset.go volcengine-sdk-metadata/metatype/...
go-bindata -pkg structset -o structset/structset.go volcengine-sdk-metadata/structure/...

#clean git cache after build
rm -rf volcengine-sdk-metadata
rm -rf .git/modules/volcengine-sdk-metadata
git config --local --unset submodule.volcengine-sdk-metadata.url 2>/dev/null || true
git config --local --unset submodule.volcengine-sdk-metadata.active 2>/dev/null || true
git rm --cached volcengine-sdk-metadata 2>/dev/null || true

rm -rf .gitmodules

echo "==> build_asset done (target=$target)"
