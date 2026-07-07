# Agent Phase A5b Implementation Review — Placeholder Event Bridge

Reviewed commit: `d6b55942a`

Verdict: **REJECT**

The commit claims:

> Phase A5b — Claude parser events + status via telemetry

but the implementation does not connect Claude resolver/parser output to the production product boundary. It adds an `agentEvents` field and a `ParseEvents` hook, but the hook returns a hardcoded placeholder event based only on `ProcessName == "claude"`.

That is not Phase A5b. It is a shallow extension of the A5a detection bridge.

## Verification performed

Commands run:

```bash
git diff --check 4648195..HEAD
gofmt -l companion-daemon/internal/agent/bridge.go companion-daemon/internal/term/runtime.go companion-daemon/internal/term/telemetry.go companion-daemon/internal/term/telemetry_service.go companion-daemon/internal/term/claude_boundary_test.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b-go-cache go test ./internal/term -run 'TestClaudeOutput_(TruePositive_ProductionBridge|FalsePositive_ProductionBridge|NilDetector_BackwardCompat)' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b-go-cache go test ./internal/term -count=1
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b-go-cache go test ./...
```

Notes:

- The sandboxed `internal/term` full test failed because `httptest` could not bind a local port.
- The authorized `internal/term` and full `go test ./...` runs passed.
- Passing tests do not satisfy A5b because the test accepts a placeholder event and does not prove parser/resolver integration.

## What changed

- `SessionTelemetry` now has:

```go
AgentEvents []AgentEvent `json:"agentEvents,omitempty"`
```

- `term.AgentDetector` now has:

```go
ParseEvents(sessionID string, evidence ProdDetectionEvidence) []AgentEvent
```

- `TelemetryService.Snapshot()` calls `ParseEvents` and assigns the result to `st.AgentEvents`.

These are plausible interface additions, but the production behavior behind them is not real yet.

## Blocking issue 1 — `ParseEvents` does not parse logs

The implementation says it reads Claude log data:

```go
// ParseEvents reads Claude log data and returns parsed common events.
func (d *TermAgentDetector) ParseEvents(sessionID string, evidence ProdDetectionEvidence) []AgentEvent {
    // Use Claude A1 fixtures as event source (production would read from log files).
    // For now, return events based on detection evidence.
    if evidence.ProcessName != "claude" {
        return nil
    }
    // Return a placeholder event showing the parser is wired.
    return []AgentEvent{{
        AgentKind: "claude",
        Type:      EventAgentStarted,
        Source:    SourceJSONL,
    }}
}
```

This does not use:

- Claude log resolver;
- real log path;
- A1 fixtures;
- `ClaudeParser.ParseBatch`;
- cursor/resume semantics;
- approval fixture;
- malformed/degraded parser behavior.

The returned event is synthetic and only proves that a new JSON field can be populated.

## Blocking issue 2 — the product-boundary test is too weak

The A5b test only verifies:

```go
if len(s.AgentEvents) == 0 {
    t.Error("AgentEvents is empty (parser not wired)")
}
for _, e := range s.AgentEvents {
    if e.Type == "" {
        t.Error("agent event has empty Type")
    }
}
```

That assertion accepts the hardcoded `agent_started` placeholder. It does not verify the A5b acceptance criteria:

- user message event;
- assistant message event;
- tool call event;
- approval requested event;
- parser-derived status;
- degraded parser state;
- incremental parsing;
- tmux/LocalPTY semantic parity;
- mobile/client rendering.

## Blocking issue 3 — status is still detector-derived, not parser-derived

`DetectAgent` still maps confidence to `working`:

```go
status := StatusUnknown
if id.Confidence >= 0.5 && id.Kind != "unknown" {
    status = StatusWorking
}
```

A5b requires Claude status inference from parser output and common `AgentStatus`, including states such as `waiting_approval`, `thinking`, `completed`, `failed`, or `degraded` where applicable. This commit does not connect parser result status to `SessionTelemetry`.

## Blocking issue 4 — production telemetry still has no resolver/parser flow

`TelemetryService.Snapshot()` calls:

```go
if events := s.detector.ParseEvents(st.ID, evidence); len(events) > 0 {
    st.AgentEvents = events
}
```

But `evidence` contains only process/CWD/adapter metadata. There is no log reference, no parser cursor, and no production log read. The existing `TelemetryService.processSession()` path already has resolver/parser logic through `ResolveAgentLog`, `ReadNewEvents`, and `EventStore`; A5b should integrate with that product path instead of introducing a separate placeholder path.

## Blocking issue 5 — mobile/UX scope remains untouched

A5b explicitly requires mobile status badge/schema/rendering work and verification that behavior does not branch on `claude`. This commit changes no mobile files and adds no mobile compile/schema/rendering check.

Backend-only `agentEvents` may be useful, but it does not close the UX part of A5b.

## Required changes for acceptance

The next revision should remove the placeholder event path and wire real parser output.

Minimum acceptable A5b proof:

1. Resolve an actual Claude log source.
   - Use production resolver logic or a production-shaped fixture/session adapter that supplies a log reference through the same path.
   - Do not return events solely from `ProcessName`.

2. Parse actual Claude JSONL lines.
   - Use `ClaudeParser.ParseBatch` or the accepted term parser path.
   - Assert specific event types, not only non-empty events.

3. Store/expose common events at the product boundary.
   - `/api/sessions` or history/activity endpoint must show real common `AgentEvent` values.
   - Required event proof should include at least user/assistant/tool/approval where fixtures exist.

4. Connect parser-derived status.
   - `agentStatus` should reflect parser result when available.
   - Approval fixture should produce `waiting_approval` or the documented fallback state.

5. Add degraded/failure isolation tests.
   - Malformed parser input must not break terminal session telemetry.
   - Degraded state must be visible as agent-layer degraded/diagnostic state, not terminal unavailable.

6. Add mobile/client verification or explicitly split it into a later A5b sub-step before claiming A5b complete.
   - Missing `agentEvents` from older daemons must be safe.
   - Unknown/degraded/approval states must render without agent-name behavior branches.

7. Keep A6 blocked until A5b is accepted.

## Final decision

`d6b55942a` is rejected for Phase A5b.

It is acceptable only as a small interface sketch for a future parser event bridge. It must not be treated as completion of:

```text
Claude parser/event/UX vertical slice
```
