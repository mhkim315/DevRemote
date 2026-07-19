# PA3 Step 6a R2 Evidence — Captured cleanup + instance-guarded rollback

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `c8070cca244ba77e937e7a3ec156e1077e3c0140`

## Changes

### Wiring gap fixed

- `CatalogEntry.recorder` field added — generation-bound Recorder captured at create
- `register()` stores Recorder on CatalogEntry (all 3 call sites updated)
- Replacement `oldCap` captures `existing.recorder` (not nil) for instance-guarded cleanup
- `StartRecorderUnconditional` clears terminated flag so replacement creates fresh Recorder

### ID-addressed rollback removed

- `createWithCapture`: handle-capture failure uses `DeleteRecorderIfSame(canonicalID, rec)` (instance-guarded)
- Pre-install barrier: `oldCap.Execute()` → `DeleteRecorderIfSame` + `RetireIfGeneration` (instance-guarded)
- Zero ID-addressed `DeleteRecorder` or `terminateAdapterSession` in accepted path

### Focused tests

- `TestStep6a_ReplacementCapturesOldRecorder`: replacement captures old Recorder, old cleanup instance-guarded, new Recorder is different instance
- `TestStep6a_RollbackCleanupIsInstanceGuarded`: replacement executes old cleanup before publishing, cannot target new session

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	14.259s
ok  	devremote/companion-daemon/cmd/devremote	33.194s
```
