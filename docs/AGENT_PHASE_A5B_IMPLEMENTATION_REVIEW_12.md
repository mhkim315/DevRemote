# Agent Phase A5b Implementation Review 12 — AgentStatus Uses Terminal State

Reviewed commit: `43521a3fd`

Verdict: **REJECT FOR A5b / PARTIAL AGENTSTATUS WIRING**

This commit changes `SessionTelemetry.AgentStatus` to come from the telemetry state machine instead of directly from detector output:

```go
kind, _, confidence := s.detector.DetectAgent(...)
st.AgentKind = kind
st.AgentConfidence = confidence
if sd, ok := stateCopies[st.ID]; ok && sd.State != "" {
    st.AgentStatus = sd.State
}
```

That is directionally closer to parser-derived status. However, it still does not satisfy the Agent Adapter Layer status contract because it exposes terminal state values such as:

```text
waiting
```

instead of common agent status values such as:

```text
waiting_approval
```

The targeted boundary test still only logs:

```text
events=4, agentKind=claude, state=waiting
```

It does not assert `agentStatus`, and the implementation would currently expose `agentStatus=waiting`, not `waiting_approval`.

## Verification performed

Commands run:

```bash
git diff --check 669e262..HEAD
gofmt -l companion-daemon/internal/term/telemetry_service.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b12-go-cache go test ./internal/term -run 'TestClaudeLog_ProductionEventsPath|TestEvaluateState|TestTelemetry' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b12-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b12-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b12-go-cache go test ./...
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

### 1. Detector no longer supplies `agentStatus`

The previous path used detector output for all agent fields:

```go
kind, status, confidence := s.detector.DetectAgent(...)
st.AgentStatus = status
```

This was wrong for A5b because approval/thinking/working status should be parser or telemetry derived, not merely detection derived.

The commit removes that direct detector status assignment.

### 2. `agentStatus` is now based on telemetry state

This is closer to the production path exercised by parsed events:

```text
Claude log
→ ReadNewEvents
→ evaluateState
→ stateData.State
→ SessionTelemetry.AgentStatus
```

## Why A5b is still rejected

### Blocking issue 1 — `agentStatus` values are not common AgentStatus values

A5b requires common statuses such as:

```text
waiting_approval
thinking
working
degraded
```

The telemetry state machine uses older terminal states:

```text
idle
thinking
working
waiting
```

For an approval request, `evaluateState` sets:

```go
stateData.State = "waiting"
```

and this commit copies that to:

```go
st.AgentStatus = sd.State
```

So approval becomes:

```text
agentStatus=waiting
```

not:

```text
agentStatus=waiting_approval
```

That does not meet the common contract.

### Blocking issue 2 — no product-boundary assertion for `agentStatus`

The production path test does not assert:

```go
s.AgentStatus == "waiting_approval"
```

It only asserts event types and detector identity/confidence.

The key A5b contract remains untested.

### Blocking issue 3 — degraded parser behavior remains missing

There is still no telemetry/API boundary proof for:

```text
malformed Claude log
→ parser degraded/failure metadata
→ terminal session remains listed
→ API/mobile distinguishes degraded from no activity
```

### Blocking issue 4 — production resolver cancellation regression remains

The resolver still uses:

```go
return ResolveAgentLog(context.Background(), p)
```

The sampling context is still not preserved.

### Blocking issue 5 — mobile UX remains unverified

No mobile code or mobile verification changed. A5b still requires:

- common status/event rendering;
- no Claude-name behavior branch;
- waiting/degraded/unknown schema behavior.

### Blocking issue 6 — `normalizeEvents` dead helper remains

`normalizeEvents` is still present and unused.

## Required next step

Do not proceed to A6 yet.

Minimum acceptable next commit:

1. Map terminal `state=waiting` caused by `approval_requested` to `agentStatus=waiting_approval`.
2. Add a product-boundary assertion:

   ```go
   if s.AgentStatus != "waiting_approval" { ... }
   ```

3. Preserve resolver context propagation.
4. Add degraded parser/failure isolation at the telemetry/API boundary.
5. Add mobile schema/rendering/name-branch verification, or explicitly split mobile into a documented remaining A5b sub-step before claiming A5b completion.
6. Remove unused `normalizeEvents`.

## Final decision

`43521a3fd` is accepted as partial AgentStatus wiring.

It is rejected as Phase A5b completion.

A6 remains blocked.
