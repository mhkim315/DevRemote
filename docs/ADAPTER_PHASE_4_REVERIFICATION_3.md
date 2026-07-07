# Adapter Phase 4 Reverification 3

Date: 2026-07-07

Executor commit: `71cdaec8a`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope

This pass reviewed the Phase 4 correction after
`docs/ADAPTER_PHASE_4_REVERIFICATION_2.md`.

The revision updates:

- `companion-daemon/internal/mux/adapter_contract_test.go`
- `companion-daemon/internal/mux/tmux_adapter_test.go`

No production code changed.

## Automated verification

Commands run:

```sh
GOCACHE=/tmp/devremote-phase4-71cdaec-go-cache go test ./internal/mux -run 'Test(Tmux|Cmux)Adapter_Contract|Test.*Contract|TestRegistry_SlowAdapterDoesNotBlock|TestProbeAdapter' -count=20 -v
GOCACHE=/tmp/devremote-phase4-71cdaec-go-cache go test ./internal/mux -count=1
GOCACHE=/tmp/devremote-phase4-71cdaec-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase4-71cdaec-go-cache go test ./...
GOCACHE=/tmp/devremote-phase4-71cdaec-go-cache go test -race ./...
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

Sandboxed full test and race runs failed only because `httptest` could not bind
localhost ports:

```text
bind: operation not permitted
```

The same commands passed with local listener permission.

## Improvements confirmed

The revision improves the previous state:

- `testFactoryCtxTimeout` now fails on nil error instead of logging a passing
  warning.
- tmux now declares `ExpectStreamOpener: true`.
- tmux now runs `RunLiveStreamContract`.
- `RunLiveStreamContract` now checks `OpenStream` errors, read progress, and
  `Resize` errors more carefully.
- `RunProcessSnapshotContract` now cross-checks snapshot keys against
  discovered session IDs.

These are useful changes. The remaining issue is that the harness can still
pass broken adapters in important Phase 4 contract areas.

## Blocking findings

### 1. Factory cancellation contract still allows false success

Current `testFactoryCtxCancel`:

```go
sessions, err := adapter.ListSessions(ctx)
if sessions != nil && err == nil {
    t.Error("ListSessions with cancelled ctx returned sessions without error")
}
```

This still passes if an adapter returns `nil, nil` for a cancelled context. It
also does not assert `errors.Is(err, context.Canceled)`.

The previous verifier action was explicit: cancelled context must not be a
successful result, and the cancellation taxonomy should be preserved unless the
suite is deliberately testing a lower-level runner contract instead.

Required executor action:

- Change cancellation to a hard contract:
  - `err` must be non-nil for a cancelled context;
  - `errors.Is(err, context.Canceled)` must pass for factory adapter paths that
    claim context support.
- If a specific adapter requires a runner-level test instead, make that
  explicit in config and fail if neither adapter-level nor runner-level
  cancellation is verified.

### 2. Live stream write contract is still incomplete

Phase 4 requires live read/write/resize/close. The current live suite checks
`InputWriter` if the session implements it, but does not exercise
`TerminalStream.Write`.

This matters for tmux: `tmuxSession` exposes live input through the opened PTY
stream's `Write`, not through `InputWriter`. In the targeted run,
`TestTmuxAdapter_Contract/LiveStream/WriteInput` was skipped:

```text
no InputWriter
```

So tmux now runs the live suite, but its live write path is still not contract
tested.

Required executor action:

- Add a live write assertion for `TerminalStream.Write`.
- If an adapter uses `InputWriter`, test that path too.
- Do not treat missing `InputWriter` as sufficient coverage for adapters whose
  live stream itself is writable.

### 3. Live read contract still accepts EOF-only behavior

The read test now fails on `n == 0 && err == nil`, but a stream that immediately
returns `0, io.EOF` still passes. That is not a useful live read contract for
the mock-backed adapters.

Required executor action:

- For mock-backed contract factories, assert expected output bytes or at least
  `n > 0`.
- If a real backend attach path cannot provide deterministic output, split that
  into a documented smoke-style test and keep the mock-backed contract strict.

### 4. Process snapshot identity checks only key subset, not coverage

`RunProcessSnapshotContract` now checks that each snapshot key appears in the
discovered session set. That is an improvement, but it still allows a snapshot
with only one arbitrary discovered session while omitting other sessions.

Phase 4's `process snapshot key identity` requirement is meant to prevent key
mismatch across telemetry consumers. The suite should define whether snapshot
coverage is expected for all discovered sessions or only for sessions with
agent processes, and assert that rule.

Required executor action:

- Define the intended coverage rule.
- For cmux's mock contract, assert the exact expected key set.
- Avoid accepting any non-empty subset as sufficient identity coverage.

## Verdict

Phase 4 is **not accepted** at `71cdaec8a`.

The revision is closer and all automated tests pass, but the harness still does
not reliably fail broken adapters for cancellation, live write/read, and process
snapshot identity. Since Phase 4 is specifically the gate before fixture adapter
expansion, these false-positive paths need to be closed before proceeding to
Phase 5.
