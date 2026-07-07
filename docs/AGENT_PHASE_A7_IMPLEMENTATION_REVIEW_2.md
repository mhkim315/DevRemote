# Agent Phase A7 Implementation Review 2 — EventBubble Vendor Label Fix

Reviewed commit:

- `153729f52` / local short hash `153729f` — `fix: Phase A7 — remove hardcoded Claude from EventBubble`

Verdict: **REJECT FOR A7 COMPLETION / ACCEPT SMALL UX FIX**

This commit fixes one valid A7 issue from the previous review: the common `EventBubble` no longer hardcodes `🤖 Claude`.

However, Phase A7 is still not complete. The commit changes only one line in mobile UI and does not add the required mobile schema/rendering proof for agent fields, unknown status, degraded visualization, or unknown event types.

## Verification performed

Commands run:

```bash
git diff --check bec15fc..HEAD
cd mobile && ./node_modules/.bin/tsc --noEmit
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-2-go-cache go test ./internal/term -run 'TestAgnostic|TestCodexLog_ProductionEventsPath|TestClaudeLog_ProductionEventsPath|TestClaudeDegraded_MalformedLog|TestClaudeDetection_FalsePositive' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-2-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-2-go-cache go test ./...
rg -n "agentKind|agentStatus|agentConfidence" mobile/src --glob '*.ts' --glob '*.tsx'
rg -n "Claude|Codex|claude|codex|agentKind.*===|agentKind.*==|switch.*agentKind|case ['\"]claude|case ['\"]codex|case ['\"]gemini" mobile/src --glob '*.ts' --glob '*.tsx'
```

Results:

- `git diff --check`: passed.
- Mobile TypeScript compile: passed.
- Targeted A7/A6/A5 term tests: passed.
- `go vet ./...`: passed.
- Sandboxed `go test ./...`: failed only because `httptest` could not bind a local port in the sandbox.
- Authorized external `go test ./...`: passed.
- Mobile vendor literal/branch grep: no matches after this commit.
- Mobile `agentKind|agentStatus|agentConfidence` grep: still no matches.

## Accepted improvement

The previous common UI hardcoding:

```tsx
'🤖 Claude'
```

was replaced with:

```tsx
'🤖 ' + (runnerId || 'Agent')
```

This removes the explicit Claude label from the shared event bubble. That is directionally correct.

## Remaining blockers

### Blocking issue 1 — mobile still has no agent schema fields

The A7 gate requires mobile schema/rendering proof for:

- `agentKind`;
- `agentStatus`;
- `agentConfidence`.

But:

```bash
rg -n "agentKind|agentStatus|agentConfidence" mobile/src --glob '*.ts' --glob '*.tsx'
```

still returns no matches.

So mobile still does not model or render the A5/A6 agent product fields. That means unknown `AgentKind` and unknown `AgentStatus` graceful rendering is not proven.

### Blocking issue 2 — degraded visualization still does not exist

A7 requires the UI to distinguish:

- no session;
- terminal unavailable;
- no activity;
- parser/agent degraded.

This commit does not add:

- degraded badge/copy;
- degraded helper;
- degraded fixture;
- degraded rendering test.

Backend DTO tests alone are not enough for A7 because A7 is a product/UX gate.

### Blocking issue 3 — unknown event type mobile fallback still unproven

The current `EventBubble` still classifies only:

- `user`;
- `tool_use`;
- `file_edit`;
- `approval_request`;
- `tool_result`;
- `message`.

That is fine if unknown events fall back to a neutral bubble intentionally, but there is no explicit fixture/test proving this behavior. A7 requires unknown event type graceful rendering.

### Blocking issue 4 — `runnerId` is not `agentKind`

The replacement label uses:

```tsx
runnerId || 'Agent'
```

`runnerId` is the legacy/profile runner value used for animation/color, not the A5/A6 `agentKind` contract field.

This avoids the Claude hardcode, but it does not prove future `AgentKind` support. A7 should keep behavior vendor-neutral while adding explicit optional agent fields and safe formatting helpers.

## Required next commit

The next commit needs to address the product gate directly:

1. Add mobile optional fields to `SessionTelemetry`:

```ts
agentKind?: string;
agentStatus?: string;
agentConfidence?: number;
```

2. Add vendor-neutral formatting helpers:

```ts
formatAgentLabel(agentKind?: string): string
formatAgentStatus(agentStatus?: string, fallbackState?: string): string
isAgentDegraded(agentStatus?: string): boolean
```

3. Add typed mobile fixture/test coverage proving:

- `agentKind: "future_agent"` renders safely;
- `agentStatus: "blocked_by_future_runtime"` renders safely;
- `agentStatus: "degraded"` renders distinctly;
- missing optional fields render safely;
- unknown event type renders as neutral fallback.

4. Keep common UI free of vendor-specific behavior branch:

```bash
rg -n "agentKind.*===|agentKind.*==|switch.*agentKind|case ['\"]claude|case ['\"]codex|case ['\"]gemini" mobile/src --glob '*.ts' --glob '*.tsx'
```

5. Keep A5/A6 backend regressions passing.

## Final decision

`153729f52` is accepted as a small UX cleanup.

It is rejected as Phase A7 completion because mobile still does not model/render the agent fields, degraded visualization is absent, and unknown event type fallback is not explicitly proven.
