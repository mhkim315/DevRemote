# Adapter Expansion Phase 1 Reverification 8

Baseline under review: `5834608` (`Phase 1 수정 (8차)`)

Previous verifier baseline: `4d91ff2` (`docs: keep adapter phase 1 blocked after seventh revalidation`)

Verdict: **REJECTED**

Phase 1 remains blocked. The eighth revision improves the timeout regression test stability, adds a `FindSession` stale-cache failure test, and removes duplicate validation in `pty.go`. Those are useful changes, but two core Phase 1 contract gaps remain unclosed: actual adapter/handler create canonicalization and real URL encode/decode behavior.

## Verification performed

- `git fetch origin feature/phase10-multi-adapter`
- `git merge --ff-only origin/feature/phase10-multi-adapter`
- `git diff --check 4d91ff2..HEAD`
- Code review of:
  - `companion-daemon/internal/mux/phase1_test.go`
  - `companion-daemon/internal/term/pty.go`
  - actual create paths in `tmux_adapter.go`, `cmux_adapter.go`, and `pty.go`
- Targeted repeated mux test:

```sh
GOCACHE=/tmp/devremote-phase1-reverify8-go-cache go test ./internal/mux \
  -run "TestRefresh|TestFindSession|TestCanonicalID|TestCreateSession|TestStaleCacheOnFailure" \
  -count=100
```

Result: **PASS**

- Handler-adjacent package test, rerun outside the sandbox because `httptest` local listener creation was blocked by the sandbox:

```sh
GOCACHE=/tmp/devremote-phase1-reverify8-go-cache go test ./internal/mux ./internal/term -count=1
```

Result: **PASS**

Full `go vet ./...`, `go test ./...`, `go test -race ./...`, and mobile `npx tsc --noEmit` were not used as acceptance gates because the code review still found Phase 1 contract coverage gaps.

## Improvements confirmed

### 1. Timeout test is now stable under repeated execution

`TestRefresh_TimeoutWrapsErrTimeout` now uses a `blockingAdapter` that waits on `ctx.Done()` and returns `ctx.Err()`. The targeted `TestRefresh` suite passed with `-count=100`.

This resolves the immediate flaky-test symptom from the seventh revalidation. The production timeout semantics are still mostly inherited from the prior implementation, but the specific nondeterministic test failure was addressed.

### 2. Duplicate `SessionRef.Validate()` calls were removed from `pty.go`

The duplicate validation blocks in POST and DELETE handling were removed. The remaining code validates once before extracting the adapter/local ID.

### 3. A `FindSession` stale-cache failure test was added

`TestFindSession_StaleCacheAndRefreshFailure` now covers the high-level behavior that a cached session must not be returned as live when forced refresh fails.

However, the test should still be strengthened in the next revision by asserting:

- the returned session value is nil;
- the stale snapshot still retains the cached session after the failed lookup.

Those assertions are part of the Phase 1 contract and should be pinned explicitly.

## Blocking findings

### 1. Exactly-once create canonicalization is still not proven for the actual adapters or handler boundary

The remaining create test is still:

```go
func TestCreateSession_TmuxCanonicalContract(t *testing.T) {
    reg := MustNewRegistry(&testAdapter{name: "tmux"})
    localID, err := reg.CreateSession(context.Background(), "tmux", CreateOptions{Name: "test-session"})
    ...
}
```

This uses `testAdapter.CreateSession`, which simply returns `opts.Name`. It does not exercise:

- `tmuxAdapter.CreateSession`;
- `cmuxAdapter.CreateSession`;
- cmux output parsing from `surface:42`;
- cmux malformed-output failure;
- `Handlers.HandleSessionCRUD` canonicalizing exactly once at the HTTP boundary.

The production code appears plausible:

- `tmuxAdapter.CreateSession` returns `opts.Name`;
- `cmuxAdapter.CreateSession` parses `surface:<N>` and returns local `surface:<N>`;
- `pty.go` wraps `createdID` with `SessionRef{Adapter: adapterName, LocalID: createdID}.Canonical()`.

But Phase 1 acceptance requires regression tests against those actual paths. A mock adapter that cannot regress the cmux parser or handler canonicalization is not enough.

Required correction:

- Add a cmux adapter test with a mock command runner:
  - runner output containing `surface:42` must return local ID `surface:42`;
  - malformed output must return an error and must not fabricate `unknown` or any other fallback ID.
- Add handler/API tests for exactly-once canonicalization:
  - tmux-style local ID -> response ID exactly `tmux:<local>`;
  - cmux-style local ID `surface:42` -> response ID exactly `cmux:surface:42`;
  - no response may double-prefix, e.g. `cmux:cmux:surface:42`.

### 2. URL round-trip test is still not a real URL encode/decode test

The new test is named `TestCanonicalID_URLEncodeRoundTrip`, but it manually replaces substrings:

```go
encoded := strings.ReplaceAll(canonical, ":", "%3A")
encoded = strings.ReplaceAll(encoded, "_", "%5F")
decoded := strings.ReplaceAll(encoded, "%3A", ":")
decoded = strings.ReplaceAll(decoded, "%5F", "_")
```

This is not URL encoding. It does not exercise Go's `net/url` behavior, query parsing, UTF-8 escaping, `+` versus `%20`, percent-decoding failure modes, or the handler/websocket URL boundary where this contract matters.

The test also leaves the previous parser-only `TestCanonicalID_URLRoundTrip` in place, which is fine as a parser test but should not be considered URL coverage.

Required correction:

- Replace the manual `strings.ReplaceAll` simulation with `net/url`, for example:
  - build `url.Values{"session": {canonical}}`;
  - call `.Encode()`;
  - parse via `url.ParseQuery`;
  - assert the decoded session value equals the exact canonical ID;
  - parse via `ParseSessionID` and assert adapter/local ID.
- Include local IDs with both Unicode and colon characters, for example `작업:1` or `session:한글:1`.
- Prefer a handler-level test where feasible, because the user-visible boundary is an HTTP/WebSocket query parameter.

### 3. Stale `FindSession` test should assert the full stale/list contract

`TestFindSession_StaleCacheAndRefreshFailure` now verifies an error wrapping `ErrAdapterUnavailable`, but it discards the session return value and does not inspect the retained snapshot.

Current test shape:

```go
_, err := reg.FindSession(context.Background(), "test:s1")
if !errors.Is(err, ErrAdapterUnavailable) { ... }
```

Required strengthening:

- assign `session, err := ...` and assert `session == nil`;
- inspect `reg.Snapshot("test")` after the failed lookup;
- assert the stale snapshot still contains `s1`.

This is important because Phase 1 distinguishes two behaviors:

- live lookup must not return stale sessions;
- list/snapshot display may retain stale sessions with error state.

## Required next revision

Do not advance to Phase 2 yet. The next executor revision should be narrow:

1. Add real `net/url` encode/decode coverage for canonical IDs with colon and Unicode.
2. Add cmux adapter create tests for successful `surface:42` parsing and malformed-output error.
3. Add handler/API exactly-once canonicalization tests for tmux and cmux-style local IDs.
4. Strengthen the stale `FindSession` test to assert nil returned session and retained stale snapshot.
5. Then run:

```sh
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./internal/mux ./internal/term -count=1
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./internal/mux -run 'TestRefresh|TestFindSession|TestCreateSession|TestCanonicalID|TestCmux' -count=100
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./...
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test -race ./...
```

Phase 1 can be accepted only after the actual adapter/handler boundaries and real URL boundary are pinned by tests, not just mock/parser-level tests.
