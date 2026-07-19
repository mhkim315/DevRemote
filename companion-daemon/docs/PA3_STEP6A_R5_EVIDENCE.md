# PA3 Step 6a R5 Evidence — Non-vacuous failure test

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `879e34ea50d0400ff99c9f549f74d414f7e20d26`

## Test: TestStep6a_FailedCreateLeavesReplacementIntact

Reproduces real post-capture failure scenario:

1. Create session A (`sleep 30`) — Recorder + Session captured on CatalogEntry
2. Inject post-capture failure: `DeleteRecorderIfSame(idA, entryA.recorder)` stops A's Recorder
3. Create replacement B (same ID) — pre-install barrier:
   - Captures `existing.session` (NON-NIL — verified by test assertion at step A)
   - `oldCap.Execute()` runs: `RetireIfGeneration` (instance-guarded) + `DeleteRecorderIfSame` (no-op, already stopped) + `CompareAndTerminate` with captured Session
   - `<-oldCap.Completion.Done()` waits for old cleanup
4. B created with fresh Recorder + Session
5. Assert: B.recorder != A.recorder, B.session != A.session

**No manual DeleteRecorder, DeleteRecorderIfSame, terminateAdapterSession calls between create calls** — the pre-install barrier's `cap.Execute()` handles all cleanup.

## Production code

No production code changes (R4 fix on owned_pty_runtime.go:193 already committed).

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	14.217s
ok  	devremote/companion-daemon/cmd/devremote	33.343s
```
