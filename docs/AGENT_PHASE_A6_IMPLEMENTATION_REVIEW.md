# Agent Phase A6 Implementation Review — Codex Backend First Slice

Reviewed commits:

- `e79a111` — `feat: Phase A6 — CodexAdapter (parser + detector + resolver)`
- `c112518cd` — `feat: Phase A6 — term CodexParser normalization + boundary test`

Verdict: **REJECT FOR A6 COMPLETION / ACCEPTABLE BACKEND PROGRESS**

This is a meaningful first A6 implementation. It adds:

- `internal/agent` Codex parser/detector/resolver contract implementation;
- `internal/term` Codex parser normalization to common event names;
- a product-boundary Codex telemetry test through `/api/sessions.Events`;
- regression coverage showing Claude A5 tests still pass.

However, it is not enough to accept A6 completion yet. The Codex product-boundary test proves events, but it does not assert the common status and identity gates required by the A5 foundation reuse rule.

## Verification performed

Commands run:

```bash
git diff --check d091cc1..HEAD
gofmt -l companion-daemon/internal/agent/codex_adapter.go companion-daemon/internal/agent/codex_adapter_test.go companion-daemon/internal/term/codex_boundary_test.go companion-daemon/internal/term/codex_parser.go companion-daemon/internal/term/parser_test.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-1-go-cache go test ./internal/agent -run 'TestCodex|TestClaude|TestMock' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-1-go-cache go test ./internal/term -run 'TestCodexLog_ProductionEventsPath|TestCodexParser|TestClaudeLog_ProductionEventsPath|TestClaudeDegraded_MalformedLog' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-1-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-1-go-cache go test ./...
cd mobile && node_modules/.bin/tsc --noEmit
rg -n "if .*agent|agentKind|agentStatus|claude|codex|gpt|antigravity|gemini" mobile/src -g '*.tsx' -g '*.ts'
```

Notes:

- `git diff --check` passed.
- `gofmt -l` returned no files.
- Codex/Claude agent contract tests passed.
- Codex/Claude term boundary tests passed.
- `go vet ./...` passed.
- Mobile TypeScript compile passed.
- Mobile agent-name/status grep returned no matches.
- Sandboxed full `go test ./...` failed because `httptest` could not bind a local port.
- Authorized full `go test ./...` passed.

## What improved

### 1. Codex parser contract exists

`internal/agent/codex_adapter.go` adds a Codex parser that passes the common parser contract suite.

The contract fixture coverage includes:

- `agent_started`;
- `user_message`;
- `approval_requested`;
- `approval_resolved`;
- malformed/unknown input handling.

### 2. Codex term parser now emits common event types

`internal/term/codex_parser.go` now normalizes legacy term events before returning them.

The existing term parser test now expects:

- `user_message`;
- `assistant_message`;
- `tool_call_started`;
- `tool_call_finished`.

### 3. Codex reaches `/api/sessions.Events`

`TestCodexLog_ProductionEventsPath` exercises:

```text
TelemetryService.Run
→ processSession
→ SetLogResolver / LogRef{Agent: "codex"}
→ ReadNewEvents
→ term.CodexParser
→ EventStore.Append
→ /api/sessions.Events
```

The test asserts:

- `agent_started`;
- `user_message`;
- `approval_requested`.

Observed targeted test output:

```text
codex: events=4, agentKind=codex, state=waiting
```

### 4. Claude A5 backend proof still passes

The targeted run included:

- `TestClaudeLog_ProductionEventsPath`;
- `TestClaudeDegraded_MalformedLog`;
- `TestClaudeParser_Contract`;
- `TestClaudeDetector_Contract`.

Those passed.

## Why A6 is still rejected

### Blocking issue 1 — Codex `agentStatus=waiting_approval` is not asserted

A6 must reuse the A5 common event/status path.

The Codex boundary test currently logs:

```text
state=waiting
```

but it does not assert:

```go
s.AgentStatus == "waiting_approval"
```

Given A5's accepted contract, this must be a hard product-boundary assertion for Codex as well.

### Blocking issue 2 — Codex `agentKind=codex` is logged but not asserted

The test logs:

```text
agentKind=codex
```

but does not fail if `agentKind` changes. A6 should explicitly assert:

```go
s.AgentKind == "codex"
```

and a confidence threshold if confidence remains part of the product contract.

### Blocking issue 3 — product detection still flows through `TermAgentDetector` backed by `ClaudeDetector`

The new `CodexDetector` passes contract tests, but the product telemetry bridge still uses:

```go
agent.NewTermAgentDetector()
```

and `TermAgentDetector` currently wraps `ClaudeDetector`.

ClaudeDetector happens to detect `ProcessName: "codex"` today, so the Codex boundary test logs `agentKind=codex`. But the new `CodexDetector` is not what proves the product-boundary Codex detection path.

This may be acceptable as a transitional implementation, but A6 completion needs either:

- a unified detector registry/composite detector used by `TermAgentDetector`; or
- an explicit documented decision that `TermAgentDetector` is the product detector and individual CodexDetector is contract-only for now.

### Blocking issue 4 — duplicated parser paths can diverge

There are now two Codex parser implementations:

- `internal/agent.CodexParser` for the common parser contract;
- `internal/term.CodexParser` for the production telemetry path.

This mirrors the existing Claude situation, but it is still a risk. A6 completion should prove that both paths agree on the core event/status semantics, especially approval → `waiting_approval`.

At minimum, the product-boundary test must assert status. Preferably, a shared fixture or mapping test should show the contract parser and term parser agree on common event types for the same redacted Codex log shape.

## Required next step

Do not claim A6 completion yet.

Minimum acceptable next commit:

1. Update `TestCodexLog_ProductionEventsPath` to assert:
   - `s.AgentKind == "codex"`;
   - `s.AgentStatus == "waiting_approval"`;
   - confidence threshold if applicable.
2. Add a false-positive boundary check for non-agent/bash if Codex detector wiring changes.
3. Clarify or implement product detector wiring:
   - composite detector registry; or
   - documented transitional use of `TermAgentDetector`.
4. Keep Claude A5 backend proof passing.
5. Keep mobile no-name-branch and TypeScript compile checks passing.

## Final decision

`e79a111` and `c112518cd` are accepted as A6 backend progress.

They are rejected as Phase A6 completion until Codex product-boundary identity/status assertions are added and detector wiring is clarified.
