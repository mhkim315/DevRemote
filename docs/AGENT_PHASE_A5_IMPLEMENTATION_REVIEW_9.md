# Phase A5 Implementation Review 9 — Evidence-Based Detection Attempt

Date: 2026-07-07
Reviewed commit: `e905ab510`
Previous rejection: `a14f74f` (`docs: reject agent phase A5 false positive bridge`)
Verdict: **REJECT**

## Summary

`e905ab510` fixes two important safety issues from the previous review:

- adds the CLI flag `--enable-agent-detection`;
- removes the hardcoded `ProcessName: "claude"` from the production bridge.

It also adds false-positive coverage for `bash` and empty evidence.

This is safer than the previous bridge. However, Phase A5 still cannot be accepted,
because the production bridge now has the opposite problem: it never has enough evidence
to detect Claude. The telemetry path returns `unknown`, not `claude`.

The current test output confirms this:

```text
production bridge: AgentKind=unknown AgentStatus=unknown Confidence=0.10
```

That is correct for the current evidence, but it does not prove the Phase A5 Claude
vertical slice.

## Verified

Commands run:

```bash
git diff --check a14f74f..e905ab5
gofmt -l companion-daemon/cmd/devremote/main.go companion-daemon/internal/agent/bridge.go companion-daemon/internal/term/claude_boundary_test.go
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a5-go-cache go vet ./...
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/term -run 'TestClaudeOutput_(TelemetryPath|FalsePositivePrevention)' -count=1 -v
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./...
```

Notes:

- Full tests were run outside the sandbox because existing tests use local `httptest`
  listener binding.
- All checks passed.

Changed files:

```text
companion-daemon/cmd/devremote/main.go
companion-daemon/internal/agent/bridge.go
companion-daemon/internal/term/claude_boundary_test.go
```

## Positive findings

### 1. CLI flag exists

`--enable-agent-detection` is now parsed and passed into `Config.EnableAgentDetection`.

### 2. Hardcoded Claude false positive was removed

`TermAgentDetector` no longer sets `ProcessName: "claude"` unconditionally.

### 3. False-positive tests were added

The new test confirms `bash` and empty evidence are not detected as Claude.

These are necessary fixes.

## Blocking issues

### P0 — The production bridge cannot detect a real Claude session

`TermAgentDetector.DetectAgent` now builds evidence as:

```go
DetectionEvidence{
    TermAdapter: adapterName,
}
```

That evidence intentionally contains no process name, CWD, process args, log paths, or
screen text. As a result, `ClaudeDetector` correctly returns low-confidence `unknown`.

This prevents false positives, but it also means the production bridge cannot produce a
Claude true positive. Phase A5 requires the product boundary to show Claude output for a
Claude session.

### P0 — The telemetry path test no longer proves Claude vertical slice

`TestClaudeOutput_TelemetryPath` now accepts:

```text
AgentKind=unknown
AgentStatus=unknown
Confidence=0.10
```

That proves safe unknown fallback, not Claude product-boundary integration.

The test name and comments still say "Claude adapter output flows through production
telemetry path", but the asserted behavior is unknown fallback.

### P0 — Real process evidence is available elsewhere but not passed into detector

The telemetry package already collects process snapshots:

```go
collectProcessSnapshots(ctx, s.reg.Adapters())
```

and sessions/adapters can expose:

- `mux.ProcessProvider`
- `mux.ProcessSnapshotProvider`
- `models.ProcessInfo{PID, CWD, ...}`

But `TermAgentDetector.DetectAgent` receives only:

```go
sessionID, adapterName, localID
```

It cannot use the process evidence that the telemetry loop already collected. The
interface is too thin for evidence-based detection.

### P0 — Resolver/parser/events/approvals still do not reach product boundary

The bridge still does not use:

- `ClaudeLogResolver`;
- `ClaudeParser`;
- `ParseResult.Events`;
- `ParseResult.Approvals`;
- parser degraded/diagnostics;
- resolver degraded/diagnostics.

Therefore A5 still does not prove Claude user/assistant/tool/approval event flow through
product behavior.

### P1 — Mobile schema remains unupdated

Backend optional fields exist, but mobile TypeScript types and mobile-facing schema
tests still do not include:

- `agentKind`;
- `agentStatus`;
- `agentConfidence`.

## Required next changes

To reach Phase A5 acceptance:

1. Expand `term.AgentDetector` or add a new bridge API that accepts real
   `DetectionEvidence` or `models.ProcessInfo`.
2. Feed actual process evidence collected by `TelemetryService` into the agent bridge.
3. Add a positive test where a session with process evidence `ProcessName: "claude"` and
   realistic CWD is detected as Claude through `/api/sessions`.
4. Keep the new false-positive tests for `bash`, empty evidence, and unknown process.
5. Integrate resolver/parser output or explicitly revise A5 acceptance before requesting
   acceptance.
6. Update mobile schema/golden for the new optional fields if they remain part of the
   product boundary.

## Current judgment

`e905ab510` is safer than `5bbd13465`, but it no longer proves the Claude vertical slice.
It proves unknown fallback. That is necessary, but insufficient for Phase A5.

Do not proceed to Phase A6 until the product telemetry path can detect a real Claude
session using actual evidence and still avoids false positives.
