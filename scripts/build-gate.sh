#!/bin/sh
# P3 Build Gate — reproducible verification script.
# Run from project root: sh scripts/build-gate.sh
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DAEMON_DIR="$PROJECT_ROOT/companion-daemon"
MOBILE_DIR="$PROJECT_ROOT/mobile"

echo "=== POKIT Build Gate ==="
echo ""

# ── Backend ──
echo "--- Backend ---"
cd "$DAEMON_DIR"

echo -n "  go build ... "
go build ./... && echo "OK" || { echo "FAILED"; exit 1; }

echo -n "  go vet ... "
go vet ./... && echo "OK" || { echo "FAILED"; exit 1; }

echo -n "  go test -race ... "
go test -race ./... -count=1 > /dev/null 2>&1 && echo "OK" || { echo "FAILED"; exit 1; }

echo -n "  git diff --check ... "
git diff --check > /dev/null 2>&1 && echo "OK" || { echo "FAILED"; exit 1; }

# ── Mobile (best-effort) ──
echo "--- Mobile ---"
if [ -d "$MOBILE_DIR/node_modules" ]; then
    cd "$MOBILE_DIR"
    echo -n "  npx tsc --noEmit ... "
    npx tsc --noEmit > /dev/null 2>&1 && echo "OK" || { echo "FAILED"; exit 1; }
else
    echo "  Mobile: not-run (node_modules absent)"
fi

# ── Invariants ──
echo "--- Invariants ---"

echo -n "  vendor branch scan ... "
if grep -rn "agentKind.*===" "$MOBILE_DIR/src/" 2>/dev/null; then
    echo "FAILED: vendor branch found"
    exit 1
fi
echo "OK"

echo -n "  ID inference scan ... "
if grep -rn "opt\.id === 'approve'\|opt\.id === 'reject'" "$MOBILE_DIR/src/" 2>/dev/null; then
    echo "FAILED: ID inference found"
    exit 1
fi
echo "OK"

# ── Security ──
echo "--- Security ---"
echo -n "  secret scan ... "
# Exclude: test fixtures (testdata/), redaction patterns (diagnostic.go), redaction tests (approval_test.go),
# documentation examples, and auth test mocks (auth_test.go).
SECRETS=$(grep -rn "sk-[A-Za-z0-9]\|ghp_\|xox[baprs]-\|Bearer [A-Za-z0-9]" \
    "$DAEMON_DIR/internal/" "$DAEMON_DIR/docs/" "$PROJECT_ROOT/docs/" 2>/dev/null \
    | grep -v "testdata/" \
    | grep -v "diagnostic\.go" \
    | grep -v "approval_test\.go" \
    | grep -v "auth_test\.go" \
    | grep -v "redact" \
    | grep -v "fake" \
    | grep -v "REDACTED" \
    | grep -v "redactions" \
    | grep -v "AGENT_ADAPTER_LAYER_PLAN" \
    || true)
if [ -n "$SECRETS" ]; then
    echo "$SECRETS"
    echo "FAILED: potential secret found"
    exit 1
fi
echo "OK"

echo ""
echo "=== ALL GATES PASSED ==="
