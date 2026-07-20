#!/bin/sh
# Install the POKIT daemon binary.
# Usage:
#   sh scripts/install.sh           local install to ~/.local/bin
#   sh scripts/install.sh --global  system install to /usr/local/bin (requires sudo)
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DAEMON_DIR="$PROJECT_ROOT/companion-daemon"
VERSION=$(cd "$DAEMON_DIR" && git describe --tags --always --dirty 2>/dev/null || echo "dev")

if [ "$1" = "--global" ]; then
    INSTALL_DIR="/usr/local/bin"
    if [ "$(id -u)" != "0" ] && ! sudo -n true 2>/dev/null; then
        echo "Global install requires sudo. Run:"
        echo "  sudo sh scripts/install.sh --global"
        echo ""
        echo "Or install locally:"
        echo "  sh scripts/install.sh"
        exit 1
    fi
    SUDO="sudo"
else
    INSTALL_DIR="${HOME}/.local/bin"
    SUDO=""
fi

mkdir -p "$INSTALL_DIR"

echo "=== POKIT Daemon Install ==="
echo "Version: $VERSION"
echo "Target:  $INSTALL_DIR"

cd "$DAEMON_DIR"
echo "Building..."
go build -ldflags="-X main.version=$VERSION" -o "$INSTALL_DIR/devremote" ./cmd/devremote

echo "Installed: $INSTALL_DIR/devremote ($VERSION)"

if [ "$1" = "--global" ]; then
    echo ""
    echo "To uninstall globally:"
    echo "  sudo sh scripts/uninstall.sh --global"
else
    echo ""
    echo "To uninstall:"
    echo "  sh scripts/uninstall.sh"
fi

echo ""
echo "To start the daemon:"
echo "  devremote daemon --insecure-local-only --enable-agent-detection"
