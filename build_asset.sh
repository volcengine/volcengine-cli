#!/bin/bash
# Build CLI bindata assets from vestack/volcengine-sdk-metadata.
#
# Usage:
#   sh build_asset.sh <metadata-git-url> [branch] [--target all|explorer|param|metadata]
#
# Targets:
#   all       Generate explorer + param descriptions, then bindata (default)
#   explorer  Generate Action/service descriptions only (param product left empty)
#   param     Generate param descriptions; also refreshes explorer (cheap) so Action text is not wiped
#   metadata  Submodule + bindata only; description products empty (HTTP skipped)
#
# Non-interactive:
#   --target is set, or stdin/stdout is not a TTY → no menu (default target=all)
#   BUILD_ASSET_TARGET env overrides default when --target is omitted
#
# Env:
#   SKIP_PARAM_DESCRIPTIONS=1   same as excluding param generation (with all/param)
#   PARAM_DESC_DELAY            default 150ms
#   PARAM_DESC_LANG             default both
#   BUILD_ASSET_TARGET          default target when --target omitted and non-interactive

set -e

usage() {
  cat <<'USAGE'
Usage: sh build_asset.sh <metadata-git-url> [branch] [--target all|explorer|param|metadata]

  all       Explorer descriptions + param descriptions + bindata (default)
  explorer  Explorer descriptions only + bindata (param descriptions become empty)
  param     Param descriptions + explorer refresh (cheap) + bindata
  metadata  Submodule + bindata only (no explorer/param HTTP generation)

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
    echo "  2) explorer  - Action/service descriptions only"
    echo "  3) param     - parameter descriptions (+ refresh explorer)"
    echo "  4) metadata  - metadata/metatype/structure bindata only (no HTTP)"
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

echo "==> build_asset target=$target url=$url branch=${urlBranch:-<default>}"

do_explorer=0
do_param=0
case "$target" in
  all)
    do_explorer=1
    do_param=1
    ;;
  explorer)
    do_explorer=1
    do_param=0
    ;;
  param)
    # Explorer is cheap; refresh so Action text is not wiped when rewriting asset.go.
    do_explorer=1
    do_param=1
    ;;
  metadata)
    do_explorer=0
    do_param=0
    ;;
esac

if [ "${SKIP_PARAM_DESCRIPTIONS}" = "1" ]; then
  echo "==> SKIP_PARAM_DESCRIPTIONS=1 → disable param generation"
  echo "warning: param_descriptions will be empty in asset.go for this build" >&2
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

write_empty_explorer() {
  mkdir -p volcengine-sdk-metadata/explorer_descriptions
  printf '{}\n' > volcengine-sdk-metadata/explorer_descriptions/descriptions.json
}

write_empty_param() {
  mkdir -p volcengine-sdk-metadata/param_descriptions
  printf '{ "apis": {} }\n' > volcengine-sdk-metadata/param_descriptions/params.json
}

# Action / service descriptions (lightweight: explorer/apis)
if [ "$do_explorer" = "1" ]; then
  echo "==> generating explorer descriptions"
  if ! go run ./scripts/generate_explorer_descriptions.go \
    --metadata-dir volcengine-sdk-metadata/metadata \
    --out volcengine-sdk-metadata/explorer_descriptions/descriptions.json
  then
    echo "warning: explorer descriptions generation failed; writing empty product" >&2
    write_empty_explorer
  fi
else
  echo "==> skip explorer descriptions generation (target=$target)"
  echo "warning: explorer_descriptions will be empty in asset.go for this build" >&2
  write_empty_explorer
fi

# Parameter descriptions (heavier: explorer/api-swagger per action, rate-limited)
if [ "$do_param" = "1" ]; then
  echo "==> generating param descriptions (may take a long time)"
  PARAM_DELAY="${PARAM_DESC_DELAY:-150ms}"
  PARAM_LANG="${PARAM_DESC_LANG:-both}"
  if ! go run ./scripts/generate_param_descriptions.go \
    --metadata-dir volcengine-sdk-metadata/metadata \
    --out volcengine-sdk-metadata/param_descriptions/params.json \
    --delay "${PARAM_DELAY}" \
    --lang "${PARAM_LANG}"
  then
    echo "warning: param descriptions generation failed; writing empty product" >&2
    write_empty_param
  fi
else
  echo "==> skip param descriptions generation (target=$target)"
  if [ "$target" = "explorer" ] || [ "$target" = "metadata" ]; then
    echo "warning: param_descriptions will be empty in asset.go for this build" >&2
  fi
  write_empty_param
fi

echo "==> go-bindata"
go-bindata -pkg asset -o asset/asset.go \
  volcengine-sdk-metadata/metadata/... \
  volcengine-sdk-metadata/explorer_descriptions/... \
  volcengine-sdk-metadata/param_descriptions/...
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
