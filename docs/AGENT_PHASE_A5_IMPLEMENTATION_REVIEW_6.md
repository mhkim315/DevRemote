# Phase A5 Implementation Review 6 — Production Telemetry Path Attempt

Date: 2026-07-07
Reviewed commit: `cd4bbae83`
Previous rejection: `6995b2d` (`docs: reject agent phase A5 telemetry bridge`)
Verdict: **REJECT**

## Summary

`cd4bbae83` moves closer to a real product-boundary bridge:

- adds an optional `term.AgentDetector` interface to `Handlers`;
- adds `buildSimpleSnapshotWithDetector`;
- changes the fallback `/api/sessions` path to populate `SessionTelemetry` agent fields
  when a detector is provided;
- adds `TestClaudeOutput_ProductPath`, which calls the actual `/api/sessions` handler
  and verifies `agentKind`, `agentStatus`, and `agentConfidence`.

This is a meaningful improvement over manually constructing `SessionTelemetry`.

However, Phase A5 is still not acceptable. The implementation only wires agent detection
into the fallback path used when `Handlers.Telemetry == nil`. In the real daemon, the
handler uses `TelemetryService.Snapshot(reg)` when telemetry is configured, and that path
does not apply the detector or populate the new agent fields.

## Verified

Commands run:

```bash
git diff --check 6995b2d..cd4bbae
gofmt -l companion-daemon/internal/term/runtime.go companion-daemon/internal/term/telemetry.go companion-daemon/internal/term/claude_boundary_test.go companion-daemon/internal/term/fixture_e2e_test.go
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a5-go-cache go vet ./...
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/term -run TestClaudeOutput_ProductPath -count=1 -v
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/term -count=1 -v
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./...
```

Notes:

- `internal/term` and full tests were run outside the sandbox because existing tests use
  `httptest` local listener binding.
- All checks passed.

Changed files:

```text
companion-daemon/internal/term/claude_boundary_test.go
companion-daemon/internal/term/fixture_e2e_test.go
companion-daemon/internal/term/runtime.go
companion-daemon/internal/term/telemetry.go
```

## Positive findings

### 1. The test now calls the actual `/api/sessions` handler

This is better than the previous manual `SessionTelemetry` marshal test.

### 2. Backward compatibility is covered for nil detector

The test verifies that when `AgentDetector` is nil, the optional agent fields stay empty.

### 3. Existing regression tests pass

The added optional fields and fallback detector path do not break current Go tests.

## Blocking issues

### P0 — The real telemetry branch does not populate agent fields

`HandleSessionsV2` currently does:

```go
if h.Telemetry != nil {
    json.NewEncoder(w).Encode(h.Telemetry.Snapshot(reg))
    return
}

res := buildSimpleSnapshotWithDetector(reg, h.Events, h.AgentDetector)
```

So `AgentDetector` is only used when `Telemetry == nil`.

The production daemon wires a `TelemetryService`. In that production branch,
`TelemetryService.Snapshot(reg)` still builds `SessionTelemetry` without setting:

- `AgentKind`
- `AgentStatus`
- `AgentConfidence`

Therefore the agent fields do not flow through the real telemetry path.

### P0 — `cmd/devremote` does not wire an AgentDetector

`Handlers` now has:

```go
AgentDetector AgentDetector
```

but `cmd/devremote` does not construct or inject any implementation. The test injects a
test-only `claudeDetectorAdapter`; production does not.

That means the real daemon will still expose empty agent fields.

### P0 — Test adapter is not a real Claude adapter production bridge

`TestClaudeOutput_ProductPath` uses a test-only adapter:

```go
type claudeDetectorAdapter struct { ... }
func (d *claudeDetectorAdapter) DetectAgent(...) (string, string, float64) {
    ev := agent.DetectionEvidence{ProcessName: "claude", CWD: "/Users/test/project"}
    id := d.detector.Detect(ev)
    return id.Kind, string(agent.StatusWorking), id.Confidence
}
```

This ignores actual session process evidence, resolver output, parser output, degraded
state, approvals, and events. It returns `StatusWorking` unconditionally.

The test proves only that if a detector is manually injected into the fallback handler,
three scalar fields can appear in JSON. It does not prove the Claude adapter pipeline is
integrated with production telemetry.

### P0 — Parser/events/approvals still do not reach the product boundary

The accepted Phase A5 requires Claude user/assistant/tool/approval events and approval
state to appear through common product behavior.

This commit only surfaces:

- `agentKind`
- `agentStatus`
- `agentConfidence`

It does not convert or expose:

- `agent.AgentEvent`
- `agent.AgentApproval`
- parser `Degraded` / `Diagnostics`
- resolver diagnostics
- approval requests

The existing `SessionTelemetry.Events` still uses `models.AgentEvent` from the older
`internal/term` parser path.

### P0 — Mobile boundary remains unmodified

The mobile `SessionTelemetry` type still does not include the new optional fields, and
there is no mobile-facing schema or rendering test for `agentKind`, `agentStatus`, or
`agentConfidence`.

### P1 — AgentDetector interface is too thin for A5

The new `term.AgentDetector` returns only:

```go
(agentKind string, agentStatus string, agentConfidence float64)
```

It cannot carry:

- events;
- approvals;
- diagnostics;
- degraded state;
- log references;
- source/confidence details.

That interface may be acceptable for a minimal badge, but it is insufficient for the A5
vertical slice described in the plan.

### P1 — Failure isolation is still not tested on the agent path

The current test only proves nil detector omits fields. It does not simulate:

- detector panic/failure;
- resolver degraded result;
- parser malformed input;
- adapter conversion failure;
- telemetry snapshot continuing while agent detection fails.

### P1 — Previous resolver/cursor/approval identity issues remain

Still unresolved:

- Claude resolver synthesizes paths from `ev.CWD` rather than proving A1 log discovery;
- cursor embeds a raw first-line prefix and is collision-prone;
- approval IDs are batch-level, not stable per approval.

## Required next changes

To reach Phase A5 acceptance:

1. Wire agent detection into the real `TelemetryService` path, not only the
   `Telemetry == nil` fallback.
2. Inject a production `AgentDetector` implementation from `cmd/devremote`.
3. Use real session/process evidence rather than hardcoded `ProcessName: "claude"`.
4. Integrate resolver/parser output so that events, approvals, degraded state, and
   diagnostics can reach product boundaries.
5. Add tests for `/api/sessions` with `Telemetry != nil` proving agent fields are
   populated by the actual production path.
6. Add failure-isolation tests where detector/resolver/parser failure does not break
   terminal session telemetry.
7. Update mobile TypeScript schema and add mobile-facing tests or golden checks.
8. Fix resolver discovery, cursor generation, and approval ID stability.

## Current judgment

`cd4bbae83` is a useful step, but it still wires only a fallback/test path. The production
telemetry path remains unconnected to the Agent Adapter Layer.

Do not proceed to Phase A6 until agent output is populated through the real telemetry
path or the plan is explicitly revised to accept a narrower milestone.
