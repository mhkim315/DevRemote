# Agent Phase A5b Degraded Follow-up Review — Malformed Log Survival

Reviewed commit: `0a9e6a46f`

Verdict: **ACCEPT SURVIVAL GUARD / DEGRADED STATUS UX STILL FOLLOW-UP**

This commit adds `TestClaudeDegraded_MalformedLog`, which verifies that malformed Claude log input does not hide or fail the terminal session.

That is a useful and accepted regression guard.

It does **not** prove full degraded product-boundary behavior. The test explicitly permits:

```go
// Degraded may show as idle/unknown — that's correct.
```

and the observed test output was:

```text
malformed: events=1, agentKind=claude, state=thinking
```

So the current proof is:

```text
malformed log
→ parser skips bad records
→ valid records can still flow
→ /api/sessions still returns the terminal session
```

It is not:

```text
malformed log
→ agentStatus=degraded
→ diagnostics exposed safely
→ mobile distinguishes degraded from no activity
```

## Verification performed

Commands run:

```bash
git diff --check 9d98197..HEAD
gofmt -l companion-daemon/internal/term/claude_boundary_test.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b14-go-cache go test ./internal/term -run 'TestClaudeLog_ProductionEventsPath|TestClaudeDegraded_MalformedLog|TestClaudeDetection|TestEvaluateState|TestClaudeParser' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b14-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b14-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b14-go-cache go test ./...
cd mobile && node_modules/.bin/tsc --noEmit
rg -n "claude|codex|gpt|antigravity|gemini|agentStatus|agentKind|approval_requested|waiting_approval|degraded" mobile/src -g '*.tsx' -g '*.ts'
```

Notes:

- `git diff --check` passed.
- `gofmt -l` returned no files.
- Targeted A5b term tests passed.
- `internal/agent` contract tests passed.
- `go vet ./...` passed.
- Mobile TypeScript compile passed.
- Mobile agent-name/status grep returned no matches.
- Sandboxed full `go test ./...` failed because `httptest` could not bind a local port.
- Authorized full `go test ./...` passed.

## What is accepted

### 1. Malformed log survival

`TestClaudeDegraded_MalformedLog` asserts:

- `/api/sessions` returns HTTP 200;
- `tmux:claude-session` is still present;
- malformed log lines do not hide the terminal session.

This satisfies the minimum isolation guard:

```text
parser/log corruption must not become terminal session failure
```

### 2. Mobile compile/name-branch smoke

Mobile TypeScript compile passed.

The mobile grep for direct agent-name/status branch terms returned no matches in `mobile/src`, which supports the claim that this commit did not introduce a mobile agent-name behavior branch.

## What remains follow-up

### 1. Degraded status and diagnostics

There is still no product-boundary field proving:

- `agentStatus=degraded`;
- parser diagnostics;
- safe redacted diagnostic text;
- degraded vs no activity distinction.

That can be a later diagnostic/UX phase, but it should not be described as complete degraded boundary behavior.

### 2. Mobile UX rendering

Mobile compile and no-name-branch smoke are useful, but there is still no mobile rendering/schema test for:

- `agentStatus=waiting_approval`;
- `approval_requested`;
- degraded/unknown display states.

This remains product UX follow-up.

## Decision

With `0c7c5f47b` plus `0a9e6a46f`, Phase A5b backend core is accepted with an additional malformed-log survival guard.

The accepted scope is:

```text
Claude parser/event/status backend core
plus malformed log does not break terminal session listing
```

The remaining scope is:

```text
degraded status/diagnostics UX
mobile rendering/schema proof
```

A6 may proceed for backend parser expansion if these follow-ups remain tracked explicitly and A6 reuses the accepted common event/status path.
