# Adapter Phase 6 Acceptance

Date: 2026-07-07

Executor commit: `4d8d70dc9`

Verifier decision: **ACCEPT**

Next phase permission: **ALLOWED**

## Scope reviewed

- `companion-daemon/cmd/devremote/app.go`
- `companion-daemon/cmd/devremote/main.go`
- `companion-daemon/internal/mux/localpty_adapter.go`
- `companion-daemon/internal/mux/localpty_adapter_test.go`
- `companion-daemon/internal/term/localpty_e2e_test.go`
- Prior reviews:
  - `docs/ADAPTER_PHASE_6_IMPLEMENTATION_REVIEW.md`
  - `docs/ADAPTER_PHASE_6_IMPLEMENTATION_REVIEW_2.md`
- Accepted scope:
  - `docs/ADAPTER_PHASE_6_SCOPE_ACCEPTANCE.md`

This review accepted the Phase 6 LocalPTY implementation.

## Automated verification

```sh
git diff --check
GOCACHE=/tmp/devremote-phase6-4d8d70d-go-cache go test ./internal/mux -run 'TestLocalPTYAdapter_(NaturalExit|TerminateThenList|Contract)' -count=20 -v
GOCACHE=/tmp/devremote-phase6-4d8d70d-go-cache go test ./internal/term -run TestLocalPTY_E2E_Lifecycle -count=20 -v
GOCACHE=/tmp/devremote-phase6-4d8d70d-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase6-4d8d70d-go-cache go test ./...
GOCACHE=/tmp/devremote-phase6-4d8d70d-go-cache go test -race ./...
cd mobile && node_modules/.bin/tsc --noEmit
```

Results:

- `git diff --check`: PASS
- LocalPTY targeted lifecycle/contract repeated run: PASS
- LocalPTY term E2E repeated run: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- mobile TypeScript compile: PASS

Sandbox note:

Full Go and LocalPTY E2E tests require local `httptest` listener binding, so
they were run with local listener permission.

## Acceptance findings

### 1. LocalPTY is feature-flagged and default-off

Accepted.

`app.go` registers `mux.NewLocalPTYAdapter()` only when
`Config.EnableLocalPTY` is true. `main.go` now exposes
`--enable-localpty` and wires it into `Config.EnableLocalPTY`.

This satisfies the Phase 6 requirement that production registration starts
behind a default-off feature flag.

### 2. Existing tmux/cmux production files were not modified

Accepted.

The Phase 6 implementation adds LocalPTY files and touches the composition
root/entrypoint for feature-flagged registration. Existing tmux/cmux
production adapter files were not modified to make LocalPTY work.

This preserves the extension property proven in Phase 5.

### 3. Create-only LocalPTY discovery is implemented

Accepted.

LocalPTY owns app-created child PTY sessions in memory. It does not attempt to
discover arbitrary OS terminals or external processes. `ListSessions` returns
only sessions not marked exited.

The previous blocker was natural child process exit. At `4d8d70dc9`, LocalPTY
now tracks natural process exit with adapter-owned synchronized state and
filters exited sessions from discovery. The new regression test
`TestLocalPTYAdapter_NaturalExit` passed repeatedly.

### 4. LocalPTY live I/O vertical slice is covered

Accepted.

`TestLocalPTY_E2E_Lifecycle` covers:

- create LocalPTY session;
- discover it through `/api/sessions`;
- connect through `/term/ws`;
- send input through WebSocket;
- receive live PTY output;
- delete the session through `/api/sessions`;
- verify it no longer appears.

The previous flaky interactive `bash` dependency was replaced with deterministic
`cat` echo behavior. The E2E test passed 20 repeated runs.

### 5. Unsupported screen/history capabilities remain explicit

Accepted.

LocalPTY does not implement `ScreenReader` or `HistoryReader`, matching the
accepted Phase 6 scope. The LocalPTY contract test includes an explicit
unsupported screen/history assertion.

### 6. Race safety is restored

Accepted.

The prior `ProcessState` race is no longer present. `go test -race ./...`
passes.

## Non-blocking follow-ups

These do not block Phase 6:

- LocalPTY natural-exit tracking currently marks sessions exited and filters
  them from discovery, but does not remove them from the internal map. A future
  cleanup can reclaim ended sessions.
- The LocalPTY contract test still uses a mock adapter for the shared contract
  suite, so the suite output skips mock `WriteInput`. The term E2E exercises
  production `localptySession.WriteInput` through `HandleWS`, so this is not a
  Phase 6 blocker.
- `CreateSession` and `TerminateSession` still mostly ignore context
  cancellation. This should be revisited if PTY creation/termination becomes
  slow or host-dependent.
- Normal WebSocket shutdown still logs EOF in repeated E2E runs. Tests and
  race checks pass, but expected-close logging can be cleaned later.

## Verdict

Phase 6 is **accepted** at `4d8d70dc9`.

LocalPTY is now a real third backend behind a feature flag, with create-only
discovery, live PTY I/O through the existing API/WebSocket path, unsupported
capabilities represented safely, and race-clean lifecycle handling. Phase 7 may
begin.
