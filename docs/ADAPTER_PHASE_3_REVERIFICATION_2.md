# Adapter Expansion Phase 3 Reverification 2

Baseline under review: `fe5e964` (`Phase 3 수정 (2차)`)

Previous verifier baseline: `f5298c9` (`docs: keep adapter phase 3 blocked after self check`)

Verdict: **REJECTED — PHASE 3 STILL NOT COMPLETE**

The second Phase 3 revision addresses some review items, but the core Phase 3 discovery-isolation contract is still not proven and appears not to be met as written. The new slow-adapter test passes while taking the full caller timeout, which means the slow adapter still delays `Registry.Sessions`.

## Verification performed

- `git fetch origin feature/phase10-multi-adapter`
- `git merge --ff-only origin/feature/phase10-multi-adapter`
- `git diff --check f5298c9..HEAD`
- Code review of:
  - `companion-daemon/internal/mux/registry.go`
  - `companion-daemon/internal/mux/phase1_test.go`
  - `companion-daemon/internal/term/gemini_resolver.go`
- Targeted mux regression:

```sh
GOCACHE=/tmp/devremote-phase3-reverify2-go-cache go test ./internal/mux \
  -run "TestRegistry_SlowAdapterDoesNotBlock|TestProbeAdapter_(Healthy|Unavailable|Timeout|Cancel)|Test.*Tmux|Test.*Runner" \
  -count=20 -v
```

Result: **PASS**, but with a critical caveat:

```text
--- PASS: TestRegistry_SlowAdapterDoesNotBlock (2.00s)
registry: adapter slow refresh failed (retaining stale cache): context deadline exceeded
```

The test passes only after the 2-second caller deadline expires. That does not prove “slow adapter does not block”; it proves the slow adapter blocks until timeout while the fast adapter result is eventually included.

## Improvements confirmed

### 1. `ProbeAdapter` timeout/cancel tests were added

`TestProbeAdapter_Timeout` and `TestProbeAdapter_Cancel` now cover basic timeout and cancelled parent context cases. This improves probe coverage.

### 2. `Registry.Sessions` now has a collector timeout path

`Sessions` now uses a `collectCtx` and returns collected results when the collection deadline fires:

```go
case <-collectCtx.Done():
    remaining = 0
```

This prevents indefinite hangs.

### 3. `gemini_resolver.go` no longer says “Phase 3 deferred”

The comment was moved to “Phase 4 deferred.” This removes the direct contradiction with claiming Phase 3 complete, although the comment itself is still awkward and should be cleaned up later.

## Blocking findings

### 1. slow adapter still delays `Registry.Sessions` until timeout

The current implementation still waits for every adapter result unless `collectCtx` expires:

```go
remaining := len(adapters)
for remaining > 0 {
    select {
    case r := <-results:
        remaining--
        ...
    case <-collectCtx.Done():
        remaining = 0
    }
}
```

The new test uses:

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
sessions := reg.Sessions(ctx)
```

and does not assert elapsed time. In observed execution, the test takes `2.00s` every time. That means the fast adapter is not returned promptly; the caller waits for the slow adapter deadline.

This fails the Phase 3 acceptance criterion:

> adapter refresh를 독립적인 timeout 아래 병렬화해 느린 adapter가 다른 adapter의 discovery 응답을 지연시키지 않는다.

Required correction:

- Add elapsed-time assertion, e.g. fast result returned in `<200ms` while slow adapter remains blocked.
- Implement early partial return if that is the intended contract.
- If the intended contract is “wait for all adapters up to a bounded collection timeout,” update `ADAPTER_EXPANSION_PLAN.md`; the current acceptance text says slow adapters must not delay healthy discovery.

### 2. `ProbeAdapter` tests still do not assert timeout taxonomy or snapshot isolation

`TestProbeAdapter_Timeout` only asserts:

```go
errors.Is(err, ErrAdapterUnavailable)
```

It does not assert whether `context.DeadlineExceeded` is preserved or intentionally hidden. It also does not verify that probing does not mutate registry snapshots.

Required correction:

- Assert expected timeout taxonomy explicitly.
- Add a snapshot isolation test:
  - populate a registry snapshot;
  - run `ProbeAdapter` against an adapter failure/timeout;
  - assert the registry snapshot and stale state are unchanged.

### 3. tmux runner contract tests are still missing

No new tmux mock-runner tests were added in this revision. The implementation may route more paths through `CommandRunner`, but Phase 3 needs tests proving it:

- create passes `CommandOptions.Dir`;
- terminate resolves target and kills the resolved target;
- `ReadScreen` and `ReadHistory` use runner commands;
- `ProcessInfo` parses runner output and preserves runner errors;
- target resolution failure fallback is intentional and tested.

Required correction:

- Add a `mockCommandRunner` for tmux.
- Assert exact commands and options for create/terminate/screen/history/process paths.

### 4. tmux binary discovery policy is still not fixed

`tmux_adapter.go` still contains:

```go
// No binary discovery: tmux is a core macOS/Linux tool always in PATH.
```

This remains a risky unsupported assumption under LaunchAgent or minimal environments. Phase 3 requires binary discovery/environment diagnostics. A comment claiming tmux is always available is not sufficient.

Required correction:

- Add tmux binary lookup/diagnostic policy or document a supported-runtime requirement in the plan.
- Add no-binary/path-failure tests through injected runner.

### 5. `gemini_resolver.go` comment was moved but not cleanly resolved

The new comment says:

```go
// Phase 4 deferred: adapter-agnostic resolver. This hardcoded tmux command will be replaced when Phase 4 introduces per-adapter runner injection for resolver/telemetry paths. Currently hardcoded
// to tmux; Phase 3 will inject adapter runner for backend-agnostic resolution.
```

It still contains “Phase 3 will inject...” on the next line. This is internally inconsistent.

Required correction:

- Rewrite the comment so it has one clear owner phase and one clear reason.
- If resolver runner injection is Phase 4, remove the stale “Phase 3 will inject” sentence.

## Required next revision

Do not advance to Phase 4 yet. The next revision should:

1. Fix or redefine `Registry.Sessions` slow-adapter behavior.
2. Add elapsed-time tests proving the intended behavior.
3. Add `ProbeAdapter` timeout taxonomy and snapshot-isolation tests.
4. Add tmux mock-runner contract tests.
5. Resolve tmux binary discovery policy.
6. Clean up the contradictory `gemini_resolver.go` comment.

Suggested verification:

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
