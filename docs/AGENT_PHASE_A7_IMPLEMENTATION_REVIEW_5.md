# Agent Phase A7 Implementation Review 5 — AgentDisplay Helper and Compile-Time Fixtures

Reviewed commit:

- `7aacb8213` / local short hash `7aacb82` — `feat: Phase A7 — agentDisplay helper + compile-time fixtures`

Verdict: **REJECT FOR A7 COMPLETION / ACCEPT HELPER EXTRACTION PROGRESS**

This commit improves the structure by extracting vendor-neutral display helpers into `mobile/src/lib/agentDisplay.ts`. That is a useful step toward making A7 testable.

However, it does not close the two blockers from the previous review:

1. `GlobalFeedScreen` still drops `agentKind`.
2. The mobile compatibility proof does not cover the exact future/unknown/degraded cases required by the A7 gate.

## Verification performed

Commands run:

```bash
git diff --check 61446be..HEAD
cd mobile && ./node_modules/.bin/tsc --noEmit
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-5-go-cache go test ./internal/term -run 'TestAgnostic|TestCodexLog_ProductionEventsPath|TestClaudeLog_ProductionEventsPath|TestClaudeDegraded_MalformedLog|TestClaudeDetection_FalsePositive' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-5-go-cache go test ./internal/agent -run 'TestCodex|TestClaude|TestMock' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-5-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-5-go-cache go test ./...
rg -n "future_agent|blocked_by_future_runtime|future_event_type|formatAgentLabel|formatAgentStatus|isAgentDegraded|agentKind|agentStatus|agentConfidence" mobile/src --glob '*.ts' --glob '*.tsx'
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
- Vendor branch grep found no behavior branch, but it did find vendor literals inside compile-time fixtures:
  - `mobile/src/lib/agentDisplay.ts` uses `gemini` and `claude`.

## Accepted improvements

### 1. Display helpers are now extracted

New file:

```text
mobile/src/lib/agentDisplay.ts
```

It exports:

```ts
formatAgentKind(kind?: string): string
formatAgentStatus(status?: string): string
isDegraded(confidence?: number, kind?: string): boolean
```

This makes the display logic more testable than when `formatAgentStatus` was local to `AgentCard.tsx`.

### 2. `AgentCard` uses the shared helper

`AgentCard` now imports the helper functions instead of owning local status formatting.

This is the right direction for keeping agent/status display behavior centralized.

### 3. Compile-time fixtures exist

`agentDisplay.ts` now includes compile-time fixture expressions. That is better than no mobile compatibility proof at all.

## Remaining blockers

### Blocking issue 1 — `GlobalFeedScreen` still drops `agentKind`

This was the first required next step in the previous review. It is still unresolved.

`GlobalFeedScreen` still flattens events with:

```ts
runnerId: s.runner,
runnerColor: s.runnerColor,
```

and renders:

```tsx
<EventBubble event={item} runnerId={item.runnerId} runnerColor={item.runnerColor} />
```

It does not propagate:

```ts
agentKind: s.agentKind
```

and does not pass:

```tsx
agentKind={item.agentKind}
```

Therefore one common activity surface still loses the future agent kind. A7 cannot be accepted while this path remains inconsistent.

### Blocking issue 2 — required future/unknown fixture values are not covered

The A7 gate explicitly called out:

- `future_agent`
- `blocked_by_future_runtime`
- `future_event_type`

The new compile-time fixture uses:

```ts
formatAgentKind('gemini')
formatAgentStatus('custom_state')
isDegraded(0.3, 'claude')
```

This is weaker than the requested proof:

- `gemini` is a known planned vendor example, not a neutral future kind.
- `custom_state` is generic, but the gate requested `blocked_by_future_runtime`.
- `future_event_type` is not represented at all.
- `claude` in mobile fixture data is unnecessary and weakens the vendor-agnostic signal.

The fixture should use neutral future values:

```ts
formatAgentKind('future_agent')
formatAgentStatus('blocked_by_future_runtime')
```

and include a typed unknown event fixture for `future_event_type`.

### Blocking issue 3 — helper names do not match the reviewed contract

The previous review suggested:

```ts
formatAgentLabel
formatAgentStatus
isAgentDegraded
```

The implementation uses:

```ts
formatAgentKind
formatAgentStatus
isDegraded
```

This is not a blocker by itself, but be consistent in docs/tests. The important part is that the helpers are vendor-neutral and directly exercised by future/unknown fixtures.

### Blocking issue 4 — compile-time fixtures do not assert failure

The fixture object only computes booleans and is discarded:

```ts
const _fixtures = { ... };
void _fixtures;
```

This proves the code type-checks, but it does not fail if a helper returns the wrong value.

If no test runner is available, a stronger compile-time pattern is to assign expected literal types, or to export a `AGENT_DISPLAY_COMPATIBILITY_FIXTURES` object that can be inspected and imported. Best is to add a lightweight test runner later, but A7 can accept a compile-time fixture if it is explicit and uses the required values.

## Required next commit

Minimum acceptable next commit:

1. Wire `agentKind` through `GlobalFeedScreen`:

```ts
const eventsWithRunner: (AgentEvent & {
  runnerId?: string;
  runnerColor?: string;
  agentKind?: string;
})[] = [];

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

2. Replace compile-time fixtures with the exact A7 future values:

```ts
formatAgentKind('future_agent')
formatAgentStatus('blocked_by_future_runtime')
```

3. Add an explicit typed unknown event fixture:

```ts
const futureEvent: AgentEvent = {
  id: 'fixture-future-event',
  session: 'fixture',
  type: 'future_event_type',
  summary: 'Future event',
  detail: 'Future event detail',
  timestamp: new Date(0).toISOString(),
};
```

4. Avoid vendor literals in mobile compatibility fixtures unless the fixture is specifically proving display labels for known examples. A7's proof should use neutral future values.

5. Keep the existing verification commands passing.

## Final decision

`7aacb8213` is accepted as helper extraction and compile-time fixture progress.

It is rejected as Phase A7 completion because `GlobalFeedScreen` still loses `agentKind`, and the mobile compatibility proof does not cover the exact future/unknown event/status cases required by the A7 gate.
