# Adapter Phase 3 Executor Onboarding

Date: 2026-07-07

Audience: a fresh **execution agent** with no previous conversation context.

This document exists because the prior execution agent lost context. It is the
bootstrap instruction set for continuing the adapter expansion implementation
work without relying on chat history.

## Your role

You are the implementation/execution agent.

There is a separate verifier role. The verifier reviews your commits, writes
accept/reject documents, and decides whether a phase may proceed. Do not treat a
phase as accepted unless a verifier document explicitly says it is accepted.

Your task is to implement the smallest coherent correction requested by the
latest verifier document, commit it, push it, and hand the commit back for
verification.

## Branch and synchronization

Work on:

```text
feature/phase10-multi-adapter
```

Before editing:

```sh
git fetch origin feature/phase10-multi-adapter
git status --short --branch
git log --oneline -12
```

If your local branch is behind, fast-forward or rebase your local work onto
`origin/feature/phase10-multi-adapter`. Do not force-push. Do not discard
existing user/verifier commits.

## Current known timeline

Accepted phases:

- Phase 0 accepted: executor `d6166fd8`, verifier `efbde27`
- Phase 1 accepted: executor `1f1b5de`, verifier `056189a`
- Phase 2 accepted: executor `7a4018b`, verifier `c62a1b7`

Phase 3 is the active phase.

Known Phase 3 commits:

- `d2eb546` — initial Phase 3 attempt, rejected
- `0a6bd8e` — self-check revision, rejected
- `fe5e964` — second revision, rejected
- `4a41eff` — third revision, rejected by `fd70496`
- `baa2a3d` — newer executor revision observed after `fd70496`; treat as
  unaccepted until a verifier document accepts it

This onboarding document may not be the newest document by the time you read it.
Always use the latest verifier document as the primary source of truth.

## Required reading order

Read the documents first, then inspect code.

Documents:

1. `docs/ADAPTER_EXPANSION_PLAN.md`
2. `docs/ADAPTER_PHASE_0_ACCEPTANCE.md`
3. `docs/ADAPTER_PHASE_1_ACCEPTANCE.md`
4. `docs/ADAPTER_PHASE_2_ACCEPTANCE.md`
5. Latest Phase 3 verifier document, in chronological order if needed:
   - `docs/ADAPTER_PHASE_3_REVERIFICATION.md`
   - `docs/ADAPTER_PHASE_3_SELF_CHECK_REVIEW.md`
   - `docs/ADAPTER_PHASE_3_REVERIFICATION_2.md`
   - `docs/ADAPTER_PHASE_3_REVERIFICATION_3.md`
   - any newer `docs/ADAPTER_PHASE_3_*` document

Code:

1. `companion-daemon/internal/mux/adapter.go`
2. `companion-daemon/internal/mux/registry.go`
3. `companion-daemon/internal/mux/tmux_adapter.go`
4. `companion-daemon/internal/mux/cmux_adapter.go`
5. `companion-daemon/internal/mux/phase1_test.go`
6. `companion-daemon/internal/term/gemini_resolver.go`
7. Any file named by the latest verifier document

Use `rg` to find existing tests and hardcoded backend assumptions before
editing:

```sh
rg -n 'tmux|cmux|ProbeAdapter|SlowAdapter|CommandRunner|Runner|Sessions\\(|deadline|timeout|DeadlineExceeded|ErrTimeout|ErrAdapterUnavailable' companion-daemon/internal
```

## Work protocol

1. Identify the exact latest verifier decision.
2. Extract only the required executor actions from that decision.
3. Make minimal implementation/test changes to satisfy those actions.
4. Preserve accepted Phase 0/1/2 behavior.
5. Run targeted tests repeatedly.
6. Run package tests and vet.
7. Commit and push.
8. Report the commit hash and verification commands.

Do not implement unrelated cleanup while fixing a verifier rejection. If you
discover a real unrelated bug, document it in the handoff instead of expanding
the current patch.

## Phase 3 purpose

Phase 3 standardizes adapter execution environment and lifecycle behavior.

The goal is not to add a third backend yet. The goal is to make tmux/cmux
behavior precise enough that a third backend can later be added without
guessing lifecycle, timeout, runner, or error semantics.

Phase 3 acceptance requires evidence that:

- command execution is injectable/testable without real tmux/cmux binaries;
- binary/exec/stderr/timeout failures preserve useful diagnostics;
- availability probe and runtime refresh behavior are distinguishable;
- one bad/slow adapter does not block healthy adapter discovery;
- stale snapshots survive transient refresh failures;
- adapter health/error state remains observable;
- cmux invalidation does not require unsafe Registry back-calls;
- tests pin these contracts tightly enough to prevent regressions.

## Current known Phase 3 correction areas

These are the known areas from the last rejected verifier pass. If a newer
verifier document supersedes any item, follow the newer document.

### 1. Registry session collection latency

The verifier rejected a version where `Registry.Sessions` avoided a 3 second
delay but used a hidden fixed `500ms` collector deadline.

Required execution behavior:

- Do not let a slow adapter delay healthy adapter discovery beyond the explicit
  contract.
- If there is a grace window, name it, document it, and assert it directly.
- If the intended contract is immediate return of available healthy results,
  implement that instead of a hidden fixed wait.
- Ensure returning early does not leak goroutines or corrupt stale snapshots.

Tests should assert the actual latency contract, not a loose condition like
`elapsed < 2*time.Second`.

### 2. Tmux runner injection contract

The verifier rejected boolean-only tests that merely proved a runner was called.

Required execution behavior:

- Record runner calls in tests.
- Assert exact command arguments and options for:
  - `CreateSession`
  - `TerminateSession`
  - `ReadScreen`
  - `ReadHistory`
  - `ProcessInfo`
- For terminate, assert the expected lookup/resolve step and kill step.
- For create, assert session name handling and working directory behavior.
- Assert representative runner error propagation.

Do not leave tests that would pass if the wrong tmux command were invoked.

### 3. Probe/refresh error taxonomy and stale snapshots

Required execution behavior:

- Timeout errors should be checkable with `errors.Is`.
- Cancellation errors should be checkable with `errors.Is`.
- Adapter unavailability should not be converted to a successful empty list.
- Failed refresh/probe should preserve the last successful snapshot.

Tests must prove these behaviors directly.

### 4. Tmux binary and command failure policy

The verifier rejected the assumption that tmux is always in `PATH`.

Required execution behavior:

- Remove comments that claim tmux is always available.
- Preserve lookup/exec failure details.
- Preserve stderr/combined-output diagnostics where applicable.
- Preserve timeout/cancellation semantics.
- Do not build a broad binary discovery framework unless required by the latest
  verifier document.

## Hard constraints

Do not:

- start Phase 4;
- add the fixture adapter;
- add a real third backend;
- rewrite the mobile UI unless the latest verifier document explicitly requires
  it;
- change canonical ID format;
- remove support for local IDs containing `:`;
- break tmux internal `$session_id` targeting;
- replace cmux GUI socket/reconnect behavior with generic polling;
- hide adapter errors by returning `nil, nil` or empty successful results;
- broad-rename packages or interfaces outside the verifier's requested scope.

## Preservation requirements

Preserve these behaviors unless a verifier document explicitly changes the
contract:

- canonical ID format remains `<adapter>:<local-id>`;
- local IDs may contain `:` and Unicode;
- duplicate adapter names fail;
- deterministic session ordering remains Registry policy;
- stale snapshots are retained after transient adapter failures;
- tmux display name and internal tmux target remain distinct;
- cmux reconnect/invalidation behavior remains adapter-scoped;
- existing mobile/API compatibility from accepted phases remains intact.

## Test expectations

From `companion-daemon`:

```sh
GOCACHE=/tmp/devremote-phase3-executor-go-cache go test ./internal/mux -count=1
GOCACHE=/tmp/devremote-phase3-executor-go-cache go test ./internal/mux ./internal/term -count=1
GOCACHE=/tmp/devremote-phase3-executor-go-cache go vet ./...
```

Run targeted tests repeatedly for the area you changed. Example:

```sh
GOCACHE=/tmp/devremote-phase3-executor-go-cache go test ./internal/mux -run 'TestRegistry_SlowAdapterDoesNotBlock|TestProbeAdapter|TestTmuxAdapter|Test.*Runner' -count=20 -v
```

If you changed concurrency, run the relevant package with `-race`:

```sh
GOCACHE=/tmp/devremote-phase3-executor-go-cache go test -race ./internal/mux -count=1
```

If `internal/term` fails only because a sandbox blocks localhost listeners:

```text
httptest: failed to listen on a port: bind: operation not permitted
```

rerun in an environment that allows local listener tests and state that the
first failure was sandbox-related.

If mobile files are changed:

```sh
cd mobile
npx tsc --noEmit
```

## Commit requirements

Commit only your intentional changes.

Use a clear message, for example:

```text
fix: tighten phase 3 adapter lifecycle contracts
```

or, if the change is test-only:

```text
test: pin phase 3 adapter lifecycle contracts
```

Push to `feature/phase10-multi-adapter`.

## Handoff back to verifier

After pushing, report:

```text
Phase 3 correction ready
Commit: <full or short hash>

What changed:
- ...

Verifier actions addressed:
- ...

Verification run:
- ...

Known deferrals:
- ...
```

Be precise. The verifier will independently inspect the diff and rerun tests.
Do not claim acceptance. Only the verifier can accept the phase.
