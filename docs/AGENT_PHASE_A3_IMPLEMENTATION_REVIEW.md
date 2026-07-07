# Agent Adapter Phase A3 Review

Date: 2026-07-07

Executor commit under review: `94220f3bb`

Verifier decision: **REJECT**

## Summary

The commit adds:

- `companion-daemon/internal/agent/parser.go`
- `companion-daemon/internal/agent/parser_contract_test.go`

The direction is correct: a reusable contract entrypoint exists, parsers are expected
not to panic, malformed input is treated as non-fatal, and the package compiles.

However, Phase A3 is not accepted because the harness is still a Claude-shaped smoke
test rather than the common Agent Adapter parser contract described in the approved
plan and A2 acceptance.

## Verification performed

```sh
git diff --check HEAD~1..HEAD
gofmt -l companion-daemon/internal/agent/parser.go companion-daemon/internal/agent/parser_contract_test.go
```

Result: both passed with no output.

```sh
cd companion-daemon
GOCACHE=/tmp/devremote-agent-a3-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a3-go-cache go vet ./internal/agent
```

Result: passed.

## Findings

### P1 — Contract harness hardcodes Claude fixtures

File: `companion-daemon/internal/agent/parser_contract_test.go`

The generic harness loads:

```go
loadFixtures(t, "claude", "user_assistant.jsonl")
loadFixtures(t, "claude", "malformed.jsonl")
```

Why this blocks Phase A3:

- Phase A3 is supposed to create a common parser contract for all AgentAdapters.
- A1 accepted fixtures for Claude, Codex, and Antigravity.
- A2 accepted `expectedEvents` as common AgentEvent expectations and
  `observedSourceTypes` as raw source evidence.
- A generic harness that uses only Claude fixtures will not catch Codex/Antigravity
  assumptions leaking into the parser contract.

Required fix:

- Parameterize the contract with fixture agent/scenario paths, or load fixture metadata
  from `companion-daemon/internal/agent/testdata/*/metadata.json`.
- The harness should be able to run against at least the accepted A1 fixture metadata
  for Claude, Codex, and Antigravity, even if only mock parsers are used initially.

### P1 — Harness does not validate `expectedEvents`

Files:

- `companion-daemon/internal/agent/parser_contract_test.go`
- `companion-daemon/internal/agent/testdata/*/metadata.json`

Current `testValidRecord` checks only that parsed events have non-empty fields:

```go
if event.Type == "" { ... }
if event.Source == "" { ... }
if event.AgentKind == "" { ... }
```

It does not check that the parser output contains the common events declared in fixture
metadata:

```json
"expectedEvents": [...]
```

Why this blocks Phase A3:

- A2 explicitly established that `expectedEvents` is the Common AgentEvent contract.
- A parser could return `EventUnknown` for every valid record and still pass most of
  the current harness.
- The contract therefore does not prove semantic mapping.

Required fix:

- Load each fixture metadata file.
- Parse all referenced JSONL fixtures.
- Assert that the produced common event types cover the metadata's `expectedEvents`,
  with clear policy for optional/deferred events.
- Keep raw `observedSourceTypes` out of behavior assertions except as parser-specific
  evidence.

### P1 — Required Phase A3 contract items are missing

Approved Phase A3 required contract items include:

- valid fixture parse
- malformed record skip
- unknown field ignore
- missing field fallback
- duplicate event prevention
- event ordering
- incremental cursor resume
- status transition
- approval detection
- tool call detection
- parser panic recover
- low-confidence unknown fallback

Current harness covers only a subset:

- valid record parse, but only weak non-empty checks
- malformed skip
- unknown field ignore
- missing field fallback
- panic recover
- empty input

Missing:

- duplicate event prevention
- event ordering
- incremental cursor resume
- status transition
- approval detection
- tool call detection
- low-confidence unknown fallback

Why this blocks Phase A3:

- A3's purpose is to prevent a future Claude parser from becoming the architecture.
- Without cursor/order/duplicate/status/approval/tool assertions, later parsers can pass
  while violating the shared semantics.

Required fix:

- Either implement the required contract items now, or explicitly split A3 into
  subphases and mark the current commit as A3 progress, not completion.
- If splitting, add a document that states which contract items are intentionally
  deferred and which commit will block Phase A4.

### P1 — Parser interface is too small for the accepted A2/A3 contract

File: `companion-daemon/internal/agent/parser.go`

Current interface:

```go
type AgentParser interface {
    Parse(line []byte) (*AgentEvent, error)
}
```

Why this blocks:

- A2 plan and accepted model include approval state and status semantics.
- A3 required contract includes incremental cursor resume, duplicate prevention, event
  ordering, and status transition.
- A single-line `Parse(line)` returning one event cannot naturally express:
  - batch fixture parse results;
  - cursor updates;
  - status;
  - approvals;
  - diagnostics/degraded parse results;
  - duplicate suppression across records.

Required fix:

Align the parser contract with the planned shape, or document a phased split with a
clear extension point. The plan's earlier shape was closer:

```go
type AgentParseInput struct {
    Session  SessionContext
    Identity AgentIdentity
    Logs     []AgentLogRef
    Cursor   AgentCursor
    Screen   string
}

type AgentParseResult struct {
    Events      []AgentEvent
    Status      AgentStatus
    Cursor      AgentCursor
    Approvals   []AgentApproval
    Diagnostics []AgentDiagnostic
}
```

The final names can change, but the contract must be able to test the Phase A3
requirements.

### P2 — Mock parser is Claude-shaped

File: `companion-daemon/internal/agent/parser_contract_test.go`

The self-test mock parser is named `mock-claude`, returns `AgentKind: "claude"`, and
parses raw Claude-style fields such as `type=user`, `type=assistant`, and `tool_use`.

Why this matters:

- A self-test mock is useful, but a Claude-shaped mock does not prove the harness is
  adapter-neutral.

Required fix:

- Add at least one neutral fixture/mock path, or run the harness against multiple
  fixture families with mock parsers.
- Keep agent-specific parsing logic out of the reusable contract harness itself.

## Required executor actions

1. Remove Claude fixture hardcoding from the reusable contract path.
2. Load A1 fixture metadata and validate common `expectedEvents`.
3. Add or explicitly document the remaining required contract items:
   - duplicate prevention
   - event ordering
   - incremental cursor resume
   - status transition
   - approval detection
   - tool call detection
   - low-confidence unknown fallback
4. Revisit `AgentParser` so the contract can represent events, status, cursor,
   approvals, and diagnostics, or document a phased interface with a non-deferred
   blocker before Phase A4.
5. Keep raw logs out of the repository.
6. Re-run:

```sh
gofmt -l companion-daemon/internal/agent/parser.go companion-daemon/internal/agent/parser_contract_test.go
GOCACHE=/tmp/devremote-agent-a3-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a3-go-cache go vet ./internal/agent
```

## Next phase permission

**BLOCKED**

Do not proceed to Phase A4 until the parser contract harness validates common
AgentEvent semantics across accepted fixture metadata rather than only smoke-testing a
Claude-shaped parser.
