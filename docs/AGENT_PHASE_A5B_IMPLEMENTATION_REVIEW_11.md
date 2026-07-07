# Agent Phase A5b Implementation Review 11 — Approval Event and Waiting State

Reviewed commit: `f3a024466`

Verdict: **REJECT FOR A5b / ACCEPTABLE APPROVAL-STATE PROGRESS**

This commit addresses two concrete issues from the previous review:

1. The `/api/sessions` production path test now explicitly asserts `approval_requested`.
2. `evaluateState` now understands common event type names and maps `approval_requested` to terminal telemetry `state=waiting`.

The targeted boundary test confirms:

```text
events=4, agentKind=claude, state=waiting
```

That is real progress. However, Phase A5b is still not complete because parser-derived `agentStatus=waiting_approval`, degraded parser behavior, resolver context propagation, and mobile UX verification remain unresolved.

## Verification performed

Commands run:

```bash
git diff --check 5da67df..HEAD
gofmt -l companion-daemon/internal/term/claude_boundary_test.go companion-daemon/internal/term/telemetry.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b11-go-cache go test ./internal/term -run 'TestClaudeLog_ProductionEventsPath|TestEvaluateState|TestTelemetry' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b11-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b11-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b11-go-cache go test ./...
```

Notes:

- `git diff --check` passed.
- `gofmt -l` returned no files.
- Targeted boundary/state tests passed.
- `internal/agent` contract tests passed.
- `go vet ./...` passed.
- Sandboxed full `go test ./...` failed because `httptest` could not bind a local port.
- Authorized full `go test ./...` passed.

## What improved

### 1. `approval_requested` is now asserted at the session API boundary

The production path test now requires:

```go
[]string{"user_message", "thinking", "tool_call_started", "approval_requested"}
```

This closes the previous gap where the parser emitted approval but the product boundary did not prove it.

### 2. Common event names are now understood by `evaluateState`

`evaluateState` now accepts both legacy and common names:

```go
case "user", "user_message":
case "tool_use", "tool_call_started":
case "tool_result", "tool_call_finished":
case "thinking":
case "approval_requested":
case "message", "assistant_message":
```

This fixes the mismatch where the parser returned common names but the state machine still switched only on legacy names.

### 3. Approval now maps to terminal telemetry `state=waiting`

`approval_requested` now sets:

```go
stateData.State = "waiting"
stateData.Load = 0
```

The boundary test log confirms the API session reaches:

```text
state=waiting
```

## Why A5b is still rejected

### Blocking issue 1 — `agentStatus=waiting_approval` is still not connected

A5b is about the Agent Adapter Layer product contract, not only the older terminal `state` field.

`SessionTelemetry.AgentStatus` is still set by detector output:

```go
kind, status, confidence := s.detector.DetectAgent(...)
st.AgentStatus = status
```

There is still no proof that parsed approval sets:

```text
agentStatus = waiting_approval
```

`state=waiting` is useful, but it is not the same contract as parser-derived `agentStatus=waiting_approval`.

### Blocking issue 2 — degraded parser behavior is still missing at the product boundary

There is still no test for:

```text
malformed Claude log
→ parser degraded/failure metadata
→ terminal session remains listed
→ API/mobile distinguishes degraded from no activity
```

The `internal/agent` package has degraded parser contract coverage, but this telemetry/API path does not expose degraded status or diagnostics.

### Blocking issue 3 — production resolver cancellation regression remains

The default production resolver still uses:

```go
return ResolveAgentLog(context.Background(), p)
```

This drops the sampling context. The resolver should preserve the caller context.

### Blocking issue 4 — mobile UX remains unverified

No mobile code or mobile verification changed. A5b still requires:

- common status/event rendering;
- no Claude-name behavior branch;
- status badge/schema behavior for waiting/degraded/unknown states.

### Blocking issue 5 — `normalizeEvents` dead helper remains

`normalizeEvents` is still defined but not called. This is not a hard blocker by itself, but it should be cleaned up before claiming completion.

## Required next step

Do not proceed to A6 yet.

Minimum acceptable next commit:

1. Connect parser-derived approval to `SessionTelemetry.AgentStatus = "waiting_approval"`.
2. Add an assertion for `AgentStatus == "waiting_approval"` in the production boundary test.
3. Add degraded parser/failure isolation at the telemetry/API boundary.
4. Fix resolver context propagation.
5. Add mobile schema/rendering/name-branch verification, or explicitly split mobile into a documented remaining A5b sub-step before claiming A5b completion.
6. Remove the unused `normalizeEvents` helper.

## Final decision

`f3a024466` is accepted as approval-event and waiting-state progress.

It is rejected as Phase A5b completion.

A6 remains blocked.
