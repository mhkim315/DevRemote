# Phase A5 Implementation Review 2 — Claude Vertical Slice Fix

Date: 2026-07-07
Reviewed commit: `2319ed9ba`
Previous rejection: `b3f1837` (`docs: reject agent phase A5 vertical slice`)
Verdict: **REJECT**

## Summary

`2319ed9ba` improves two items from the previous review:

- the resolver no longer points to `latest.jsonl`;
- the A5 scope document no longer says A5 is purely verification-only;
- the cursor is slightly stronger than the previous line-count/length-only token.

These changes are useful, but Phase A5 is still not acceptable. The main P0 from the
previous review remains unresolved: the Claude adapter is still isolated inside
`internal/agent` tests and is not connected to production Agent Adapter behavior,
Activity feed, mobile status, or common Approval UX.

## Verified

Commands run:

```bash
git diff --check b3f1837..2319ed9
gofmt -l companion-daemon/internal/agent/claude_adapter.go
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a5-go-cache go vet ./...
GOCACHE=/tmp/devremote-agent-a5-go-cache go test -race ./internal/agent -count=1
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./...
```

All checks passed.

Changed files:

```text
companion-daemon/internal/agent/claude_adapter.go
docs/AGENT_PHASE_A5_SCOPE.md
```

No `cmd/devremote`, `internal/term`, API, telemetry, Activity feed, or mobile files were
changed.

## Resolved or partially improved

### Improved — resolver no longer uses `latest.jsonl`

The resolver now returns:

```go
Path:        ev.CWD + "/.claude/projects/" + project + "/*.jsonl"
DisplayPath: "<PROJECT>/.claude/projects/<PROJECT>/<UUID>.jsonl"
```

This is better than the previous synthetic `latest.jsonl` path.

However, it still constructs the path under `ev.CWD/.claude/...`, while the A1 inventory
and A5 scope describe Claude logs under user-level `~/.claude/projects/...` and
`~/.claude/history.jsonl`. This is still not a reliable production resolver.

### Partially improved — cursor token

The cursor now includes a first-line prefix, line count, and total byte length.

This is better than only line count plus total length, but it remains collision-prone and
contains raw log content in the cursor prefix. A production cursor should avoid raw
content and should use a stable offset, record ID, or non-reversible content hash.

## Blocking issues

### P0 — Phase A5 vertical slice is still not connected to production behavior

The accepted Phase A5 plan requires Claude semantics to reach product boundaries:

- `AgentIdentity` displayed for Claude sessions;
- Claude user/assistant/tool/approval events displayed as common `AgentEvent`s;
- Claude approval request surfaced through common Approval UX;
- Activity feed connection;
- mobile status badge display;
- parser degraded state display;
- equivalent semantics for tmux + Claude and LocalPTY + Claude;
- no Claude-name-based mobile behavior branch.

This commit changes only the isolated Claude adapter and the A5 scope document. There is
still no production registration or bridge from `internal/agent` into the existing
telemetry/event store/API/mobile flow.

The adapter contract tests prove the parser/detector/resolver can satisfy local
interfaces. They do not prove the A5 vertical slice.

### P0 — Scope document now claims production registration but code does not implement it

`docs/AGENT_PHASE_A5_SCOPE.md` now lists:

```text
cmd/devremote/app.go | Production registration (behind feature flag, optional)
```

But `cmd/devremote/app.go` is not changed in this commit, and repository search does not
show `NewClaudeParser`, `NewClaudeDetector`, or `NewClaudeLogResolver` used outside the
`internal/agent` package tests.

The scope document and implementation are therefore out of sync.

### P0 — Scope document still has contradictory acceptance criteria

The same scope document lists `cmd/devremote/app.go` as production registration, but its
acceptance section still says:

```text
0 production registration changes
```

That is incompatible with the accepted A5 vertical slice goal. If production registration
is deferred, this should be explicitly renamed to a pre-A5 or A5a adapter-only milestone,
not accepted as Phase A5.

### P1 — Claude resolver still does not implement A1 log discovery

The resolver should be based on accepted A1 evidence:

```text
~/.claude/projects/*/<uuid>.jsonl
~/.claude/history.jsonl
```

The current implementation derives a log glob from `ev.CWD`, not from the user-level
Claude log root. It also does not perform actual glob expansion, latest-file selection,
or stale/missing-path degraded handling.

For A5 acceptance, resolver tests should prove that a session CWD maps to the correct
redacted-safe Claude log reference using the accepted A1 layout.

### P1 — Approval IDs are still batch-level

Approval requests still use the batch cursor as the approval ID:

```go
ID: lineHash
```

Multiple approval records in one batch can share an ID, and the ID changes when the
surrounding batch changes. Common Approval UX needs stable per-approval identity.

## Required next changes

To reach A5 acceptance:

1. Connect the Claude adapter into the production Agent Adapter flow behind a safe flag
   if necessary.
2. Add tests proving Claude identity/status/events/approvals/degraded state reach the
   API/telemetry/Activity/mobile boundary as common model data.
3. Make `AGENT_PHASE_A5_SCOPE.md` internally consistent and aligned with the accepted A5
   plan.
4. Implement Claude log discovery from the accepted A1 log root, with safe `DisplayPath`
   and degraded behavior for missing/permission-denied logs.
5. Replace cursor generation with a production-suitable opaque token that does not embed
   raw log content.
6. Use stable per-approval IDs.

## Current judgment

`2319ed9ba` is a valid adapter-contract improvement, but it is still not the Phase A5
Claude vertical slice promised by the plan.

Do not proceed to Phase A6 until Phase A5 proves the product-boundary flow.
