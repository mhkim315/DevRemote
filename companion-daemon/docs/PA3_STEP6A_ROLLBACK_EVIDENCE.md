# PA3 Step 6a Rollback Proof — Evidence

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `a375ee916e6882e3b95daff3e3ffb45247087ea7`

## Test: TestStep6a_RollbackProof

Proves the production deferred `cap.Execute()` rollback runs when
`createWithCapture` fails after `CreateSessionAndCapture` but before
publication (`register`).

### Test flow

1. **Create session A** (`sleep 30`) — succeeds. Captures exact `Session` +
   `Recorder` from the `CatalogEntry`. Verifies both are non-nil and
   Recorder is alive.

2. **Enable failure injector** — set `testCreateWithCaptureFailAfterCapture`
   (package-level test seam in `owned_pty_runtime.go`) to return an
   injected error.

3. **Call Create for same-ID replacement** — returns the injected error.
   The production flow is:
   - Pre-install barrier: `oldCap.Execute()` terminates A's session
     (`CompareAndTerminate`) + stops A's Recorder
     (`DeleteRecorderIfSame`)
   - `CreateSessionAndCapture` creates replacement B (installed in
     adapter map)
   - `cap` created with `Generation: 0`, `defer` set up
   - `StartRecorderUnconditional` creates B's Recorder
   - `newTerminalTransport` creates B's Transport
   - `ManagedProcess` check passes
   - `newCleanup` builds B's cleanup closure
   - **Seam fires** — returns injected error
   - **Deferred `cap.Execute()` runs**:
     - `Transport.RetireIfGeneration(0)` — retires B's Transport
     - `DeleteRecorderIfSame(canonicalID, cap.Recorder)` — stops B's
       Recorder, removes from registry
     - `CompareAndTerminate(ctx, localID, cap.Session)` — terminates
       B's session from adapter map
   - Function returns error

4. **PROVE the deferred `cap.Execute()` ran**:
   - d.1: A's Recorder is stopped → proves `oldCap.Execute` ran in
     pre-install barrier
   - d.2: `GetRecorder(canonicalID)` returns `nil` → proves
     `cap.Execute()` ran for B. B's Recorder was created by
     `StartRecorderUnconditional` and the **only** cleanup of B's
     Recorder on this code path is the deferred `cap.Execute()`.
     No manual `DeleteRecorderIfSame` exists between the seam and
     the return.
   - d.3: Adapter has no session for the local ID → both A and B
     were terminated via `CompareAndTerminate`

5. **Disable failure injector**, create session C (same ID) — succeeds.

6. **Assert C has fresh `Recorder` and `Session`**, distinct from A's.

7. **Assert A's old capability cannot affect C** — C's Recorder stays
   alive and C's session remains in the adapter after a sleep window.

### PROHIBITED in test code

The test does NOT call any of:
- `DeleteRecorderIfSame`
- `DeleteRecorder`
- `terminateAdapterSession`
- `CompareAndTerminate`
- `GenerationCleanupCapability.Execute`
- Any equivalent manual cleanup operation

The production deferred `cap.Execute()` is the **only** cleanup path for
the failed replacement B.

### Production seam

`owned_pty_runtime.go` adds a one-line test seam:
```go
var testCreateWithCaptureFailAfterCapture func() error
```

And a check in `createWithCapture` between `newCleanup` and `register()`:
```go
if testCreateWithCaptureFailAfterCapture != nil {
    if err := testCreateWithCaptureFailAfterCapture(); err != nil {
        return "", err
    }
}
```

Nil in production → zero overhead. Non-nil only in this test.

### Frozen mechanisms (unchanged)

- `CreateSessionAndCapture` releases `a.mu` before `SpawnPTY` I/O
- `oldCap.Session = existing.session` (exact non-nil Session)
- `CatalogEntry.recorder` (exact Recorder instance)
- `createWithCapture` path: instance-guarded recorder cleanup
- `register` with exact session identity
- `GenerationCleanupCapability.Execute` nil-safe
- `GenerationCompletion` `sync.Once`
- PA3 Steps 1–5

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./... -count=1
ok  	devremote/companion-daemon/cmd/devremote	35.621s
ok  	devremote/companion-daemon/internal/agent	3.176s
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	4.027s
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	12.288s
ok  	devremote/companion-daemon/internal/agent/contract	2.044s
ok  	devremote/companion-daemon/internal/agent/doctor	130.612s
ok  	devremote/companion-daemon/internal/devicetrust	6.486s
ok  	devremote/companion-daemon/internal/mux	7.162s
ok  	devremote/companion-daemon/internal/sessionid	4.927s
ok  	devremote/companion-daemon/internal/term	19.365s
ok  	devremote/companion-daemon/internal/transcript	3.829s
ok  	devremote/companion-daemon/internal/watcher	3.875s

$ cd companion-daemon && git diff --check
(no output — clean)

$ cd mobile && npx tsc --noEmit
(no output — success, zero diff)
```
