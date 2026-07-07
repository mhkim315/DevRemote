# Agent Phase A6 Review — `e2c225288`

Reviewed commit:

- `e2c225288` / local short hash `e2c2252` — `fix: Phase A6 — composite detector, Codex assertions`

Verdict: **REJECT FOR A6 COMPLETION / ACCEPTABLE BACKEND FIX**

This commit resolves the main A6 structural issues from the prior review:

- `TermAgentDetector` is now a composite production detector over Claude and Codex detectors.
- The production bridge now chooses the highest-confidence non-unknown detection result.
- The Codex `/api/sessions` boundary test now asserts:
  - `AgentKind == "codex"`;
  - `AgentStatus == "waiting_approval"`.

That is the right direction and materially improves the A6 backend slice. However, A6 should not be accepted as complete yet because one A5 product-boundary field remains unasserted for Codex.

## Verification performed

Commands run:

```bash
git show --stat --oneline --no-renames HEAD
git diff --check f42664e..HEAD
gofmt -l companion-daemon/internal/agent/bridge.go companion-daemon/internal/term/codex_boundary_test.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-2-go-cache go test ./internal/agent -run 'TestCodex|TestClaude|TestMock' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-2-go-cache go test ./internal/term -run 'TestCodexLog_ProductionEventsPath|TestCodexParser|TestClaudeLog_ProductionEventsPath|TestClaudeDegraded_MalformedLog' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-2-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-2-go-cache go test ./...
cd mobile && ./node_modules/.bin/tsc --noEmit
rg -n "if .*agent|agentKind|agentStatus|claude|codex|gpt|antigravity|gemini" mobile --glob '!node_modules' --glob '!dist' --glob '!build'
```

Results:

- `git diff --check`: passed.
- `gofmt -l`: returned no files.
- Agent contract tests: passed.
- Codex/Claude production boundary tests: passed.
- `go vet ./...`: passed.
- Sandboxed `go test ./...`: failed because `httptest` could not bind a local port inside the sandbox.
- Authorized external `go test ./...`: passed.
- Mobile TypeScript compile: passed.
- Mobile name-branch grep: no source matches; only `mobile/package-lock.json` contained an incidental hash substring.

Observed targeted boundary output:

```text
codex: events=4, agentKind=codex, state=waiting
```

## Accepted improvements

### 1. Production detector bridge is now composite

`TermAgentDetector` no longer wraps only `ClaudeDetector`. It now contains:

```go
claude *ClaudeDetector
codex  *CodexDetector
```

and chooses the highest-confidence result.

This addresses the prior blocker where Codex product detection was effectively proven through ClaudeDetector behavior.

### 2. Codex identity is now asserted at the product boundary

`TestCodexLog_ProductionEventsPath` now fails if:

```go
s.AgentKind != "codex"
```

This addresses the prior issue where `agentKind=codex` was only logged.

### 3. Codex parser-derived approval status is now asserted

`TestCodexLog_ProductionEventsPath` now fails if:

```go
s.AgentStatus != "waiting_approval"
```

This proves the important A5 reuse rule for Codex:

```text
Codex log → common approval event → parser-derived AgentStatus → /api/sessions
```

### 4. Claude A5 regression still passes

The targeted term run included:

- `TestClaudeLog_ProductionEventsPath`
- `TestClaudeDegraded_MalformedLog`

Both passed.

## Remaining blocker

### Blocking issue — Codex `AgentConfidence` is still not asserted at `/api/sessions`

A5 accepted the detection bridge as a product-boundary contract carrying:

- `AgentKind`
- `AgentConfidence`

Claude's production boundary test asserts:

```go
if s.AgentConfidence < 0.5 {
    t.Errorf("confidence %.2f < 0.5", s.AgentConfidence)
}
```

Codex should have the same assertion. Without it, a future regression could return:

```text
agentKind=codex
agentStatus=waiting_approval
agentConfidence=0
```

and the Codex product-boundary test would still pass. That leaves one piece of the A5 detection bridge unprotected for A6.

## Required next commit

Minimum change:

```go
if s.AgentConfidence < 0.5 {
    t.Errorf("confidence %.2f < 0.5", s.AgentConfidence)
}
```

Add this inside `TestCodexLog_ProductionEventsPath`, matching the Claude boundary test.

Then rerun:

```bash
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-3-go-cache go test ./internal/term -run 'TestCodexLog_ProductionEventsPath|TestClaudeLog_ProductionEventsPath|TestClaudeDegraded_MalformedLog' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-3-go-cache go test ./internal/agent -run 'TestCodex|TestClaude|TestMock' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-3-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-3-go-cache go test ./...
cd mobile && ./node_modules/.bin/tsc --noEmit
```

## Final decision

`e2c225288` is accepted as a backend fix.

It is not accepted as Phase A6 completion until the Codex product-boundary test also asserts `AgentConfidence >= 0.5`.
