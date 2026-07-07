# Agent Phase A5b Implementation Review 8 — Legacy Parser Path Proof

Reviewed commit: `0f179e816`

Verdict: **REJECT FOR A5b / ACCEPTABLE BACKEND PROGRESS**

This commit is a real improvement over the previous EventStore-only proof. It now exercises a production-shaped backend path:

```text
TelemetryService.Run
→ collectProcessSnapshots / ProcessInfo
→ injectable log resolver
→ ReadNewEvents
→ term.ClaudeParser
→ EventStore.Append
→ TelemetryService.Snapshot
→ /api/sessions.Events
```

That closes the specific gap where the previous test bypassed parsing and appended events directly.

However, it still does not complete Phase A5b. The path uses the legacy `term.ClaudeParser` event schema, not the Agent Adapter Layer common event/status model defined by the A5b plan. It also does not prove approval, parser-derived `agentStatus`, degraded parser behavior, or mobile UX behavior.

## Verification performed

Commands run:

```bash
git diff --check 43d782c..HEAD
gofmt -l companion-daemon/internal/term/telemetry.go companion-daemon/internal/term/telemetry_service.go companion-daemon/internal/term/claude_boundary_test.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b8-go-cache go test ./internal/term -run 'TestClaudeLog_ProductionEventsPath|TestClaudeDetection' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b8-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b8-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b8-go-cache go test ./...
```

Notes:

- `git diff --check` passed.
- `gofmt -l` returned no files.
- Targeted `internal/term` A5b tests passed.
- `internal/agent` contract tests passed.
- `go vet ./...` passed.
- Sandboxed `go test ./...` failed because `httptest` could not bind a local port.
- Authorized `go test ./...` passed.

## What improved

### 1. Direct EventStore append was removed from the proof

The previous test manually called:

```go
events.Append(...)
```

The new `TestClaudeLog_ProductionEventsPath` writes a temp Claude JSONL log and lets telemetry read it through:

```go
ReadNewEvents(cursor, parser, 500)
s.events.Append(id, newEvents)
```

This is materially better.

### 2. The test now fails if the session is missing

The missing `found` assertion from the previous review was fixed:

```go
if !found {
    t.Fatal("session tmux:claude-session not found in API response")
}
```

### 3. The API boundary is exercised through `/api/sessions`

The test calls:

```go
h.HandleSessionsAPI(rec, req)
```

and checks the returned `SessionTelemetry.Events`. That is the right backend product boundary.

## Why A5b is still rejected

### Blocking issue 1 — this is the legacy term parser schema, not the Agent Adapter common event schema

The A5b plan requires common Agent Adapter event types such as:

```text
user_message
tool_call_started
approval_requested
```

The new test asserts:

```go
for _, want := range []string{"user", "tool_use"} {
```

Those are legacy `term.ClaudeParser` event strings. They are not the common `internal/agent` event contract introduced by the Agent Adapter Layer:

```go
EventUserMessage       = "user_message"
EventToolCallStarted   = "tool_call_started"
EventApprovalRequested = "approval_requested"
```

So this proves the old telemetry parser path, not the A5b common agent event path.

### Blocking issue 2 — approval is not exposed

The fixture includes:

```json
{"type":"permission-mode","permissionMode":"ask","sessionId":"<UUID>"}
```

But `term.ClaudeParser` does not emit an event for `permission-mode`, and the test only observes 3 events. There is no assertion for:

```text
approval_requested
```

This leaves approval behavior unproven.

### Blocking issue 3 — parser-derived `agentStatus` is not connected

The test logs:

```text
events=3, agentKind=claude, state=working
```

It does not assert `agentStatus`, and the implementation still populates `agentStatus` from detection:

```go
kind, status, confidence := s.detector.DetectAgent(...)
st.AgentStatus = status
```

That means parser-derived states such as:

- `waiting_approval`;
- `thinking`;
- `degraded`;
- `failed`;

are still not proven at the `/api/sessions` boundary.

### Blocking issue 4 — degraded parser behavior is still missing

A5b requires parser failure isolation and degraded product/mobile boundary behavior:

```text
malformed log
→ parser degraded diagnostic
→ terminal session still listed
→ product/mobile boundary distinguishes degraded from no activity
```

This commit only covers the valid-log path.

### Blocking issue 5 — mobile UX is still unverified

The A5b plan explicitly requires:

- mobile status badge/schema/rendering update;
- no mobile behavior branch like `if agent == "claude"`;
- status/event/capability-driven rendering.

This commit changes only daemon code and daemon tests.

### Blocking issue 6 — production resolver cancellation regressed

`processSession` used to pass the sampling context into `ResolveAgentLog`:

```go
ResolveAgentLog(ctx, info)
```

The new wrapper calls:

```go
return ResolveAgentLog(context.Background(), p)
```

That drops cancellation/deadline propagation for the default production resolver path. The injectable resolver is fine as a test seam, but the production path should preserve `ctx`, e.g.:

```go
func (s *TelemetryService) resolveLog(ctx context.Context, p models.ProcessInfo) (LogRef, error)
```

and call `ResolveAgentLog(ctx, p)`.

## Required next step

Do not proceed to A6 yet.

Minimum acceptable next commit:

1. Preserve context propagation in `resolveLog`.
2. Decide the event source of truth:
   - either replace/bridge `term.ClaudeParser` with the common `internal/agent` Claude parser output;
   - or add an explicit mapper from legacy term parser events to common Agent Adapter events.
3. Make `/api/sessions.Events` contain common event types:
   - `user_message`;
   - `tool_call_started`;
   - `approval_requested`.
4. Connect parser-derived status into `SessionTelemetry.AgentStatus`, at least proving `waiting_approval`.
5. Add malformed-log/degraded test proving parser failure does not hide or fail the terminal session.
6. Add or update mobile verification proving common status/event rendering and no Claude-name behavior branch.
7. Avoid a 3-second sleep in the unit test if possible. Since this test is in package `term`, it can set a short telemetry interval before `Run`.

## Final decision

`0f179e816` is accepted as backend progress: it proves the legacy term parser can feed `EventStore` and `/api/sessions.Events` through telemetry.

It is rejected as Phase A5b completion.

A6 remains blocked until common Agent Adapter events, parser-derived status, degraded behavior, and mobile UX are proven at the product boundary.
