# Agent Phase A7 Implementation Review 4 — Status Formatter and Degraded Visualization

Reviewed commit:

- `e63725101` / local short hash `e637251` — `fix: Phase A7 — status formatter, degraded viz, unknown types, missing fields`

Verdict: **REJECT FOR A7 COMPLETION / ACCEPT MAJOR A7 PROGRESS**

This is the strongest A7 implementation so far. It adds visible `agentStatus` rendering, a degraded label, common event type mapping, and missing optional field backend coverage.

However, A7 should still not be accepted as complete. The implementation is close, but it leaves one common activity surface without `agentKind` propagation and still lacks explicit mobile-side fixture/test proof for the exact unknown/future cases required by the A7 gate.

## Verification performed

Commands run:

```bash
git diff --check b0fb76f..HEAD
cd mobile && ./node_modules/.bin/tsc --noEmit
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-4-go-cache go test ./internal/term -run 'TestAgnostic|TestCodexLog_ProductionEventsPath|TestClaudeLog_ProductionEventsPath|TestClaudeDegraded_MalformedLog|TestClaudeDetection_FalsePositive' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-4-go-cache go test ./internal/agent -run 'TestCodex|TestClaude|TestMock' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-4-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-4-go-cache go test ./...
rg -n "agentKind|agentStatus|agentConfidence|degraded|blocked_by_future_runtime|future_agent|future_event_type|formatAgent|isAgent|GlobalFeed|EventBubble" mobile/src companion-daemon/internal/term/agent_agnostic_test.go --glob '*.ts' --glob '*.tsx' --glob '*.go'
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
- Mobile vendor-specific branch/literal grep found no vendor branch. It only found `agentKind === 'unknown'`, which is an allowed fallback branch, not a vendor branch.

## Accepted improvements

### 1. `agentStatus` is now user-visible

`AgentCard` now includes `formatAgentStatus` and renders `session.agentStatus` when present.

Known statuses now map to neutral status labels:

- `waiting_approval` → `Awaiting Approval`
- `thinking`
- `working`
- `waiting_input`
- `degraded`
- `unknown`
- `idle`

Unknown statuses fall through as raw text. That is acceptable as a graceful fallback, though a future polish pass may want humanized formatting.

### 2. Degraded state is now visibly distinct

`agentStatus === "degraded"` renders as:

```text
⚠️ Degraded
```

This materially improves A7 because degraded is no longer silently collapsed into no activity or a generic terminal state.

### 3. Common event names are recognized by mobile EventBubble

`EventBubble` now recognizes A5/A6 common event names:

- `user_message`
- `tool_call_started`
- `approval_requested`
- `tool_call_finished`
- `assistant_message`
- `thinking`
- `agent_started`

This is the right direction for Agent Adapter common event UX.

### 4. Missing optional backend agent fields are covered

`TestAgnostic_MissingOptionalFields` proves backend JSON omits unset agent fields and round-trips safely.

This supports schema evolution on the backend DTO side.

## Remaining blockers

### Blocking issue 1 — GlobalFeed still drops `agentKind`

`FeedScreen` passes:

```tsx
agentKind={sessionData?.agentKind}
```

but `GlobalFeedScreen` still flattens events with only:

```ts
runnerId: s.runner,
runnerColor: s.runnerColor,
```

and renders:

```tsx
<EventBubble event={item} runnerId={item.runnerId} runnerColor={item.runnerColor} />
```

So one common activity surface still cannot render a future agent kind even though the source `SessionTelemetry` has it.

A7 is specifically about product/mobile common behavior. If `GlobalFeedScreen` is an activity surface, it must either:

1. propagate `agentKind` into each flattened event and pass it to `EventBubble`; or
2. be explicitly documented out of A7 scope.

The simpler fix is to add `agentKind?: string` to the flattened event type and pass it through.

### Blocking issue 2 — no explicit mobile fixture/test for required A7 cases

The implementation passes TypeScript compile and can be inspected manually, but A7 requested proof for:

- `agentKind: "future_agent"` graceful rendering;
- `agentStatus: "blocked_by_future_runtime"` graceful rendering;
- `agentStatus: "degraded"` distinct rendering;
- missing optional mobile fields;
- unknown event type fallback.

The backend tests cover DTO serialization, but no mobile-side fixture/test exists for these rendering cases.

This matters because A7 is a UX/product gate. At minimum, add a lightweight TypeScript-only compatibility fixture/test file or exported pure helper tests. If the project does not have a test runner, add a documented compile-time fixture module that instantiates the typed cases and exercises exported formatting helpers.

### Blocking issue 3 — formatting helpers are not exported or directly testable

`formatAgentStatus` is local to `AgentCard.tsx`. That makes it hard to test the exact status compatibility contract without rendering the component.

For A7, prefer moving vendor-neutral helpers to a small module, for example:

```text
mobile/src/lib/agentDisplay.ts
```

with pure functions:

```ts
formatAgentLabel(agentKind?: string): string
formatAgentStatus(agentStatus?: string, fallbackState?: string): string
isAgentDegraded(agentStatus?: string): boolean
```

Then a compile-time or unit test can directly cover future/unknown values.

This is not about adding abstraction for its own sake. It makes the A7 compatibility contract testable.

## Required next commit

Minimum acceptable next commit:

1. Wire `agentKind` through `GlobalFeedScreen`:

```ts
eventsWithRunner.push({
  ...e,
  runnerId: s.runner,
  runnerColor: s.runnerColor,
  agentKind: s.agentKind,
});
```

and:

```tsx
<EventBubble
  event={item}
  runnerId={item.runnerId}
  runnerColor={item.runnerColor}
  agentKind={item.agentKind}
/>
```

2. Add explicit mobile compatibility proof for:

- `future_agent`;
- `blocked_by_future_runtime`;
- `degraded`;
- missing optional fields;
- `future_event_type`.

3. Prefer exporting pure display helpers so this proof is not only manual inspection.

4. Keep these checks passing:

```bash
cd mobile && ./node_modules/.bin/tsc --noEmit
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-5-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-5-go-cache go test ./...
rg -n "agentKind.*===|agentKind.*==|switch.*agentKind|case ['\"]claude|case ['\"]codex|case ['\"]gemini" mobile/src --glob '*.ts' --glob '*.tsx'
```

## Final decision

`e63725101` is accepted as major A7 progress.

It is rejected as Phase A7 completion until `GlobalFeedScreen` preserves `agentKind` and the mobile compatibility proof covers the exact future/unknown/degraded cases required by the A7 gate.
