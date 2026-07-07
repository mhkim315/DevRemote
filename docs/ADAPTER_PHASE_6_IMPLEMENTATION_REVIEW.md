# Adapter Phase 6 Implementation Review

Date: 2026-07-07

Executor commit: `7b83cf722`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope reviewed

- `companion-daemon/cmd/devremote/app.go`
- `companion-daemon/cmd/devremote/main.go`
- `companion-daemon/internal/mux/localpty_adapter.go`
- `companion-daemon/internal/mux/localpty_adapter_test.go`
- `companion-daemon/internal/term/localpty_e2e_test.go`
- Accepted scope: `docs/ADAPTER_PHASE_6_SCOPE_ACCEPTANCE.md`

This review evaluated the first Phase 6 LocalPTY implementation commit.

## Automated verification

```sh
git diff --check
GOCACHE=/tmp/devremote-phase6-7b83cf7-go-cache go test ./internal/mux -run 'TestLocalPTY|Test.*Contract' -count=10 -v
GOCACHE=/tmp/devremote-phase6-7b83cf7-go-cache go test ./internal/term -run TestLocalPTY_E2E_Lifecycle -count=10 -v
GOCACHE=/tmp/devremote-phase6-7b83cf7-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase6-7b83cf7-go-cache go test ./...
GOCACHE=/tmp/devremote-phase6-7b83cf7-go-cache go test -race ./...
cd mobile && node_modules/.bin/tsc --noEmit
```

Results:

- `git diff --check`: PASS
- Targeted mux contract run: PASS
- Targeted LocalPTY E2E repeated run: FAIL
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: FAIL
- mobile TypeScript compile: PASS

Sandbox note:

The first sandboxed LocalPTY E2E attempt failed because `httptest` local
listener binding was denied. The same test was rerun with local listener
permission.

## Accepted progress

- LocalPTY is implemented as a new adapter file.
- Existing tmux/cmux production files were not modified.
- `app.go` registers LocalPTY behind `cfg.EnableLocalPTY`.
- The implementation uses app-owned, create-only in-memory sessions.
- The term E2E test attempts the correct vertical slice shape:
  create -> API discovery -> WebSocket output/input -> delete -> verify gone.

These are the right directions for Phase 6, but the implementation is not
stable or fully wired yet.

## Blocking findings

### 1. `go test -race ./...` fails with a LocalPTY data race

Race output:

```text
WARNING: DATA RACE
Write at ... by goroutine:
  os/exec.(*Cmd).Wait()
  devremote/companion-daemon/internal/mux.SpawnPTY.func1()
      internal/mux/session.go:60

Previous read at ... by goroutine:
  devremote/companion-daemon/internal/mux.(*localptyAdapter).ListSessions()
      internal/mux/localpty_adapter.go:37
```

The race is caused by `ListSessions` reading `s.native.Cmd.ProcessState` while
the goroutine in `SpawnPTY` is concurrently calling `cmd.Wait()`, which writes
that state.

Relevant code:

- `internal/mux/localpty_adapter.go:37`
- `internal/mux/session.go:60`

Expected correction:

- Do not read `exec.Cmd.ProcessState` concurrently without synchronization.
- Track LocalPTY process exit state through adapter-owned synchronized state,
  a done channel, or another race-free lifecycle mechanism.
- `go test -race ./...` must pass.

### 2. LocalPTY E2E is flaky and fails under repetition

Repeated command:

```sh
GOCACHE=/tmp/devremote-phase6-7b83cf7-go-cache go test ./internal/term -run TestLocalPTY_E2E_Lifecycle -count=10 -v
```

Observed failure:

```text
localpty_e2e_test.go:105: echo output not received in:
"For more details, please visit https://support.apple.com/kb/HT208050.\r\n"
```

The test reads exactly one frame after writing `echo LOCALPTY_E2E_OK\n`. On
macOS, interactive `bash` can emit startup/banner text first, so the test can
miss the echo result even though it may arrive in a later frame.

Relevant code:

- `internal/term/localpty_e2e_test.go:82-105`

Expected correction:

- Use a deterministic child command for E2E instead of interactive `bash`, or
  read frames in a loop until the marker appears or the deadline expires.
- The test should not depend on shell banner/prompt ordering.
- Repeated LocalPTY E2E should pass.

### 3. The feature flag is not exposed through CLI or environment

`app.go` registers LocalPTY behind `cfg.EnableLocalPTY`, but `main.go` never
sets that field:

```go
runDaemon(Config{
    OwnerUUID:          *ownerUUID,
    SupabaseProjectRef: *supabaseRef,
    InsecureLocalOnly:  *insecureLocalOnly,
})
```

Relevant code:

- `cmd/devremote/app.go:117`
- `cmd/devremote/main.go:26-40`

The accepted scope said LocalPTY should be behind a default-off feature flag,
for example `POKIT_ENABLE_LOCALPTY=1` or a config flag. At `7b83cf722`, the
field exists but production users cannot enable it from the daemon entrypoint.

Expected correction:

- Add a real default-off CLI flag or environment variable.
- Wire it into `Config.EnableLocalPTY`.
- Add tests proving default-off and enabled registration behavior.

### 4. Scope says `InputWriter` is supported, but the implementation does not implement it

`docs/07-phase6-localpty-scope.md` lists `InputWriter` as supported.

At `7b83cf722`, `localptySession` only implements `OpenStream`; it does not
implement `WriteInput`. WebSocket input still works through the fallback
`stream.Write` path, but the implementation no longer matches the accepted
capability table.

Evidence from the contract run:

```text
TestLocalPTYAdapter_Contract/LiveStream/WriteInput
    adapter_contract_test.go:652: no InputWriter
```

Expected correction:

- Either implement `WriteInput` for LocalPTY, or update the Phase 6 scope so
  LocalPTY explicitly uses `TerminalStream.Write` rather than `InputWriter`.
- The contract and documentation must agree.

## Non-blocking observations

- `CreateSession` and `TerminateSession` currently ignore their contexts.
  This is not the top blocker, but Phase 6 should be careful not to regress the
  context/timeout isolation established in Phase 3 and Phase 4.
- `CreateSession` defaults to `bash`. A deterministic command may be better for
  tests, while production can still default to the user's shell or an explicit
  configured command later.

## Verdict

Phase 6 implementation is not accepted at `7b83cf722`.

The implementation proves that the right vertical slice is being attempted,
but it cannot be accepted until the race failure is fixed, the LocalPTY E2E is
deterministic, the feature flag is externally usable, and the InputWriter
capability mismatch is resolved.
