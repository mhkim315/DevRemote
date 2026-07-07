# Agent Phase A7 Implementation Review 3 — Mobile AgentKind Wiring

Reviewed commit:

- `0772debac` / local short hash `0772deb` — `fix: Phase A7 — mobile agent schema + agentKind wiring`

Verdict: **REJECT FOR A7 COMPLETION / ACCEPT PARTIAL MOBILE SCHEMA PROGRESS**

This commit makes real progress compared to the previous A7 attempts:

- mobile `SessionTelemetry` now declares optional `agentKind`, `agentStatus`, and `agentConfidence`;
- `FeedScreen` passes `sessionData?.agentKind` into `EventBubble`;
- `EventBubble` uses `agentKind` before falling back to `runnerId` or `Agent`;
- mobile TypeScript compile passes.

However, A7 is still not complete. The change proves only minimal `agentKind` wiring in one activity path. It does not prove unknown status rendering, degraded visualization, missing-field compatibility, or unknown event fallback at the mobile boundary.

## Verification performed

Commands run:

```bash
git diff --check a5abb11..HEAD
cd mobile && ./node_modules/.bin/tsc --noEmit
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-3-go-cache go test ./internal/term -run 'TestAgnostic|TestCodexLog_ProductionEventsPath|TestClaudeLog_ProductionEventsPath|TestClaudeDegraded_MalformedLog|TestClaudeDetection_FalsePositive' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-3-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a7-3-go-cache go test ./...
rg -n "agentKind|agentStatus|agentConfidence|degraded|blocked_by_future_runtime|future_agent|unknown|Agent" mobile/src --glob '*.ts' --glob '*.tsx'
rg -n "agentKind.*===|agentKind.*==|switch.*agentKind|case ['\"]claude|case ['\"]codex|case ['\"]gemini|Claude|Codex|claude|codex|gemini|qwen|responses|antigravity" mobile/src --glob '*.ts' --glob '*.tsx'
```

Results:

- `git diff --check`: passed.
- Mobile TypeScript compile: passed.
- Targeted A7/A6/A5 term tests: passed.
- `go vet ./...`: passed.
- Sandboxed `go test ./...`: failed only because `httptest` could not bind a local port in the sandbox.
- Authorized external `go test ./...`: passed.
- Mobile vendor-specific branch/literal grep: no matches.

## Accepted improvements

### 1. Mobile schema now includes optional agent fields

`mobile/src/components/AgentCard.tsx` now includes:

```ts
agentKind?: string;
agentStatus?: string;
agentConfidence?: number;
```

This addresses the previous "no mobile schema field" blocker.

### 2. Feed activity path passes `agentKind`

`FeedScreen` now passes:

```tsx
agentKind={sessionData?.agentKind}
```

to `EventBubble`.

### 3. EventBubble no longer uses runner as the only agent label source

`EventBubble` now uses:

```tsx
agentKind || runnerId || 'Agent'
```

This is better than the previous `runnerId || 'Agent'` because it at least consumes the A5/A6 `agentKind` field.

## Remaining blockers

### Blocking issue 1 — no `agentStatus` rendering or fallback exists

Although `agentStatus?: string` is declared, it is not rendered or formatted anywhere.

A7 requires:

- unknown `AgentStatus` graceful rendering;
- degraded status visualization;
- status UI driven by current state/status, not vendor.

Current mobile usage grep shows `agentStatus` only in the interface declaration. There is no helper or UI path for:

- `waiting_approval`;
- `degraded`;
- `blocked_by_future_runtime`;
- unknown status fallback.

This is still a blocker.

### Blocking issue 2 — degraded visualization is still absent

A7 requires degraded to be visibly distinct from:

- no session;
- terminal unavailable;
- no activity.

This commit does not add:

- degraded badge;
- degraded copy;
- degraded color/status mapping;
- degraded fixture/test;
- any UI behavior tied to `agentStatus === "degraded"` or equivalent neutral helper.

The backend test named `TestAgnostic_DegradedState` still proves only DTO round-trip and terminal survival, not product UX.

### Blocking issue 3 — unknown event type fallback is still untested

`EventBubble` still classifies known event types and lets everything else fall into the generic bot bubble.

That may be acceptable, but A7 requires proof. There is no fixture/test for an event like:

```ts
{ type: "future_event_type", ... }
```

showing it renders as a neutral fallback without crashing or misleading the user.

### Blocking issue 4 — only FeedScreen passes `agentKind`; GlobalFeed does not

`FeedScreen` now passes `agentKind`, but `GlobalFeedScreen` still builds events with only:

```ts
runnerId
runnerColor
```

and renders:

```tsx
<EventBubble event={item} runnerId={item.runnerId} runnerColor={item.runnerColor} />
```

If global activity feed is part of the common activity UI, it also needs the same agent-agnostic behavior or a documented reason it is out of A7 scope.

### Blocking issue 5 — no explicit mobile fixture/test proves missing optional fields

The TypeScript interface fields are optional, but there is no mobile-side fixture/test proving old daemon responses with missing agent fields render safely after the new code.

TypeScript compile is useful, but it is not enough as an A7 schema evolution proof.

## Required next commit

Minimum A7 completion still needs:

1. Add vendor-neutral mobile helpers, for example:

```ts
formatAgentLabel(agentKind?: string): string
formatAgentStatus(agentStatus?: string, fallbackState?: string): string
isAgentDegraded(agentStatus?: string): boolean
```

2. Render status/degraded state somewhere user-visible without vendor branching:

- session card badge/copy, or
- activity header/copy, or
- explicit degraded notice in the activity view.

3. Add mobile fixture/test or typed compatibility test proving:

- `agentKind: "future_agent"` renders safely;
- `agentStatus: "blocked_by_future_runtime"` renders safely;
- `agentStatus: "degraded"` renders distinctly;
- missing `agentKind`, `agentStatus`, `agentConfidence` is safe;
- unknown event type renders as neutral fallback.

4. Wire `agentKind` consistently across common activity surfaces, including `GlobalFeedScreen`, unless explicitly documented out of scope.

5. Keep vendor-specific behavior grep clean:

```bash
rg -n "agentKind.*===|agentKind.*==|switch.*agentKind|case ['\"]claude|case ['\"]codex|case ['\"]gemini" mobile/src --glob '*.ts' --glob '*.tsx'
```

6. Keep A5/A6 backend regressions passing.

## Final decision

`0772debac` is accepted as partial mobile schema and `agentKind` wiring progress.

It is rejected as Phase A7 completion because `agentStatus` and degraded visualization remain unimplemented, unknown event fallback is unproven, missing-field compatibility is untested, and not all activity surfaces receive `agentKind`.
