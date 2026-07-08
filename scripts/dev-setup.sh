#!/bin/sh
# Full POKIT development environment setup.
# Run from project root: sh scripts/dev-setup.sh
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DAEMON_DIR="$PROJECT_ROOT/companion-daemon"
MOBILE_DIR="$PROJECT_ROOT/mobile"

echo "=== POKIT Dev Setup ==="
echo ""

# ── Prerequisites ──
check_cmd() {
    printf "  %-20s " "$1"
    if command -v "$2" > /dev/null 2>&1; then
        echo "OK ($($2 --version 2>/dev/null | head -1 | cut -c1-40))"
    else
        echo "MISSING — install $1"
    fi
}

echo "--- Prerequisites ---"
check_cmd "Go"       "go"
check_cmd "Node.js"  "node"
check_cmd "npm"      "npm"
echo ""

# ── Backend ──
echo "--- Backend ---"

cd "$DAEMON_DIR"

echo -n "  go mod download ... "
go mod download > /dev/null 2>&1 && echo "OK" || echo "FAILED"

echo -n "  go build        ... "
go build ./... > /dev/null 2>&1 && echo "OK" || echo "FAILED"

echo -n "  go vet          ... "
go vet ./... > /dev/null 2>&1 && echo "OK" || echo "FAILED"

echo ""

# ── Mobile ──
echo "--- Mobile ---"

cd "$MOBILE_DIR"

if [ -f package.json ]; then
    if [ -d node_modules ]; then
        echo "  node_modules already installed"
    else
        echo -n "  npm ci ... "
        npm ci > /dev/null 2>&1 && echo "OK" || { echo "FAILED"; echo "  Run: cd mobile && npm ci"; }
    fi

    echo -n "  npx tsc --noEmit ... "
    npx tsc --noEmit > /dev/null 2>&1 && echo "OK" || echo "FAILED (type errors)"
else
    echo "  Mobile dir not found — skipping"
fi

echo ""

# ── Install daemon ──
echo "--- Install daemon ---"
sh "$PROJECT_ROOT/scripts/install.sh"

echo ""
echo "=== Setup complete ==="
echo ""
echo "Quick start:"
echo "  devremote daemon --insecure-local-only --enable-localpty --enable-agent-detection"
echo "  cd mobile && EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1 npx expo run:android"
echo ""
echo "Run build gate: sh scripts/build-gate.sh"
