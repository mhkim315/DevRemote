# Agent Adapter Phase A3 Review 2

Date: 2026-07-07

Executor commit under review: `e7a20578c`

Verifier decision: **REJECT**

## Summary

This commit materially improves Phase A3:

- the reusable contract no longer hardcodes only Claude fixtures;
- the harness runs against Claude, Codex, and Antigravity fixture families;
- metadata `expectedEvents` is loaded and checked against parsed Common AgentEvent types;
- the parser interface now supports batch parsing and cursor return;
- basic duplicate, ordering, cursor, panic, malformed, unknown-field, and missing-field
  checks exist;
- `go test ./internal/agent` and `go vet ./internal/agent` pass.

However, Phase A3 is still not accepted. The current harness is closer, but it does not
yet enforce the full approved contract. The largest remaining issue is that the parser
interface still cannot express status, approvals, or diagnostics, so the contract cannot
test several Phase A3 requirements.

## Verification performed

```sh
git diff --check HEAD~1..HEAD
gofmt -l companion-daemon/internal/agent/parser.go companion-daemon/internal/agent/parser_contract_test.go
```

Result: passed with no output.

```sh
cd companion-daemon
GOCACHE=/tmp/devremote-agent-a3-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a3-go-cache go vet ./internal/agent
```

Result: passed.

## Findings

### P1 — Parser result still cannot represent status, approvals, or diagnostics

File: `companion-daemon/internal/agent/parser.go`

Current interface:

```go
type AgentParser interface {
    ParseBatch(lines [][]byte, cursor string) (events []AgentEvent, newCursor string, err error)
    AgentKind() string
}
```

Approved Phase A3 requirements include:

- status transition;
- approval detection;
- parser failure/degraded diagnostics;
- malformed fixture records must not imply terminal session failure.

The accepted A2 model also added `AgentApproval`, `AgentStatus`, `AgentEvent`, and
source/confidence fields specifically so the parser contract can normalize more than
event lists.

Why this blocks:

- A parser can pass the current contract while never producing `AgentApproval`.
- A parser can pass while never producing an `AgentStatus`.
- A parser can drop malformed/degraded diagnostics silently.
- Phase A8 approval UX and Phase A10 diagnostics would have no contract-backed parser
  output to consume.

Required fix:

Introduce a parser result type, even if minimal:

```go
type ParseResult struct {
    Events      []AgentEvent
    Status      AgentStatus
    Cursor      string
    Approvals   []AgentApproval
    Diagnostics []AgentDiagnostic // or an explicitly deferred typed placeholder
}
```

If diagnostics are intentionally deferred, document the exact deferred field and still
provide a stable extension point. Do not leave only `error`, because `error` cannot
represent degraded-but-usable parse results without encouraging terminal/session
failure semantics.

### P1 — Cursor resume test does not fail on broken resume behavior

File: `companion-daemon/internal/agent/parser_contract_test.go`

Current code:

```go
if len(e1)+len(e2) != len(eAll) {
    t.Logf("cursor resume: split=%d+%d, full=%d (may differ if parser re-evaluates context)", ...)
}
```

The test logs a mismatch but still passes. The current mock parser actually shows this
for Codex:

```text
cursor resume: split=4+1, full=9
```

Why this blocks:

- Phase A3 explicitly requires incremental cursor resume.
- A test that logs mismatch but passes does not enforce the contract.
- Later production parsers could duplicate or drop events across polling intervals and
  still pass.

Required fix:

- Define cursor semantics clearly.
- Make cursor mismatch fail, or split the test into:
  - strict cursor contract for parsers declaring cursor support;
  - explicit no-cursor mode for parsers that return an empty cursor.
- Do not silently pass a known mismatch.

### P1 — Approval detection is only inferred through event type, not `AgentApproval`

Files:

- `companion-daemon/internal/agent/parser.go`
- `companion-daemon/internal/agent/parser_contract_test.go`

The harness checks `expectedEvents`, so `approval_requested` appears as an event. It
does not check `AgentApproval` output because the parser interface cannot return one.

Why this blocks:

- A2 accepted `AgentApproval` as part of the common model.
- Phase A8 will need approval options/default/source/confidence, not just an
  `approval_requested` event.
- The contract should at least assert that fixtures containing approval/waiting evidence
  can produce a normalized approval object or explicitly document that approval object
  extraction is deferred to a named later contract.

Required fix:

- Add `Approvals []AgentApproval` to parser result and assert approval fixture coverage,
  or explicitly split approval-object extraction into a documented subphase that blocks
  Phase A8.

### P1 — Status transition contract is absent

Files:

- `companion-daemon/internal/agent/parser.go`
- `companion-daemon/internal/agent/parser_contract_test.go`

No test asserts `AgentStatus`. The parser interface has no status output.

Why this blocks:

- Phase A2 defined `AgentStatus`.
- Phase A3 required status transition.
- Phase A9 UX depends on status being common, not parser-specific.

Required fix:

- Add status to parser result.
- Add at least baseline assertions:
  - valid active/tool/approval fixtures do not return empty/unknown status unless
    metadata says unknown;
  - malformed-only fixtures degrade or remain unknown without failing the session.

### P2 — Duplicate prevention test is too weak

File: `companion-daemon/internal/agent/parser_contract_test.go`

Current code only fails when the second parse produces more events than the first:

```go
if len(e2) > len(e1) { ... }
```

Why this matters:

- Re-parsing with a cursor should normally produce zero already-consumed events.
- Producing the same number or fewer duplicates can still be wrong.

Recommended fix:

- Track event IDs or raw refs once the parser result contract includes stable identity.
- At minimum, require `len(e2) == 0` when parsing the exact same lines with the returned
  cursor, unless the parser explicitly declares cursor unsupported.

### P2 — Mock parser remains parser-specific, though the reusable harness is improved

File: `companion-daemon/internal/agent/parser_contract_test.go`

The reusable harness is now metadata-driven, which is the important part. The mock parser
still contains Claude/Codex/Antigravity raw mapping logic. That is acceptable for a
self-test mock, but it should not become the production parser pattern.

Recommended fix:

- Add a short comment stating that the mock parser is only a harness self-test adapter
  and production parsers must live outside the reusable contract harness.

## Required executor actions

1. Replace the tuple return with a parser result type that can carry events, cursor,
   status, approvals, and diagnostics/degraded information.
2. Make cursor resume semantics enforceable; do not pass on known cursor mismatch.
3. Add status assertions to the contract.
4. Add approval-object assertions or explicitly document a subphase that blocks Phase A8.
5. Strengthen duplicate prevention, preferably using stable event IDs/raw refs or strict
   cursor behavior.
6. Keep fixture metadata-driven expectedEvents validation.
7. Re-run:

```sh
gofmt -l companion-daemon/internal/agent/parser.go companion-daemon/internal/agent/parser_contract_test.go
GOCACHE=/tmp/devremote-agent-a3-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a3-go-cache go vet ./internal/agent
```

## Next phase permission

**BLOCKED**

Do not proceed to Phase A4 until the contract can verify status, approvals, cursor
resume, and degraded diagnostics semantics rather than only normalized event lists.
