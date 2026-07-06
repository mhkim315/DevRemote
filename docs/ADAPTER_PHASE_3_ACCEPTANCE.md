# Adapter Phase 3 Acceptance

Date: 2026-07-07

Executor commit: `2da2c81c80b167bae2b1a683678eaec093af1bca`

Verifier decision: **ACCEPT**

Next phase permission: **ALLOWED**

## Scope

This verification reviewed the Phase 3 correction after the previous blocked
decision in `docs/ADAPTER_PHASE_3_REVERIFICATION_3.md`.

The accepted executor change modifies:

- `companion-daemon/internal/mux/registry.go`
- `companion-daemon/internal/mux/tmux_adapter.go`
- `companion-daemon/internal/mux/phase1_test.go`

The onboarding document added after the prior verifier pass is not part of the
runtime acceptance decision.

## Automated verification

Commands run:

```sh
GOCACHE=/tmp/devremote-phase3-2da2c81-go-cache go test ./internal/mux -run 'TestRegistry_SlowAdapterDoesNotBlock|TestProbeAdapter|TestTmuxAdapter|Test.*Runner|TestRefresh_(TimeoutWrapsErrTimeout|CancelNotTimeout)' -count=50 -v
GOCACHE=/tmp/devremote-phase3-2da2c81-go-cache go test ./internal/mux -count=1
GOCACHE=/tmp/devremote-phase3-2da2c81-go-cache go test ./internal/mux ./internal/term -count=1
GOCACHE=/tmp/devremote-phase3-2da2c81-go-cache go test -race ./internal/mux -count=1
GOCACHE=/tmp/devremote-phase3-2da2c81-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase3-2da2c81-go-cache go test ./...
npx tsc --noEmit
```

Results:

- Targeted mux tests passed for 50 iterations.
- `TestRegistry_SlowAdapterDoesNotBlock` consistently completed at about
  `0.20s`, matching the newly documented collection-window behavior.
- `go test ./internal/mux` passed.
- `go test ./internal/mux ./internal/term` passed when run outside the sandbox
  listener restriction.
- `go test -race ./internal/mux` passed.
- `go vet ./...` passed.
- `go test ./...` passed when run outside the sandbox listener restriction.
- `npx tsc --noEmit` passed.

Sandbox note:

The sandboxed full Go test run failed only because `httptest` could not bind
localhost ports:

```text
bind: operation not permitted
```

The same tests passed with local listener permission.

## Findings from this pass

### 1. Registry slow-adapter behavior is now explicitly bounded

Accepted.

`Registry.Sessions` now documents and implements this Phase 3 behavior:

- each adapter refresh gets its own 3 second timeout;
- results are collected as they arrive;
- after the first adapter result, a 200ms collection window batches
  near-simultaneous results;
- the caller context remains the hard upper bound.

This is materially different from the rejected hidden 500ms global wait. The
remaining delay is now an explicit batching policy and is exercised repeatedly
by tests.

The verifier still expects Phase 4's shared contract harness to make this even
more systematic, but Phase 3 no longer blocks on this item.

### 2. Tmux runner contract tests are sufficiently strengthened

Accepted.

The runner tests now record calls and assert command/option behavior for the
major tmux paths:

- `CreateSession`
- `TerminateSession`
- `ReadScreen`
- `ReadHistory`
- `ProcessInfo`

They also cover representative runner error propagation. This closes the prior
gap where tests only checked that a runner was called.

### 3. Probe and refresh error taxonomy is sufficiently covered

Accepted.

The tests now assert:

- refresh timeout wraps `ErrTimeout` and `context.DeadlineExceeded`;
- cancellation preserves `context.Canceled` and is not misclassified as
  timeout;
- probe cancellation preserves `context.Canceled`;
- probe deadline behavior preserves `context.DeadlineExceeded`;
- adapter unavailability remains observable through `ErrAdapterUnavailable`;
- stale snapshots remain present after failed refresh paths.

This is enough for Phase 3. Phase 4 can consolidate these into reusable adapter
contract tests.

### 4. Unsafe tmux binary assumption was removed

Accepted.

The previous comment claiming tmux is always in `PATH` was replaced with the
actual behavior: `tmuxExecRunner` shells out through `exec.CommandContext`, and
callers must handle lookup/exec failures rather than treating them as an empty
successful result.

The tmux list path already preserves command output details on failure, and the
new runner-error tests cover representative failure propagation without needing
real tmux.

## Non-blocking follow-ups

These are not Phase 3 blockers:

- Phase 4 should move the registry latency, timeout/cancel taxonomy, stale
  snapshot, and command-runner behavior into a reusable contract harness.
- The `ProcessInfo` runner test can be made stricter by asserting the full
  display-message argument list rather than only the command prefix.
- The tmux exec runner could get a focused unit test for an intentionally
  missing binary command, parallel to the cmux lookup-error test.

## Verdict

Phase 3 is **accepted** at
`2da2c81c80b167bae2b1a683678eaec093af1bca`.

The previous strict blockers are resolved enough to proceed. The next work may
start Phase 4: common adapter contract test harness.
