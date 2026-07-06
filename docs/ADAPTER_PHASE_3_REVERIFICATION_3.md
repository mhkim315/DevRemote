# Adapter Phase 3 Reverification 3

Date: 2026-07-07

Target commit: `4a41eff565ff`

Verifier verdict: **blocked**

## Scope

This pass reviewed the third Phase 3 revision after the previous rejection at
`d6f7be0`. The review is limited to validating whether Phase 3 now proves
adapter lifecycle/concurrency, runner injection, and probe/error contracts well
enough to proceed.

Changed files since the previous verifier commit:

- `companion-daemon/internal/mux/registry.go`
- `companion-daemon/internal/mux/phase1_test.go`
- `companion-daemon/internal/term/gemini_resolver.go`

## Verification performed

Commands:

```sh
GOCACHE=/tmp/devremote-phase3-reverify3-go-cache go test ./internal/mux -run "TestRegistry_SlowAdapterDoesNotBlock|TestProbeAdapter_(Healthy|Unavailable|Timeout|Cancel)|TestTmuxAdapter_(CreateSessionUsesRunner|TerminateSessionUsesRunner)|Test.*Runner" -count=50 -v
GOCACHE=/tmp/devremote-phase3-reverify3-go-cache go test ./internal/mux ./internal/term -count=1
npx tsc --noEmit
```

Results:

- Targeted mux tests passed repeatedly.
- Full `internal/mux` and `internal/term` tests passed when rerun outside the
  filesystem/network sandbox. The sandboxed run failed only because `httptest`
  could not bind a local port.
- TypeScript check passed.

## Improvements confirmed

### Registry slow-adapter behavior improved

`Registry.Sessions` no longer waits for the full per-adapter 3 second timeout
before returning fast adapter results. The repeated targeted test run showed
`TestRegistry_SlowAdapterDoesNotBlock` completing at about `0.50s`.

This is a real improvement over the previous `2.00s` behavior.

### Minimal tmux runner smoke coverage added

`TestTmuxAdapter_CreateSessionUsesRunner` and
`TestTmuxAdapter_TerminateSessionUsesRunner` now confirm that those paths call
the injected runner at least once.

### Resolver deferral comment is less contradictory

The duplicate Phase 3/Phase 4 contradiction in `gemini_resolver.go` was reduced.
The code still hardcodes `tmux`, but this is now at least consistently deferred
to a later adapter-agnostic resolver phase.

## Remaining blockers

### 1. `Registry.Sessions` still has a fixed 500ms collector delay

The implementation now uses:

```go
deadline := time.After(500 * time.Millisecond)
```

This prevents the old 3 second slow-adapter block, but it still makes every
multi-adapter discovery wait up to 500ms when any adapter is slow. The Phase 3
intent was that slow adapters must not delay healthy adapter discovery.

If 500ms is the intended Phase 3 SLA, the plan and tests need to say that
explicitly. If the intent is immediate return after currently available healthy
results, the implementation still does not satisfy it.

The new test is also too loose:

```go
if elapsed > 2*time.Second {
    t.Errorf(...)
}
```

That assertion proves only "not 3 seconds"; it does not lock the intended
contract. It would allow a large discovery latency regression.

Required fix:

- Define the intended discovery latency contract.
- Assert that contract directly in tests.
- Avoid a hidden fixed delay unless it is an explicit, documented API behavior.

### 2. tmux runner tests still do not prove the command contract

The new tmux tests only assert that a runner was called. They do not verify:

- exact command and argument sequence;
- create-session name handling;
- create-session working directory option handling;
- terminate-session resolve-then-kill behavior;
- error propagation from the runner;
- `ReadScreen`, `ReadHistory`, and `ProcessInfo` runner usage.

This leaves the Phase 3 runner-injection contract under-specified. A regression
could call the runner with the wrong command and these tests would still pass.

Required fix:

- Record runner calls and assert exact args/options for create, terminate,
  screen, history, and process-info paths.
- Add failure-path assertions for runner errors.

### 3. ProbeAdapter timeout taxonomy and snapshot behavior remain under-tested

`TestProbeAdapter_Timeout` still checks only that an error exists and that it
matches `ErrAdapterUnavailable`. It does not prove whether timeout errors
preserve `context.DeadlineExceeded` / `ErrTimeout` semantics.

The tests also still do not prove that probe failures preserve the last
successful snapshot, which is central to the Phase 3 adapter lifecycle contract.

Required fix:

- Assert timeout taxonomy explicitly.
- Assert cancellation taxonomy explicitly.
- Add a stale-snapshot preservation test for failed probes/refreshes.

### 4. tmux binary and stderr policy is still unsafe

`tmux_adapter.go` still contains:

```go
// No binary discovery: tmux is a core macOS/Linux tool always in PATH.
```

That is not a safe production assumption. Phase 3 does not have to implement a
full adapter binary discovery system, but it should at least avoid documenting
an unsafe invariant and should test the no-binary / lookup-error path.

Required fix:

- Replace the unsafe comment with the actual current behavior.
- Add tests for binary lookup failure, stderr/combined output diagnostics, and
  timeout/cancellation propagation.

## Verdict

Phase 3 is **not accepted** at `4a41eff565ff`.

The revision fixes the most visible symptom from the previous pass: a slow
adapter no longer forces a full multi-second wait. However, the current code and
tests still do not lock down the Phase 3 contracts tightly enough to use this as
the base for real third-backend expansion.

The next revision should focus on contract precision, not broad refactoring:

1. make the `Registry.Sessions` latency contract explicit and test it directly;
2. strengthen tmux runner tests from "called" to exact command/option contracts;
3. prove ProbeAdapter timeout/cancel taxonomy and stale-snapshot preservation;
4. remove or test the unsafe tmux binary assumption.
