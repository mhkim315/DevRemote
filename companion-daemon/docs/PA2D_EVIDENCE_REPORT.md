# PA2d — Remove Registry Seam from OwnedPTYRuntime Evidence Report

Status: **REVIEW REQUEST**

Implementation SHA: `c85da49619b973d6490353cd06979d42ac409795`
Evidence/report SHA: (this commit)
Gate execution SHA: `c85da49619b973d6490353cd06979d42ac409795`

## Ancestry

| Ancestor | SHA | Role |
| --- | --- | --- |
| PA2c final ACCEPT | `958ce612f` | accepted baseline; local==remote verified clean before work began |
| PA2d implementation | `c85da49619b973d6490353cd06979d42ac409795` | this packet (9 files) |

## Contract mapping

All seven PA2d scope items from `docs/PA2_LIFECYCLE_TRANSPORT_CONTRACT.md`:

| # | Requirement | Resolution |
| --- | --- | --- |
| 1 | Remove `reg *mux.Registry` from OwnedPTYRuntime struct | Replaced by `spawn mux.Adapter` |
| 2 | Replace spawnControlled (Registry.CreateSession proxy) | `ownSpawn` → direct `adapter.CreateSession` + `startRecorder` (adapter.ListSessions, no Registry) |
| 3 | Replace captureHandle (Registry.FindSession) | `captureSession` → `adapter.ListSessions`, iterates by local ID |
| 4 | Replace newCleanup (Registry.CompareAndTerminateSession) | Direct `adapter.CompareAndTerminate` (SessionIdentityTerminator) |
| 5 | Update NewOwnedPTYRuntime constructor | Takes `spawn mux.Adapter` instead of `reg *mux.Registry` |
| 6 | Update all callers | `app.go`: captures `ctlAdapter`; 7 test files: extract adapter from registry |
| 7 | Preserve PA2c guarantees | Immutable handle capture, generation-bound cleanup, atomic termination under adapter lock, no lock across I/O — all verified by unchanged test suites |

## Gate results

```
$ go test -race ./internal/mux -run "TestPA2cR6|TestPA2cR5" -count=20 → ok (1.3s)
$ go test -race ./internal/term -run "TestPA2c|TestLifecycle" -count=20 → ok (21.0s)
$ go test ./internal/term ./cmd/devremote -count=1                  → ok / ok (23.9s / 32.1s)
$ go test -race ./... -count=1                                      → exit 0, 12 ok
```

| Gate | Result |
| --- | --- |
| `go build ./...` / `go vet ./...` | PASS |
| `gofmt -l .` | CLEAN (exit 0) |
| `git diff --check` | PASS |
| Secret scan (changed files) | CLEAN |
| PA2a zero-reference / PA2b duplicate-parser | 0 / 0 |
| PA2b focused tests | 2/2 PASS |
| Mobile invariant + tsc | CLEAN / not-run (zero mobile changes) |

## Final state

| Condition | Value |
| --- | --- |
| Pushed HEAD | (this commit) |
| Local == Remote | verified in worker_done |
| Worktree | clean |
| Rollback SHA | `958ce612f` (PA2c final ACCEPT) |
