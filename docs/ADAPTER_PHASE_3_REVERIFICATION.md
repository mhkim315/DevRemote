# Adapter Expansion Phase 3 Reverification

Baseline under review: `d2eb546` (`Phase 3 완료`)

Previous accepted baseline: `c62a1b7` (`docs: accept adapter expansion phase 2`)

Verdict: **REJECTED — PHASE 3 NOT COMPLETE**

Phase 3 is not accepted. The commit makes useful progress on cmux invalidation and partial tmux runner injection, but it does not satisfy the Phase 3 acceptance criteria from `docs/ADAPTER_EXPANSION_PLAN.md`.

## Verification performed

- `git fetch origin feature/phase10-multi-adapter`
- `git merge --ff-only origin/feature/phase10-multi-adapter`
- `git diff --check c62a1b7..HEAD`
- Code review of:
  - `companion-daemon/internal/mux/adapter.go`
  - `companion-daemon/internal/mux/registry.go`
  - `companion-daemon/internal/mux/tmux_adapter.go`
  - `companion-daemon/internal/mux/cmux_adapter.go`
  - `companion-daemon/internal/mux/cmux_adapter_mock_test.go`
  - Phase 3 requirements in `docs/ADAPTER_EXPANSION_PLAN.md`
- Targeted mux regression:

```sh
GOCACHE=/tmp/devremote-phase3-verify-go-cache go test ./internal/mux \
  -run "TestNewCmux|TestPollScreenFailures|TestSerialCommandRunner|TestRegistry|TestProbe|TestTmux" \
  -count=1 -v
```

Result: **PASS**

- Targeted mux/term test:

```sh
GOCACHE=/tmp/devremote-phase3-verify-go-cache go test ./internal/mux ./internal/term -count=1
```

Initial sandboxed run failed because the sandbox blocked `httptest` listener and Unix socket binding. The same command was rerun outside the sandbox.

Result outside sandbox: **PASS**

- Mobile typecheck:

```sh
npx tsc --noEmit
```

Result: **PASS**

Full `go vet ./...`, `go test ./...`, and `go test -race ./...` were not used as acceptance gates because code review found Phase 3 contract blockers.

## Improvements confirmed

### 1. cmux no longer calls registry refresh directly from stream polling

`RegistryHealth` was replaced by `InvalidationSender`:

```go
type InvalidationSender interface {
    Invalidate()
}
```

`CmuxStream.pollScreen` now calls `Invalidate()` after repeated read-screen failures instead of synchronously calling `Refresh(ctx, "cmux", true)`.

This is aligned with the Phase 3 requirement to move from bidirectional registry refresh callbacks toward one-way invalidation.

### 2. cmux command runner behavior remains injectable and serialized

The existing `CommandRunner` and `serialCommandRunner` remain in place, and the updated tests verify invalidation on poll failure instead of registry refresh.

### 3. tmux list discovery is partially injectable

`tmuxAdapter` now has a `runner CommandRunner`, and `ListSessions` uses:

```go
a.runner.Run(ctx, CommandOptions{}, "tmux", "list-sessions", "-F", tmuxSessionFormat)
```

This is a step toward injectable runner support.

### 4. availability probe was introduced

`ProbeAdapter(ctx, adapter, timeout)` was added and wraps `ListSessions` with a timeout, returning `ErrAdapterUnavailable` on error.

## Blocking findings

### 1. tmux command execution is not uniformly injectable

Phase 3 requires:

> command 실행을 adapter별 injectable runner로 통일한다.

The tmux adapter now injects only `ListSessions`. Other production tmux operations still shell out directly:

- `tmuxSession.ProcessInfo`
- `tmuxSession.ReadScreen`
- `tmuxSession.ReadHistory`
- `tmuxAdapter.CreateSession`
- `tmuxAdapter.TerminateSession`
- `resolveTmuxTarget`
- `tmuxSession.OpenStream` still calls `SpawnPTY(..., "tmux", "attach", ...)`

This means major tmux error paths still require a real tmux binary or cannot be unit tested through a mock runner.

Required correction:

- Route all tmux command execution through an injectable runner or explicitly document any irreducible live PTY path and cover it separately.
- Add mock-runner tests for create, terminate, screen/history, process info, and target resolution error paths.

### 2. adapter refresh is still sequential and lacks independent per-adapter timeout isolation

Phase 3 acceptance requires:

> adapter refresh를 독립적인 timeout 아래 병렬화해 느린 adapter가 다른 adapter의 discovery 응답을 지연시키지 않는다.

`Registry.Sessions` is still sequential:

```go
for _, adapter := range r.Adapters() {
    snap, _ := r.Refresh(ctx, adapter.Name(), false)
    ...
}
```

Also, `Refresh` still contains a deferred comment saying singleflight context isolation is not complete:

```go
// Phase 3 deferred: singleflight shares first waiter's context across all
// concurrent callers. If the first caller cancels, other waiters get the
// cancelled result. Phase 3 lifecycle/concurrency will add per-caller
// timeout isolation.
```

This directly contradicts marking Phase 3 complete.

Required correction:

- Add per-adapter discovery timeout policy.
- Refresh adapters in parallel for list/discovery.
- Add tests with one slow/hanging adapter and one healthy adapter proving the healthy adapter is returned without waiting for the slow adapter.
- Resolve or move the `Phase 3 deferred` comment with a concrete follow-up phase if intentionally out of scope.

### 3. `ProbeAdapter` has no tests and is not integrated

`ProbeAdapter` was added, but no tests were found for:

- healthy adapter;
- failing adapter wraps `ErrAdapterUnavailable`;
- timeout/cancellation behavior;
- ensuring probe failures do not mutate runtime stale snapshots.

It is also not used by production diagnostics yet, so the Phase 3 availability-probe/runtime-health split is not fully proven.

Required correction:

- Add unit tests for `ProbeAdapter`.
- Define whether probes affect snapshots.
- Integrate or document the production diagnostics path that consumes probe results.

### 4. binary discovery/environment/socket/timeout/stderr policy is only partial

cmux has `execCommandRunner` with `LookPath`, stderr capture via `exec.ExitError`, and structured `CmuxError`.

tmux still mostly uses direct `exec.CommandContext` without the same structured policy. There is no unified test proving:

- binary not found;
- stderr preserved;
- timeout/cancellation taxonomy;
- environment/socket-path differences;
- command failure does not become `nil, nil` or empty-session success.

Required correction:

- Add tmux mock-runner tests for binary lookup/failure/stderr/timeout paths.
- Keep cmux structured error tests.
- Define adapter-specific environment/socket diagnostics, especially LaunchAgent/foreground/GUI socket differences.

### 5. cmux invalidation ownership is only partially specified

Replacing refresh with `Invalidate()` is good, but the current `InvalidationSender` only invalidates all snapshots via `Registry.Invalidate()`. It does not identify the adapter and has no tests for:

- backpressure;
- close/shutdown ordering;
- repeated invalidation coalescing;
- one adapter invalidation not unnecessarily evicting unrelated adapters.

Required correction:

- Decide whether invalidation should be global or adapter-scoped.
- Add tests proving cmux stream failure invalidation does not block I/O and does not synchronously refresh.
- Consider adapter-scoped invalidation if global invalidation causes unrelated cache churn.

## Required next revision

Do not advance to Phase 4 yet. The next executor revision should:

1. Complete tmux runner injection or explicitly narrow/document live PTY exceptions with tests.
2. Parallelize registry discovery with independent per-adapter timeout, or provide a concrete plan if this is intentionally moved out of Phase 3.
3. Add tests proving a slow/broken adapter does not delay a healthy adapter's session listing.
4. Add `ProbeAdapter` tests and define whether probes mutate runtime health/snapshots.
5. Add tmux/cmux command failure tests for no-binary, stderr, timeout/cancellation, and non-empty failure preservation.
6. Clarify adapter-scoped versus global invalidation and test the intended ownership/backpressure semantics.

Suggested verification before the next review:

```sh
GOCACHE=/tmp/devremote-phase3-verifier-go-cache go test ./internal/mux ./internal/term -count=1
GOCACHE=/tmp/devremote-phase3-verifier-go-cache go test ./internal/mux -run 'Test.*Probe|Test.*Timeout|Test.*Runner|Test.*Invalidat|Test.*Slow|Test.*Parallel|Test.*Tmux|Test.*Cmux' -count=100
GOCACHE=/tmp/devremote-phase3-verifier-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase3-verifier-go-cache go test ./...
GOCACHE=/tmp/devremote-phase3-verifier-go-cache go test -race ./...
```

and from `mobile/`:

```sh
npx tsc --noEmit
```
