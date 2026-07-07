# Agent Phase A5b Implementation Review 2 — Fixture Fallback Parser

Reviewed commit: `67c955894`

Verdict: **REJECT**

This revision is an improvement over the previous placeholder event bridge because it calls `ClaudeParser.ParseBatch` and tests for concrete event types. However, it still does not satisfy Phase A5b.

The implementation proves:

```text
Claude process evidence → fallback to committed A1 fixtures → parse fixture lines → expose agentEvents
```

It does not prove:

```text
actual session log resolution → actual Claude log read → parser output → product boundary
```

The fallback to A1 fixtures is inside production code, so a real Claude process with no resolved/readable log can still show fixture-derived `agentEvents`. That hides resolver/parser integration failures and can expose events unrelated to the actual running session.

## Verification performed

Commands run:

```bash
git diff --check 6596034..HEAD
gofmt -l companion-daemon/internal/agent/bridge.go companion-daemon/internal/term/claude_boundary_test.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b2-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b2-go-cache go test ./internal/term -run 'TestClaudeOutput_(TruePositive_ProductionBridge|FalsePositive_ProductionBridge|NilDetector_BackwardCompat)' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b2-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b2-go-cache go test ./internal/term -count=1
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b2-go-cache go test ./...
```

Notes:

- The sandboxed `internal/term` full test failed because `httptest` could not bind a local port.
- The authorized `internal/term` and full `go test ./...` runs passed.
- Passing tests do not satisfy A5b because the product-boundary test relies on production fixture fallback rather than actual session logs.

## What improved

- `ParseEvents` no longer returns a single hardcoded placeholder event.
- It now calls `ClaudeParser.ParseBatch`.
- The boundary test checks for specific event types:
  - `user_message`
  - `thinking`
- `DetectAgent` now attempts to infer status from parsed events.

These are useful steps, but the product-boundary proof is still not valid.

## Blocking issue 1 — production code falls back to committed fixtures

`ParseEvents` tries to read resolved logs, then falls back to A1 fixtures:

```go
// Fallback: use A1 test fixtures when no production logs found.
if len(allLines) == 0 && evidence.ProcessName == "claude" {
    allLines = loadA1Fixtures()
}
```

`loadA1Fixtures` reads from repository paths:

```go
candidates := []string{
    filepath.Join("internal", "agent", "testdata", "claude"),
    filepath.Join("testdata", "claude"),
    filepath.Join("..", "agent", "testdata", "claude"),
}
```

This must not be in production detection/parsing flow. Test fixtures are acceptable in tests, but production code must not synthesize session activity from repo fixtures.

Impact:

- A Claude process with no readable log gets fake events.
- The `/api/sessions` response can show user/thinking/tool/approval events that did not occur in the actual session.
- Resolver failures are hidden instead of surfacing as unavailable/degraded/no-activity states.

## Blocking issue 2 — resolver path is not production-valid

The agent-layer resolver returns a path containing a glob:

```go
Path: ev.CWD + "/.claude/projects/" + project + "/*.jsonl"
```

`ParseEvents` then calls:

```go
os.ReadFile(lr.Path)
```

`os.ReadFile` does not expand globs. This path will not read real logs unless there is literally a file named `*.jsonl`. The code therefore predictably falls back to fixtures in the product-boundary test.

The repo already has term-layer resolver/parser infrastructure through:

- `TelemetryService.processSession`
- `ResolveAgentLog`
- `ReadNewEvents`
- `EventStore`
- term-layer `ClaudeParser`

A5b should either reuse that production path or introduce a production-valid equivalent. A glob string plus fixture fallback is not enough.

## Blocking issue 3 — test proves fixture fallback, not session log parsing

The test session only supplies:

```go
models.ProcessInfo{Command: "claude", CWD: "/Users/test/project"}
```

It does not supply:

- a real log path;
- a temp fixture log file;
- a resolver result pointing to that file;
- parser cursor state;
- a production-shaped log resolution path.

The asserted `user_message` and `thinking` events come from repo fixtures, not from the test session.

## Blocking issue 4 — A5b coverage is still incomplete

The A5b plan requires more than user/thinking event proof:

- user/assistant/tool/approval events at product boundary;
- parser-derived status including approval states;
- degraded parser state visible at product/mobile boundary;
- parser failure isolation;
- mobile status badge/schema/rendering;
- no mobile agent-name behavior branch;
- tmux + Claude and LocalPTY + Claude semantic parity.

This commit only checks `user_message` and `thinking`. It does not check approval, tool call, degraded, mobile, or LocalPTY parity.

## Blocking issue 5 — status assertion is non-assertive

The test includes:

```go
if s.AgentStatus != "working" && s.AgentStatus != "waiting_input" && s.AgentStatus != "thinking" {
    t.Logf("AgentStatus=%s (parser-derived)", s.AgentStatus)
}
```

This only logs unexpected statuses. It does not fail. A5b needs a real assertion for parser-derived status, especially `waiting_approval` from the approval fixture.

## Required changes for acceptance

Minimum next revision:

1. Remove production fixture fallback from `ParseEvents`.
   - Fixtures may be used by tests only.
   - Production code must return no activity or degraded/unavailable when logs cannot be resolved/read.

2. Use a real production-shaped log source in tests.
   - Create a temp Claude JSONL log file in the test.
   - Make the test session/resolver point to that file through the same interface production uses.
   - Assert that the parsed events come from that file, not from repo fixtures.

3. Use production-valid resolver behavior.
   - Do not pass glob paths to `os.ReadFile`.
   - Either resolve a concrete file path or expand/choose candidates deliberately with diagnostics.

4. Assert concrete A5b event coverage.
   - user message
   - assistant message or thinking
   - tool call started/finished
   - approval requested

5. Assert parser-derived status.
   - Approval fixture/log must produce `waiting_approval` or the documented fallback state.
   - Unexpected status must fail the test.

6. Add degraded/failure isolation tests.
   - Malformed log record should surface degraded parser state without hiding/killing the terminal session.
   - Missing/unreadable log should not synthesize fixture events.

7. Handle mobile/client scope.
   - Either add mobile schema/rendering verification in A5b, or split A5b into backend and mobile sub-steps in the plan before claiming completion.

8. Keep A6 blocked until A5b is accepted.

## Final decision

`67c955894` is rejected for Phase A5b.

It is a useful intermediate step from placeholder events to parser-backed fixture events, but it still does not prove the required production vertical slice.
