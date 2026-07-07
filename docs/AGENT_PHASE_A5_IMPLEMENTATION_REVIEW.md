# Phase A5 Implementation Review — Claude Vertical Slice

Date: 2026-07-07
Reviewed commit: `583ddb076`
Baseline: `792714d` (`docs: accept agent phase A4 detector foundation`)
Verdict: **REJECT**

## Summary

`583ddb076` adds a Claude parser, detector, resolver, and contract tests under
`companion-daemon/internal/agent`.

The direction is useful, and the new adapter is registered with the A3/A4 contract
harness. However, this is not yet an acceptable Phase A5 vertical slice.

The accepted Phase A5 plan requires Claude agent semantics to flow through the Agent
Adapter pipeline into common event/status/approval behavior, Activity feed, and mobile
status display without Claude-specific UI branching. This commit explicitly scopes itself
as verification-only and makes no production registration, Activity feed, mobile, or
common approval UX integration changes.

## Verified

Commands run:

```bash
git diff --check HEAD~1..HEAD
gofmt -l companion-daemon/internal/agent/claude_adapter.go companion-daemon/internal/agent/claude_adapter_test.go
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a5-go-cache go vet ./...
GOCACHE=/tmp/devremote-agent-a5-go-cache go test -race ./internal/agent -count=1
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./...
```

Notes:

- `go test ./...` required execution outside the filesystem/network sandbox because
  existing `httptest` cases need local port binding.
- All listed checks passed after allowing local listener binding.

Positive findings:

- `TestClaudeParser_Contract` calls `RunParserContract`.
- `TestClaudeDetector_Contract` calls `RunDetectorContract`.
- No raw Claude log files were added by this commit.
- Claude-specific raw field names are contained in the Claude adapter, tests, fixtures,
  and docs, not the common Go model.
- The existing A2/A3/A4 public contracts were not weakened.

## Blocking issues

### P0 — Phase A5 vertical slice is not connected to production behavior

The Phase A5 plan requires:

- Claude session `AgentIdentity` displayed;
- Claude user/assistant/tool/approval events shown as common `AgentEvent`s;
- Claude approval request shown through common Approval UX;
- Activity feed connection;
- mobile status badge display;
- parser degraded state display;
- same semantics for tmux + Claude and LocalPTY + Claude;
- no Claude-name-based mobile behavior branch.

The actual changed files are only:

```text
companion-daemon/internal/agent/claude_adapter.go
companion-daemon/internal/agent/claude_adapter_test.go
docs/AGENT_PHASE_A5_SCOPE.md
```

There is no production registration or bridge from `internal/agent` into the existing
telemetry/event store/API/mobile flow. As a result, the commit proves that the Claude
adapter can satisfy isolated contracts, but not that Claude semantics reach the product
boundary.

This is the main blocker.

### P0 — The A5 scope document contradicts the accepted Phase A5 goal

`docs/AGENT_PHASE_A5_SCOPE.md` says:

```text
(no production registration yet — Phase A5 is verification-only)
```

That conflicts with the accepted Phase A5 plan, where A5 is the first real Claude
vertical slice and must demonstrate Activity/mobile/common approval behavior.

If the intended work is a pre-A5 implementation spike, it should be renamed and judged as
`A5a` or `A5-prep`. It should not be presented as Phase A5 acceptance.

### P1 — The Claude resolver does not resolve the documented Claude log location

The A1 inventory and A5 scope describe Claude logs under:

```text
~/.claude/projects/*/<uuid>.jsonl
~/.claude/history.jsonl
```

The implementation instead synthesizes:

```go
ev.CWD + "/.claude/projects/" + project + "/latest.jsonl"
```

That path is not the documented Claude Code layout and is unlikely to point at the real
log file for a project. The resolver contract currently checks safe `DisplayPath`, but it
does not prove real Claude log discovery.

For A5, resolver behavior must be based on the accepted A1 inventory, not a fabricated
`latest.jsonl` under the working directory.

### P1 — Cursor token is collision-prone for a real parser

`claudeHashFirstLine` returns:

```go
fmt.Sprintf("%d-%d", totalLineLength, lineCount)
```

This passes the current contract for exact replay, but different batches with the same
line count and total byte length collide. A real incremental parser should use a stronger
opaque cursor, such as byte offset, last processed log record identity, or a content hash
with enough entropy.

This is not the primary rejection reason, but it should be fixed before accepting a real
production vertical slice.

### P1 — Approval identity is batch-level, not event-level

Claude approvals are emitted with:

```go
ID: lineHash
```

`lineHash` is computed from the whole batch, so multiple approval records in the same
batch can share the same ID. A common Approval UX needs stable per-approval identity,
preferably derived from redacted-safe source position or record identity, not the entire
batch fingerprint.

## Required next changes

To make Phase A5 acceptable:

1. Register or route the Claude adapter through the production Agent Adapter pipeline.
2. Show that detected Claude identity, status, events, approvals, and degraded parser
   state reach the existing API/telemetry/Activity/mobile boundary as common model data.
3. Add tests proving no mobile or common UI behavior branches on Claude-specific raw
   fields.
4. Add tmux + Claude and LocalPTY + Claude semantic equivalence coverage, even if based
   on fixture/injected evidence rather than live Claude execution.
5. Update `AGENT_PHASE_A5_SCOPE.md` so it no longer downgrades A5 to
   “verification-only”, or rename the current scope as a pre-A5 spike.
6. Fix Claude resolver path discovery to match A1 inventory.
7. Replace the cursor token with a non-colliding production-suitable cursor.
8. Use stable per-approval IDs.

## Non-blocking notes

The new adapter is still a useful foundation:

- parser and detector are isolated under `internal/agent`;
- the common model remains agent-neutral;
- contract harness registration is present;
- raw fixtures remain redacted.

Those are necessary conditions, but not sufficient for Phase A5 acceptance.
