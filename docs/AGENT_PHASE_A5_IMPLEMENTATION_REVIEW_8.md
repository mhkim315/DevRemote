# Phase A5 Implementation Review 8 — Production Bridge Wiring

Date: 2026-07-07
Reviewed commit: `5bbd13465`
Previous rejection: `3848479` (`docs: reject agent phase A5 nil detector`)
Verdict: **REJECT**

## Summary

`5bbd13465` adds a production bridge file:

```text
companion-daemon/internal/agent/bridge.go
```

and wires `cmd/devremote/app.go` to pass `agent.NewTermAgentDetector()` into
`TelemetryService` when `Config.EnableAgentDetection` is true.

This addresses part of the previous rejection: there is now a non-test implementation of
`term.AgentDetector`.

However, the implementation is not acceptable for Phase A5. The production bridge
hardcodes every session as Claude:

```go
ProcessName: "claude"
```

and returns `working` unless confidence is below threshold. It does not use actual
session/process evidence, resolver output, parser output, approvals, degraded state, or
diagnostics. It would mark unrelated tmux/cmux/localpty sessions as Claude if enabled.

Additionally, `EnableAgentDetection` is added to `Config`, but there is no CLI flag in
`cmd/devremote/main.go` to set it, so normal daemon execution still cannot enable the
bridge.

## Verified

Commands run:

```bash
git diff --check 3848479..5bbd134
gofmt -l companion-daemon/cmd/devremote/app.go companion-daemon/internal/agent/bridge.go
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a5-go-cache go vet ./...
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/term -run TestClaudeOutput_TelemetryPath -count=1 -v
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./...
```

Notes:

- Full tests were run outside the sandbox because existing tests use local `httptest`
  listener binding.
- All checks passed.

Changed files:

```text
companion-daemon/cmd/devremote/app.go
companion-daemon/internal/agent/bridge.go
```

## Positive findings

### 1. A production bridge type now exists

`agent.TermAgentDetector` is no longer test-only.

### 2. `cmd/devremote` has an internal config gate

`Config.EnableAgentDetection` can choose whether to inject the bridge into
`TelemetryService`.

### 3. Existing tests still pass

No current Go test regression was found.

## Blocking issues

### P0 — The production detector hardcodes all sessions as Claude

`TermAgentDetector.DetectAgent` builds evidence as:

```go
DetectionEvidence{
    ProcessName: "claude",
    TermAdapter: adapterName,
}
```

This means the detector does not inspect the session at all. It ignores:

- actual process name;
- PID;
- CWD;
- terminal adapter process snapshot;
- log path;
- screen text;
- manual link.

If enabled, every session passed through this detector will look like Claude with high
confidence. That violates the A4 low-confidence unknown fallback requirement and is a
severe false-positive risk.

### P0 — The feature cannot be enabled from CLI

`Config.EnableAgentDetection` exists, but `cmd/devremote/main.go` does not define or pass
an `--enable-agent-detection` flag.

Normal daemon startup therefore still cannot enable the bridge. Only tests or direct
`NewAppWithDeps` callers can set it.

### P0 — Bridge does not use resolver/parser output

The Phase A5 goal is not just a Claude name badge. The bridge should demonstrate the
Claude adapter pipeline:

```text
detect → resolve logs → parse → status/events/approvals/degraded → product boundary
```

Current bridge does only:

```text
hardcoded ProcessName=claude → detector → scalar status
```

It does not use:

- `ClaudeLogResolver`;
- `ClaudeParser`;
- `ParseResult.Events`;
- `ParseResult.Approvals`;
- `ParseResult.Degraded`;
- `ParseResult.Diagnostics`.

### P0 — No test catches false positives

There is no test proving:

- non-Claude sessions remain `unknown`;
- empty evidence remains `unknown`;
- LocalPTY/tmux/cmux non-agent sessions do not get tagged as Claude;
- detector respects low-confidence fallback in the production bridge.

Given the hardcoded `ProcessName: "claude"`, such a test should currently fail.

### P1 — Status is not derived from agent state

The bridge returns:

```go
status := StatusWorking
```

unless confidence is below threshold. That is not status inference from parser output.
It cannot represent:

- idle;
- thinking;
- waiting approval;
- waiting input;
- degraded;
- failed/interrupted.

### P1 — Mobile schema remains unupdated

Backend optional fields exist, but mobile `SessionTelemetry` still does not include:

- `agentKind`;
- `agentStatus`;
- `agentConfidence`.

There is still no mobile-facing test/golden for safe display or safe ignore.

### P1 — Previous resolver/cursor/approval identity issues remain

Still unresolved:

- Claude resolver synthesizes paths from `ev.CWD`;
- cursor embeds raw first-line prefix and is collision-prone;
- approval IDs are batch-level.

## Required next changes

To reach Phase A5 acceptance:

1. Do not hardcode `ProcessName: "claude"` in production bridge.
2. Build `DetectionEvidence` from real session/process data:
   - use `mux.ProcessSnapshotProvider` or session `ProcessProvider` where available;
   - include process name, CWD, adapter name, log refs, and safe fallback evidence.
3. Unknown or incomplete evidence must return `unknown` with low confidence.
4. Add tests proving non-Claude sessions are not tagged as Claude.
5. Add a CLI flag or documented config path for `EnableAgentDetection`, if the bridge is
   meant to be user-enabled in Phase A5.
6. Integrate resolver/parser output or explicitly narrow A5 acceptance before requesting
   acceptance.
7. Update mobile schema/golden if new backend fields are part of product boundary.

## Current judgment

`5bbd13465` adds a production bridge shell, but its current behavior is unsafe because it
tags every session as Claude when enabled. This cannot be accepted as Phase A5.

Do not proceed to Phase A6 until production detection is evidence-based and false
positives are covered by tests.
