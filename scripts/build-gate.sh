#!/bin/sh
# P3 Build Gate — reproducible verification script.
# Run from project root:
#   sh scripts/build-gate.sh              # strict: all gates must pass
#   ALLOW_MOBILE_NOT_RUN=1 sh scripts/build-gate.sh   # backend-only
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DAEMON_DIR="$PROJECT_ROOT/companion-daemon"
MOBILE_DIR="$PROJECT_ROOT/mobile"
MOBILE_NOT_RUN="${ALLOW_MOBILE_NOT_RUN:-0}"
MOBILE_SKIPPED=0

gate_pass()  { printf "  %s ... OK\n" "$1"; }
gate_fail()  { printf "  %s ... FAILED\n" "$1"; exit 1; }

echo "=== POKIT Build Gate ==="
echo ""

# ── Backend ──
echo "--- Backend ---"
cd "$DAEMON_DIR"

go build ./...                     && gate_pass "go build"        || gate_fail "go build"
go vet ./...                       && gate_pass "go vet"          || gate_fail "go vet"
# Capture test output to diagnose failures (Blocker 5 fix).
TEST_OUTPUT=$(go test -race ./... -count=1 2>&1) && gate_pass "go test -race" || {
    echo "$TEST_OUTPUT"
    gate_fail "go test -race"
}
git diff --check > /dev/null 2>&1  && gate_pass "git diff --check" || gate_fail "git diff --check"

# ── Mobile ──
echo "--- Mobile ---"
if [ -d "$MOBILE_DIR/node_modules" ]; then
    cd "$MOBILE_DIR"
    TSC_OUTPUT=$(npm run typecheck 2>&1) && gate_pass "npm run typecheck" || {
    echo "$TSC_OUTPUT"
    gate_fail "npm run typecheck"
}
    TEST_OUTPUT=$(npm test -- --ci 2>&1) && gate_pass "npm test" || {
    echo "$TEST_OUTPUT"
    gate_fail "npm test"
}
else
    MOBILE_SKIPPED=1
    if [ "$MOBILE_NOT_RUN" = "1" ]; then
        printf "  Mobile: not-run (node_modules absent)\n"
    else
        printf "  FAILED: mobile dependencies missing. Run: cd mobile && npm ci\n"
        exit 1
    fi
fi

# ── Invariants ──
echo "--- Invariants ---"

if grep -rn "agentKind.*===" "$MOBILE_DIR/src/" 2>/dev/null; then
    gate_fail "vendor branch scan"
fi
gate_pass "vendor branch scan"

if grep -rn "opt\.id === 'approve'\|opt\.id === 'reject'" "$MOBILE_DIR/src/" 2>/dev/null; then
    gate_fail "ID inference scan"
fi
gate_pass "ID inference scan"

# ── Security ──
echo "--- Security ---"
# Require a credential-like bearer value length. The previous one-character
# pattern matched identifiers such as "isActiveBearer reports" and harmless
# unit-test values ("Bearer xyz"), producing false failures as auth coverage
# grew. Real bearer/JWT values are substantially longer.
SECRETS=$(grep -rn "sk-[A-Za-z0-9]\|ghp_\|xox[baprs]-\|Bearer [A-Za-z0-9._~-]\{20,\}" \
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
    | grep -v "E6_NO_LOGIN_REPORT" \
    || true)
if [ -n "$SECRETS" ]; then
    echo "$SECRETS"
    gate_fail "secret scan"
fi
gate_pass "secret scan"

echo ""
if [ "$MOBILE_SKIPPED" = "1" ]; then
    echo "=== BACKEND GATES PASSED; MOBILE NOT RUN ==="
else
    echo "=== ALL GATES PASSED ==="
fi
