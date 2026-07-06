# Adapter Expansion Phase 1 Reverification 9

Baseline under review: `3eb75b4` (`Phase 1 수정 (9차)`)

Previous verifier baseline: `de2068d` (`docs: keep adapter phase 1 blocked after eighth revalidation`)

Verdict: **REJECTED**

Phase 1 remains blocked. The ninth revision makes some progress on URL encoding and stale snapshot assertions, but it still does not test the actual cmux adapter create path or the HTTP handler/API canonicalization boundary. The remaining gap is now narrow but still material: the contract must be pinned at the real production boundaries, not by duplicating implementation fragments inside a test.

## Verification performed

- `git fetch origin feature/phase10-multi-adapter`
- `git merge --ff-only origin/feature/phase10-multi-adapter`
- `git diff --check de2068d..HEAD`
- Code review of:
  - `companion-daemon/internal/mux/phase1_test.go`
  - existing cmux test utilities in `companion-daemon/internal/mux/cmux_adapter_mock_test.go`
  - existing API golden test adapter in `companion-daemon/internal/term/api_golden_test.go`
- Targeted repeated mux test:

```sh
GOCACHE=/tmp/devremote-phase1-reverify9-go-cache go test ./internal/mux \
  -run "TestRefresh|TestFindSession|TestCanonicalID|TestCreateSession|TestCmux|TestStaleCacheOnFailure" \
  -count=100
```

Result: **PASS**

Full `go vet ./...`, `go test ./...`, `go test -race ./...`, and mobile `npx tsc --noEmit` were not used as acceptance gates because code review still found missing Phase 1 contract coverage.

## Improvements confirmed

### 1. URL test now uses `net/url`

`TestCanonicalID_URLEncodeRoundTrip` now uses:

```go
encoded := url.QueryEscape(canonical)
decoded, err := url.QueryUnescape(encoded)
```

This is a real improvement over the previous manual `strings.ReplaceAll` simulation. It covers colon plus Unicode in a canonical ID.

Remaining caveat: this still does not test a full query parameter parse via `url.Values` / `url.ParseQuery` or a handler/WebSocket request boundary. It is acceptable as parser-level URL coverage, but it is not handler-boundary coverage.

### 2. Stale snapshot assertion was partially strengthened

`TestFindSession_StaleCacheAndRefreshFailure` now verifies the snapshot still contains one stale session after failed refresh. This closes most of the stale/list split requirement.

Remaining caveat: the test still discards the returned session value. It should assert `session == nil` explicitly so the live-lookup side of the contract is pinned directly.

## Blocking findings

### 1. cmux create test still does not exercise `cmuxAdapter.CreateSession`

The new `TestCmuxCreateSession_ParseSurfaceID` does not instantiate `cmuxAdapter` and does not use the existing `mockCmuxRunner`. Instead, it copies the regex logic into the test:

```go
outStr := "Created new surface: surface:42 (workspace: workspace:1)"
matches := regexp.MustCompile(`surface:\s*(\d+)`).FindStringSubmatch(outStr)
localID := "surface:" + matches[1]
```

This tests the duplicated regex in the test, not the production method:

```go
func (a *cmuxAdapter) CreateSession(ctx context.Context, opts CreateOptions) (string, error)
```

As a result, this test would still pass if `cmuxAdapter.CreateSession` regressed tomorrow and returned:

- `cmux:surface:42`;
- `surface:unknown`;
- `42`;
- or a nil error on malformed output.

Required correction:

- Add tests that instantiate `&cmuxAdapter{runner: &mockCmuxRunner{...}}`.
- Success case:
  - mock runner returns output containing `surface:42`;
  - call `adapter.CreateSession(...)`;
  - assert returned ID is exactly `surface:42`.
- Failure case:
  - mock runner returns malformed output with nil command error;
  - assert `CreateSession` returns an error and an empty ID.

The repository already has `mockCmuxRunner` in `cmux_adapter_mock_test.go`, so this should be a small test addition, not new architecture.

### 2. Handler/API exactly-once canonicalization is still missing

No test was added under `internal/term` or equivalent handler coverage for `Handlers.HandleSessionCRUD` / `POST /api/sessions` canonicalization.

The required contract is still unpinned:

- tmux-style local ID returns response ID exactly `tmux:<local>`;
- cmux-style local ID `surface:42` returns response ID exactly `cmux:surface:42`;
- no double prefix such as `cmux:cmux:surface:42` is possible.

The current `TestCreateSession_TmuxCanonicalContract` still uses `testAdapter`, and the current cmux test manually canonicalizes with `SessionRef`. Neither test reaches the HTTP handler boundary where double-prefix bugs become user-visible.

Required correction:

- Add handler/API tests with fake adapters registered as `tmux` and `cmux`.
- For cmux, have the fake creator return local `surface:42`, then assert the JSON response ID is exactly `cmux:surface:42`.
- Include a negative/double-prefix guard if practical.

### 3. Stale live lookup should assert nil session return explicitly

The stale failure test currently calls:

```go
_, err := reg.FindSession(context.Background(), "test:s1")
```

It then checks the error and the retained snapshot. It should also assert the returned session is nil:

```go
session, err := reg.FindSession(...)
if session != nil { t.Fatalf(...) }
```

This is a small but important assertion because the Phase 1 live lookup contract is “do not return stale sessions as live,” not only “return an error.”

## Non-blocking cleanup note

`TestCanonicalID_URLEncodeRoundTrip` contains an unnecessary local variable:

```go
import_url := `"net/url"`
_ = import_url
```

This is harmless but should be removed. It is a sign of patch noise and makes the test look less deliberate.

## Required next revision

Do not advance to Phase 2 yet. The next executor revision should be very narrow:

1. Replace the regex-copy cmux test with actual `cmuxAdapter.CreateSession` tests using `mockCmuxRunner`.
2. Add malformed cmux output test: error returned, empty ID.
3. Add handler/API exactly-once canonicalization tests for tmux and cmux-style local IDs.
4. Strengthen stale `FindSession` test with `session == nil`.
5. Remove the `import_url` noise.
6. Then run:

```sh
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./internal/mux ./internal/term -count=1
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./internal/mux -run 'TestRefresh|TestFindSession|TestCreateSession|TestCanonicalID|TestCmux' -count=100
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./...
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test -race ./...
```

Phase 1 is close, but not accepted until the real adapter and handler boundaries are covered.
