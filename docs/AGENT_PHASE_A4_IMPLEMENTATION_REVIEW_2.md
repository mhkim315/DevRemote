# Agent Adapter Phase A4 Review 2

Date: 2026-07-07

Executor commit under review: `8df45651a`

Verifier decision: **REJECT**

## Summary

This commit fixes several previous A4 blockers:

- `LogRef` now distinguishes internal `Path` from safe `DisplayPath`;
- `ResolveResult` carries `Logs`, `Diagnostics`, and `Degraded`;
- `ManualEvidence` exists and has priority in the mock detector;
- Claude and Codex known-agent cases are covered;
- the detector comments now state `Confidence < 0.5 => unknown`;
- `go test ./internal/agent -v` and `go vet ./internal/agent` pass.

However, Phase A4 is still blocked because the contract does not yet enforce the most
important resolver/degraded and path-safety semantics. The comments are better, but the
tests still allow unsafe implementations to pass.

## Verification performed

```sh
git diff --check HEAD~1..HEAD
gofmt -l companion-daemon/internal/agent/detector.go companion-daemon/internal/agent/detector_contract_test.go
```

Result: passed with no output.

```sh
cd companion-daemon
GOCACHE=/tmp/devremote-agent-a4-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a4-go-cache go vet ./internal/agent
GOCACHE=/tmp/devremote-agent-a4-go-cache go test ./...
```

Result: passed.

Note: as in prior phases, the first sandboxed full `go test ./...` run required local
listener permission because existing tests use `httptest`.

## Findings

### P1 — Resolver degraded semantics are documented but not enforced

File: `companion-daemon/internal/agent/detector_contract_test.go`

Current test:

```go
result, err := r.Resolve(DetectionEvidence{
    CWD: "/root",
    ProcessName: "claude",
})
if err != nil {
    t.Errorf(...)
}
// Resolver may or may not set Degraded depending on implementation.
// The contract is: no error, Degraded flag conveys issues.
_ = result.Degraded
```

This does not enforce the contract. A resolver can return:

```go
ResolveResult{Degraded:false, Diagnostics:nil}, nil
```

for permission-denied-like evidence and still pass.

Why this blocks Phase A4:

- The approved A4 requirement is to represent permission denied/path failures as
  degraded diagnostics without failing terminal/session semantics.
- If the contract does not require `Degraded=true` and non-empty diagnostics for the
  degraded scenario, future resolvers can silently hide resolver failures as "no logs".

Required fix:

- For the degraded test case, require:

```go
if !result.Degraded { t.Error(...) }
if len(result.Diagnostics) == 0 { t.Error(...) }
```

- Also require `err == nil` for this recoverable degraded case.

### P1 — Safe DisplayPath boundary is not contract-tested

Files:

- `companion-daemon/internal/agent/detector.go`
- `companion-daemon/internal/agent/detector_contract_test.go`

`LogRef` now has:

```go
Path        string // absolute path (internal, never exposed to API/mobile)
DisplayPath string // redacted display path (safe for diagnostics/API)
```

But the contract does not assert that returned log refs have a non-empty safe
`DisplayPath`, nor that `DisplayPath` avoids raw home paths.

Why this blocks Phase A4:

- The field exists, but unsafe resolvers can return `DisplayPath: "/Users/..."` or
  `DisplayPath: ""` and still pass.
- A4 is the correct place to enforce path safety before diagnostics/mobile code consumes
  resolver output.

Required fix:

- In a resolver test that returns logs, assert:

```go
if log.DisplayPath == "" { t.Error(...) }
if strings.Contains(log.DisplayPath, "/Users/") || strings.Contains(log.DisplayPath, "/home/") {
    t.Error(...)
}
```

- Optionally assert that `Path` and `DisplayPath` are not equal when `Path` is absolute.

### P1 — Low-confidence unknown rule is not generally enforced

File: `companion-daemon/internal/agent/detector_contract_test.go`

The test checks some specific cases, but the general invariant still appears only inside
`testDetectKnown`, where it is unreachable after the preceding confidence check:

```go
if id.Confidence < 0.5 {
    t.Errorf(...)
}
if id.Confidence < 0.5 && id.Kind != "unknown" {
    t.Errorf(...)
}
```

Why this matters:

- The central rule is cross-cutting: any detector result with confidence below 0.5 must
  be unknown.
- It should be enforced through a helper applied to all detector test outputs.

Required fix:

- Add helper:

```go
func assertLowConfidenceUnknown(t *testing.T, id AgentIdentity) {
    if id.Confidence < 0.5 && id.Kind != "unknown" { ... }
}
```

- Call it for empty, unknown, known, false-positive, low-confidence, manual, and
  with-logs results.

### P2 — Manual link does not include DisplayName/log display path expectations

File: `companion-daemon/internal/agent/detector_contract_test.go`

Manual override checks kind and confidence. It does not verify display name or safe
manual log path behavior.

Recommended fix:

- For manual link, require non-empty `DisplayName`.
- If `ManualEvidence.LogPath` is used later, require it to pass through the same
  redaction/display boundary as resolver paths.

### P2 — ResolveResult diagnostics are untyped strings

File: `companion-daemon/internal/agent/detector.go`

`Diagnostics []string` is acceptable for A4, but Phase A10 may need typed diagnostics.
This is not a blocker if degraded semantics are enforced now.

## Required executor actions

1. Make the degraded resolver test require `Degraded=true` and non-empty diagnostics.
2. Add contract assertions for non-empty, redacted `DisplayPath`.
3. Add a shared low-confidence invariant helper and apply it to all detector results.
4. Optionally strengthen manual link display/log path expectations.
5. Re-run:

```sh
gofmt -l companion-daemon/internal/agent/detector.go companion-daemon/internal/agent/detector_contract_test.go
GOCACHE=/tmp/devremote-agent-a4-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a4-go-cache go vet ./internal/agent
```

## Next phase permission

**BLOCKED**

Do not proceed to Phase A5 until resolver degraded behavior and path redaction safety
are enforced by the contract, not only documented in comments.
