# Adapter Expansion Phase 3 Self-Check Review

Baseline under review: `0a6bd8e` (`Phase 3 셀프 검증 완료`)

Previous verifier baseline: `42ed624` (`docs: keep adapter phase 3 blocked after revalidation`)

Verdict: **REJECTED — PHASE 3 STILL NOT COMPLETE**

The self-check revision makes meaningful progress, but Phase 3 is still not complete. The main remaining issue is that key Phase 3 behavior is implemented or claimed without strong contract tests, and the registry parallel discovery implementation still waits for every adapter result before returning.

## Verification performed

- `git fetch origin feature/phase10-multi-adapter`
- `git merge --ff-only origin/feature/phase10-multi-adapter`
- `git diff --check 42ed624..HEAD`
- Code review of:
  - `companion-daemon/internal/mux/adapter.go`
  - `companion-daemon/internal/mux/registry.go`
  - `companion-daemon/internal/mux/tmux_adapter.go`
  - `companion-daemon/internal/mux/cmux_adapter.go`
  - `companion-daemon/internal/mux/phase1_test.go`
  - `companion-daemon/internal/mux/registry_test.go`
- Targeted mux regression:

```sh
GOCACHE=/tmp/devremote-phase3-selfcheck-go-cache go test ./internal/mux \
  -run "Test.*Probe|Test.*Timeout|Test.*Runner|Test.*Invalidat|Test.*Slow|Test.*Parallel|Test.*Tmux|Test.*Cmux|TestRegistry" \
  -count=100
```

Result: **PASS**

Important caveat: this command passes largely because several required tests do not yet exist. There are no strong `Slow`/`Parallel` registry-discovery tests and no `ProbeAdapter` timeout test.

- Targeted mux/term test:

```sh
GOCACHE=/tmp/devremote-phase3-selfcheck-go-cache go test ./internal/mux ./internal/term -count=1
```

Initial sandboxed run failed because the sandbox blocked `httptest` listener creation. The same command was rerun outside the sandbox.

Result outside sandbox: **PASS**

- Mobile typecheck:

```sh
npx tsc --noEmit
```

Result: **PASS**

Full `go vet ./...`, `go test ./...`, and `go test -race ./...` were not used as acceptance gates because code review still found Phase 3 contract blockers.

## Improvements confirmed

### 1. tmux runner injection coverage was expanded

The tmux adapter now routes more operations through the injected `CommandRunner`:

- `ListSessions`
- `ProcessInfo`
- `ReadScreen`
- `ReadHistory`
- `CreateSession`
- `TerminateSession`
- `resolveTmuxTarget`

This closes much of the previous “list-only injection” gap.

### 2. cmux invalidation is now adapter-scoped

`InvalidationSender` now takes an adapter name:

```go
InvalidateAdapter(name string)
```

`CmuxStream.pollScreen` calls:

```go
s.session.invalidate.InvalidateAdapter("cmux")
```

`Registry.InvalidateAdapter(name)` invalidates only the named adapter snapshot. This is better than global invalidation.

### 3. `Registry.Sessions` now starts refreshes concurrently

`Sessions` now spawns one goroutine per adapter and each goroutine uses a 5-second child context:

```go
adapterCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
snap, _ := r.Refresh(adapterCtx, adapter.Name(), false)
```

This is a step toward independent per-adapter refresh.

### 4. basic `ProbeAdapter` tests were added

There are now tests for healthy and unavailable probe behavior.

## Blocking findings

### 1. parallel discovery still waits for every adapter result

Phase 3 requires:

> adapter refresh를 독립적인 timeout 아래 병렬화해 느린 adapter가 다른 adapter의 discovery 응답을 지연시키지 않는다.

The current implementation starts goroutines, but then waits for exactly `len(adapters)` results:

```go
for i := 0; i < len(adapters); i++ {
    r := <-results
    ...
}
```

Each adapter goroutine has a 5-second timeout. That means one slow/hanging adapter can still delay the entire `Sessions` response until its 5-second timeout fires. This does not satisfy “healthy adapter is returned without waiting for slow adapter” unless 5 seconds is considered acceptable and explicitly tested as the intended SLA.

Required correction:

- Add a test with one healthy adapter and one blocking adapter.
- Assert the healthy session is returned within a short bound that is not the slow adapter timeout.
- If the intended behavior is “wait up to per-adapter timeout for all adapters,” update the plan/contract; otherwise implement early partial return.

### 2. the required slow/broken adapter isolation test is missing

No test currently proves:

- slow adapter does not delay healthy adapter session listing;
- broken adapter records `LastError`;
- healthy adapter remains visible while another adapter fails/hangs.

The targeted regex included `Slow|Parallel`, but there are no matching registry behavior tests.

Required correction:

- Add explicit registry tests for slow, hanging, and failing adapters.
- Verify both returned session set and elapsed time.
- Verify stale snapshot/last error observability.

### 3. `ProbeAdapter` timeout and snapshot isolation are untested

Only healthy and unavailable cases were added. Missing probe tests:

- timeout behavior wraps `ErrAdapterUnavailable` and/or preserves `context.DeadlineExceeded`;
- parent context cancellation behavior;
- probe does not mutate registry runtime snapshots or stale health state.

Required correction:

- Add a blocking adapter probe timeout test.
- Assert `errors.Is(err, ErrAdapterUnavailable)` and timeout/cancellation taxonomy as intended.
- Assert probe does not populate or mutate registry snapshots when used independently.

### 4. tmux runner injection has limited contract tests

The implementation now routes many operations through the runner, but tests do not prove this for create, terminate, screen/history, process info, and target resolution failure paths.

Required correction:

- Add mock-runner tests for:
  - `CreateSession` passes `CommandOptions.Dir`;
  - `TerminateSession` resolves target through runner and kills the target;
  - `ReadScreen` and `ReadHistory` use runner commands;
  - `ProcessInfo` parses runner output and preserves runner errors;
  - command failure includes stderr/output in the returned error where policy requires it.

### 5. tmux binary discovery policy is asserted by comment, not by behavior

The new comment says:

```go
// No binary discovery: tmux is a core macOS/Linux tool always in PATH.
```

This is not a safe Phase 3 diagnostic policy. `tmux` is not guaranteed to be installed or in PATH, especially under LaunchAgent environments.

Phase 3 explicitly calls out:

> binary discovery, environment, socket path, timeout, stderr 보존 정책을 정의한다.

Required correction:

- Either implement tmux binary lookup/diagnostics similar to cmux or explicitly document a supported-runtime requirement and test the failure path.
- Add no-binary/path failure tests through the injected runner.

### 6. remaining adapter-specific command execution is still deferred in term layer

`gemini_resolver.go` still says:

```go
// Phase 3 deferred: adapter-specific command execution. Currently hardcoded
// to tmux; Phase 3 will inject adapter runner for backend-agnostic resolution.
```

This still points at Phase 3 while the user claims Phase 3 complete. It must be resolved, moved to a later phase with plan text, or tested as intentionally out-of-scope.

Required correction:

- Update the comment and plan if intentionally deferred.
- Prefer moving resolver command execution behind an injectable adapter-aware runner in Phase 3, or add a scoped follow-up with explicit acceptance criteria.

## Required next revision

Do not advance to Phase 4 yet. The next executor revision should:

1. Add registry slow/hanging/failing adapter isolation tests with elapsed-time assertions.
2. Decide whether `Sessions` should return partial healthy results before slow adapter timeout. Implement or update contract accordingly.
3. Add `ProbeAdapter` timeout/cancellation/snapshot-isolation tests.
4. Add tmux mock-runner tests for create, terminate, screen/history, process info, and target resolution.
5. Define/test tmux binary discovery and stderr/timeout policy.
6. Resolve the `gemini_resolver.go` “Phase 3 deferred” comment.

Suggested verification before the next review:

```sh
GOCACHE=/tmp/devremote-phase3-verifier-go-cache go test ./internal/mux ./internal/term -count=1
GOCACHE=/tmp/devremote-phase3-verifier-go-cache go test ./internal/mux -run 'Test.*Probe|Test.*Timeout|Test.*Runner|Test.*Invalidat|Test.*Slow|Test.*Parallel|Test.*Tmux|Test.*Cmux|TestRegistry' -count=100
GOCACHE=/tmp/devremote-phase3-verifier-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase3-verifier-go-cache go test ./...
GOCACHE=/tmp/devremote-phase3-verifier-go-cache go test -race ./...
```

and from `mobile/`:

```sh
npx tsc --noEmit
```
