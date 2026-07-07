# Agent Phase A5b Implementation Review 9 — Snapshot-only Type Normalization

Reviewed commit: `a98e802ec`

Verdict: **REJECT FOR A5b / ACCEPTABLE PARTIAL NORMALIZATION**

This commit maps some legacy `term.ClaudeParser` event strings to common-looking Agent Adapter event strings at the `/api/sessions` snapshot boundary:

```text
user       → user_message
tool_use   → tool_call_started
tool_result → tool_call_finished
message    → assistant_message
```

That is a small improvement over the previous commit because the main session API now exposes two common event names in the A5b proof test:

- `user_message`
- `tool_call_started`

However, this is still not Phase A5b completion. It is a snapshot-only string normalization layer over the legacy parser path. Approval, parser-derived status, degraded behavior, mobile UX, and resolver cancellation remain unresolved.

## Verification performed

Commands run:

```bash
git diff --check 70626e6..HEAD
gofmt -l companion-daemon/internal/term/telemetry.go companion-daemon/internal/term/telemetry_service.go companion-daemon/internal/term/claude_boundary_test.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b9-go-cache go test ./internal/term -run 'TestClaudeLog_ProductionEventsPath|TestClaudeDetection' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b9-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b9-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b9-go-cache go test ./...
```

Notes:

- `git diff --check` passed.
- `gofmt -l` returned no files.
- Targeted A5b daemon tests passed.
- `internal/agent` contract tests passed.
- `go vet ./...` passed.
- Sandboxed `go test ./...` failed because `httptest` could not bind a local port.
- Authorized `go test ./...` passed.

## What improved

### 1. `/api/sessions` now exposes two common event names

The A5b proof test now expects:

```go
[]string{"user_message", "tool_call_started"}
```

instead of:

```go
[]string{"user", "tool_use"}
```

This is directionally correct for the session snapshot boundary.

### 2. EventStore data is copied before normalization

`memoryEventStore.List` returns a copy, so `normalizeEvents` does not mutate the in-memory store for that implementation.

That avoids one obvious side effect in the default store.

## Why A5b is still rejected

### Blocking issue 1 — `approval_requested` is still missing

The A5b plan requires approval to cross the product boundary as:

```text
approval_requested
```

The test fixture still includes:

```json
{"type":"permission-mode","permissionMode":"ask","sessionId":"<UUID>"}
```

But the term parser does not emit an approval event, and the test does not assert `approval_requested`.

This remains a hard blocker because approval is one of the main user-visible Agent Adapter capabilities.

### Blocking issue 2 — parser-derived `agentStatus` is still not connected

The implementation still populates `agentStatus` from detection:

```go
kind, status, confidence := s.detector.DetectAgent(...)
st.AgentStatus = status
```

There is no proof that parser-derived status reaches the API, especially:

```text
waiting_approval
```

The test logs only:

```text
events=3, agentKind=claude, state=working
```

and does not assert `AgentStatus`.

### Blocking issue 3 — normalization happens only at some read boundaries

`TelemetryService.Snapshot` and `buildSimpleSnapshot` call:

```go
normalizeEvents(...)
```

But `HandleSessionsV2` history mode still returns raw `h.Events.List(historyID)` events without normalization.

That means the same stored event can appear with different type names depending on endpoint/path:

```text
/api/sessions          → user_message
/api/sessions?history= → user
```

The common event contract should be consistent at the product boundary.

### Blocking issue 4 — this is still not the common `internal/agent` parser path

The Agent Adapter Layer already has common models and parser contracts in `internal/agent`, including:

```go
EventUserMessage
EventToolCallStarted
EventApprovalRequested
StatusWaitingApproval
StatusDegraded
```

This commit does not bridge the `internal/agent` parser result into telemetry. It maps legacy strings after parsing.

That may be acceptable as a short transitional step, but it is not enough to claim A5b completion.

### Blocking issue 5 — thinking is normalized as `assistant_message`

Legacy `term.ClaudeParser` uses `Type: "message"` for both normal assistant text and thinking content.

The new mapper converts all `message` events to:

```text
assistant_message
```

So Claude thinking is not represented as the common `thinking` event type. That weakens status/UX fidelity.

### Blocking issue 6 — degraded parser behavior is still missing

There is still no product-boundary proof for:

```text
malformed log
→ parser degraded diagnostic
→ terminal session remains listed
→ API/mobile distinguishes degraded from no activity
```

### Blocking issue 7 — resolver cancellation regression remains

The previous review called out that production resolver context propagation regressed:

```go
return ResolveAgentLog(context.Background(), p)
```

This commit does not fix it. The production path should preserve the sampling context.

### Blocking issue 8 — mobile UX remains unverified

No mobile code or mobile verification changed. A5b still requires:

- status badge/schema/rendering verification;
- no `if agent == "claude"` behavior branch;
- capability/status/event-driven rendering.

## Required next step

Do not proceed to A6 yet.

Minimum acceptable next commit:

1. Fix resolver context propagation.
2. Make the event contract consistent across `/api/sessions` and history paths.
3. Emit or bridge `approval_requested` from Claude logs.
4. Connect parser-derived `waiting_approval` into `SessionTelemetry.AgentStatus`.
5. Preserve `thinking` as `thinking`, not generic `assistant_message`, where the log provides thinking content.
6. Add degraded parser/failure isolation at the product boundary.
7. Add mobile schema/rendering/name-branch verification, or explicitly split mobile into a documented A5b sub-step before claiming completion.

## Final decision

`a98e802ec` is accepted as partial snapshot normalization.

It is rejected as Phase A5b completion.

A6 remains blocked.
