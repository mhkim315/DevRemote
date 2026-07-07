# Phase A5 Implementation Review 7 — TelemetryService Detector Path

Date: 2026-07-07
Reviewed commit: `abd6e2493`
Previous rejection: `49e6909` (`docs: reject agent phase A5 fallback path`)
Verdict: **REJECT**

## Summary

`abd6e2493` fixes one important blocker from the previous review:

- `TelemetryService` now accepts an optional `AgentDetector`;
- `TelemetryService.Snapshot` populates `AgentKind`, `AgentStatus`, and
  `AgentConfidence` when that detector is present;
- `TestClaudeOutput_TelemetryPath` verifies the `TelemetryService + HandleSessionsV2`
  path, not just the `Telemetry == nil` fallback.

This is real progress.

However, Phase A5 still cannot be accepted because production app wiring still passes
`nil` as the detector:

```go
telemetry := term.NewTelemetryService(reg, events, links, notifier, nil)
```

So the real daemon does not enable the agent detector path. The test manually provides a
test-only detector, but production does not.

## Verified

Commands run:

```bash
git diff --check 49e6909..abd6e24
gofmt -l companion-daemon/cmd/devremote/app.go companion-daemon/cmd/devremote/app_test.go companion-daemon/internal/term/claude_boundary_test.go companion-daemon/internal/term/fixture_e2e_test.go companion-daemon/internal/term/linkstore_test.go companion-daemon/internal/term/telemetry_service.go companion-daemon/internal/term/telemetry_test.go
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a5-go-cache go vet ./...
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/term -run TestClaudeOutput_TelemetryPath -count=1 -v
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./...
```

Notes:

- Full tests were run outside the sandbox because existing tests use `httptest` local
  listener binding.
- All checks passed.

Changed files:

```text
companion-daemon/cmd/devremote/app.go
companion-daemon/cmd/devremote/app_test.go
companion-daemon/internal/term/claude_boundary_test.go
companion-daemon/internal/term/fixture_e2e_test.go
companion-daemon/internal/term/linkstore_test.go
companion-daemon/internal/term/telemetry_service.go
companion-daemon/internal/term/telemetry_test.go
```

## Positive findings

### 1. The real `TelemetryService.Snapshot` path can now carry agent fields

This is materially better than the previous fallback-only approach.

### 2. The boundary test now exercises `TelemetryService + HandleSessionsV2`

`TestClaudeOutput_TelemetryPath` calls `/api/sessions` with a non-nil telemetry service
and verifies the response includes the agent fields.

### 3. Backward compatibility is preserved when detector is nil

Existing tests pass and nil detector produces empty optional fields.

## Blocking issues

### P0 — Production app wiring still disables the detector

`cmd/devremote/app.go` constructs telemetry with:

```go
telemetry := term.NewTelemetryService(reg, events, links, notifier, nil)
```

Therefore the actual daemon does not use any agent detector. The code path added in
`TelemetryService.Snapshot` remains inactive in production.

For Phase A5 acceptance, production must wire a real detector implementation or a
feature-flagged detector implementation. Passing `nil` means the vertical slice is still
test-only.

### P0 — No production implementation of `term.AgentDetector`

The only non-nil detector implementation is in `claude_boundary_test.go`:

```go
type claudeDetectorAdapter struct{}
func (d *claudeDetectorAdapter) DetectAgent(...) (...) {
    det := agent.NewClaudeDetector()
    id := det.Detect(agent.DetectionEvidence{ProcessName: "claude", CWD: "/Users/test/project"})
    return id.Kind, string(agent.StatusWorking), id.Confidence
}
```

This is test-only and hardcoded. There is still no production adapter that:

- reads actual session/process evidence;
- uses `ClaudeLogResolver`;
- uses `ClaudeParser`;
- handles degraded state;
- returns unknown safely for non-Claude sessions.

### P0 — Parser/events/approvals still do not reach product boundary

Even with `TelemetryService.Snapshot` agent fields, only three scalar fields are exposed:

- `agentKind`
- `agentStatus`
- `agentConfidence`

The Phase A5 plan also requires Claude user/assistant/tool/approval events and approval
state to flow through common product behavior. This commit still does not bridge:

- `agent.AgentEvent`;
- `agent.AgentApproval`;
- parser diagnostics/degraded state;
- resolver diagnostics.

### P1 — Mobile schema remains unupdated

The backend can emit optional fields when a detector is supplied, but mobile
`SessionTelemetry` still does not include:

- `agentKind`;
- `agentStatus`;
- `agentConfidence`.

There is no mobile-facing schema/golden proving safe display or safe ignore behavior for
these fields.

### P1 — Agent status mapping remains thin

The detector interface returns an arbitrary string status. The test returns
`StatusWorking` unconditionally. There is no mapping from actual parser status to the
existing terminal/mobile state model, nor any proof that values like `waiting_approval`
or `degraded` display safely.

## Required next changes

To accept Phase A5:

1. Add a production `term.AgentDetector` implementation, preferably in `internal/agent`
   or a bridging package, that uses accepted detector/resolver/parser contracts.
2. Wire that implementation into `cmd/devremote` with a safe feature flag if necessary.
3. Use real session/process evidence instead of hardcoded `ProcessName: "claude"`.
4. Add tests proving production `NewAppWithDeps` wires a non-nil detector when the flag
   is enabled.
5. Add `/api/sessions` tests through the production app/handler path, not only a
   test-built handler.
6. Bridge parser events, approvals, diagnostics, and degraded state, or explicitly revise
   the A5 acceptance criteria before asking for acceptance.
7. Update mobile TypeScript schema and add a mobile-facing schema/golden test.

## Current judgment

`abd6e2493` resolves the previous fallback-only problem inside `TelemetryService`, but
the actual daemon still passes `nil` and therefore does not enable the vertical slice.

Do not proceed to Phase A6 until production wiring uses a real detector implementation
and the product boundary carries actual Claude adapter output.
