#!/bin/sh
# Install the POKIT daemon binary.
# Usage: sh scripts/install.sh [--global]
set -e

INSTALL_DIR="${HOME}/.local/bin"
GLOBAL=0

if [ "$1" = "--global" ]; then
    INSTALL_DIR="/usr/local/bin"
    GLOBAL=1
fi

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DAEMON_DIR="$PROJECT_ROOT/companion-daemon"

echo "=== POKIT Daemon Install ==="
echo "Target: $INSTALL_DIR"

cd "$DAEMON_DIR"
echo "Building..."
go build -o "$INSTALL_DIR/devremote" ./cmd/devremote

echo "Installed: $INSTALL_DIR/devremote"
"$INSTALL_DIR/devremote" --help 2>/dev/null | head -3 || true

if [ $GLOBAL -eq 1 ]; then
    echo ""
    echo "Global install requires sudo for /usr/local/bin."
    echo "To run without sudo, use: sh scripts/install.sh"
fi

echo ""
echo "To start the daemon:"
echo "  devremote daemon --insecure-local-only --enable-localpty --enable-agent-detection"
echo ""
echo "For local testing with no-login mobile builds:"
echo "  cd mobile && EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1 npx expo run:android"
