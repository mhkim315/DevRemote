# PA3 Step 6 R1 Evidence — Complete contracted scope

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `a6bd74c537bfc52eee59d4e0b1f61fa8963fd186`
Contract SHA: `194f6cd070cd39a8acb6ec0d8cfbf8811f7112ec` (PA3 contract FROZEN)

## Lifecycle/identity additions (contract lines 1090-1118)

| Addition | File |
|----------|------|
| GenerationCleanupCapability + GenerationCompletion | `term/cleanup_capability.go` (new) |
| StartRecorderUnconditional | `term/recorder.go` |
| RetireIfGeneration | `term/terminal_transport.go` |
| ReplaceTranscript + SetTranscriptGeneration + generation tracking | `transcript/service.go` |
| Generation field on TranscriptResponse | `transcript/arbitration.go` |
| SessionCreatorWithIdentity interface | `mux/adapter.go` |

## Physical deletions retained from Step 6

- activity.go, eventstore.go, activity_test.go, eventstore_test.go (deleted)
- activity_stub.go, eventstore_stub.go (minimal compilation stubs)

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	14.528s
ok  	devremote/companion-daemon/cmd/devremote	33.244s
```
