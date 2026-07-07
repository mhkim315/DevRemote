# Agent Adapter Phase A3 Acceptance

Date: 2026-07-07

Executor commit under review: `24cbc60f4`

Verifier decision: **ACCEPT**

## Scope verification

Phase A3 is accepted as the initial parser contract harness.

Reviewed files:

- `companion-daemon/internal/agent/parser.go`
- `companion-daemon/internal/agent/parser_contract_test.go`

The commit changes only the agent parser contract and its reusable test harness. It does
not add production parsers, detectors, mobile behavior, or raw fixture data.

## Acceptance criteria

### 1. Parser contract has a result shape that can support later phases

Accepted.

`ParseResult` now carries:

- `Events []AgentEvent`
- `Cursor string`
- `Status AgentStatus`
- `Approvals []AgentApproval`
- `Degraded bool`
- `Diagnostics []string`

This is sufficient for Phase A4+ detector/resolver work and later parser vertical
slices to report events, status, approval state, incremental cursor, and degraded parse
state without turning parser issues into terminal session failure.

### 2. Harness is fixture-metadata driven

Accepted.

The harness loads `testdata/<agent>/metadata.json` and validates metadata
`expectedEvents` against parsed Common AgentEvent types. It runs against the accepted A1
fixture families:

- Claude
- Codex
- Antigravity

The reusable contract is no longer Claude-fixture-only.

### 3. Required contract behaviors are covered

Accepted.

The harness now covers:

- agent kind
- valid fixture parse
- metadata `expectedEvents`
- status baseline
- approval detection
- malformed handling
- unknown field handling
- missing field fallback
- duplicate prevention for exact replay with returned cursor
- cursor resume no-loss check
- event ordering
- panic safety
- empty input
- low-confidence unknown mapping

### 4. Strict blockers from previous review are resolved

Accepted.

Previous blocker status:

- same lines + returned cursor must produce zero duplicate events: resolved;
- `Degraded=true` requires non-empty diagnostics: resolved;
- pending approval fixture requires `StatusWaitingApproval`: resolved;
- valid fixtures cannot return `StatusUnknown` when events are produced: resolved.

### 5. Automated verification

Executed:

```sh
git diff --check HEAD~1..HEAD
gofmt -l companion-daemon/internal/agent/parser.go companion-daemon/internal/agent/parser_contract_test.go
```

Both produced no output.

```sh
cd companion-daemon
GOCACHE=/tmp/devremote-agent-a3-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a3-go-cache go vet ./internal/agent
GOCACHE=/tmp/devremote-agent-a3-go-cache go test ./...
```

All passed.

Note: the first sandboxed full `go test ./...` run failed because `httptest` could not
bind local ports under sandbox restrictions. The same command passed when rerun with
local listener permission.

## Non-blocking follow-ups

These do not block Phase A3:

- The mock parser's cursor token is intentionally simple and only self-tests the
  harness. Production parsers should use stable source offsets, byte offsets, record
  IDs, or raw refs as appropriate.
- Cursor resume currently enforces no event loss for split parsing and zero duplicates
  for exact replay. Later production parser tests should add overlap-window cases once
  real parser cursor semantics exist.
- `Diagnostics []string` is acceptable for A3. Phase A10 may introduce typed diagnostic
  codes/severity if needed by doctor/mobile UX.
- Antigravity has no approval fixture, so approval detection is skipped only for that
  fixture family. Claude and Codex approval fixtures are covered.

## Next phase permission

**ALLOWED**

Phase A4 may begin.

Required constraints for Phase A4:

1. Use `ParseResult` rather than reintroducing tuple-style parser output.
2. Keep detector/resolver evidence separate from common AgentEvent behavior.
3. Confidence-based unknown fallback should prefer unknown over false-positive agent
   detection.
4. Do not let parser failure hide terminal sessions.
5. Keep raw logs out of the repository.
