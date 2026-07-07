# Agent Phase A5 Scoped Acceptance — Production Detection Bridge

Reviewed commit: `c15659568`

Verdict: **SCOPED ACCEPT**

This commit satisfies the narrowed Phase A5 product-boundary bridge acceptance documented in `docs/AGENT_PHASE_A5_SCOPE.md`:

> production-like process evidence → production `agent.NewTermAgentDetector()` → `TelemetryService` → `/api/sessions` → `SessionTelemetry` agent fields

It does **not** complete the broader original Phase A5 vertical slice from `docs/AGENT_ADAPTER_LAYER_PLAN.md`, which still includes parser-derived events, approval UX, parser degraded state, mobile status badge work, and tmux/LocalPTY semantic parity. Those items must remain explicit follow-up work unless the master plan is amended.

## Verification performed

Commands run:

```bash
git diff --check 22d0451..HEAD
gofmt -l companion-daemon/internal/term/telemetry_service.go companion-daemon/internal/term/claude_boundary_test.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/term -run 'TestClaudeOutput_(TruePositive_ProductionBridge|FalsePositive_ProductionBridge|NilDetector_BackwardCompat)' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/term -count=1
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./...
```

Notes:

- `internal/term` full test and `go test ./...` require local `httptest` port binding, so the authorized runs were used for final verification.
- All final verification commands passed.

## What is accepted

### 1. Production detector is now used in the product-boundary test

The true-positive boundary test now uses:

```go
detector := agent.NewTermAgentDetector()
```

This removes the previous blocker where a test-only detector injected Claude evidence.

### 2. Production telemetry now builds process evidence

`TelemetryService.Snapshot()` now constructs `ProdDetectionEvidence` from the session when it implements `mux.ProcessProvider`:

```go
evidence := ProdDetectionEvidence{TermAdapter: ref.Adapter}
if pp, ok := sess.(mux.ProcessProvider); ok {
    if info, err := pp.ProcessInfo(context.Background()); err == nil {
        evidence.ProcessName = info.Command
        evidence.CWD = info.CWD
    }
}
kind, status, confidence := s.detector.DetectAgent(st.ID, ref.Adapter, ref.LocalID, evidence)
```

This satisfies the minimum bridge requirement from the previous review:

```text
production-like session process metadata
→ TelemetryService evidence construction
→ agent.NewTermAgentDetector()
→ /api/sessions
→ agentKind=claude, agentStatus=working, agentConfidence>=threshold
```

### 3. True-positive and false-positive paths are both covered

Accepted boundary tests:

- `TestClaudeOutput_TruePositive_ProductionBridge`
  - session implements `ProcessProvider`
  - process command is `claude`
  - `/api/sessions` returns `agentKind=claude`
  - confidence is above threshold

- `TestClaudeOutput_FalsePositive_ProductionBridge`
  - session implements `ProcessProvider`
  - process command is `bash`
  - `/api/sessions` remains `unknown` / low-confidence

- `TestClaudeOutput_NilDetector_BackwardCompat`
  - nil detector keeps legacy behavior
  - agent fields remain omitted/empty

### 4. The implementation preserves backward compatibility

The detector remains optional. If the daemon is not configured with agent detection, the existing telemetry response shape remains compatible because the new fields are `omitempty`.

## Boundaries not accepted as complete

This acceptance is intentionally scoped. The following original A5 items are **not** completed by `c15659568`:

- Claude user/assistant/tool/approval events shown as common `AgentEvent`s at the product boundary.
- Claude approval request shown through common Approval UX.
- Parser degraded state shown at the product/mobile boundary.
- Mobile status badge implementation and TypeScript verification.
- tmux + Claude and LocalPTY + Claude semantic parity.
- Incremental parser output flowing through `/api/sessions` or activity/history UI.

These are not regressions in this commit; they are outside the narrowed `AGENT_PHASE_A5_SCOPE.md` bridge acceptance. However, they are still present in the master `AGENT_ADAPTER_LAYER_PLAN.md` Phase A5 description. The next planning step must reconcile this scope split.

## Follow-up requirements

Before marking the broader Agent Adapter Layer Phase A5 fully complete, do one of the following:

1. Amend the master plan to explicitly split A5 into:
   - A5a: production detection bridge — accepted here;
   - A5b: parser/events/approval/mobile vertical slice.

2. Or keep the original A5 definition and treat this commit as partial acceptance only.

Additional engineering follow-ups:

- Avoid unbounded `ProcessInfo(context.Background())` calls in request-time `Snapshot()`.
  - Prefer using the telemetry loop's existing process snapshot data, a cached per-session evidence value, or a short timeout context.
  - This is not a blocker for scoped acceptance, but it should be fixed before relying on this path under production latency.

- Add mobile/client schema verification if the new fields are intended for immediate client consumption.

- Add explicit unknown-agent rendering tests before exposing this broadly in UX.

## Final scoped decision

`c15659568` is accepted for:

```text
Phase A5 scoped production detection bridge
```

It is not accepted as:

```text
full original Claude Agent Adapter vertical slice
```

The next executor handoff should use that distinction explicitly.
