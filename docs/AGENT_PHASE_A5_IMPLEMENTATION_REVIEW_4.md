# Phase A5 Implementation Review 4 — Term Boundary Tests

Date: 2026-07-07
Reviewed commit: `c142647f4`
Previous rejection: `09a4b67` (`docs: reject agent phase A5 product slice`)
Verdict: **REJECT**

## Summary

`c142647f4` improves the previous state in two concrete ways:

- removes the optional host `~/.claude/projects` probe from the normal agent test;
- adds `internal/term/claude_boundary_test.go` to exercise Claude adapter output from
  the `term` package.

The tests pass, and the change is directionally useful. However, it is still not
acceptable as Phase A5 under the accepted plan.

The new scope document explicitly redefines Phase A5 as **not** building product
integration wiring:

```text
Phase A5는 product integration infrastructure(telemetry wiring, Activity feed)를
구축하는 단계가 아니라 ...
Product-boundary wiring ... 은 Phase A8-A9에서 수행한다.
```

That is a narrower milestone than the accepted Phase A5 vertical slice. A scope reduction
like this may be reasonable as an `A5a` or `A5-boundary-compat` milestone, but it cannot
be accepted as the original Phase A5 without updating the master plan and acceptance
criteria first.

## Verified

Commands run:

```bash
git diff --check 09a4b67..c142647
gofmt -l companion-daemon/internal/agent/claude_adapter_test.go companion-daemon/internal/term/claude_boundary_test.go companion-daemon/internal/agent/claude_adapter.go
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/agent ./internal/term -count=1 -v
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/term -count=1 -v
GOCACHE=/tmp/devremote-agent-a5-go-cache go test -race ./internal/term -count=1
GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./...
```

Notes:

- `internal/term` and full tests require local `httptest` listener binding, so they were
  re-run outside the sandbox.
- All checks passed after allowing local listener binding.

Changed files:

```text
companion-daemon/internal/agent/claude_adapter_test.go
companion-daemon/internal/term/claude_boundary_test.go
docs/AGENT_PHASE_A5_SCOPE.md
```

## Positive findings

### 1. Host-local Claude probing was removed

The normal `internal/agent` test no longer probes the developer machine's
`~/.claude/projects` directory. This fixes one previous hermeticity concern.

### 2. A term-package compatibility test now exists

`internal/term/claude_boundary_test.go` imports `internal/agent` and confirms that Claude
adapter output can be JSON-serialized and does not break a basic `/api/sessions` handler
case.

This is useful as a compatibility smoke test.

### 3. The scope document is more honest

The document now clearly states that telemetry wiring, Activity feed, and mobile status
badge work are deferred to later phases.

That honesty is good, but it also confirms that this commit is not the previously
accepted A5 product vertical slice.

## Blocking issues

### P0 — Phase A5 scope was reduced below the accepted plan

The accepted Phase A5 plan required:

- Claude session identity visible through product behavior;
- Claude events/status/approvals flowing through common telemetry/API/Activity/mobile
  boundaries;
- common Approval UX behavior;
- parser degraded state visible at the product boundary;
- tmux + Claude and LocalPTY + Claude semantic equivalence;
- no Claude-name-based mobile behavior branch.

`c142647f4` explicitly defers product-boundary wiring to Phase A8-A9. That means the
commit may be a valid preparatory milestone, but it does not close Phase A5 as originally
defined.

If this narrower scope is intended, update the master plan and handoff documents first:

- rename current work to `A5a` / `A5-boundary-compat`;
- define the real product-boundary slice as a later named phase;
- state explicitly that the previous A5 acceptance criteria have been superseded.

Without that plan change, the verifier should not silently accept a lowered bar.

### P0 — The new "SessionTelemetry schema compatibility" test does not use SessionTelemetry

`TestClaudeOutput_TelemetrySchema` creates a local test-only type:

```go
type mockActivity struct {
    AgentKind string             `json:"agentKind"`
    Status    agent.AgentStatus  `json:"status"`
    Events    []agent.AgentEvent `json:"events,omitempty"`
}
```

But the actual product boundary is:

```go
type SessionTelemetry struct {
    ID           string
    State        string
    Load         int
    Runner       string
    Adapter      string
    Capabilities []string
    Events       []models.AgentEvent
}
```

and mobile currently expects:

```ts
state: 'idle' | 'thinking' | 'working' | 'waiting'
events?: AgentEvent[]
```

The new test serializes `agent.AgentEvent`, not the existing `models.AgentEvent` used by
`SessionTelemetry.Events`, `/api/sessions?history=...`, and mobile `AgentCard`.

So the test proves "Claude adapter output is JSON serializable", but it does not prove
compatibility with the real `SessionTelemetry` schema.

### P0 — There is still no production bridge from `internal/agent` to telemetry/API/mobile

Repository search shows the Claude adapter is used in:

- `internal/agent` tests;
- `internal/term/claude_boundary_test.go`.

The existing production telemetry path still uses the older `internal/term` parser stack
and `internal/models.AgentEvent`. The new `internal/agent` model is not converted into
the existing event store, telemetry snapshot, history API, Activity feed, or mobile
types.

### P1 — The unsupported-session test does not prove agent failure isolation

`TestClaudeOutput_UnsupportedBySession` confirms a basic `/api/sessions` request does
not break when there is no agent capability. That is useful, but it does not exercise:

- Claude parser failure;
- resolver degraded state;
- detector low-confidence fallback;
- telemetry continuing after agent-layer failure;
- terminal session continuing after agent-layer failure.

This still falls short of the Phase A5 failure-isolation requirement.

### P1 — Resolver/cursor/approval identity remain unresolved

The previous technical weaknesses remain:

- resolver still synthesizes paths from `ev.CWD` and does not prove real A1 log discovery;
- cursor still embeds raw first-line prefix and is collision-prone;
- approval IDs are still batch-level, not stable per approval.

These are secondary to the scope/product-boundary blocker, but they matter before
production exposure.

## Required next changes

Choose one of two paths.

### Path A — Keep original A5 acceptance

Implement the product-boundary slice:

1. Add a real bridge from `internal/agent.ParseResult` to the existing
   `term.SessionTelemetry` / `models.AgentEvent` / event store path.
2. Add API tests proving Claude-derived events/status/approval/degraded state appear in
   actual `/api/sessions` or history responses.
3. Add mobile-facing schema tests using the real response shape.
4. Prove parser/resolver/detector failures do not break terminal sessions.
5. Add tmux + Claude and LocalPTY + Claude semantic equivalence coverage.
6. Fix resolver discovery, cursor, and approval ID stability.

### Path B — Intentionally split A5

If product wiring should really move to A8-A9:

1. Update `AGENT_ADAPTER_LAYER_PLAN.md` and `NEXT_SESSION_AGENT_ADAPTER_HANDOFF.md` to
   rename this milestone as `A5a` / `A5-boundary-compat`.
2. Add a new explicit phase for product-boundary wiring.
3. Move the original A5 acceptance bullets there.
4. Ask for verifier acceptance of the revised plan before requesting implementation
   acceptance.

## Current judgment

`c142647f4` is a useful compatibility-test improvement, but it is not Phase A5 completion
under the accepted plan.

Do not proceed to Phase A6 unless the team explicitly revises the plan and accepts the
scope split.
