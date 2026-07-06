# Adapter Expansion Phase 1 Reverification 7

Baseline under review: `3776ed7` (`fix: Phase 1 ctx.Done timeout wrapping, stale/ended/cancel tests`)

Previous verifier baseline: `3c98476` (`docs: keep adapter phase 1 blocked after sixth revalidation`)

Verdict: **REJECTED**

Phase 1 is still blocked. The seventh revision improves coverage around ended sessions and cancellation, but it does not close the remaining identity/discovery contract gaps. One newly added timeout test is itself nondeterministic and failed under repeated execution.

## Verification performed

- `git fetch origin feature/phase10-multi-adapter`
- `git merge --ff-only origin/feature/phase10-multi-adapter`
- `git diff --check 3c98476..HEAD`
- Code review of:
  - `companion-daemon/internal/mux/registry.go`
  - `companion-daemon/internal/mux/phase1_test.go`
  - related create/handler paths in `tmux_adapter.go`, `cmux_adapter.go`, and `pty.go`
- Targeted repeated Go test:

```sh
GOCACHE=/tmp/devremote-phase1-reverify7-go-cache go test ./internal/mux \
  -run "TestRefresh_(TimeoutPreservation|TimeoutWrapsErrTimeout|CancelNotTimeout)|TestFindSession_(EndedSession|AdapterUnavailable)|TestCanonicalID_URLRoundTrip|TestCreateSession_(ReturnsLocalID|TmuxCanonicalContract)|TestStaleCacheOnFailure" \
  -count=20 -v
```

Result: **FAIL**

Observed failure:

```text
--- FAIL: TestRefresh_TimeoutWrapsErrTimeout (0.00s)
    phase1_test.go:221: Refresh with expired context: got nil, want error
FAIL
FAIL    devremote/companion-daemon/internal/mux
```

Full `go vet ./...`, `go test ./...`, `go test -race ./...`, and mobile `npx tsc --noEmit` were not used as acceptance gates because the targeted Phase 1 regression suite already failed.

## Blocking findings

### 1. Timeout taxonomy is still nondeterministic

`registry.Refresh` now wraps the `ctx.Done()` select branch with `ErrTimeout` when `ctx.Err()` is `context.DeadlineExceeded`:

```go
case <-ctx.Done():
    snap, _ := r.Snapshot(name)
    if errors.Is(ctx.Err(), context.DeadlineExceeded) {
        return snap, fmt.Errorf("%w: %w", ErrTimeout, ctx.Err())
    }
    return snap, ctx.Err()
```

However, the refresh goroutine can still win the select race and return a successful result even when the caller's context was already expired. The new `TestRefresh_TimeoutWrapsErrTimeout` demonstrates this: with `context.WithTimeout(..., 0)`, the mock adapter ignores the context and returns success immediately, so `Refresh` sometimes returns `nil` instead of an error.

This is not just a flaky test. It shows the production contract remains ambiguous: if the caller context is already done, `Refresh` can return success depending on scheduling and adapter behavior.

Required correction:

- Define and enforce deterministic semantics for already-expired and expired-during-refresh contexts.
- If the intended Phase 1 contract is “caller deadline always wins once expired,” `Refresh` must check `ctx.Err()` before accepting a successful result, or otherwise document and test the opposite behavior.
- Add deterministic tests that do not rely on select scheduling.

### 2. Stale live lookup failure case is still missing

The revision adds `TestFindSession_EndedSession`, which covers:

- cache had session
- forced refresh succeeds
- refreshed adapter list no longer contains the session
- `FindSession` returns `ErrSessionNotFound`

That is useful, but it still does not cover the second stale-live lookup requirement from the previous rejection:

- cache had session
- forced refresh fails
- stale snapshot is retained internally
- `FindSession` must return `ErrAdapterUnavailable`
- no stale session may be returned as live
- snapshot must still retain the stale session for list/stale display semantics

Existing `TestStaleCacheOnFailure` only verifies `Refresh` and `Snapshot`; it does not call `FindSession` after the failed forced refresh. Therefore the live lookup contract remains unpinned.

Required correction:

- Add a `FindSession`-level test for refresh failure with an existing stale cached session.
- Assert both sides of the contract:
  - lookup returns an error wrapping `ErrAdapterUnavailable`
  - no `Session` is returned as live
  - snapshot still contains the stale session after the failed refresh

### 3. Exactly-once create canonicalization is still not proven for real adapters

The new `TestCreateSession_TmuxCanonicalContract` uses `testAdapter`, whose `CreateSession` simply returns `opts.Name`. This proves only the mock, not the tmux adapter, cmux adapter, or HTTP handler contract.

The previous requirement remains open:

- tmux create returns local `test-session`
- cmux mock runner output `surface:42` returns local `surface:42`
- handler/API response returns exactly `tmux:<local>` or `cmux:surface:<N>`
- no double prefix is possible
- cmux parse failure returns an error and never fabricates `unknown`

Current code appears to have plausible behavior in `tmux_adapter.go` and `cmux_adapter.go`, but Phase 1 acceptance needs regression tests that lock the actual production adapters/handler, not a hand-written mock that cannot regress the real parser.

Required correction:

- Add tests against `NewTmuxAdapter` only if shelling out can be mocked or isolated; otherwise test the concrete adapter behavior through injectable command runners where available.
- Add cmux adapter tests using a mock command runner:
  - output `surface:42` -> returned local ID `surface:42`
  - malformed output -> error
- Add handler/API create tests that canonicalize exactly once for both tmux and cmux-style local IDs.

### 4. URL round-trip test is not an actual URL round-trip and omits Unicode

`TestCanonicalID_URLRoundTrip` does this:

```go
canonical := ref.Canonical()
parsed := ParseSessionID(canonical)
```

That is parser round-trip only. It does not exercise URL query encoding/decoding, `url.QueryEscape`, `url.ParseQuery`, HTTP request construction, or the websocket/session handler path.

It also only covers colon-containing ASCII local IDs, while the previous requirement explicitly included colon plus Unicode coverage.

Required correction:

- Add a real URL query round-trip test for canonical IDs containing both colons and Unicode, for example a local ID such as `작업:1`.
- Assert that the decoded query value is still the exact canonical session ID and then parses into the same adapter/local ID pair.
- Prefer adding a handler-level test when feasible, because the user-visible failure mode is URL/API boundary corruption.

### 5. Handler still contains duplicate validation

`companion-daemon/internal/term/pty.go` still validates the same parsed `SessionRef` twice in a row:

```go
ref := mux.ParseSessionID(req.ID)
if err := ref.Validate(); err != nil { ... }

if err := ref.Validate(); err != nil { ... }
```

This is not the primary blocker for Phase 1, but it is a signal that the repeated patching is still not being cleaned up carefully. Remove the duplicate while making the next contract fix.

## Required next revision

Do not advance to Phase 2 yet. The next executor revision should:

1. Make `Refresh` timeout/cancellation semantics deterministic and update tests so repeated execution is stable.
2. Add the missing `FindSession` stale-cache-on-refresh-failure test.
3. Add real adapter/handler create canonicalization tests for tmux and cmux, including cmux parse failure.
4. Replace parser-only URL coverage with actual URL encode/decode coverage including Unicode and colon-containing local IDs.
5. Remove duplicate validation in `pty.go`.
6. Re-run at minimum:

```sh
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./internal/mux ./internal/term -count=1
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./internal/mux -run 'TestRefresh|TestFindSession|TestCreateSession|TestCanonicalID' -count=100
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./...
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test -race ./...
```

Phase 1 should be considered complete only after these contract tests pass deterministically and the remaining API/mobile checks pass.
