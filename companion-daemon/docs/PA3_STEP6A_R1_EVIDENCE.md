# PA3 Step 6a R1 Evidence — Lock-I/O fix + Create wiring

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `6dbdc2037d7167ac70c78aa175d9def737ca09c6`

## Defect fixes

### 1. Lock-I/O (controlled_pty_adapter.go)

CreateSessionAndCapture: `a.mu` released BEFORE `SpawnPTYWithDir` (PTY I/O).
ID allocation under lock, I/O outside lock, map insertion under lock.
PA2d §5 invariant preserved — no adapter lock across external I/O.

### 2. Wire Create (owned_pty_runtime.go)

OwnedPTYRuntime.Create restructured:
- Prefers `SessionCreatorWithIdentity.CreateSessionAndCapture` for atomic session identity
- Pre-install barrier: checks `o.entries[canonicalID]` under `o.mu` → extracts old `GenerationCleanupCapability` → `Execute()` + `<-Completion.Done()` outside locks → THEN calls `CreateSessionAndCapture`
- `GenerationCleanupCapability` constructed immediately after create (`defer` rollback on failure)
- `StartRecorderUnconditional`, transport, register, `watchExit`
- Legacy `createLegacy` fallback for adapters without `SessionCreatorWithIdentity`
- Captured cleanup, no ID-addressed rollback

### 3. gofmt

All four changed files formatted.

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	14.152s
ok  	devremote/companion-daemon/cmd/devremote	33.615s
```

Zero mobile diff. Lock-I/O scan: CreateSessionAndCapture releases mutex before SpawnPTY.
