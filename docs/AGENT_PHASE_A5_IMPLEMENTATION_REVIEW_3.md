# Phase A5 Implementation Review 3 — Claude E2E Pipeline Test

Date: 2026-07-07
Reviewed commit: `77d000725`
Previous rejection: `0440761` (`docs: keep agent phase A5 blocked`)
Verdict: **REJECT**

## Summary

`77d000725` adds `TestClaudeAdapter_E2E_Pipeline` and cleans up the A5 scope document.
The new test is useful as an `internal/agent` package integration test:

- detector returns Claude identity for simulated Claude process evidence;
- resolver returns a non-empty Claude-like log path;
- parser maps A1 fixtures into common event/status/approval output;
- contract tests still pass.

However, this still does not satisfy the accepted Phase A5 vertical slice. The test is
not connected to the production telemetry/API/Activity/mobile boundary. It exercises
Claude detector/resolver/parser directly inside the same package.

Phase A5 remains blocked.

## Verified

Commands run:

```bash
git diff --check 0440761..77d0007
gofmt -l companion-daemon/internal/agent/claude_adapter_test.go companion-daemon/internal/agent/claude_adapter.go
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a5-go-cache go vet ./...
GOCACHE=/tmp/devremote-agent-a5-go-cache go test -race ./internal/agent -count=1
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./...
```

All checks passed.

Changed files:

```text
companion-daemon/internal/agent/claude_adapter_test.go
docs/AGENT_PHASE_A5_SCOPE.md
```

No production API, telemetry, Activity feed, terminal runtime, or mobile files changed.

## Positive findings

### 1. Scope document contradiction is improved

The previous document contradiction around `cmd/devremote/app.go` and
`0 production registration changes` was removed.

### 2. A package-level Claude pipeline test now exists

`TestClaudeAdapter_E2E_Pipeline` covers:

- detect;
- resolve;
- parse;
- expected event types;
- status inference;
- approval detection.

This is a useful adapter-level integration test.

### 3. Existing contracts remain intact

The Claude parser and detector continue to run through the common contract harness.

## Blocking issues

### P0 — The new E2E test is not the Phase A5 vertical slice

The test directly instantiates:

```go
NewClaudeDetector()
NewClaudeLogResolver()
NewClaudeParser()
```

and then asserts their in-package outputs.

That proves internal adapter composition, but it does not prove the accepted A5 product
flow:

- Claude `AgentIdentity` visible through daemon/API output;
- Claude `AgentEvent`s stored or exposed through the existing Activity/history path;
- Claude approval surfaced through common Approval UX;
- Claude status/degraded state represented at the mobile boundary;
- no Claude-specific mobile behavior branch;
- same semantics for tmux + Claude and LocalPTY + Claude.

Repository search still shows the new `internal/agent` Claude adapter is used only in
`internal/agent` tests. The existing production telemetry path continues to use the older
`internal/term` parser stack.

### P0 — The A5 scope was narrowed below the accepted plan

The accepted A5 plan required Activity feed connection, mobile status badge display,
approval UX behavior, degraded parser display, and tmux/LocalPTY semantic equivalence.

`docs/AGENT_PHASE_A5_SCOPE.md` now defines A5 acceptance as:

```text
Claude A1 fixtures pass parser contract
Claude detector passes detector contract
E2E pipeline test: detect→parse→status→approval chain verified
go vet, go test, go test -race, gofmt clean
```

That is a narrower adapter-package milestone. It is not the original Phase A5 vertical
slice. If this narrower scope is intentional, it should be renamed to `A5a` or
`A5-adapter-internal`, and the real product-boundary A5 should remain open.

### P1 — Resolver path discovery is still weak

The E2E test checks only that the returned path contains `.claude/projects` or
`.claude/history`.

It does not prove that:

- `~/.claude/projects/*/<uuid>.jsonl` is discovered from a real project/session mapping;
- `~/.claude/history.jsonl` is handled;
- glob expansion selects existing logs;
- missing, stale, or permission-denied logs degrade safely;
- internal `Path` is never exposed as `DisplayPath`.

The implementation still synthesizes a path from `ev.CWD`, so this remains short of a
production resolver.

### P1 — Test reads host Claude state opportunistically

`TestClaudeAdapter_E2E_Pipeline` checks:

```go
os.UserHomeDir()
~/.claude/projects
```

and logs the local directory count if present.

Because the result is optional, this does not currently fail CI. Still, adapter tests
should avoid depending on a developer machine's private Claude state unless the test is
clearly separated as a local/manual smoke test. The contract suite should remain
fixture-driven and hermetic.

### P1 — Cursor and approval identity remain unresolved

The previous issues remain:

- cursor embeds a raw first-line prefix and remains collision-prone;
- approval IDs are still based on the batch cursor, not stable per-approval identity.

These are not the main A5 blocker, but they should be fixed before production exposure.

## Required next changes

To accept Phase A5:

1. Add a production Agent Adapter integration point, behind a feature flag if necessary.
2. Prove Claude identity/status/events/approvals/degraded state reach the existing
   daemon API/telemetry/Activity/mobile boundary as common model data.
3. Add regression coverage showing mobile/common behavior does not branch on raw Claude
   fields or Claude-only event names.
4. Add tmux + Claude and LocalPTY + Claude semantic equivalence coverage using injected
   evidence or fixture-backed sessions.
5. Make Claude log discovery match A1 inventory with hermetic tests for found, missing,
   stale, and permission-denied logs.
6. Remove host-local Claude directory probing from normal unit tests, or mark it as a
   manual smoke test outside the default suite.
7. Replace cursor generation and approval IDs with production-safe opaque/stable values.

## Current judgment

`77d000725` is an improvement over `2319ed9ba`, but it is still an adapter-internal
integration test, not the Phase A5 product vertical slice.

Do not proceed to Phase A6 until Phase A5 proves the product-boundary flow.
