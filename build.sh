#!/bin/bash
# Build script for mcp-exec
#
# The target platform is auto-detected from the machine running the build (see
# detect_goos/detect_goarch below); explicit TARGET_OS/TARGET_ARCH still win,
# e.g. TARGET_OS=freebsd TARGET_ARCH=amd64 ./build.sh

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# --- Platform auto-detection ------------------------------------------------
# `go env` is authoritative — it reports exactly what a plain `go build` targets by
# default; uname(1) is the fallback for the case when the Go toolchain is missing.
# Supported TARGET_OS: linux, darwin, windows, freebsd, netbsd, openbsd, dragonfly
# (DragonFly BSD is built for amd64 only by the Go toolchain)

detect_goos() {
    local os=""
    if command -v go &> /dev/null; then
        os=$(go env GOOS 2>/dev/null || true)
    fi
    if [ -n "$os" ]; then
        echo "$os"
        return 0
    fi
    # Fallback: uname -s mapped to Go's GOOS names
    case "$(uname -s 2>/dev/null | tr '[:upper:]' '[:lower:]')" in
        linux)     echo linux ;;
        darwin)    echo darwin ;;
        freebsd)   echo freebsd ;;
        netbsd)    echo netbsd ;;
        openbsd)   echo openbsd ;;
        dragonfly) echo dragonfly ;;
        # Git-Bash/Cygwin/MSYS report these; WSL's uname returns Linux (mapped above)
        mingw*|msys*|cygwin*) echo windows ;;
        *)         echo linux ;;
    esac
}

detect_goarch() {
    local arch=""
    if command -v go &> /dev/null; then
        arch=$(go env GOARCH 2>/dev/null || true)
    fi
    if [ -n "$arch" ]; then
        echo "$arch"
        return 0
    fi
    # Fallback: uname -m mapped to Go's GOARCH names
    case "$(uname -m 2>/dev/null | tr '[:upper:]' '[:lower:]')" in
        x86_64|amd64)          echo amd64 ;;
        i386|i486|i586|i686)   echo 386 ;;
        aarch64|arm64)         echo arm64 ;;
        armv8*|armv9*)         echo arm64 ;;
        armv7*|armv6*|arm)     echo arm ;;
        ppc64le)               echo ppc64le ;;
        ppc64)                 echo ppc64 ;;
        s390x)                 echo s390x ;;
        riscv64)               echo riscv64 ;;
        *)                     echo amd64 ;;
    esac
}

# Parse arguments (defaults = the platform we are building on, auto-detected)
BUILD_DIR="${BUILD_DIR:-.}"
TARGET_OS_FROM="$(detect_goos)"
TARGET_ARCH_FROM="$(detect_goarch)"
TARGET_OS="${TARGET_OS:-$TARGET_OS_FROM}"
TARGET_ARCH="${TARGET_ARCH:-$TARGET_ARCH_FROM}"
VERSION="${VERSION:-1.0.0}"
LDFLAGS="-X main.version=${VERSION} -s -w"

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go is not installed${NC}"
    exit 1
fi

# Get Go version
GO_VERSION=$(go version | awk '{print $3}')
echo -e "${YELLOW}Using Go version: ${GO_VERSION}${NC}"

# Build
echo -e "${GREEN}Building for ${TARGET_OS}/${TARGET_ARCH}...${NC}"
if [ "$TARGET_OS" = "$TARGET_OS_FROM" ] && [ "$TARGET_ARCH" = "$TARGET_ARCH_FROM" ]; then
    echo -e "${YELLOW}Target auto-detected from this host (cross-build: TARGET_OS=freebsd TARGET_ARCH=amd64 ./build.sh)${NC}"
fi
GOOS="${TARGET_OS}" GOARCH="${TARGET_ARCH}" go build \
    -ldflags="${LDFLAGS}" \
    -o "${BUILD_DIR}/mcp-exec" \
    ./cmd/mcp-exec

# Set permissions
chmod +x "${BUILD_DIR}/mcp-exec"

echo -e "${GREEN}Build successful!${NC}"
echo -e "${GREEN}Binary: ${BUILD_DIR}/mcp-exec${NC}"

# Show file info
ls -lh "${BUILD_DIR}/mcp-exec"

# BSD note: the binary runs natively on FreeBSD/DragonFlyBSD/OpenBSD/NetBSD
# (chroot/setsid/syslog work there — see the README platform matrix)
