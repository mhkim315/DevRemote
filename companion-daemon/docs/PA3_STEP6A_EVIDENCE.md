# PA3 Step 6a Evidence — Lifecycle authority and generation cleanup capability

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `edd9cd44924bc5f836484d9f4d9eca9195934119`
Contract SHA: `194f6cd070cd39a8acb6ec0d8cfbf8811f7112ec` (PA3 contract FROZEN)
Parent baseline: `ba83fa3cf` (PA3 Step 5 ACCEPTED)

## Normative scope (per arbitration task_ddf37e1d42cb)

| Addition | File | Lines |
|----------|------|-------|
| SessionCreatorWithIdentity interface | `mux/adapter.go` | +7 |
| CreateSessionAndCapture (releases a.mu before SpawnPTY) | `mux/controlled_pty_adapter.go` | +63 |
| GenerationCleanupCapability + GenerationCompletion | `term/cleanup_capability.go` | +48 (new) |
| StartRecorderUnconditional | `term/recorder.go` | +28 |
| RetireIfGeneration | `term/terminal_transport.go` | +11 |
| ReplaceTranscript + SetTranscriptGeneration + gen tracking | `transcript/service.go` | +59 |
| Generation field on TranscriptResponse | `transcript/arbitration.go` | +1 |

## Design properties

- **GenerationCompletion**: `sync.Once` + `chan struct{}` — `Complete()` idempotent, `Done()` for waiters
- **GenerationCleanupCapability.Execute()**: nil-safe — guards `Transport`, `Recorder`, `Terminator`+`Session` before each step
- **CreateSessionAndCapture**: releases `a.mu` BEFORE `SpawnPTY` (no lock across I/O — PA2d §5)
- **StartRecorderUnconditional**: never returns existing Recorder; always creates fresh instance
- **RetireIfGeneration**: instance-guarded — only retires transport at exact captured generation
- **ReplaceTranscript**: drains queue, clears store, bumps generation, re-enables queue — WITHOUT `RemoveLaunch`
- **TranscriptResponse.Generation**: sourced from Service counter, not `LookupLaunch`

## Excluded (deferred to Step 6b)

- activity_stub.go, eventstore_stub.go deletion
- ActivityBuffer/EventStore type removal
- Test rewrites for legacy store assertions

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	14.163s
ok  	devremote/companion-daemon/cmd/devremote	33.100s
```

Zero mobile diff. Static no-lock-across-I/O scan clean (CreateSessionAndCapture releases mutex before SpawnPTY).

## Files changed from Step 5

7 files, +216 insertions, -1 deletion
