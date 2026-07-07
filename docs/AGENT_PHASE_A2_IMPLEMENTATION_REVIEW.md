# Agent Adapter Phase A2 Review

Date: 2026-07-07

Executor commit under review: `c8d1ce46b`

Verifier decision: **REJECT**

## Summary

The commit adds the first Go definitions for the Common Agent Model and a model mapping
document:

- `companion-daemon/internal/agent/models.go`
- `docs/AGENT_PHASE_A2_MODEL_DEFINITION.md`

The direction is correct: common event names are fixture-driven, raw source names are
kept in documentation, and no production parser/mobile behavior is introduced.

However, Phase A2 is not accepted yet because the committed model diverges from the
approved plan in fields that will become API/UX contract, and `models.go` is not
gofmt-clean.

## Verification performed

```sh
git diff --check HEAD~1..HEAD
```

Result: passed.

```sh
GOCACHE=/tmp/devremote-agent-a2-go-cache go test ./internal/agent -count=1
```

Result: passed.

```sh
GOCACHE=/tmp/devremote-agent-a2-go-cache go vet ./internal/agent
```

Result: passed.

```sh
gofmt -l companion-daemon/internal/agent/models.go
```

Result:

```text
companion-daemon/internal/agent/models.go
```

## Findings

### P1 — `models.go` is not gofmt-clean

File: `companion-daemon/internal/agent/models.go`

`gofmt -l` reports the file.

Why this blocks:

- This is a new Go package and should be clean before becoming the shared model base.
- Later phases will build contract harness and parser code on this package.

Required fix:

```sh
gofmt -w companion-daemon/internal/agent/models.go
```

### P1 — `AgentEventSource` values diverge from the approved model without rationale

File: `companion-daemon/internal/agent/models.go`

Committed code:

```go
const (
    SourceLog     AgentEventSource = "log"
    SourceScreen  AgentEventSource = "screen"
    SourceProcess AgentEventSource = "process"
    SourceManual  AgentEventSource = "manual"
    SourceUnknown AgentEventSource = "unknown"
)
```

Approved plan in `docs/AGENT_ADAPTER_LAYER_PLAN.md`:

```go
const (
    SourceJSONL      AgentEventSource = "jsonl"
    SourceLogFile    AgentEventSource = "log_file"
    SourceScreen     AgentEventSource = "screen"
    SourceProcess    AgentEventSource = "process"
    SourceManualLink AgentEventSource = "manual_link"
)
```

Why this blocks:

- Phase A1 fixtures explicitly distinguish JSONL/log-file evidence from screen/process
  fallback.
- Phase A2 is defining the contract that later API/mobile code will consume.
- Changing `jsonl`/`log_file` into generic `log`, and `manual_link` into `manual`,
  may be a valid simplification, but it must be reflected in the plan and justified in
  the A2 model document before becoming code.

Required fix:

Choose one of these:

1. Align `models.go` with the approved plan (`jsonl`, `log_file`, `manual_link`), or
2. Update `docs/AGENT_ADAPTER_LAYER_PLAN.md` and
   `docs/AGENT_PHASE_A2_MODEL_DEFINITION.md` to explicitly justify the collapsed source
   taxonomy and show why fixtures/diagnostics do not need separate `jsonl` vs `log_file`
   sources.

Do not leave the code and plan with different source contracts.

### P1 — `AgentApproval` is missing required common approval fields

File: `companion-daemon/internal/agent/models.go`

Committed code:

```go
type AgentApproval struct {
    ID         string
    SessionID  string
    AgentKind  string
    Status     string
    Message    string
    CreatedAt  time.Time
    ResolvedAt *time.Time
}
```

Approved plan:

```go
type AgentApproval struct {
    ID          string
    SessionID   string
    AgentKind   string
    Prompt      string
    Options     []ApprovalOption
    Default     string
    Source      AgentEventSource
    Confidence  float64
    Metadata    map[string]string
}

type ApprovalOption struct {
    ID      string
    Label   string
    Payload string
}
```

Why this blocks:

- Phase A2 includes `AgentApproval` in the common model definition.
- Phase A8 depends on common approval CTA behavior.
- The committed model has no `Options`, `Default`, `Source`, `Confidence`, or
  `Metadata`, so it cannot express the planned fallback behavior:
  - approve/reject/send_text/send_key/open_terminal;
  - low-confidence detection;
  - source-specific diagnostics;
  - agent-specific details isolated in metadata.
- `Status string` is also unconstrained. If status is needed, it should be a typed
  enum or clearly documented.

Required fix:

- Add `ApprovalOption`.
- Add approval fields needed by the plan: prompt/message, options, default, source,
  confidence, metadata.
- If `Status`, `CreatedAt`, and `ResolvedAt` are intentionally added, document them in
  `docs/AGENT_PHASE_A2_MODEL_DEFINITION.md` and preferably type status.

### P1 — A2 model document claims "no raw JSONL fields" while raw names remain in mappings

File: `docs/AGENT_PHASE_A2_MODEL_DEFINITION.md`

The document ends with:

```text
No Claude-specific field names. No `type`, `sessionId`, `payload` from raw JSONL.
```

But the same document necessarily references raw evidence fields in the mapping section:

```text
Claude type=user
Codex type=session_meta
payload.type=waiting_for_approval
```

Why this matters:

- The intended rule is correct for the Go/common model, but the wording is imprecise.
- A future reader could misinterpret raw mapping documentation as a violation.

Required fix:

Reword to something like:

```text
The Go common model does not expose raw JSONL fields such as `type`, `sessionId`, or
`payload`. Raw source names may appear only in fixture metadata and mapping docs.
```

This is a documentation precision issue, but because A2 defines the model boundary, it
should be fixed with the model changes.

## Non-blocking observations

- `AgentIdentity.Kind` necessarily contains stable agent keys such as `claude`, `codex`,
  and `antigravity`; this is acceptable. The rule is not "no agent names anywhere", but
  "no common UX behavior driven by agent names".
- `AgentEvent.Metadata map[string]string` matches the plan's "metadata must not drive UX"
  rule. Later phases should preserve this boundary.

## Required executor actions

1. Run gofmt on `companion-daemon/internal/agent/models.go`.
2. Align `AgentEventSource` between code and plan, or document and approve the changed
   source taxonomy.
3. Expand `AgentApproval` to match the planned common approval model, including
   `ApprovalOption`, `Source`, `Confidence`, and `Metadata`, or explicitly update the
   plan with a defensible alternative.
4. Tighten the A2 document wording around raw source fields vs common model fields.
5. Re-run:

```sh
gofmt -l companion-daemon/internal/agent/models.go
GOCACHE=/tmp/devremote-agent-a2-go-cache go test ./internal/agent -count=1
GOCACHE=/tmp/devremote-agent-a2-go-cache go vet ./internal/agent
```

## Next phase permission

**BLOCKED**

Do not proceed to Phase A3 until the common model code and documents define the same
contract.
