# Agent Phase A5b Backend Acceptance — Claude Parser/Event/Status Core

Reviewed commit: `0c7c5f47b`

Verdict: **ACCEPT BACKEND CORE / DEFER DEGRADED AND MOBILE FOLLOW-UP**

This commit resolves the remaining backend contract gaps from the previous A5b reviews:

- Claude parser events reach `/api/sessions.Events` through the production telemetry path.
- The product boundary asserts the common event types:
  - `user_message`
  - `thinking`
  - `tool_call_started`
  - `approval_requested`
- `SessionTelemetry.AgentStatus` now exposes `waiting_approval` for approval state.
- The production resolver path preserves the caller context.
- The unused `normalizeEvents` helper was removed.

This is sufficient to accept the **backend core** of Phase A5b.

The original A5b plan also included degraded parser boundary behavior and mobile UX rendering verification. Those are not implemented in this commit. They must remain explicit follow-up work before claiming the broader Agent Adapter UX slice is complete.

## Verification performed

Commands run:

```bash
git diff --check 7ed5e12..HEAD
gofmt -l companion-daemon/internal/term/claude_boundary_test.go companion-daemon/internal/term/telemetry.go companion-daemon/internal/term/telemetry_service.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b13-go-cache go test ./internal/term -run 'TestClaudeLog_ProductionEventsPath|TestEvaluateState|TestClaudeParser|TestTelemetry' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b13-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b13-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b13-go-cache go test ./...
cd mobile && node_modules/.bin/tsc --noEmit
```

Notes:

- `git diff --check` passed.
- `gofmt -l` returned no files.
- Targeted Claude boundary/parser/state tests passed.
- `internal/agent` contract tests passed.
- `go vet ./...` passed.
- Mobile TypeScript compile passed.
- Sandboxed full `go test ./...` failed because `httptest` could not bind a local port.
- Authorized full `go test ./...` passed.

## Accepted backend proof

### 1. Common event types reach `/api/sessions`

`TestClaudeLog_ProductionEventsPath` now asserts all expected event types:

```go
[]string{
    "user_message",
    "thinking",
    "tool_call_started",
    "approval_requested",
}
```

The path exercised is production-shaped:

```text
TelemetryService.Run
→ processSession
→ resolveLog(ctx, ProcessInfo)
→ ReadNewEvents
→ ClaudeParser
→ EventStore.Append
→ TelemetryService.Snapshot
→ /api/sessions.Events
```

### 2. Approval maps to common `agentStatus`

The boundary test now asserts:

```go
if s.AgentStatus != "waiting_approval" {
    t.Errorf("AgentStatus=%q, want waiting_approval", s.AgentStatus)
}
```

The implementation maps the legacy terminal state:

```go
state=waiting
```

to the common Agent Adapter status:

```text
waiting_approval
```

via `mapLegacyState`.

### 3. Resolver context propagation is restored

The production resolver path now keeps the sampling context:

```go
logRef, logErr = s.resolveLog(ctx, info)
...
return ResolveAgentLog(ctx, p)
```

This fixes the previous `context.Background()` regression in the resolver path.

### 4. Dead normalization helper was removed

The unused `normalizeEvents` helper is gone. Event normalization now happens at parser output, where it belongs for this backend path.

## Remaining follow-up work

### 1. Degraded parser boundary

Still missing:

```text
malformed Claude log
→ parser degraded/failure metadata
→ terminal session remains listed
→ API/mobile distinguishes degraded from no activity
```

The `internal/agent` contract tests cover degraded parser behavior inside the agent package, but the telemetry/API/mobile product boundary does not yet expose degraded status or diagnostics.

### 2. Mobile UX rendering

Mobile TypeScript compile passes, but no mobile schema/rendering test was added for:

- `agentStatus=waiting_approval`
- `approval_requested`
- degraded/unknown states
- no Claude-name behavior branch in agent UX

The mobile side may already tolerate the fields, but A5b did not add proof of product UX behavior.

## Decision

`0c7c5f47b` is accepted for the backend Claude parser/event/status core of Phase A5b.

Do not treat degraded parser UX or mobile Agent Adapter UX as complete. They should be tracked as explicit follow-up work before the broader Agent Adapter Layer is called product-complete.

Phase A6 can proceed for backend parser expansion only if this scope split is kept explicit: A6 should reuse the accepted common backend event/status path and must not erase the remaining degraded/mobile follow-up obligations.
