# Agent Phase A5b Implementation Review 3 — Temp Log Path Hook

Reviewed commit: `d8961be35`

Verdict: **REJECT**

This revision fixes two important issues from the previous review:

- production fixture fallback was removed from `ParseEvents`;
- the boundary test now parses a concrete temp Claude JSONL file and asserts specific event types plus `waiting_approval`.

However, Phase A5b is still not acceptable because the log path enters the product boundary through a test-only, ad-hoc `LogPath()` session hook. Real production adapters do not implement this hook, and the existing production resolver/parser path is not connected to `agentEvents`.

The commit proves:

```text
test session implements LogPath() → Snapshot builds evidence.LogPath → ParseEvents reads temp JSONL → /api/sessions.agentEvents
```

It still does not prove:

```text
tmux/cmux/LocalPTY process evidence → production ResolveAgentLog/ReadNewEvents path → common AgentEvent/EventStore → /api/sessions/mobile boundary
```

## Verification performed

Commands run:

```bash
git diff --check 860e4cc..HEAD
gofmt -l companion-daemon/internal/agent/bridge.go companion-daemon/internal/term/claude_boundary_test.go companion-daemon/internal/term/telemetry_service.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b3-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b3-go-cache go test ./internal/term -run 'TestClaudeOutput_(RealLogParsing|MissingLog|FalsePositive_ProductionBridge|NilDetector_BackwardCompat)' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b3-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b3-go-cache go test ./internal/term -count=1
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b3-go-cache go test ./...
```

Notes:

- The sandboxed `internal/term` full test failed because `httptest` could not bind a local port.
- The authorized `internal/term` and full `go test ./...` runs passed.
- Passing tests do not satisfy A5b because the product-boundary test uses a non-production `LogPath()` hook that no real adapter implements.

## What improved

- `ParseEvents` now requires `evidence.LogPath`.
- Missing/unreadable logs no longer synthesize fixture events.
- The test creates a temp Claude JSONL file.
- The test verifies:
  - `user_message`
  - `thinking`
  - `tool_call_started`
  - `approval_requested`
  - `agentStatus=waiting_approval`

These are meaningful improvements over the previous fixture-fallback implementation.

## Blocking issue 1 — log path is supplied by a test-only hook

`TelemetryService.Snapshot()` now populates `evidence.LogPath` like this:

```go
if lp, ok := sess.(interface{ LogPath() string }); ok {
    evidence.LogPath = lp.LogPath()
}
```

The only implementation found is in the boundary test:

```go
func (s *claudeLogSession) LogPath() string { return s.logPath }
```

No real tmux, cmux, or LocalPTY session implements this interface. Therefore the new `agentEvents` field will not be populated for production sessions through this path.

This is not a production vertical slice. It is a test-specific bypass around the production resolver.

## Blocking issue 2 — existing production parser path remains disconnected

`TelemetryService.processSession()` already has the production-shaped parser path:

```go
ResolveAgentLog(ctx, pinfo)
→ LogCursor
→ ClaudeParser/CodexParser/GeminiParser
→ ReadNewEvents
→ s.events.Append(id, newEvents)
→ evaluateState(...)
```

But the new `agentEvents` path in `Snapshot()` does not use that state. It separately calls:

```go
s.detector.ParseEvents(st.ID, evidence)
```

That means there are now two parsing paths:

1. the existing term telemetry parser/event store path;
2. the new agent detector `ParseEvents` path.

A5b should not create a parallel test-only parser route unless the plan explicitly says so. The product boundary should expose the existing production parser results or move the production parser ownership into the agent layer with a real adapter contract.

## Blocking issue 3 — production adapters still cannot provide `agentEvents`

Because production sessions do not implement `LogPath()`, `ParseEvents` sees:

```go
evidence.LogPath == ""
```

and returns nil:

```go
if evidence.LogPath == "" {
    return nil
}
```

So for real sessions:

- `agentKind` may be populated from process evidence;
- `agentStatus` will usually fall back to `idle` or unknown/working behavior;
- `agentEvents` remains empty even when `processSession()` may have parsed events into `EventStore`.

This fails the A5b goal of making Claude parser events visible at the product boundary.

## Blocking issue 4 — degraded state is still not proven

The implementation maps parser degraded result to an `unknown` event with metadata:

```go
Metadata: map[string]string{"degraded": "true"}
```

But there is no product-boundary test for malformed input, unreadable log diagnostics, or parser failure isolation. `TestClaudeOutput_MissingLog` verifies no crash and no events, which is useful, but it does not verify degraded parser state.

A5b requires degraded state to be distinguishable from:

- no activity;
- missing log;
- unknown agent;
- terminal unavailable.

That distinction is still absent.

## Blocking issue 5 — mobile/client boundary remains untouched

A5b requires mobile status badge/schema/rendering work or an explicit scope split. This commit changes no mobile files and adds no mobile compile/schema/rendering verification.

The backend now exposes `agentEvents`, but mobile `SessionTelemetry` still does not model:

- `agentKind`;
- `agentStatus`;
- `agentConfidence`;
- `agentEvents`;
- degraded parser state.

## Blocking issue 6 — request-time process/log work remains unresolved

`Snapshot()` still calls:

```go
pp.ProcessInfo(context.Background())
```

at request time, and now also reads/parses logs through `ParseEvents`. This compounds the earlier follow-up: request-time `/api/sessions` can perform filesystem reads and full log parsing rather than using cached telemetry-loop state.

For production safety, A5b should prefer:

- telemetry-loop parsing and cached state;
- short timeout contexts;
- bounded incremental reads;
- EventStore-backed output.

## Required changes for acceptance

Minimum next revision:

1. Remove the ad-hoc `interface{ LogPath() string }` test hook from the product proof, or promote it into a real documented adapter/session capability and implement it for at least one production-relevant backend.

2. Connect A5b to the existing production parser path.
   - Prefer using `TelemetryService.processSession()` results and `EventStore`.
   - `/api/sessions.agentEvents` should be derived from real parsed events already stored for the session, not re-parsed from a test-only log path.

3. Add a product-boundary test where the same production path that resolves/parses logs also feeds `/api/sessions`.
   - A temp log file is fine.
   - The resolver/path must be supplied through production-shaped resolver state, not a hidden test-only session method.

4. Add degraded/failure isolation tests.
   - malformed log → degraded parser state visible;
   - missing log → no activity or unavailable, not fake events;
   - parser failure does not hide/kill terminal session.

5. Add mobile/client verification or explicitly split A5b into backend and mobile sub-steps before claiming A5b complete.

6. Keep A6 blocked until A5b is accepted.

## Final decision

`d8961be35` is rejected for Phase A5b.

It is a good intermediate backend parser test, but it is not yet a production vertical slice.
