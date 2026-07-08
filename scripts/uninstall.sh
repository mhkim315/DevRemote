#!/bin/sh
# Uninstall the POKIT daemon binary. Idempotent.
# Usage:
#   sh scripts/uninstall.sh           local uninstall
#   sh scripts/uninstall.sh --global  system uninstall (requires sudo)
set -e

if [ "$1" = "--global" ]; then
    INSTALL_DIR="/usr/local/bin"
    if [ "$(id -u)" != "0" ] && ! sudo -n true 2>/dev/null; then
        echo "Global uninstall requires sudo. Run:"
        echo "  sudo sh scripts/uninstall.sh --global"
        echo ""
        echo "Or uninstall locally:"
        echo "  sh scripts/uninstall.sh"
        exit 1
    fi
    SUDO="sudo"
else
    INSTALL_DIR="${HOME}/.local/bin"
    SUDO=""
fi

TARGET="$INSTALL_DIR/devremote"

if [ -f "$TARGET" ]; then
    echo "Removing $TARGET ..."
    $SUDO rm -f "$TARGET"
    echo "Uninstalled: $TARGET"
else
    echo "Not installed: $TARGET (nothing to do)"
fi
