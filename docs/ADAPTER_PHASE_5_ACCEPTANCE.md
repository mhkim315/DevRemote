# Adapter Phase 5 Acceptance

Date: 2026-07-07

Executor commit: `3e8c53ea8`

Verifier decision: **ACCEPT**

Next phase permission: **ALLOWED**

## Scope

This pass reviewed the Phase 5 correction after
`docs/ADAPTER_PHASE_5_REVERIFICATION_8.md`.

The accepted Phase 5 work adds test-only fixture coverage and clarifies the
Phase 5 scope contract:

- `companion-daemon/internal/mux/fixture_adapter_test.go`
- `companion-daemon/internal/term/fixture_e2e_test.go`
- `docs/ADAPTER_EXPANSION_PLAN.md`

Phase 4 to Phase 5 product-code diff, excluding docs, is limited to two new
test files:

```text
A companion-daemon/internal/mux/fixture_adapter_test.go
A companion-daemon/internal/term/fixture_e2e_test.go
```

No tmux/cmux production files changed for the fixture adapter.

## Automated verification

Commands run at `3e8c53ea8`:

```sh
git diff --check
GOCACHE=/tmp/devremote-phase5-3e8c53e-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase5-3e8c53e-go-cache go test ./...
GOCACHE=/tmp/devremote-phase5-3e8c53e-go-cache go test -race ./...
cd mobile && node_modules/.bin/tsc --noEmit
```

Previously run against the same Phase 5 fixture implementation at
`6fe6b13b9`:

```sh
GOCACHE=/tmp/devremote-phase5-6fe6b13-go-cache go test ./internal/term -run 'TestFixture|Fixture' -count=20 -v
```

Results:

- `git diff --check` passed.
- Targeted fixture E2E tests passed for 20 iterations.
- `go vet ./...` passed.
- `go test ./...` passed with local listener permission.
- `go test -race ./...` passed with local listener permission.
- `node_modules/.bin/tsc --noEmit` passed.

Sandbox note:

Sandboxed Go tests that use `httptest` local listeners fail with
`bind: operation not permitted`. The same commands pass when local listener
binding is allowed.

## Acceptance findings

### 1. Fixture adapter proves test-only extension without tmux/cmux edits

Accepted.

The fixture adapter is introduced only in tests. From the accepted Phase 4
commit to `3e8c53ea8`, the non-document product diff adds only fixture test
files. tmux/cmux runtime files are not changed to accommodate the fixture.

This satisfies the Phase 5 requirement that a new adapter shape can be added
without editing existing backend implementations.

### 2. Discovery and API/WebSocket boundaries are covered

Accepted.

The term fixture E2E tests cover:

- fixture sessions appearing through the sessions API;
- create/delete lifecycle behavior;
- WebSocket initial screen and live stream output;
- WebSocket input forwarding into the fixture session;
- direct stream content behavior.

This is sufficient to prove that a non-tmux/non-cmux backend can pass through
the term handler path without handler-specific adapter branches.

### 3. Unsupported capability path is covered

Accepted.

The fixture includes a session that intentionally omits optional capabilities.
The E2E test verifies that requesting unavailable history returns the expected
not-found/unavailable behavior instead of panic, fallback, or silent success.

### 4. Resize scope is now explicitly defined

Accepted with documented scope.

The previous blocker was that Phase 5 required resize but the product has no
server-side `/term/size` Go handler. At `3e8c53ea8`,
`docs/ADAPTER_EXPANSION_PLAN.md` now explicitly states:

- production has no server-side `/term/size` handler;
- tmux/cmux rely on xterm.js client-side resize;
- Phase 5 verifies `TerminalStream.Resize` at the interface/contract level;
- HTTP boundary resize proof is scoped out of Phase 5;
- server-side resize handler work is deferred to Phase 6+.

That scope is coherent with the current architecture and closes the prior
contract mismatch.

### 5. Mobile schema scope is now explicitly defined

Accepted with documented scope.

The previous blocker was that the mobile schema boundary was represented by a
backend JSON key test rather than a mobile-side typed fixture test. At
`3e8c53ea8`, `docs/ADAPTER_EXPANSION_PLAN.md` now explicitly states:

- mobile uses `adapter: string`, not an enum of known backend names;
- unknown adapter names such as `fixture` are intended to compile;
- required JSON key verification is performed in Go backend tests;
- no mobile-specific typed fixture test infrastructure exists in this phase;
- `npx tsc --noEmit` is the accepted mobile schema boundary for Phase 5.

That scope is sufficient for Phase 5 because the goal is to prevent
backend-name coupling before a real third backend is introduced.

## Non-blocking follow-ups

These do not block Phase 5:

- Normal WebSocket test shutdown can log
  `WS stream read err: io: read/write on closed pipe`. Tests and race checks
  pass, but future cleanup could quiet expected close paths.
- Phase 6 should avoid widening the now-accepted resize/mobile scope unless it
  explicitly updates the adapter contract and tests.

## Verdict

Phase 5 is **accepted** at `3e8c53ea8`.

The fixture adapter work now proves the intended no-production-registration,
no-tmux/cmux-edit extension path, and the remaining resize/mobile boundary
questions are explicitly scoped in the project plan. Phase 6 may begin.
