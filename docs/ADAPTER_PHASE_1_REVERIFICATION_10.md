# Adapter Expansion Phase 1 Reverification 10

Baseline under review: `9aba218` (`Phase 1 수정 (10차)`)

Previous verifier baseline: `a9a9a77` (`docs: keep adapter phase 1 blocked after ninth revalidation`)

Verdict: **REJECTED**

Phase 1 remains blocked. The tenth revision replaces the copied cmux regex test with a real `cmuxAdapter.CreateSession` success-path test, which is the right direction. However, the revision still misses the malformed cmux output test, still does not add handler/API exactly-once canonicalization coverage, and does not actually assert that stale `FindSession` returns a nil session.

## Verification performed

- `git fetch origin feature/phase10-multi-adapter`
- `git merge --ff-only origin/feature/phase10-multi-adapter`
- `git diff --check a9a9a77..HEAD`
- Code review of:
  - `companion-daemon/internal/mux/phase1_test.go`
  - existing API golden test coverage in `companion-daemon/internal/term/api_golden_test.go`
- Targeted repeated mux test:

```sh
GOCACHE=/tmp/devremote-phase1-reverify10-go-cache go test ./internal/mux \
  -run "TestRefresh|TestFindSession|TestCanonicalID|TestCreateSession|TestCmux|TestStaleCacheOnFailure" \
  -count=100
```

Result: **PASS**

Full `go vet ./...`, `go test ./...`, `go test -race ./...`, and mobile `npx tsc --noEmit` were not used as acceptance gates because code review still found missing Phase 1 contract coverage.

## Improvements confirmed

### 1. cmux success-path create now calls the production adapter method

`TestCmuxAdapter_CreateSessionReturnsLocalID` now instantiates:

```go
adapter := &cmuxAdapter{runner: mockRunner}
id, err := adapter.CreateSession(context.Background(), CreateOptions{})
```

This is a real improvement over the ninth revision, which only copied the regex in the test. The success path now verifies that output containing `surface:42` returns local ID `surface:42`.

### 2. URL test cleanup was done

The unnecessary `import_url` local variable was removed.

## Blocking findings

### 1. malformed cmux create output is still untested

The required failure-path test is still missing:

- mock runner returns malformed output with nil command error;
- `cmuxAdapter.CreateSession` returns an error;
- returned ID is empty.

This is part of the Phase 1 create contract because the adapter must never fabricate an ID such as `unknown`, `42`, or a partially parsed value when cmux output cannot be parsed.

Required correction:

```go
func TestCmuxAdapter_CreateSessionMalformedOutput(t *testing.T) {
    adapter := &cmuxAdapter{runner: &mockCmuxRunner{
        runFunc: func(context.Context, CommandOptions, ...string) ([]byte, error) {
            return []byte("created but no surface id"), nil
        },
    }}
    id, err := adapter.CreateSession(context.Background(), CreateOptions{})
    if err == nil { t.Fatal("...") }
    if id != "" { t.Fatalf("id = %q, want empty", id) }
}
```

### 2. handler/API exactly-once canonicalization is still missing

No test was added under `internal/term`, and existing `TestAPIGolden_PostCreateSession_ReturnsCanonicalID` only covers a tmux-shaped golden adapter. There is still no handler/API test proving:

- a tmux-style local ID returns exactly `tmux:<local>`;
- a cmux-style local ID `surface:42` returns exactly `cmux:surface:42`;
- no double prefix such as `cmux:cmux:surface:42` is emitted.

The new cmux test still manually does:

```go
canonical := SessionRef{Adapter: "cmux", LocalID: id}.Canonical()
```

That verifies `SessionRef.Canonical`, not the HTTP/API boundary where the response ID is generated.

Required correction:

- Add handler/API tests with fake `tmux` and `cmux` adapters implementing `CreateSession`.
- Exercise the actual POST handler used by the app.
- Assert JSON response IDs exactly:
  - `tmux:test`
  - `cmux:surface:42`
- Include an explicit guard that the response is not `cmux:cmux:surface:42`.

### 3. stale `FindSession` still does not assert nil returned session

The requested strengthening was:

```go
session, err := reg.FindSession(...)
if session != nil { t.Fatalf(...) }
```

The tenth revision instead added a duplicated `err == nil` block:

```go
_, err := reg.FindSession(...)
if err == nil { ... }
if err == nil { ... }
```

The duplicate error check does not pin the live lookup contract. Phase 1 requires explicit proof that stale sessions are not returned as live values when refresh fails.

Required correction:

- Replace the duplicated `err == nil` check with `session, err := ...`.
- Assert `session == nil`.
- Keep the stale snapshot retention assertion.

### 4. duplicate `err == nil` checks were introduced

Two tests now contain adjacent duplicate nil-error checks:

- `TestFindSession_AdapterUnavailable`
- `TestFindSession_StaleCacheAndRefreshFailure`

This is low-risk but should be cleaned up in the next narrow revision.

## Required next revision

Do not advance to Phase 2 yet. The next executor revision should be minimal:

1. Add `cmuxAdapter.CreateSession` malformed-output test.
2. Add handler/API exactly-once canonicalization tests for tmux and cmux-style local IDs.
3. Replace duplicated stale lookup error check with `session == nil` assertion.
4. Remove duplicate `err == nil` checks.
5. Then run:

```sh
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./internal/mux ./internal/term -count=1
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./internal/mux -run 'TestRefresh|TestFindSession|TestCreateSession|TestCanonicalID|TestCmux' -count=100
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test ./...
GOCACHE=/tmp/devremote-phase1-verifier-go-cache go test -race ./...
```

Phase 1 is still close, but not accepted until the malformed cmux path and real handler/API canonicalization boundary are covered.
