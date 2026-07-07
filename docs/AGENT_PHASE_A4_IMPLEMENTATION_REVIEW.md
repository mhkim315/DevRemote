# Agent Adapter Phase A4 Review

Date: 2026-07-07

Executor commit under review: `339f59d83`

Verifier decision: **REJECT**

## Summary

The commit adds the first detector/resolver interfaces and contract tests:

- `companion-daemon/internal/agent/detector.go`
- `companion-daemon/internal/agent/detector_contract_test.go`

The direction is correct:

- detection evidence is separated from parsed events;
- no production terminal/mobile behavior is changed;
- resolver absence returns an empty list rather than hiding a terminal session;
- basic unknown/low-confidence behavior is tested;
- `go test ./internal/agent` and `go vet ./internal/agent` pass.

However, Phase A4 is not accepted yet. The current contract leaves critical detector
semantics unenforced and exposes raw absolute log paths without a redaction/display
boundary.

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

Note: the first sandboxed full `go test ./...` run failed because `httptest` could not
bind local ports under sandbox restrictions. The same command passed when rerun with
local listener permission.

## Findings

### P1 — Low-confidence unknown fallback is documented but not enforced

File: `companion-daemon/internal/agent/detector.go`

The interface comment says:

```go
// Confidence < 0.5 should result in AgentKind="unknown".
```

But the contract never asserts this general rule. `testDetectLowConf` only checks:

```go
if id.Confidence > 0.7 { ... }
```

It does not fail if a detector returns:

```text
Kind="claude", Confidence=0.4
```

Why this blocks Phase A4:

- The A4 plan explicitly says false positive detection should lose to unknown fallback.
- Low-confidence false positives are worse than unknown because they can drive wrong
  UX/status/log resolver behavior.
- The comment is currently a suggestion, not an enforced contract.

Required fix:

- Add a contract assertion:

```go
if id.Confidence < 0.5 && id.Kind != "unknown" {
    t.Errorf("low confidence result must be unknown")
}
```

- Apply it to empty, unknown, ambiguous, and weak evidence test cases.

### P1 — LogRef exposes raw absolute paths without a redaction/display boundary

File: `companion-daemon/internal/agent/detector.go`

Current model:

```go
type LogRef struct {
    Path string // absolute path to the log file
    ...
}
```

Why this blocks Phase A4:

- A1/A2 repeatedly established that raw paths must not leak to diagnostics/mobile.
- A4 is detector/resolver foundation; this is where the boundary should be explicit.
- If `Path` is the only path representation, later doctor/API/mobile code may
  accidentally expose `/Users/...` or project names.

Required fix:

Add an explicit safe display/diagnostic field or contract rule. For example:

```go
type LogRef struct {
    Path        string // internal only; never serialize to mobile/push
    DisplayPath string // redacted/safe diagnostic path
    ...
}
```

or document a clear rule and add tests ensuring resolver diagnostics use redacted paths.

At minimum, comments must state `Path` is internal-only and must not be surfaced to
mobile/push/diagnostics without redaction.

### P1 — Resolver error semantics conflict with the plan's failure isolation

File: `companion-daemon/internal/agent/detector.go`

Current comment:

```go
// Only returns error on unrecoverable conditions (permission denied, etc).
```

Why this blocks:

- Permission denied is usually recoverable for Agent Adapter: terminal should continue,
  agent layer should degrade/diagnose.
- The plan says log permission denied should be shown in diagnostics, and Agent Adapter
  failure must not block terminal session.
- Returning a plain `error` gives no structured way to distinguish:
  - no logs found;
  - permission denied;
  - path missing;
  - stale log;
  - resolver degraded but safe to continue.

Required fix:

Introduce resolver diagnostics/degraded result shape or typed errors. For example:

```go
type ResolveResult struct {
    Logs        []LogRef
    Diagnostics []AgentDiagnostic // or early string diagnostics
    Degraded    bool
}
```

If keeping `([]LogRef, error)`, document and test that permission denied does not imply
terminal/session failure and does not erase other evidence.

### P1 — Manual link priority is in the plan but absent from evidence/contract

Files:

- `docs/AGENT_ADAPTER_LAYER_PLAN.md`
- `companion-daemon/internal/agent/detector.go`
- `companion-daemon/internal/agent/detector_contract_test.go`

The A4 plan includes:

```text
manual link 우선순위 설계
```

Current `DetectionEvidence` has no manual-link field, and the detector contract does
not verify that manual evidence overrides weaker process/log/screen evidence.

Why this blocks:

- Manual link is the escape hatch for ambiguous or wrong auto-detection.
- Without it in the A4 contract, false positives can remain impossible to override
  cleanly.

Required fix:

- Add manual-link evidence to `DetectionEvidence`, e.g.:

```go
ManualAgentKind string
ManualConfidence float64
```

or a small struct.

- Add a contract test where manual link overrides conflicting process/log evidence.

### P2 — Known-agent contract only checks Claude

File: `companion-daemon/internal/agent/detector_contract_test.go`

`testDetectKnown` only checks `ProcessName: "claude"`. Phase A1/A2 fixtures cover
Claude, Codex, and Antigravity.

Recommended fix:

- Add known-agent cases for at least `codex`.
- For Antigravity, if process detection is not known yet, require unknown fallback or
  log/manual evidence instead of pretending process detection is available.

### P2 — Detector result lacks DisplayName fallback requirement

File: `companion-daemon/internal/agent/detector_contract_test.go`

The contract checks `Kind` and `Confidence`, but not `DisplayName`. A4 may not need full
UX polish, but a non-empty display name for confident known detections would reduce
later mobile ambiguity.

Recommended fix:

- For confident known detections, require `DisplayName` non-empty.
- For unknown, allow empty or `"Unknown Agent"` depending on chosen policy.

## Required executor actions

1. Enforce `Confidence < 0.5 => Kind == "unknown"` in the detector contract.
2. Add a redacted/display path boundary for `LogRef`, or document and test that raw
   `Path` is internal-only.
3. Rework resolver error semantics so permission/path failures degrade diagnostics
   without implying terminal/session failure.
4. Add manual-link evidence and priority contract.
5. Expand known-agent contract beyond Claude where evidence is available.
6. Re-run:

```sh
gofmt -l companion-daemon/internal/agent/detector.go companion-daemon/internal/agent/detector_contract_test.go
GOCACHE=/tmp/devremote-agent-a4-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a4-go-cache go vet ./internal/agent
```

## Next phase permission

**BLOCKED**

Do not proceed to Phase A5 until detector/resolver contracts enforce low-confidence
unknown fallback, manual override, safe log path handling, and degraded resolver
semantics.
