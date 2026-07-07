# Agent Phase A7 Implementation Review — Agent Agnostic UX/Product Gate

Reviewed commit:

- `9be8a9037` / local short hash `9be8a90` — `feat: Phase A7 — Agent Agnostic UX golden tests`

Verdict: **REJECT FOR A7 COMPLETION / ACCEPTABLE BACKEND TEST PROGRESS**

The commit adds useful backend serialization/terminal-survival tests, but it does not complete Phase A7.

A7 is not a backend-only phase. It is the product gate proving that future `AgentKind` and future `AgentStatus` values render safely without vendor-specific mobile behavior. The current commit does not modify or test the mobile rendering/schema boundary, and mobile still does not model the agent fields required by the A7 acceptance criteria.

## Verification performed

Commands run:

```bash
git diff --check 6a7aec3..HEAD
gofmt -l companion-daemon/internal/term/agent_agnostic_test.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-1-go-cache go test ./internal/term -run 'TestAgnostic|TestCodexLog_ProductionEventsPath|TestClaudeLog_ProductionEventsPath|TestClaudeDegraded_MalformedLog|TestClaudeDetection_FalsePositive' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-1-go-cache go test ./internal/agent -run 'TestCodex|TestClaude|TestMock' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-1-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-1-go-cache go test ./...
cd mobile && ./node_modules/.bin/tsc --noEmit
rg -n "agentKind|agentStatus|agentConfidence" mobile/src --glob '*.ts' --glob '*.tsx'
rg -n "agentKind.*===|agentKind.*==|switch.*agentKind|case ['\"]claude|case ['\"]codex|case ['\"]gemini|claude|codex|gpt|gemini|qwen|responses|antigravity" mobile/src --glob '*.ts' --glob '*.tsx'
rg -n "Claude|Codex|claude|codex" mobile/src --glob '*.ts' --glob '*.tsx'
```

Results:

- `git diff --check`: passed.
- `gofmt -l`: returned no files.
- Targeted A7/A6/A5 term tests: passed.
- Agent contract tests: passed.
- `go vet ./...`: passed.
- Sandboxed `go test ./...`: failed only because `httptest` could not bind a local port in the sandbox.
- Authorized external `go test ./...`: passed.
- Mobile TypeScript compile: passed.
- Mobile `agentKind|agentStatus|agentConfidence` grep: no matches.
- Mobile vendor branch grep: no lower-case branch matches.
- Mobile literal grep found an existing vendor-specific UI string:
  - `mobile/src/components/EventBubble.tsx:72` — `🤖 Claude`

## What this commit proves

### 1. Backend DTO can carry unknown agent strings

`TestAgnostic_UnknownAgentKind` proves `SessionTelemetry.AgentKind` can JSON round-trip unknown string values such as:

- `gemini`
- `qwen`
- `future`

This is useful backend DTO coverage.

### 2. Backend DTO can carry unknown status strings

`TestAgnostic_UnknownStatus` proves `SessionTelemetry.AgentStatus` can be marshaled with values such as:

- `degraded`
- `custom_state`
- `reconnecting`

This is also useful backend DTO coverage.

### 3. Terminal listing survives when no detector is present

`TestAgnostic_TerminalUnaffectedByAgentLayer` proves a session still appears through `/api/sessions` when the handler has no agent detector.

That preserves the important rule:

```text
agent layer failure must not hide terminal sessions
```

## Why A7 is rejected

### Blocking issue 1 — no mobile schema proof exists

A7 requires mobile rendering/schema proof for:

- unknown `AgentKind`;
- unknown `AgentStatus`;
- degraded state;
- missing optional agent fields;
- unknown event type.

But mobile `SessionTelemetry` currently has no fields for:

```ts
agentKind
agentStatus
agentConfidence
```

The verification grep returned no matches:

```text
rg -n "agentKind|agentStatus|agentConfidence" mobile/src --glob '*.ts' --glob '*.tsx'
```

No matches means the mobile app is not yet proving graceful rendering. It is ignoring the A5/A6 agent fields entirely.

This is not sufficient for A7. A7 must prove product/mobile behavior, not only backend JSON marshaling.

### Blocking issue 2 — degraded visualization is not implemented or tested

The new `TestAgnostic_DegradedState` builds a `SessionTelemetry` with:

```go
AgentKind: "unknown"
AgentStatus: "unknown"
AgentConfidence: 0.1
```

and verifies JSON round-trip plus basic terminal survival.

That does not prove degraded visualization. A7 requires the user-visible distinction among:

- no session;
- terminal unavailable;
- no activity;
- parser/agent degraded.

The mobile UI has no renderer/test showing degraded copy, degraded badge, or degraded fallback behavior.

### Blocking issue 3 — existing mobile UI still has a vendor-specific label

The current mobile event bubble still renders a generic bot message as:

```tsx
'🤖 Claude'
```

Location:

```text
mobile/src/components/EventBubble.tsx:72
```

A7 explicitly says:

```text
Status Visualization is about current state, not agent vendor.
```

Hardcoding `Claude` in a common event component is incompatible with Agent Agnostic UX. The fix does not need to add vendor-specific labels for Codex/Gemini/etc. It should use neutral copy such as `Agent`, `Assistant`, or a safe display label derived without behavior branching.

### Blocking issue 4 — unknown event type is not proven at the mobile boundary

A7 requires unknown event type graceful rendering. The new tests do not include a mobile fixture or renderer test for a future event type.

Backend JSON round-trip is not enough. The risk is in UI code that decides icons/copy/layout based on event type.

### Blocking issue 5 — A7 fixture values do not match the documented gate

The gate requested examples like:

- `future_agent`
- `blocked_by_future_runtime`

The implementation used:

- `future`
- `custom_state`
- `reconnecting`

This is not a fatal issue by itself, but using the documented strings would make the test intent clearer and avoid ambiguity with existing/future runtime states.

## Required next commit

Minimum acceptable A7 completion should include:

1. Add mobile `SessionTelemetry` optional fields:
   - `agentKind?: string`
   - `agentStatus?: string`
   - `agentConfidence?: number`
2. Add vendor-neutral helper/rendering functions, for example:
   - `formatAgentLabel(agentKind?: string): string`
   - `formatAgentStatus(agentStatus?: string, state?: string): ...`
   - `isDegradedAgentState(agentStatus?: string): boolean`
3. Replace common UI vendor hardcoding:
   - remove `🤖 Claude` from `EventBubble`;
   - use neutral copy or a safe display label.
4. Add mobile tests or typed fixture tests proving:
   - unknown `agentKind: "future_agent"` renders safely;
   - unknown `agentStatus: "blocked_by_future_runtime"` renders safely;
   - `agentStatus: "degraded"` renders distinctly from no session/no activity;
   - missing optional fields do not crash;
   - unknown event type renders as neutral/fallback event.
5. Add or preserve automated grep/test proving no mobile vendor-specific behavior branch:

```bash
rg -n "agentKind.*===|agentKind.*==|switch.*agentKind|case ['\"]claude|case ['\"]codex|case ['\"]gemini" mobile/src --glob '*.ts' --glob '*.tsx'
```

6. Keep backend A5/A6 regressions passing:
   - Claude boundary;
   - Codex boundary;
   - malformed log survival;
   - false-positive prevention;
   - agent contract tests.

## Final decision

`9be8a9037` is accepted as backend DTO/test progress.

It is rejected as Phase A7 completion because it does not prove mobile Agent Agnostic UX, does not visualize degraded state, and leaves vendor-specific common UI copy in place.
