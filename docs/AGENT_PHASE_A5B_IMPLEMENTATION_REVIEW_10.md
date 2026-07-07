# Agent Phase A5b Implementation Review 10 — Parser-side Canonical Types and Approval Event

Reviewed commit: `86cf00b2d`

Verdict: **REJECT FOR A5b / ACCEPTABLE PARSER PROGRESS**

This commit moves Claude event normalization closer to the source. Instead of only mapping legacy event names at `/api/sessions` snapshot time, `term.ClaudeParser` now normalizes returned events before they enter `EventStore`.

It also adds Claude `permission-mode: ask` handling as:

```text
approval_requested
```

This is meaningful progress. The valid-log production path now emits common event names for:

- `user_message`
- `thinking`
- `tool_call_started`
- `approval_requested`

However, Phase A5b is still not complete. The product boundary still does not prove parser-derived `agentStatus=waiting_approval`, degraded parser behavior, mobile rendering, or production resolver cancellation preservation.

## Verification performed

Commands run:

```bash
git diff --check cccc000..HEAD
gofmt -l companion-daemon/internal/term/claude_boundary_test.go companion-daemon/internal/term/claude_parser.go companion-daemon/internal/term/parser_test.go companion-daemon/internal/term/telemetry.go companion-daemon/internal/term/telemetry_service.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b10-go-cache go test ./internal/term -run 'TestClaudeLog_ProductionEventsPath|TestClaudeParser|TestTelemetry|TestEvaluate|Test.*State' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b10-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b10-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b10-go-cache go test ./internal/term -count=1
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b10-go-cache go test ./...
```

Notes:

- `git diff --check` passed.
- `gofmt -l` returned no files.
- Targeted Claude parser/boundary/state tests passed.
- `internal/agent` contract tests passed.
- `go vet ./...` passed.
- Sandboxed full tests failed because `httptest` could not bind a local port.
- Authorized `internal/term` and full `go test ./...` passed.

## What improved

### 1. Canonical event types are now produced by the Claude parser path

`term.ClaudeParser` now calls:

```go
normalizeEventType(&events[i])
```

before returning parsed events. That means the events stored by the normal telemetry path are already closer to the common contract.

### 2. Thinking is preserved as `thinking`

The previous commit collapsed all legacy `message` events into `assistant_message`.

This commit uses the summary hint to preserve thinking events:

```go
if containsAny(e.Summary, "Thinking", "Reasoning") {
    e.Type = "thinking"
}
```

The parser test now asserts:

```go
events[2].Type == "thinking"
```

### 3. Approval event generation was added

Claude `permission-mode` with `permissionMode: ask` now emits:

```text
approval_requested
```

The production boundary test logs `events=4`, which confirms an additional event is present compared with the previous 3-event path.

## Why A5b is still rejected

### Blocking issue 1 — `/api/sessions` still does not assert `approval_requested`

The boundary test fixture includes the approval log line, and the parser now emits an approval event, but the test only asserts:

```go
[]string{"user_message", "thinking", "tool_call_started"}
```

It does not assert:

```text
approval_requested
```

Since approval is a core A5b acceptance point, this must be explicitly proven at the product boundary.

### Blocking issue 2 — parser-derived `agentStatus=waiting_approval` is still not connected

`SessionTelemetry.AgentStatus` is still populated by detection:

```go
kind, status, confidence := s.detector.DetectAgent(...)
st.AgentStatus = status
```

The parsed `approval_requested` event does not set:

```text
agentStatus = waiting_approval
```

The boundary test logs:

```text
events=4, agentKind=claude, state=working
```

and does not assert `AgentStatus`.

This remains the main A5b blocker.

### Blocking issue 3 — state machine still uses legacy event names

`evaluateState` still switches on legacy event names:

```go
case "user":
case "tool_use":
case "tool_result":
case "message":
```

But `ClaudeParser` now returns common event names:

```text
user_message
thinking
tool_call_started
approval_requested
```

The current boundary test still ends in `state=working` because unknown/default events fall through to working. That hides the mismatch.

The state machine should either understand common event types or status should be derived from a separate parser result/status path.

### Blocking issue 4 — degraded parser behavior is still missing

There is still no product-boundary test for:

```text
malformed Claude log
→ parser degraded/failure metadata
→ terminal session remains listed
→ API/mobile distinguishes degraded from no activity
```

The existing `internal/agent` parser contract covers degraded parser behavior in that package, but the telemetry/product boundary does not expose it.

### Blocking issue 5 — production resolver cancellation regression remains

The previous reviews called out:

```go
return ResolveAgentLog(context.Background(), p)
```

This still drops the sampling context. The default production resolver path should preserve `ctx`.

### Blocking issue 6 — mobile UX remains unverified

No mobile code or mobile verification changed. A5b still requires:

- status badge/schema/rendering verification;
- no Claude-name behavior branch in mobile behavior;
- rendering based on common status/event/capability.

### Blocking issue 7 — transitional helper is now dead code

`normalizeEvents` remains defined but is no longer called. This is not an acceptance blocker by itself, but it indicates the transition from snapshot-side normalization to parser-side normalization was not fully cleaned up.

## Required next step

Do not proceed to A6 yet.

Minimum acceptable next commit:

1. Assert `approval_requested` in the `/api/sessions` production path test.
2. Connect parsed approval to `SessionTelemetry.AgentStatus = "waiting_approval"`.
3. Update state/status derivation to understand common event types, or introduce an explicit parser-result status path.
4. Fix resolver context propagation.
5. Add malformed-log/degraded product-boundary coverage.
6. Add mobile verification or explicitly split mobile into a documented remaining A5b sub-step before claiming A5b completion.
7. Remove or use the now-unused `normalizeEvents` helper.

## Final decision

`86cf00b2d` is accepted as parser-side canonicalization and approval-event progress.

It is rejected as Phase A5b completion.

A6 remains blocked.
