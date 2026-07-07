# Agent Adapter Phase A2 Review 2

Date: 2026-07-07

Executor commit under review: `28de93426`

Verifier decision: **REJECT**

## Summary

The commit fixes several previous blockers:

- `companion-daemon/internal/agent/models.go` is now gofmt-clean.
- `AgentEventSource` values now match the approved source taxonomy values:
  `jsonl`, `log_file`, `screen`, `process`, `manual_link`.
- `AgentApproval` now includes `Options`, `Default`, `Source`, `Confidence`, and
  `Metadata`.
- The A2 document now correctly says raw JSONL fields may appear in mapping docs and
  fixture metadata, but not in the common Go model.

Phase A2 is still blocked because the approval option contract is not aligned with the
approved model and cannot represent the planned terminal-input fallback.

## Verification performed

```sh
git diff --check HEAD~1..HEAD
```

Result: passed.

```sh
gofmt -l companion-daemon/internal/agent/models.go
```

Result: no output.

```sh
GOCACHE=/tmp/devremote-agent-a2-go-cache go test ./internal/agent -count=1
GOCACHE=/tmp/devremote-agent-a2-go-cache go vet ./internal/agent
```

Result: passed.

## Findings

### P1 — `ApprovalOption` cannot represent the planned approval action payload

File: `companion-daemon/internal/agent/models.go`

Approved plan in `docs/AGENT_ADAPTER_LAYER_PLAN.md`:

```go
type ApprovalOption struct {
    ID      string `json:"id"`      // approve, reject, send_text, send_key, open_terminal
    Label   string `json:"label"`
    Payload string `json:"payload,omitempty"`
}
```

Committed code:

```go
type ApprovalOption struct {
    Label       string `json:"label"`
    Action      string `json:"action"`
    IsDefault   bool   `json:"isDefault,omitempty"`
    IsDangerous bool   `json:"isDangerous,omitempty"`
}
```

Why this blocks Phase A2:

- Phase A2 defines the common model that Phase A8 approval UX will build on.
- The approved plan explicitly lists common actions:
  - `approve`
  - `reject`
  - `send_text`
  - `send_key`
  - `open_terminal`
- For `send_text` and `send_key`, the model needs a payload.
- The current `Action` field can act like an ID, but the model has no `Payload`.
- `AgentApproval.Default` is currently documented as a default option label, but the
  plan expects a default option identifier. A label is not stable enough for an action
  contract.

Required fix:

Either align with the approved plan:

```go
type ApprovalOption struct {
    ID      string `json:"id"`
    Label   string `json:"label"`
    Payload string `json:"payload,omitempty"`
}
```

or explicitly update both `docs/AGENT_ADAPTER_LAYER_PLAN.md` and
`docs/AGENT_PHASE_A2_MODEL_DEFINITION.md` with a defensible replacement that can still
represent:

- approve/reject/open_terminal without payload;
- send_text/send_key with payload;
- a stable default option reference.

If keeping `IsDangerous`, document it as an additive field rather than replacing the
planned payload contract.

### P2 — A2 model document has stale AgentEventSource summary

File: `docs/AGENT_PHASE_A2_MODEL_DEFINITION.md`

The source taxonomy table is now correct, but the Go Types summary still says:

```text
AgentEventSource — 5 string constants (log..unknown)
```

The actual constants are:

```text
jsonl
log_file
screen
process
manual_link
```

Required fix:

Update the summary to match the actual constants.

### P2 — Exported constant name differs from the plan name

File: `companion-daemon/internal/agent/models.go`

The value is correct:

```go
SourceManual AgentEventSource = "manual_link"
```

The plan names it `SourceManualLink`. This is not a semantic blocker because the JSON
contract value is correct, but consistency would reduce friction before contract tests
are added.

Recommended fix:

Rename `SourceManual` to `SourceManualLink`, or document the intentional shorter Go
constant name in the A2 model document.

## Required executor actions

1. Fix `ApprovalOption` so it can represent a stable option ID and optional payload, or
   update the plan and A2 docs with an equivalent contract.
2. Make `AgentApproval.Default` refer to the stable option ID/action, not the display
   label.
3. Fix the stale AgentEventSource summary in the A2 model document.
4. Optionally align `SourceManual` with the plan's `SourceManualLink` name.
5. Re-run:

```sh
gofmt -l companion-daemon/internal/agent/models.go
GOCACHE=/tmp/devremote-agent-a2-go-cache go test ./internal/agent -count=1
GOCACHE=/tmp/devremote-agent-a2-go-cache go vet ./internal/agent
```

## Next phase permission

**BLOCKED**

Do not proceed to Phase A3 until the approval option model can represent the planned
common approval actions, including payload-bearing terminal fallback actions.
