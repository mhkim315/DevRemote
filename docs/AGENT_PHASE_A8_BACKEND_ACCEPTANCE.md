# Agent Phase A8 Backend Acceptance

Date: 2026-07-07

Branch: `feature/phase10-multi-adapter`

Executor commits:

- `512f984d5` — initial Antigravity parser/detector/resolver implementation.
- `8a798f56b` — production telemetry path wiring.

Verifier decision: **ACCEPT**

Accepted scope: **Phase A8 — Third Agent Backend Slice**

## Meaning of this acceptance

A8 acceptance proves that Pokit is no longer limited to Claude/Codex backend
assumptions. A real third agent backend, Antigravity, can flow through the
production agent path:

```text
Antigravity actual-log evidence
→ Detector / Resolver
→ Parser
→ Common AgentEvent
→ EventStore
→ /api/sessions.Events
→ existing Mobile UX contract
```

This is a backend slice acceptance. It does not mean Approval UX completion or
Alpha/release completion.

## Accepted evidence

The final accepted implementation includes:

- actual Antigravity redacted fixtures from observed CLI logs;
- `AntigravityParser`, `AntigravityDetector`, and `AntigravityLogResolver` in
  the agent layer;
- term-layer `AntigravityParser` implementing the production `AgentLogParser`;
- `TelemetryService.processSession` parser selection for `LogRef.Agent ==
  "antigravity"`;
- `AntigravityResolver.ResolveLink()` preserving `LogRef.Agent ==
  "antigravity"`;
- production boundary tests proving:
  - `TelemetryService.processSession`;
  - `ReadNewEvents`;
  - `EventStore.Append`;
  - `HandleSessionsAPI`;
  - `/api/sessions.Events`;
- Antigravity `agentKind` preservation through API response;
- common event exposure for:
  - `user_message`;
  - `assistant_message`;
  - `tool_call_started`;
- unknown/system Antigravity records do not hide or kill the terminal session;
- non-Antigravity sessions are not misidentified as Antigravity.

## Verification commands run

Targeted:

```sh
GOCACHE=/tmp/devremote-a8-8a-go-cache go test ./internal/term -run 'TestAntigravity|TestClaude|TestCodex|TestAgnostic' -count=1 -v
GOCACHE=/tmp/devremote-a8-8a-go-cache go test ./internal/agent -count=1
```

Full daemon verification:

```sh
GOCACHE=/tmp/devremote-a8-8a-go-cache go vet ./...
GOCACHE=/tmp/devremote-a8-8a-go-cache go test ./...
GOCACHE=/tmp/devremote-a8-8a-go-cache go test -race ./...
```

Result: pass.

Mobile TypeScript:

```sh
node_modules/.bin/tsc --noEmit
```

Result: not run. `mobile/node_modules` was not present in the verification
worktree. This is not an A8 blocker because A8 changed backend/daemon code only
and A7 mobile/vendor-branch regression was checked by source search.

## Regression checks

Preserved:

- Claude A5 production event path;
- Codex A6 production event path;
- A7 unknown/future agent/status fallback;
- no mobile/common vendor-specific behavior branch added by A8;
- no raw Antigravity fixture secret/path leakage found.

Vendor-specific literals remain allowed in parser, detector, resolver,
registration, and fixture metadata. They must not drive common/mobile UX
behavior.

## Non-blocking follow-ups

The following items did not block A8 acceptance but must stay visible:

1. `LogRef.Agent` documentation was updated to include `antigravity` in the
   acceptance documentation commit.
2. The term-layer Antigravity parser currently normalizes `ERROR_MESSAGE`
   through the legacy `tool_result` path, while the agent-layer parser maps
   `ERROR_MESSAGE` to `failed`. A9/A10 should decide the product-level
   representation for failed/degraded agent events and align the parsers if
   needed.
3. Mobile TypeScript should be rerun once `mobile/node_modules` is available.

## Next phase

```text
A7 ACCEPT — Agent Agnostic UX
A8 ACCEPT — Third Agent Backend Slice
A9 NEXT — Approval UX
A10 — Diagnostics / Alpha Release Gate
```

A9 should answer this product question:

```text
Can a user safely understand and act on approval_requested /
waiting_approval states from mobile?
```
