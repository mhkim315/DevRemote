# Agent Adapter Phase A2 Acceptance

Date: 2026-07-07

Executor commit under review: `a7edf79a2`

Verifier decision: **ACCEPT**

## Scope verification

Phase A2 is accepted as the Common Agent Model baseline.

Reviewed files:

- `companion-daemon/internal/agent/models.go`
- `docs/AGENT_PHASE_A2_MODEL_DEFINITION.md`

The commit changes only the new agent model package and its A2 documentation. It does
not add parsers, detectors, mobile behavior, or production integration.

## Acceptance criteria

### 1. Common model covers A1 fixtures

Accepted.

The A2 document maps A1 fixture evidence from Claude, Codex, and Antigravity into the
common event set:

- `agent_started`
- `user_message`
- `assistant_message`
- `thinking`
- `tool_call_started`
- `tool_call_finished`
- `approval_requested`
- `approval_resolved`
- `unknown`

Deferred event types are explicitly listed:

- `waiting_input`
- `completed`
- `failed`
- `interrupted`

This is acceptable because A1 did not provide committed fixture evidence for those
states.

### 2. Raw agent fields are not part of the common Go model

Accepted.

`models.go` does not expose raw Claude/Codex/Antigravity JSONL field names such as
`type`, `sessionId`, `payload`, `tool_use`, `session_meta`, or `AGENT_OUTPUT`.

Raw field names appear only in:

- fixture files;
- fixture metadata;
- the A2 mapping document for parser implementors.

This preserves the intended boundary: parser adapters know raw formats; common API/UX
models consume semantic fields.

### 3. AgentEventSource aligns with the approved taxonomy

Accepted.

`models.go` defines:

```text
jsonl
log_file
screen
process
manual_link
```

This matches the approved plan and the A2 document.

### 4. Approval model is sufficient for planned fallback UX

Accepted.

`AgentApproval` includes:

- `Prompt`
- `Options`
- `Default`
- `Source`
- `Confidence`
- `Metadata`
- lifecycle fields `Status`, `CreatedAt`, `ResolvedAt`

`ApprovalOption` now uses a stable contract shape:

```go
type ApprovalOption struct {
    ID      string `json:"id"`
    Label   string `json:"label"`
    Payload string `json:"payload,omitempty"`
}
```

This can represent:

- `approve`
- `reject`
- `send_text`
- `send_key`
- `open_terminal`

`AgentApproval.Default` is documented as a stable `ApprovalOption.ID`, not a display
label.

### 5. Formatting and compile checks

Accepted.

Verification commands:

```sh
git diff --check HEAD~1..HEAD
gofmt -l companion-daemon/internal/agent/models.go
```

Both produced no output.

```sh
cd companion-daemon
GOCACHE=/tmp/devremote-agent-a2-go-cache go test ./internal/agent -count=1
GOCACHE=/tmp/devremote-agent-a2-go-cache go vet ./internal/agent
GOCACHE=/tmp/devremote-agent-a2-go-cache go test ./...
```

All passed.

Note: the first sandboxed `go test ./...` run failed because `httptest` could not bind
local ports under sandbox restrictions. The same command passed when rerun with local
listener permission.

## Non-blocking follow-ups

These do not block Phase A2:

- Add typed enums for approval status if Phase A8 needs stricter compile-time checking.
- Add optional `IsDangerous` or similar presentation hints later if the approval UX needs
  them. Such hints should be additive and must not replace stable option IDs/payloads.
- A3 contract tests should assert that metadata does not drive mobile behavior.

## Next phase permission

**ALLOWED**

Phase A3 may begin.

Required constraints for Phase A3:

1. Build the parser contract harness against `internal/agent/models.go`.
2. Treat `expectedEvents` from A1 metadata as common `AgentEventType` values.
3. Keep raw source names in fixture metadata or parser-specific code only.
4. Ensure malformed fixture records degrade parser output without failing terminal
   session semantics.
5. Do not add Claude-specific assumptions to the common harness.
