# Agent Phase A5b Implementation Review 4 — Renamed Resolver Test

Reviewed commit: `0b39f8928`

Verdict: **REJECT**

This commit changes only `companion-daemon/internal/term/claude_boundary_test.go`. No production code changed from the previous rejected implementation.

The test is now named `TestClaudeOutput_ProductionResolverPath`, but it still supplies the log path through the same non-production `LogPath()` session hook. It does not prove that the existing production resolver/parser path feeds `/api/sessions.agentEvents`.

## Verification performed

Commands run:

```bash
git diff --check 5ac9491..HEAD
gofmt -l companion-daemon/internal/term/claude_boundary_test.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b4-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b4-go-cache go test ./internal/term -run 'TestClaudeOutput_(ProductionResolverPath|MissingLog_NoCrash|FalsePositive|NilDetector)' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b4-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b4-go-cache go test ./internal/term -count=1
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5b4-go-cache go test ./...
```

Notes:

- The sandboxed `internal/term` full test failed because `httptest` could not bind a local port.
- The authorized `internal/term` and full `go test ./...` runs passed.
- Passing tests do not satisfy A5b because the production implementation remains unchanged and still depends on an ad-hoc `LogPath()` hook.

## What changed

The test now creates:

```text
<tmp>/.claude/projects/test/session.jsonl
```

and is named as if it verifies a production resolver path.

However, the test adapter still stores the concrete log path directly:

```go
type claudeResolverSession struct {
    cwd     string
    logPath string
}

func (s *claudeResolverSession) LogPath() string { return s.logPath }
```

`TelemetryService.Snapshot()` still consumes it through:

```go
if lp, ok := sess.(interface{ LogPath() string }); ok {
    evidence.LogPath = lp.LogPath()
}
```

No real production adapter implements this method.

## Blocking issue 1 — test name does not match the code path

The test says "ProductionResolverPath", but it does not force:

```text
ProcessInfo → ResolveAgentLog → LogRef → ReadNewEvents → EventStore → /api/sessions
```

Instead, it still uses:

```text
test session LogPath() → Snapshot evidence.LogPath → agent.ParseEvents → os.ReadFile
```

That is the same rejected architecture from the previous review.

## Blocking issue 2 — `.claude/projects` directory does not prove resolver usage

Creating a temp directory that resembles `.claude/projects/...` is not enough. The actual code under review does not derive `logPath` from that directory through a production resolver. The test passes because it explicitly passes `logPath` into the test session constructor:

```go
reg := mux.MustNewRegistry(&claudeResolverSessionAdapter{cwd: tmpDir, logPath: logPath})
```

and then exposes it via `LogPath()`.

This bypasses the production resolver.

## Blocking issue 3 — production code remains unchanged

The previous blocking implementation is still present:

- `TelemetryService.Snapshot()` performs request-time evidence construction.
- It checks an undocumented `interface{ LogPath() string }`.
- It calls `s.detector.ParseEvents(...)` separately from `TelemetryService.processSession()`.
- It does not use parsed events already appended to `EventStore`.

The actual production parser path still exists separately:

```text
TelemetryService.processSession()
→ ResolveAgentLog(...)
→ ReadNewEvents(...)
→ s.events.Append(...)
```

but the new `agentEvents` field is not populated from that path.

## Blocking issue 4 — event coverage regressed

The previous test asserted:

- `user_message`
- `thinking`
- `tool_call_started`
- `approval_requested`
- `agentStatus=waiting_approval`

This revision only asserts:

- `user_message`
- `thinking`

It no longer proves tool call, approval, or parser-derived waiting approval state at the product boundary.

## Blocking issue 5 — degraded/mobile scope remains unresolved

Still missing:

- malformed log → degraded parser state visible at product boundary;
- parser failure isolation;
- missing log vs no activity vs degraded distinction;
- mobile schema/rendering verification;
- no mobile agent-name behavior branch verification.

## Required changes for acceptance

The next revision should stop renaming the test path and actually connect the production path.

Required:

1. Remove the product proof's dependency on `interface{ LogPath() string }`, unless this becomes a documented production session capability implemented by real adapters.

2. Populate `/api/sessions.agentEvents` from `EventStore` or cached telemetry state produced by `TelemetryService.processSession()`.

3. Add a test where:
   - `ProcessProvider.ProcessInfo` is sufficient input;
   - `ResolveAgentLog` or an injectable production-shaped resolver returns the temp log path;
   - `ReadNewEvents` parses the log;
   - `EventStore` receives the parsed events;
   - `/api/sessions` exposes those same events.

4. Restore full event assertions:
   - user message;
   - assistant/thinking;
   - tool call;
   - approval requested;
   - parser-derived `waiting_approval`.

5. Add degraded parser and mobile/client verification, or explicitly split those into a documented A5b follow-up before claiming A5b complete.

6. Keep A6 blocked until A5b is accepted.

## Final decision

`0b39f8928` is rejected for Phase A5b.

It is a test rename/rework, not a production resolver-path integration.
