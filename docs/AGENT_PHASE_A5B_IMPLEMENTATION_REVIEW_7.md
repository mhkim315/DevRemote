# Agent Phase A5b Implementation Review 7 — EventStore Boundary Test Only

Reviewed commit: `417f43b89`

Verdict: **REJECT FOR A5b / ACCEPTABLE EVENTSTORE BOUNDARY TEST**

This commit adds a test proving this limited path:

```text
EventStore.Append
→ buildSimpleSnapshot / Snapshot-style session assembly
→ SessionTelemetry.Events
→ /api/sessions
```

That is a useful boundary regression test, but it is not the Phase A5b proof. A5b still requires a production-shaped Claude parser path where real Claude log lines are parsed and then appear at the product boundary.

## Verification performed

Commands run:

```bash
git diff --check 07c25f6..HEAD
gofmt -l companion-daemon/internal/term/claude_boundary_test.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b7-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b7-go-cache go test ./internal/term -run 'TestClaudeDetection|TestEventsPath' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b7-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b7-go-cache go test ./...
```

Notes:

- `git diff --check` passed.
- `gofmt -l` returned no files.
- `internal/agent` parser/detector contract tests passed.
- Targeted `internal/term` Claude boundary tests passed.
- `go vet ./...` passed.
- Sandboxed `go test ./...` failed because `httptest` could not bind a local port.
- Authorized `go test ./...` passed.

## What improved

### 1. The canonical `events` API boundary is now covered

`TestEventsPath_ProductionPipeline` appends events into `EventStore` and then calls `/api/sessions`, asserting that the session contains those events.

This protects the current API boundary from accidentally dropping `SessionTelemetry.Events`.

### 2. The test uses the existing session API

The test does not inspect only the store. It goes through:

```go
h.HandleSessionsAPI(rec, req)
```

That is the right product boundary for this layer.

## Why A5b is still rejected

### Blocking issue 1 — parser path is bypassed

The new test directly calls:

```go
events.Append("tmux:claude-session", []models.AgentEvent{...})
```

The test comment says this simulates what `processSession` does. That is the gap. A5b needs to prove what `processSession` actually does:

```text
ProcessInfo
→ ResolveAgentLog
→ ReadNewEvents
→ EventStore.Append
→ Snapshot().Events
→ /api/sessions
```

Direct `EventStore.Append` proves only the final storage/API segment.

### Blocking issue 2 — events are synthetic, not Claude parser output

The test events are manually constructed as:

```go
Type: "user"
Type: "tool_use"
Type: "message"
```

A5b needs parser-normalized Claude event types, for example:

- `user_message`;
- `assistant_message` or equivalent thinking/output event;
- `tool_call_started`;
- `approval_requested`.

This matters because parser normalization is the adapter-layer value being validated.

### Blocking issue 3 — parser-derived status is still not proven at the product boundary

The existing positive detection test logs:

```text
AgentKind=claude Status=working Confidence=0.70 Events=0
```

That confirms detection, not parser-derived state. A5b still needs a proof for parser-derived states such as:

- `waiting_approval`;
- `thinking`;
- `degraded`;
- `failed` or malformed-log isolation where applicable.

### Blocking issue 4 — the new test can silently pass if the session is missing

`TestEventsPath_ProductionPipeline` loops over sessions and asserts fields only inside:

```go
if s.ID == "tmux:claude-session" {
    ...
}
```

There is no final `found` assertion. If the session is not returned, the test can pass without proving anything.

That should be fixed even if this remains only a boundary regression test.

## Required next step

Do not add another direct-append test and call it A5b. The next implementation should add a production-shaped test.

Minimum acceptable backend proof:

1. Write a temp redacted Claude log fixture.
2. Drive the same parser path used by telemetry:
   - `ResolveAgentLog` directly, or a narrowly injectable production-shaped resolver;
   - `ReadNewEvents`;
   - `EventStore.Append`;
   - `TelemetryService.Snapshot` or `/api/sessions`.
3. Assert the product boundary contains parser-normalized events:
   - `user_message`;
   - `tool_call_started`;
   - `approval_requested`.
4. Assert the parser-derived status, at least `waiting_approval`, reaches `agentStatus`, or explicitly split this as the next A5b sub-step before claiming A5b completion.
5. Add malformed/unreadable log coverage showing parser failure becomes degraded metadata and does not fail terminal session listing.
6. Fix `TestEventsPath_ProductionPipeline` to fail when `tmux:claude-session` is not found.

## Final decision

`417f43b89` is acceptable as an EventStore-to-API regression test, subject to adding the missing `found` assertion.

It is rejected as Phase A5b completion.

A6 remains blocked until the real Claude parser production path is proven at `/api/sessions.Events` and the parser-derived status/degraded behavior is either implemented or explicitly split into a documented A5b sub-step.
