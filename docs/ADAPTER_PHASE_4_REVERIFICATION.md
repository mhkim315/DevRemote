# Adapter Phase 4 Reverification

Date: 2026-07-07

Executor commit: `e6560eb31`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope

This pass reviewed the Phase 4 contract harness work introduced by:

- `323a6ce feat: Phase 4a contract test harness with tmux/cmux wiring`
- `e6560eb feat: Phase 4b lifecycle contracts + 4c capability suites`

Changed runtime code: none.

Changed test files:

- `companion-daemon/internal/mux/adapter_contract_test.go`
- `companion-daemon/internal/mux/tmux_adapter_test.go`
- `companion-daemon/internal/mux/cmux_adapter_mock_test.go`
- `companion-daemon/internal/mux/phase1_test.go`

## Automated verification

Commands run:

```sh
GOCACHE=/tmp/devremote-phase4-e6560eb-go-cache go test ./internal/mux -run 'Test(Tmux|Cmux)Adapter_Contract|Test.*Contract|TestRegistry_SlowAdapterDoesNotBlock|TestProbeAdapter' -count=20 -v
GOCACHE=/tmp/devremote-phase4-e6560eb-go-cache go test ./internal/mux -count=1
GOCACHE=/tmp/devremote-phase4-e6560eb-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase4-e6560eb-go-cache go test ./...
GOCACHE=/tmp/devremote-phase4-e6560eb-go-cache go test -race ./...
npx tsc --noEmit
```

Results:

- Targeted tmux/cmux contract tests passed for 20 iterations.
- `go test ./internal/mux` passed.
- `go vet ./...` passed.
- `go test ./...` passed when rerun outside the sandbox listener restriction.
- `go test -race ./...` passed when rerun outside the sandbox listener
  restriction.
- `npx tsc --noEmit` passed.

Sandbox note:

The sandboxed `go test ./...` and `go test -race ./...` runs failed because
`httptest` could not bind localhost ports:

```text
bind: operation not permitted
```

The same commands passed with local listener permission.

## Improvements confirmed

### Reusable contract harness exists

`RunAdapterContract` now gives new adapters a one-call entry point for a broad
required suite. tmux and cmux both register the suite with mock runners.

Covered areas include:

- name stability;
- basic list behavior;
- snapshot/list identity;
- concurrent list safety;
- stale snapshot after transient failure;
- probe timeout/cancel taxonomy;
- sentinel error distinctness;
- canonical ID colon/Unicode/URL round-trip behavior;
- stale session ended race;
- unavailable vs not-found distinction;
- create returns local ID;
- slow adapter does not block healthy discovery.

### Capability suites were started

The new harness includes separate suites for:

- screen/history;
- process info;
- create → discover → terminate.

This is the right direction and the tests are useful. The issue is that the
Phase 4 contract is still incomplete.

## Blocking findings

### 1. Live stream read/write/resize/close contract suite is missing

Phase 4 explicitly requires a capability suite for:

```text
live read/write/resize/close
```

No reusable live stream suite exists. Searches found no `RunLive...Contract`
equivalent, and the existing stream tests remain cmux-specific
(`TestPollScreenBlockedWriteClose`,
`TestPollScreenCloseCancelsInFlightCommand`).

This is a Phase 4 blocker because the next Phase 5 fixture adapter is supposed
to prove discovery + live stream + resize without changing core handlers. A
third adapter could pass the current contract harness while having broken live
I/O, write, resize, or close behavior.

Required executor action:

- Add a reusable live stream capability suite for sessions implementing
  `StreamOpener` / `TerminalStream`.
- Assert at least:
  - `OpenStream(ctx)` returns a non-nil stream or a typed unsupported path;
  - `Read` can receive expected output from a mock-backed stream;
  - `Write` or `InputWriter` behavior is exercised according to the adapter's
    supported live I/O model;
  - `Resize(rows, cols)` is called/propagated where supported;
  - `Close()` releases/cancels blocked reads or in-flight operations.
- Wire this suite to tmux/cmux mocks where the capability is claimed.

### 2. Several required contract tests do not test the factory adapter

`RunAdapterContract` accepts an adapter factory, but some subtests bypass the
factory adapter and test helper adapters instead:

- `testListSessionsEmpty` uses `&testAdapter{name: "empty"}`.
- `testListSessionsContextCancellation` uses `blockingAdapter`.
- `testListSessionsContextTimeout` uses `blockingAdapter`.
- `testStaleSnapshotOnFailure` uses `configurableAdapter`.
- `testProbeAdapterTaxonomy` uses `blockingAdapter`.
- parts of unavailable/not-found behavior use `configurableAdapter`.

Some of these helper-based tests are valid Registry/core contract tests, but
they do not prove that the adapter under test honors the contract.

The most important gap is cancellation/timeout. A new adapter whose
`ListSessions(ctx)` ignores cancellation could still pass the current
`RunAdapterContract`, because the cancellation and timeout checks do not call
the factory adapter.

Required executor action:

- Split the harness clearly into:
  - adapter implementation contract tests that exercise the factory adapter;
  - Registry/core helper tests that intentionally use synthetic adapters.
- For factory adapter contracts, add a way for each adapter mock factory to
  expose a cancellable/blocking ListSessions path and assert that the real
  adapter path respects context cancellation/timeouts.
- If a specific adapter cannot express a certain error path through its mock
  runner, document and test the runner-level context behavior instead.

### 3. Create → discover → terminate suite does not prove termination

`RunCreateDiscoverTerminateContract` currently:

1. creates a session;
2. verifies the created ID appears in discovery;
3. calls terminate.

It does not invalidate/list again after termination to prove the session
disappears or that the termination command affected adapter state.

This is visible in the cmux mock wiring: `close-surface` returns success but
does not remove the created surface from the mock state. The contract still
passes.

Required executor action:

- After `TerminateSession`, invalidate and rediscover.
- Assert the created session no longer appears.
- Update tmux/cmux mock factories so terminate mutates mock state consistently.
- Keep separate tests for unsupported terminator behavior where applicable.

### 4. Process capability suite does not cover process snapshot key identity

Phase 4 requires:

```text
process snapshot key identity
```

The current `RunProcessInfoContract` only checks a single session's
`ProcessInfo` result and accepts any result where either PID or CWD is non-empty.
There is no reusable `ProcessSnapshotProvider` suite, no assertion that snapshot
keys match canonical/local session identity, and no coverage for batch process
snapshot behavior.

Required executor action:

- Add a reusable `ProcessSnapshotProvider` contract suite.
- Assert snapshot keys match the intended session identity rules.
- Wire it for cmux if cmux claims/provides batch process snapshots.
- For adapters without batch snapshot support, skip explicitly based on
  capability absence.

### 5. Capability coverage is not explicit enough for unsupported paths

The current capability suites skip when a session does not implement a
capability. That is acceptable for optional capabilities, but Phase 4 also
requires that a new adapter can prove unsupported behavior is predictable.

The current harness does not force an adapter author to declare which optional
capabilities are expected for the factory, and it does not fail if a mock
accidentally stops exposing a capability that the adapter is supposed to support.

Required executor action:

- Add a small expectation/config object for contract registration, or otherwise
  split required vs optional capability suites explicitly.
- For tmux/cmux, assert the expected capability suites run rather than silently
  skipping.
- Unsupported capability behavior can remain a Phase 5 fixture focus, but Phase
  4 should at least prevent accidental skip-based false positives for tmux/cmux.

## Verdict

Phase 4 is **not accepted** at `e6560eb31`.

The implementation is a useful foundation and the automated tests pass, but the
contract harness is not yet strong enough to be the gate for adding a fixture
adapter or real third backend. The main issue is false confidence: a new adapter
can pass the current suite while ignoring context cancellation, not implementing
live stream semantics, or having a no-op terminate path.

The next revision should focus on tightening the harness rather than adding
production code.
