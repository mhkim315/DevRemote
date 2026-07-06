# Adapter Expansion Phase 1 Reverification 11

Baseline under review: `6969a7e` (`Phase 1 수정 (11차)`)

Previous verifier baseline: `f0ad055` (`docs: keep adapter phase 1 blocked after tenth revalidation`)

Verdict: **REJECTED**

Phase 1 remains blocked, but the remaining issue is now very narrow. The eleventh revision correctly adds malformed cmux create-output coverage and asserts that stale `FindSession` does not return a live session. However, it still does not add the required handler/API exactly-once canonicalization test for cmux-style IDs.

## Verification performed

- `git fetch origin feature/phase10-multi-adapter`
- `git merge --ff-only origin/feature/phase10-multi-adapter`
- `git diff --check f0ad055..HEAD`
- Code review of:
  - `companion-daemon/internal/mux/phase1_test.go`
  - existing API golden coverage in `companion-daemon/internal/term/api_golden_test.go`
- Targeted repeated mux test:

```sh
GOCACHE=/tmp/devremote-phase1-reverify11-go-cache go test ./internal/mux \
  -run "TestRefresh|TestFindSession|TestCanonicalID|TestCreateSession|TestCmux|TestStaleCacheOnFailure" \
  -count=100
```

Result: **PASS**

Full `go vet ./...`, `go test ./...`, `go test -race ./...`, and mobile `npx tsc --noEmit` were not used as acceptance gates because code review still found the handler/API Phase 1 contract gap.

## Improvements confirmed

### 1. malformed cmux create output is now tested

`TestCmuxAdapter_CreateSession_MalformedOutput` was added. It calls the real `cmuxAdapter.CreateSession` with a `mockCmuxRunner` returning malformed output and verifies:

- an error is returned;
- the returned ID is empty.

This closes the cmux parse-failure requirement.

### 2. stale `FindSession` now asserts nil session

`TestFindSession_StaleCacheAndRefreshFailure` now assigns:

```go
s, err := reg.FindSession(context.Background(), "test:s1")
```

and asserts `s == nil` when forced refresh fails. It also retains the stale snapshot assertion. This closes the stale live-lookup requirement.

## Blocking finding

### handler/API exactly-once canonicalization is still missing

No test was added under `internal/term`, and `api_golden_test.go` still only has a tmux-shaped POST create test:

```go
body := strings.NewReader(`{"id":"tmux:test","runner":"claude","runnerColor":"#58a6ff"}`)
...
if resp.ID != "tmux:test" { ... }
```

There is still no test exercising the actual POST/API handler with a cmux-style local ID and asserting the response is exactly:

```text
cmux:surface:42
```

The cmux adapter tests now prove that the adapter returns local `surface:42`, and `SessionRef.Canonical` tests prove that a local ID can be formatted as `cmux:surface:42`. But Phase 1 also needs the app-facing boundary pinned: the handler must wrap exactly once in the JSON response and must not emit `cmux:cmux:surface:42`, `surface:42`, or any other variant.

Required correction:

- Add an API/handler test that registers a fake `cmux` adapter implementing `CreateSession`.
- The fake adapter should return local ID `surface:42`.
- Exercise the actual POST handler used by the app.
- Assert the JSON response ID is exactly `cmux:surface:42`.
- Add an explicit guard that it is not `cmux:cmux:surface:42`.

This can be added next to `TestAPIGolden_PostCreateSession_ReturnsCanonicalID`, either by generalizing the existing golden adapter to support a custom adapter name/create return value or by adding a small second fake adapter for cmux.

## Cleanup still needed

`TestFindSession_AdapterUnavailable` still contains duplicate adjacent `err == nil` checks:

```go
if err == nil {
    t.Fatal("FindSession with missing adapter: got nil, want error")
}
if err == nil {
    t.Fatal("FindSession got nil error, want ErrAdapterUnavailable")
}
```

This is not the main blocker, but it should be removed in the same narrow patch.

## Required next revision

Do not advance to Phase 2 yet. The next executor revision should be minimal:

1. Add handler/API exactly-once canonicalization test for cmux-style local ID `surface:42`.
2. Keep or strengthen the existing tmux handler canonicalization test.
3. Remove the duplicate `err == nil` check in `TestFindSession_AdapterUnavailable`.
4. Then run:

```sh
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./internal/mux ./internal/term -count=1
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./internal/mux -run 'TestRefresh|TestFindSession|TestCreateSession|TestCanonicalID|TestCmux' -count=100
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./...
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test -race ./...
```

Phase 1 is close, but still not accepted until the actual API response boundary is covered for cmux canonical IDs.
