# Agent Adapter Phase A3 Review 3

Date: 2026-07-07

Executor commit under review: `68b93f550`

Verifier decision: **REJECT**

## Summary

This commit improves the A3 contract harness:

- introduces `ParseResult`;
- carries events, cursor, status, approvals, degraded flag, and diagnostics;
- validates metadata-driven `expectedEvents`;
- adds status baseline and approval detection checks;
- makes cursor resume mismatch fail;
- keeps the harness running across Claude, Codex, and Antigravity fixtures;
- `gofmt`, `go test ./internal/agent -v`, and `go vet ./internal/agent` pass.

However, Phase A3 remains blocked because the contract still does not strictly enforce
duplicate prevention, degraded diagnostics, or meaningful status/approval semantics.

## Verification performed

```sh
git diff --check HEAD~1..HEAD
gofmt -l companion-daemon/internal/agent/parser.go companion-daemon/internal/agent/parser_contract_test.go
```

Result: passed with no output.

```sh
cd companion-daemon
GOCACHE=/tmp/devremote-agent-a3-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a3-go-cache go vet ./internal/agent
```

Result: passed.

## Findings

### P1 — Duplicate prevention still allows full duplicate replay

File: `companion-daemon/internal/agent/parser_contract_test.go`

Current test:

```go
r1 := p.ParseBatch(lines, "")
r2 := p.ParseBatch(lines, r1.Cursor)
if len(r2.Events) > len(r1.Events) {
    t.Errorf(...)
}
```

This allows the parser to return the same number of events on the second parse. In
practice, the mock parser does exactly that because it does not skip already-consumed
records when the same lines are replayed with the returned cursor.

Why this blocks:

- Phase A3 explicitly requires duplicate event prevention.
- Polling parsers will frequently re-read overlapping log content.
- A contract that permits full duplicate replay does not protect downstream activity
  feed, telemetry, or mobile UX from duplicate events.

Required fix:

- Define the cursor contract strictly.
- For the exact same `lines` plus returned cursor, require:

```go
len(r2.Events) == 0
```

unless the parser explicitly declares cursor unsupported. If unsupported cursor mode is
allowed, that capability must be represented explicitly and must block incremental
parse claims.

### P1 — `Degraded=true` does not require diagnostics

Files:

- `companion-daemon/internal/agent/parser.go`
- `companion-daemon/internal/agent/parser_contract_test.go`

`ParseResult` documents:

```go
Diagnostics []string // human-readable parse issues (non-empty only when Degraded)
```

The mock parser sets `Degraded=true` on malformed JSON but returns `Diagnostics: nil`.
The contract does not fail.

Why this blocks:

- Phase A3/A10 need degraded parse behavior to be observable.
- Silent degraded parsing recreates the "error hidden as empty success" problem from the
  terminal adapter work.

Required fix:

- If `result.Degraded == true`, require `len(result.Diagnostics) > 0`.
- Also assert valid fixtures do not return diagnostics.

### P1 — Approval status check is still permissive

File: `companion-daemon/internal/agent/parser_contract_test.go`

Current approval status check:

```go
if result.Status != StatusWaitingApproval && result.Status != StatusUnknown {
    t.Logf(...)
}
```

This allows `StatusUnknown` even when an approval object was detected from an
approval-specific fixture.

Why this blocks:

- Phase A8 approval UX depends on a clear common state.
- If an approval fixture produces `AgentApproval` but status remains unknown, the mobile
  status/CTA state can diverge.

Required fix:

- For approval fixture parse results with at least one pending approval, require:

```go
result.Status == StatusWaitingApproval
```

- If resolved-only approval fixtures are later added, define an explicit expected status
  policy for those fixtures.

### P1 — Status baseline only validates enum membership, not transition semantics

File: `companion-daemon/internal/agent/parser_contract_test.go`

`testStatusBaseline` only checks non-empty status and enum membership. It does not
connect status to fixture evidence.

Why this blocks:

- Phase A3 requires status transition.
- A parser can return `StatusUnknown` for every non-empty valid fixture and pass the
  current status test.

Required fix:

- Add metadata-driven expected status, or derive minimum status expectations from
  fixture categories:
  - approval fixture with pending approval → `waiting_approval`
  - tool call/tool result fixture → `working`
  - malformed-only fixture → `unknown` or `degraded`
- At minimum, valid all-fixture parse should not return `StatusUnknown` when expected
  events include working/approval evidence.

### P2 — `AgentDiagnostic` is still only `[]string`

File: `companion-daemon/internal/agent/parser.go`

`Diagnostics []string` is acceptable as an early placeholder, but A3/A10 will likely
need typed diagnostics for source, severity, and code.

This is not the primary blocker if the contract enforces non-empty diagnostics when
degraded. Keep this as a follow-up unless diagnostics need to drive API/mobile behavior
earlier.

## Required executor actions

1. Make duplicate prevention strict: same lines + returned cursor should produce zero
   duplicate events, or explicitly model unsupported cursor capability.
2. Require non-empty diagnostics when `Degraded=true`.
3. Require `StatusWaitingApproval` for pending approval fixture results.
4. Add meaningful status transition assertions instead of only enum membership.
5. Re-run:

```sh
gofmt -l companion-daemon/internal/agent/parser.go companion-daemon/internal/agent/parser_contract_test.go
GOCACHE=/tmp/devremote-agent-a3-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a3-go-cache go vet ./internal/agent
```

## Next phase permission

**BLOCKED**

Do not proceed to Phase A4 until A3 strictly enforces duplicate prevention, degraded
diagnostics, and status/approval semantics.
