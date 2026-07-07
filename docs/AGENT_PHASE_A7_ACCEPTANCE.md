# Agent Phase A7 Acceptance — Agent Agnostic UX / Product Gate

Accepted implementation commits:

- `9be8a90` — `feat: Phase A7 — Agent Agnostic UX golden tests`
- `153729f` — `fix: Phase A7 — remove hardcoded Claude from EventBubble`
- `0772deb` — `fix: Phase A7 — mobile agent schema + agentKind wiring`
- `e637251` — `fix: Phase A7 — status formatter, degraded viz, unknown types, missing fields`
- `7aacb82` — `feat: Phase A7 — agentDisplay helper + compile-time fixtures`
- `1092096` — `fix: Phase A7 — GlobalFeedScreen agentKind + future-proof fixtures`
- `5948f5e` — `fix: Phase A7 — blocked_by_future_runtime as status, future_event_type`

Verifier review history:

- `bec15fc` — first A7 review, rejected backend-only proof
- `a5abb11` — EventBubble vendor label review
- `b0fb76f` — mobile schema wiring review
- `61446be` — status/degraded visualization review
- `bb699f8` — compatibility helper review
- `b58ded4` — GlobalFeed/future fixture review
- this document — final A7 acceptance

Verdict: **ACCEPT — Phase A7 Agent Agnostic UX / Product Gate**

A7 is accepted as the phase that proves the product/mobile boundary no longer depends on Claude/Codex-specific behavior for agent identity and status display.

This does not mean the full Agent Adapter Layer is release-complete. Approval UX, deeper diagnostics, and future real agent backend slices remain separate phases.

## What A7 proves

### 1. Unknown `AgentKind` is safe

Mobile `SessionTelemetry` now carries:

```ts
agentKind?: string;
agentStatus?: string;
agentConfidence?: number;
```

`agentKind` is propagated through both activity surfaces:

- `FeedScreen`
- `GlobalFeedScreen`

and reaches `EventBubble`.

The display helper handles missing/unknown/future kinds:

```ts
formatAgentKind(kind?: string): string
```

The compatibility fixture includes:

```ts
formatAgentKind('future_agent')
formatAgentKind('unknown')
formatAgentKind(undefined)
```

### 2. Unknown `AgentStatus` is safe

`AgentCard` renders `agentStatus` through:

```ts
formatAgentStatus(status?: string): string
```

Known statuses get neutral labels. Future statuses pass through instead of crashing or being hidden.

The compatibility fixture now explicitly covers:

```ts
formatAgentStatus('blocked_by_future_runtime')
```

### 3. Degraded state is visible

`agentStatus === "degraded"` renders as a distinct degraded label.

Low-confidence or unknown agent detection is also styled as degraded/low-confidence through:

```ts
isDegraded(confidence?: number, kind?: string): boolean
```

This satisfies the A7 requirement that degraded agent state not be silently collapsed into:

- no session;
- no activity;
- terminal unavailable.

### 4. Schema evolution is safe

The agent fields are optional on mobile.

Backend coverage includes missing optional agent fields:

- no `agentKind`;
- no `agentStatus`;
- no `agentConfidence`.

Mobile compile-time compatibility fixtures cover missing agent kind and future status values.

### 5. Unknown event types have a fallback path

`EventBubble` recognizes current common event names:

- `user_message`
- `assistant_message`
- `thinking`
- `agent_started`
- `tool_call_started`
- `tool_call_finished`
- `approval_requested`

Events outside the known set fall through to a neutral bot bubble instead of a vendor-specific branch.

The A7 fixture now includes the required `future_event_type` marker. This is a lightweight compile-time proof, not a runtime renderer test. It is acceptable for A7 because the current renderer already has a neutral fallback path, but a future mobile test harness should convert this into an executable component test.

### 6. Mobile vendor-specific branching is not present

The verifier search found no common mobile behavior branch on vendor names such as Claude, Codex, Gemini, Qwen, Responses, or Antigravity.

The remaining agent-specific strings are in backend parser/detector/test contexts, not common mobile UX behavior.

## Verification performed

Commands run:

```bash
git diff --check b58ded4..HEAD
cd mobile && ./node_modules/.bin/tsc --noEmit
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-7-go-cache go test ./internal/term -run 'TestAgnostic|TestCodexLog_ProductionEventsPath|TestClaudeLog_ProductionEventsPath|TestClaudeDegraded_MalformedLog|TestClaudeDetection_FalsePositive' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-7-go-cache go test ./internal/agent -run 'TestCodex|TestClaude|TestMock' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-7-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-7-go-cache go test ./...
rg -n "future_agent|blocked_by_future_runtime|future_event_type|future_backend_v2|qwen3-max|gemini|claude|codex|responses|antigravity|agentKind|agentStatus|agentConfidence" mobile/src --glob '*.ts' --glob '*.tsx'
rg -n "agentKind.*===|agentKind.*==|switch.*agentKind|case ['\"]claude|case ['\"]codex|case ['\"]gemini|Claude|Codex|claude|codex|gemini|qwen|responses|antigravity" mobile/src --glob '*.ts' --glob '*.tsx'
```

Results:

- `git diff --check`: passed.
- Mobile TypeScript compile: passed.
- Targeted A7/A6/A5 term tests: passed.
- Agent contract tests: passed.
- `go vet ./...`: passed.
- Sandboxed `go test ./...`: failed only because `httptest` could not bind a local port in the sandbox.
- Authorized external `go test ./...`: passed.
- Mobile vendor branch grep: clean.

## Remaining follow-ups after A7

These are not A7 blockers:

- replace compile-time compatibility fixtures with executable mobile component/helper tests when a mobile test runner exists;
- polish future status label formatting if raw status strings are too technical;
- continue approval UX work in the next phase;
- continue diagnostics/doctor work in the later diagnostics phase.

## Next phase permission

Next phase is allowed.

Recommended next phase from the current plan:

```text
Phase A8 — Third Agent Backend Slice
```

A8 should add a real third agent only if a real redacted fixture exists. It must preserve the A7 guarantee:

```text
Mobile and common product UX must not add vendor-specific behavior branches.
```
