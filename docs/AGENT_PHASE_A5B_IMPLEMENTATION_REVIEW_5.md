# Agent Phase A5b Implementation Review 5 — Duplicate Event Path Removed

Reviewed commit: `9e6e50dea`

Verdict: **REJECT FOR A5b / ACCEPTABLE CLEANUP**

This commit removes the previously rejected duplicate agent-event parsing path:

- no `LogPath()` hook;
- no request-time `ParseEvents` log read;
- no production fixture fallback;
- no parallel `agentEvents` population from `agent.TermAgentDetector`.

That cleanup is correct and improves the codebase. However, it does not complete Phase A5b. It effectively returns the implementation to the A5a detection bridge plus existing term telemetry events.

## Verification performed

Commands run:

```bash
git diff --check dc09cb2..HEAD
gofmt -l companion-daemon/internal/agent/bridge.go companion-daemon/internal/term/claude_boundary_test.go companion-daemon/internal/term/telemetry_service.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b5-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b5-go-cache go test ./internal/term -run 'TestClaudeDetection_(TruePositive|FalsePositive|NilDetector)' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b5-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b5-go-cache go test ./internal/term -count=1
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b5-go-cache go test ./...
```

Notes:

- The sandboxed `internal/term` full test failed because `httptest` could not bind a local port.
- The authorized `internal/term` and full `go test ./...` runs passed.
- Passing tests confirm the cleanup did not break existing behavior, but they do not satisfy A5b acceptance.

## What improved

### 1. The rejected `LogPath()` hook was removed

The previous test-only path:

```go
interface{ LogPath() string }
```

is no longer used by `TelemetryService.Snapshot()`.

### 2. The rejected duplicate parser route was removed

`agent.TermAgentDetector.ParseEvents` now returns nil:

```go
func (d *TermAgentDetector) ParseEvents(sessionID string, evidence ProdDetectionEvidence) []AgentEvent {
    return nil
}
```

The comment says agent events flow through the existing production telemetry path:

```text
TelemetryService.processSession → ResolveAgentLog → ReadNewEvents → EventStore
```

That direction is architecturally better than the previous parallel parser route.

### 3. Detection bridge remains intact

The tests still verify:

- Claude process true-positive;
- bash false-positive prevention;
- nil detector backward compatibility.

This keeps A5a scoped detection bridge intact.

## Why A5b is still rejected

### Blocking issue 1 — no A5b event product-boundary proof

The A5b plan requires Claude parser events to reach the product boundary. This commit does not add a test proving:

```text
ResolveAgentLog → ReadNewEvents → EventStore → /api/sessions Events/agentEvents
```

The new tests are detection-only:

```go
TestClaudeDetection_TruePositive
TestClaudeDetection_FalsePositive
TestClaudeDetection_NilDetector
```

They do not assert any parsed Claude event.

### Blocking issue 2 — `AgentEvents` field remains but is not populated

`SessionTelemetry` still contains:

```go
AgentEvents []AgentEvent `json:"agentEvents,omitempty"`
```

But `Snapshot()` never sets it. The existing product event boundary is still:

```go
Events []models.AgentEvent `json:"events"`
```

This is not automatically wrong, but the API shape is now ambiguous:

- Should A5b use `events`?
- Should it use `agentEvents`?
- Should `agentEvents` be removed?
- Should `events` be documented as the canonical Activity feed?

That needs a deliberate decision before A5b can be accepted.

### Blocking issue 3 — parser-derived status is not connected

`agentStatus` is again derived from detector confidence:

```go
if id.Confidence >= 0.5 && id.Kind != "unknown" {
    status = StatusWorking
}
```

A5b requires parser-derived states such as:

- `thinking`;
- `waiting_approval`;
- `failed`;
- `degraded`.

Those are not connected to `agentStatus`.

### Blocking issue 4 — degraded/mobile requirements remain open

Still missing:

- malformed log → degraded parser state visible at product boundary;
- parser failure isolation test;
- missing log vs no activity vs degraded distinction;
- mobile schema/rendering verification;
- no mobile agent-name behavior branch verification.

## Required next step

This commit should be treated as cleanup after the failed A5b attempts. The next implementation should choose one clear product boundary:

### Option A — Use existing `events` as the A5b product boundary

Required proof:

```text
test production-shaped resolver/process path
→ TelemetryService.processSession
→ ResolveAgentLog
→ ReadNewEvents
→ EventStore.Append
→ Snapshot().Events
→ /api/sessions
```

Then remove or defer `AgentEvents` if it is not used.

### Option B — Keep `agentEvents` as a new product boundary

Required proof:

```text
TelemetryService.processSession
→ cached parsed agent events
→ Snapshot().AgentEvents
→ /api/sessions
```

Do not re-parse logs in `Snapshot()`.

Either option must add assertions for:

- user message;
- assistant/thinking;
- tool call;
- approval requested;
- parser-derived `waiting_approval`;
- degraded/failure isolation.

Mobile/client scope must be implemented or explicitly split into a later A5b sub-step.

## Final decision

`9e6e50dea` is accepted as cleanup of the wrong duplicate parser path.

It is rejected as Phase A5b completion.

A6 remains blocked.
