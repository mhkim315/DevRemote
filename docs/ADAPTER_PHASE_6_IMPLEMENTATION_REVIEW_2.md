# Adapter Phase 6 Implementation Review 2

Date: 2026-07-07

Executor commit: `60b6d7214`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope reviewed

- `companion-daemon/cmd/devremote/main.go`
- `companion-daemon/internal/mux/localpty_adapter.go`
- `companion-daemon/internal/term/localpty_e2e_test.go`
- Prior review: `docs/ADAPTER_PHASE_6_IMPLEMENTATION_REVIEW.md`
- Accepted scope: `docs/ADAPTER_PHASE_6_SCOPE_ACCEPTANCE.md`

This review evaluated the Phase 6 LocalPTY correction after `7b83cf722`.

## Automated verification

```sh
git diff --check
GOCACHE=/tmp/devremote-phase6-60b6d72-go-cache go test ./internal/mux -run 'TestLocalPTY|Test.*Contract' -count=10 -v
GOCACHE=/tmp/devremote-phase6-60b6d72-go-cache go test ./internal/term -run TestLocalPTY_E2E_Lifecycle -count=20 -v
GOCACHE=/tmp/devremote-phase6-60b6d72-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase6-60b6d72-go-cache go test ./...
GOCACHE=/tmp/devremote-phase6-60b6d72-go-cache go test -race ./...
cd mobile && node_modules/.bin/tsc --noEmit
```

Results:

- `git diff --check`: PASS
- Targeted mux contract run: PASS
- Targeted LocalPTY E2E repeated run: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- mobile TypeScript compile: PASS

Sandbox note:

Full Go and LocalPTY E2E tests require local `httptest` listener binding, so
they were run with local listener permission.

## Progress since previous review

Accepted improvements:

- The previous `ProcessState` data race is gone; `go test -race ./...` passes.
- The E2E now uses deterministic `cat` instead of interactive `bash`; 20
  repeated runs passed.
- `--enable-localpty` is wired into `Config.EnableLocalPTY`.
- `localptySession` now implements `WriteInput`, matching the accepted
  capability table.

These fix the main failures from `7b83cf722`.

## Blocking finding

### 1. Natural child process exit is not reflected in `ListSessions`

The accepted LocalPTY scope defines create-only discovery over active
in-memory sessions:

- `docs/07-phase6-localpty-scope.md:28`: `ListSessions` returns only current
  active/in-memory sessions.
- `docs/ADAPTER_PHASE_6_SCOPE_ACCEPTANCE.md:73-75`: DevRemote owns the child
  PTY lifecycle and `ListSessions` returns only active in-memory LocalPTY
  sessions.

At `60b6d7214`, `ListSessions` filters only `s.exited`:

```go
for _, s := range a.sessions {
    if s.exited {
        continue
    }
    out = append(out, s)
}
```

But `s.exited` is set only in `TerminateSession`:

```go
s.exited = true
if err := s.native.Close(); err != nil { ... }
delete(a.sessions, id)
```

There is no adapter-owned callback, done channel, monitor goroutine, or
synchronized state update for the child process exiting on its own. Therefore
if the local PTY command exits naturally, the session can remain discoverable
as an active LocalPTY session even though the process is already gone.

This matters for Phase 6 because LocalPTY is the first real backend where
DevRemote owns process lifecycle. The implementation must distinguish active
sessions from ended sessions without reintroducing the previous `ProcessState`
race.

Expected correction:

- Track child process exit through a race-free adapter-owned mechanism.
- Remove ended LocalPTY sessions from discovery, or mark them ended in a way
  that the API/telemetry can represent safely.
- Add a regression test using a short-lived deterministic command, for example
  create `true` or `sh -c 'exit 0'`, wait for exit, then verify it does not
  remain listed as an active session.
- Keep `go test -race ./...` passing.

## Non-blocking observations

- The mux contract run still uses `mockLocalPTYAdapter`, so it does not
  directly prove the production `localptySession.WriteInput` method. The term
  E2E now indirectly exercises `WriteInput` because `HandleWS` prefers
  `mux.InputWriter` when present. A direct production-adapter test would make
  this clearer but is not the blocking issue.
- `--enable-localpty` is now present. A small app-level regression test proving
  default-off and enabled registration would be useful.
- `CreateSession` and `TerminateSession` still ignore context cancellation.
  This should be tightened if LocalPTY creation or termination can block under
  real host conditions.

## Verdict

Phase 6 implementation is not accepted at `60b6d7214`.

The previous race, E2E flake, feature flag wiring, and InputWriter mismatch are
substantially addressed. The remaining blocker is LocalPTY lifecycle ownership:
ended child processes must not continue to appear as active discoverable
sessions.
