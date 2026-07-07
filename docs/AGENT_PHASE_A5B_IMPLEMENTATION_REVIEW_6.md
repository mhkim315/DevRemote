# Agent Phase A5b Implementation Review 6 — Canonical Events Boundary Cleanup

Reviewed commit: `1954ed438`

Verdict: **REJECT FOR A5b / ACCEPTABLE BOUNDARY CLEANUP**

This commit removes the unused `agentEvents` field and documents the intended canonical product boundary:

```text
TelemetryService.processSession
→ ResolveAgentLog
→ ReadNewEvents
→ EventStore
→ Snapshot().Events
→ /api/sessions
```

That direction is correct. However, the commit does not add a product-boundary proof that Claude parser events actually flow through that path. It only removes ambiguity from the API shape.

## Verification performed

Commands run:

```bash
git diff --check 77ce7d8..HEAD
gofmt -l companion-daemon/internal/term/runtime.go companion-daemon/internal/term/telemetry.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b5-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b5-go-cache go test ./internal/term -run 'TestClaudeDetection_(TruePositive|FalsePositive|NilDetector)' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b5-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b6-go-cache go test ./internal/term -count=1
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b6-go-cache go test ./...
```

Notes:

- The sandboxed `internal/term` full test failed because `httptest` could not bind a local port.
- The authorized `internal/term` and full `go test ./...` runs passed.
- Passing tests confirm this cleanup does not regress the existing suite, but they do not satisfy A5b acceptance.

## What improved

### 1. `agentEvents` ambiguity was removed

`SessionTelemetry` no longer exposes an unused `agentEvents` field. The product event boundary is now clearly the existing `events` field:

```go
Events []models.AgentEvent `json:"events"`
```

The comment documents:

```go
// Agent events flow through the existing Events field via
// TelemetryService.processSession → EventStore → Snapshot.
```

This resolves the API ambiguity called out in the previous review.

### 2. The `AgentDetector` interface no longer carries event parsing

`term.AgentDetector` now only exposes:

```go
DetectAgent(...)
```

That is cleaner. Parser events should not be produced by the detector interface.

## Why A5b is still rejected

### Blocking issue 1 — no product-boundary event proof was added

The A5b requirement is not merely to document the existing path. It must prove that Claude events reach the product boundary.

There is no test asserting:

```text
ResolveAgentLog
→ ReadNewEvents
→ EventStore.Append
→ Snapshot().Events
→ /api/sessions
```

with Claude user/tool/approval events.

The active A5b test file is detection-only:

```go
TestClaudeDetection_TruePositive
TestClaudeDetection_FalsePositive
TestClaudeDetection_NilDetector
```

These tests do not inspect `SessionTelemetry.Events`.

### Blocking issue 2 — parser-derived status is not connected

`agentStatus` is still detector-confidence-derived:

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

### Blocking issue 3 — degraded/failure/mobile scope remains open

Still missing:

- malformed Claude log → degraded parser state visible at product boundary;
- parser failure isolation;
- missing log vs no activity vs degraded distinction;
- mobile schema/rendering verification;
- no mobile agent-name behavior branch verification.

## Required next step

The next implementation should keep the cleanup from this commit and add the missing proof.

Minimum acceptable A5b backend proof:

1. Use the canonical `events` boundary.
2. Create a production-shaped test where:
   - `ProcessProvider.ProcessInfo` is enough input;
   - `ResolveAgentLog` or an injectable production-shaped resolver returns a concrete temp Claude log path;
   - `ReadNewEvents` parses the log;
   - `EventStore.Append` stores events;
   - `Snapshot().Events` / `/api/sessions` exposes those events.
3. Assert specific event types:
   - `user_message`;
   - `thinking` or assistant message;
   - `tool_call_started`;
   - `approval_requested`.
4. Assert parser-derived `waiting_approval` or explicitly defer agentStatus integration to a documented sub-step.
5. Add degraded/failure isolation tests or explicitly split A5b backend/degraded/mobile sub-steps before claiming completion.

## Final decision

`1954ed438` is accepted as boundary cleanup.

It is rejected as Phase A5b completion.

A6 remains blocked.
