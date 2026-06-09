#!/bin/bash
set -e

usage() {
  cat <<'USAGE'
Usage: sh build.sh [os] [arch]

Builds the ve CLI. Both arguments are optional; when omitted they are
auto-detected from the current machine.

  os    darwin | linux | freebsd | windows   (default: detected via uname -s)
  arch  amd64  | arm64  | 386     | arm       (default: detected via uname -m)

Examples:
  sh build.sh                # build for the current machine
  sh build.sh linux amd64    # cross-compile for linux/amd64

The output binary is "ve" ("ve.exe" on windows).
USAGE
}

case "$1" in
  -h|--help|help)
    usage
    exit 0
    ;;
esac

OS=$1
ARCH=$2

if [ -z "$OS" ]; then
  UNAME_S=$(uname -s 2>/dev/null || echo "Unknown")
  case "$UNAME_S" in
    Darwin*)  OS="darwin" ;;
    Linux*)   OS="linux" ;;
    FreeBSD*) OS="freebsd" ;;
    MINGW*|MSYS*|CYGWIN*)  OS="windows" ;;
    *)
      echo "Error: Unsupported OS: $UNAME_S" >&2
      exit 1
      ;;
  esac
  echo "Auto-Detected OS: $OS"
fi

case "$OS" in
  darwin|linux|freebsd|windows) ;;
  *)
    echo "Error: Unsupported OS: $OS" >&2
    exit 1
    ;;
esac

if [ -z "$ARCH" ]; then
  UNAME_M=$(uname -m 2>/dev/null || echo "Unknown")
  case "$UNAME_M" in
    x86_64|amd64)       ARCH="amd64" ;;
    aarch64|arm64)      ARCH="arm64" ;;
    i386|i686)          ARCH="386" ;;
    armv6l|armv7l|arm)  ARCH="arm" ;;
    *)
      echo "Error: Unsupported ARCH: $UNAME_M" >&2
      exit 1
      ;;
  esac
  echo "Auto-Detected ARCH: $ARCH"
fi

case "$ARCH" in
  amd64|arm64|386|arm) ;;
  *)
    echo "Error: Unsupported ARCH: $ARCH" >&2
    exit 1
    ;;
esac

NAME="ve"
if [ "$OS" = "windows" ]; then
  NAME="ve.exe"
fi

echo "Building for $OS/$ARCH..."
CGO_ENABLED=0 GOOS="$OS" GOARCH="$ARCH" go build -o "$NAME" -tags codegen
echo "Build complete. Binary output: $NAME"
