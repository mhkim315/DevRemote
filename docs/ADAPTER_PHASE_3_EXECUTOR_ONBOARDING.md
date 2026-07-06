# Adapter Phase 3 Executor Onboarding

Date: 2026-07-07

Audience: a fresh execution agent with no prior conversation context.

Current branch: `feature/phase10-multi-adapter`

Current verifier head before this onboarding document: `fd70496`

Latest rejected executor commit: `4a41eff565ff`

Latest unverified executor commit observed while writing this document:
`baa2a3d`

## Role

You are the **execution agent** for Phase 3 of the terminal adapter expansion
work.

Your job is not to redesign the whole adapter system. Your job is to make the
smallest coherent Phase 3 correction that satisfies the verifier's current
blockers, commit it, and push it for another verification pass.

Do not skip ahead to Phase 4. Do not implement the fixture adapter. Do not start
the real third backend.

## Required reading before editing

Read these files in this order:

1. `docs/ADAPTER_EXPANSION_PLAN.md`
2. `docs/ADAPTER_PHASE_0_ACCEPTANCE.md`
3. `docs/ADAPTER_PHASE_1_ACCEPTANCE.md`
4. `docs/ADAPTER_PHASE_2_ACCEPTANCE.md`
5. `docs/ADAPTER_PHASE_3_REVERIFICATION.md`
6. `docs/ADAPTER_PHASE_3_SELF_CHECK_REVIEW.md`
7. `docs/ADAPTER_PHASE_3_REVERIFICATION_2.md`
8. `docs/ADAPTER_PHASE_3_REVERIFICATION_3.md`

Then inspect the relevant code:

1. `companion-daemon/internal/mux/adapter.go`
2. `companion-daemon/internal/mux/registry.go`
3. `companion-daemon/internal/mux/tmux_adapter.go`
4. `companion-daemon/internal/mux/cmux_adapter.go`
5. `companion-daemon/internal/mux/phase1_test.go`
6. `companion-daemon/internal/term/gemini_resolver.go`

## Accepted baseline

These phases are already accepted and should not be reopened unless your change
would otherwise regress them:

- Phase 0 accepted at executor commit `d6166fd8`, verifier commit `efbde27`
- Phase 1 accepted at executor commit `1f1b5de`, verifier commit `056189a`
- Phase 2 accepted at executor commit `7a4018b`, verifier commit `c62a1b7`

The current Phase 3 work is not accepted yet.

## Current Phase 3 state

Executor revision `4a41eff565ff` improved behavior but was still rejected by
the verifier in `docs/ADAPTER_PHASE_3_REVERIFICATION_3.md`.

After that rejection, a newer executor commit was observed:

```text
baa2a3d fix: Phase 3 remove fixed deadline, precise runner tests, taxonomy
```

If you are starting from a checkout that includes `baa2a3d`, do not blindly
reapply the older instructions. First ask for or wait for the verifier's
decision on `baa2a3d`. If `baa2a3d` is rejected, use the verifier's latest
document as the primary source of truth and use this onboarding file only as
background context.

Confirmed improvements:

- `Registry.Sessions` no longer waits for the full per-adapter 3 second timeout
  before returning fast adapter sessions.
- The current slow-adapter test repeatedly completes in about `0.50s`.
- Basic tests now prove that `CreateSession` and `TerminateSession` call an
  injected tmux runner at least once.
- The `gemini_resolver.go` Phase 3/Phase 4 comment contradiction was reduced.

These improvements should be preserved unless the latest verifier decision says
otherwise.

## Current blockers to fix

### 1. Define and enforce the `Registry.Sessions` latency contract

Problem at rejected commit `4a41eff565ff`:

- `Registry.Sessions` uses a hidden fixed collector deadline:

  ```go
  deadline := time.After(500 * time.Millisecond)
  ```

- The test only asserts `< 2s`, which proves only that the old 3 second timeout
  is avoided.

Required correction:

- Decide and encode the actual Phase 3 contract.
- If the intended behavior is "return healthy adapter results without waiting
  for slow adapters", do not leave a hidden 500ms delay as the core policy.
- If a short grace window is intentionally required, document it as the explicit
  SLA and assert it directly with a tight test.
- The test must fail for large latency regressions.

Practical guidance:

- Keep stale snapshot preservation.
- Keep per-adapter timeout isolation.
- Avoid making one adapter's slow `ListSessions` delay every other adapter's
  healthy result path.
- Do not introduce goroutine leaks when returning early.

### 2. Strengthen tmux runner contract tests

Problem at rejected commit `4a41eff565ff`:

- `TestTmuxAdapter_CreateSessionUsesRunner` and
  `TestTmuxAdapter_TerminateSessionUsesRunner` only check a boolean `called`.

Required correction:

- Record runner calls and assert exact command arguments/options for:
  - `CreateSession`
  - `TerminateSession`
  - `ReadScreen`
  - `ReadHistory`
  - `ProcessInfo`
- For terminate, assert the expected resolve-then-kill sequence.
- For create, assert name handling and working directory option behavior.
- Add runner error propagation tests for representative paths.

The verifier will reject tests that only prove "some command was called."

### 3. Strengthen ProbeAdapter and refresh error taxonomy tests

Problem at rejected commit `4a41eff565ff`:

- Timeout/cancel tests check that an error exists, but do not prove the expected
  sentinel/context taxonomy.
- Stale snapshot preservation after failed refresh/probe is not proven tightly
  enough.

Required correction:

- Assert timeout behavior with `errors.Is`.
- Assert cancellation behavior with `errors.Is`.
- Assert that a transient adapter failure preserves the last successful
  snapshot.
- Assert that timeout/unavailable errors are not silently converted to
  `nil, nil` or an empty successful list.

### 4. Fix or test the tmux binary assumption

Problem at rejected commit `4a41eff565ff`:

`tmux_adapter.go` still says:

```go
// No binary discovery: tmux is a core macOS/Linux tool always in PATH.
```

This is not a valid production invariant.

Required correction:

- Replace the comment with the actual current behavior.
- Add tests for lookup/exec failure, stderr/combined-output diagnostics, and
  timeout/cancellation propagation where practical.
- Do not introduce a broad binary discovery subsystem unless it is necessary for
  Phase 3. The immediate requirement is to stop documenting an unsafe invariant
  and to preserve observable failure information.

## Non-goals for this correction

Do not do these in the next Phase 3 fix unless the verifier explicitly asks:

- Do not start Phase 4 contract harness.
- Do not add the fixture adapter.
- Do not add a real third backend.
- Do not rewrite the mobile UI.
- Do not change canonical ID format.
- Do not remove tmux internal `$session_id` targeting.
- Do not replace cmux GUI socket behavior with generic polling.
- Do not perform broad package renames.
- Do not hide adapter errors by returning empty successful results.

## Test expectations before handing back

At minimum, run:

```sh
cd companion-daemon
GOCACHE=/tmp/devremote-phase3-executor-go-cache go test ./internal/mux -count=1
GOCACHE=/tmp/devremote-phase3-executor-go-cache go test ./internal/mux ./internal/term -count=1
GOCACHE=/tmp/devremote-phase3-executor-go-cache go vet ./...
```

Also run the relevant targeted tests repeatedly, for example:

```sh
GOCACHE=/tmp/devremote-phase3-executor-go-cache go test ./internal/mux -run 'TestRegistry_SlowAdapterDoesNotBlock|TestProbeAdapter|TestTmuxAdapter|Test.*Runner' -count=20 -v
```

If `internal/term` fails in a sandbox with:

```text
httptest: failed to listen on a port: bind: operation not permitted
```

that is a sandbox limitation. Rerun in an environment that allows localhost
test listeners and record that distinction.

If mobile files are touched, also run:

```sh
cd mobile
npx tsc --noEmit
```

## Handoff format after your fix

When done, commit and push your changes. Then report:

```text
Phase 3 correction ready
Commit: <hash>

Summary:
- ...

Verification:
- ...

Known deferrals:
- ...
```

The verifier will independently inspect the diff and rerun tests. Do not rely on
claims in the summary as a substitute for tests and code evidence.

## Current verifier decision

Phase 3 remains blocked at `4a41eff565ff`.

`baa2a3d` was present on the remote when this onboarding document was added, but
had not yet been independently accepted in this document. Treat it as
**pending verification**, not accepted.

The next correction should focus on contract precision and test strength, not
new architecture. The most likely passing path is:

1. make `Registry.Sessions` latency behavior explicit and tightly tested;
2. upgrade tmux runner tests from boolean smoke tests to exact call-contract
   tests;
3. prove timeout/cancel/stale-snapshot error semantics;
4. remove or test the unsafe tmux binary assumption.
