# Agent Phase A7 Implementation Review 6 — GlobalFeed Fix and Future Fixtures

Reviewed commit:

- `1092096f5` / local short hash `1092096` — `fix: Phase A7 — GlobalFeedScreen agentKind + future-proof fixtures`

Verdict: **REJECT FOR A7 COMPLETION / ACCEPT GLOBALFEED FIX**

This commit closes the biggest remaining product-surface blocker: `GlobalFeedScreen` now preserves `agentKind` and passes it to `EventBubble`.

However, Phase A7 is still not complete because the compatibility proof still does not cover the exact future status and unknown event cases required by the A7 gate.

## Verification performed

Commands run:

```bash
git diff --check bb699f8..HEAD
cd mobile && ./node_modules/.bin/tsc --noEmit
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-6-go-cache go test ./internal/term -run 'TestAgnostic|TestCodexLog_ProductionEventsPath|TestClaudeLog_ProductionEventsPath|TestClaudeDegraded_MalformedLog|TestClaudeDetection_FalsePositive' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-6-go-cache go test ./internal/agent -run 'TestCodex|TestClaude|TestMock' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-6-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-6-go-cache go test ./...
rg -n "future_agent|blocked_by_future_runtime|future_event_type|gemini|claude|codex|qwen|responses|antigravity|agentKind|agentStatus|agentConfidence" mobile/src --glob '*.ts' --glob '*.tsx'
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
- Vendor branch grep found no behavior branch. It found `qwen3-max` inside fixture data only.

## Accepted improvements

### 1. GlobalFeed now preserves `agentKind`

`GlobalFeedScreen` now flattens:

```ts
agentKind: s.agentKind
```

and passes:

```tsx
agentKind={item.agentKind}
```

to `EventBubble`.

This closes the previous common activity surface blocker.

### 2. Vendor fixture literals were reduced

The previous `claude` fixture literals were removed from `agentDisplay.ts`. Low-confidence/degraded fixtures now use future-like values.

## Remaining blockers

### Blocking issue 1 — `blocked_by_future_runtime` is not tested as a status

The A7 gate required unknown status graceful rendering, specifically including:

```text
blocked_by_future_runtime
```

The current fixture contains a property named `blocked_by_future_runtime`, but it calls:

```ts
formatAgentKind('qwen3-max')
```

That does not prove `formatAgentStatus('blocked_by_future_runtime')`.

Required proof:

```ts
blocked_by_future_runtime: formatAgentStatus('blocked_by_future_runtime') === 'blocked_by_future_runtime'
```

### Blocking issue 2 — `future_event_type` is still absent

The A7 gate also required unknown event type graceful rendering.

The current helper fixture has:

```ts
unknown_event_label: formatAgentKind() === 'Agent'
```

That does not create or type-check an event with:

```ts
type: 'future_event_type'
```

Required proof should instantiate a typed event shape, for example:

```ts
type AgentDisplayFixtureEvent = {
  id: string;
  session: string;
  type: string;
  summary: string;
  detail: string;
  timestamp: string;
};

const futureEvent: AgentDisplayFixtureEvent = {
  id: 'fixture-future-event',
  session: 'fixture',
  type: 'future_event_type',
  summary: 'Future event',
  detail: 'Future event detail',
  timestamp: new Date(0).toISOString(),
};
```

This does not require a test runner. It gives a compile-time compatibility proof for the event shape.

### Blocking issue 3 — fixture value name mismatch

The previous review asked for:

```ts
formatAgentKind('future_agent')
```

The current fixture uses:

```ts
formatAgentKind('future_backend_v2')
```

This is not as important as the two blockers above, but using the documented `future_agent` value makes the acceptance proof unambiguous.

## Required next commit

Minimum acceptable next commit:

1. Change the future agent fixture to use the documented value:

```ts
future_agent: formatAgentKind('future_agent') === 'Future_agent'
```

2. Add the missing future status proof:

```ts
blocked_by_future_runtime:
  formatAgentStatus('blocked_by_future_runtime') === 'blocked_by_future_runtime'
```

3. Add a typed `future_event_type` fixture.

4. Keep:

```bash
cd mobile && ./node_modules/.bin/tsc --noEmit
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-7-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-7-go-cache go test ./...
```

passing.

## Final decision

`1092096f5` is accepted as the GlobalFeed agentKind fix.

It is rejected as Phase A7 completion until the mobile compatibility proof explicitly covers `blocked_by_future_runtime` as a status and `future_event_type` as an event shape.
