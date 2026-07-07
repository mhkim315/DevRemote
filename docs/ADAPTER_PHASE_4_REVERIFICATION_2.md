# Adapter Phase 4 Reverification 2

Date: 2026-07-07

Executor commit: `0ddc27c6b`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope

This pass reviewed the Phase 4 correction after
`docs/ADAPTER_PHASE_4_REVERIFICATION.md`.

The revision updates only test harness files:

- `companion-daemon/internal/mux/adapter_contract_test.go`
- `companion-daemon/internal/mux/tmux_adapter_test.go`
- `companion-daemon/internal/mux/cmux_adapter_mock_test.go`

## Automated verification

Commands run:

```sh
GOCACHE=/tmp/devremote-phase4-0ddc27c-go-cache go test ./internal/mux -run 'Test(Tmux|Cmux)Adapter_Contract|Test.*Contract|TestRegistry_SlowAdapterDoesNotBlock|TestProbeAdapter' -count=20 -v
GOCACHE=/tmp/devremote-phase4-0ddc27c-go-cache go test ./internal/mux -count=1
GOCACHE=/tmp/devremote-phase4-0ddc27c-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase4-0ddc27c-go-cache go test ./...
GOCACHE=/tmp/devremote-phase4-0ddc27c-go-cache go test -race ./...
npx tsc --noEmit
```

Results:

- Targeted contract tests passed for 20 iterations.
- `go test ./internal/mux` passed.
- `go vet ./...` passed.
- `go test ./...` passed when rerun outside the sandbox listener/socket
  restriction.
- `go test -race ./...` passed when rerun outside the sandbox listener/socket
  restriction.
- `npx tsc --noEmit` passed.

Sandbox note:

Sandboxed full test and race runs failed only because `httptest` and Unix socket
tests could not bind local addresses:

```text
bind: operation not permitted
```

The same commands passed with local listener/socket permission.

## Improvements confirmed

The revision addresses several previous findings in the right direction:

- `ContractConfig` now lets tmux/cmux declare expected capabilities so expected
  capability disappearance fails instead of silently skipping.
- `RunLiveStreamContract` exists and is wired for cmux.
- `RunProcessSnapshotContract` exists and is wired for cmux.
- `RunCreateDiscoverTerminateContract` now rediscoveres after termination and
  asserts the created session is gone.
- tmux and cmux mock factories now mutate create/terminate state more
  consistently.
- The harness labels factory-adapter tests separately from Registry/core helper
  tests.

These are real improvements, but the suite is still not strong enough to accept
Phase 4.

## Blocking findings

### 1. Factory context timeout/cancellation tests are non-enforcing

The previous rejection required the factory adapter path to prove context
timeout/cancellation behavior. The current tests still allow broken behavior to
pass.

Current `testFactoryCtxCancel`:

```go
sessions, err := adapter.ListSessions(ctx)
if sessions != nil && err == nil {
    t.Error("ListSessions with cancelled ctx returned sessions without error")
}
```

An adapter that returns `nil, nil` for a cancelled context passes this test. The
test also does not assert `errors.Is(err, context.Canceled)`.

Current `testFactoryCtxTimeout`:

```go
_, err := adapter.ListSessions(ctx)
if err == nil {
    t.Log("factory adapter did not reject timeout; may be mock limitation")
}
```

This is not a contract. The targeted run repeatedly printed:

```text
factory adapter did not reject timeout; may be mock limitation
```

for both tmux and cmux, while the tests still passed.

Required executor action:

- Make cancellation a hard assertion for factory adapters:
  - cancelled context must not return successful sessions;
  - error must preserve `context.Canceled`, or the suite must explicitly test
    the injected runner path that should produce that error.
- Make timeout a hard assertion where the adapter/runner supports a blocking
  path:
  - timeout must preserve `context.DeadlineExceeded`;
  - a mock limitation should be solved by the mock factory, not logged as a
    passing test.
- If a factory cannot exercise this path directly, split the requirement into a
  mandatory runner-level contract and a documented adapter-level exemption.

### 2. tmux live stream capability is implemented but not contract-tested

`tmuxSession` implements `OpenStream(ctx)`, so tmux sessions satisfy
`StreamOpener`. The Phase 4 plan requires capability suites for live
read/write/resize/close.

However, `TestTmuxAdapter_Contract` does not set `ExpectStreamOpener` and does
not run `RunLiveStreamContract`. This means tmux can lose or break its live
stream behavior without the Phase 4 harness catching it.

Required executor action:

- Either wire a safe tmux live-stream mock contract, or explicitly document why
  tmux live stream cannot be tested in the common harness and add a separate
  tmux-specific live stream contract.
- Do not leave an implemented capability outside the expected capability set.

### 3. Live stream suite is too weak as a gate

`RunLiveStreamContract` now exists, but it does not yet prove enough behavior:

- `Read` only checks that `Read` returns before timeout; it does not assert
  non-empty or expected output.
- `Resize` ignores the returned error.
- `WriteInput` skips if `InputWriter` is missing even when live input is
  expected.
- close behavior only checks that `Read` returns after `Close`, not that the
  stream releases/cancels an in-flight operation in the adapter's supported
  path.

This is an improvement over no suite, but it is not yet the live
read/write/resize/close contract required before Phase 5.

Required executor action:

- Assert expected output bytes for mock-backed live reads.
- Assert `Resize` error result.
- Add capability expectation for live input, or include it in the stream
  expectation where applicable.
- Ensure close/cancel behavior is tied to the adapter's in-flight operation
  path, not just a post-close read.

### 4. Process snapshot key identity is still under-specified

`RunProcessSnapshotContract` now exists, but it only checks:

- returned map is not nil;
- keys are not empty.

Phase 4 specifically requires process snapshot key identity. The current suite
does not verify that keys match session IDs/canonical IDs/local IDs according to
the intended contract, nor does it compare snapshot entries against discovered
sessions.

Required executor action:

- Define the expected key identity rule.
- Compare `ProcessSnapshot` keys against discovered sessions from the same
  adapter.
- Fail if snapshot keys are unrelated arbitrary non-empty strings.

## Verdict

Phase 4 is **not accepted** at `0ddc27c6b`.

The revision is closer than `e6560eb31`, and the automated tests pass. The
remaining problem is still false confidence: the harness can pass while
factory adapter timeout/cancel behavior is not enforced, an implemented tmux
live capability is outside the expected suite, and process snapshot identity is
not actually checked.

The next revision should make the new harness fail on these broken cases rather
than logging or skipping them.
