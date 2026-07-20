# PB.1 Evidence — Localpty Adapter Removal

**PB.1 IMPL SHA:** `d3fa97eaa`
**PA4 ACCEPT SHA:** `74560edd`
**PA4 IMPL SHA:** `4aaf3b76a`

## Zero Consumers Proof

```
$ grep -rn "localpty|LocalPTY|EnableLocalPTY|enable-localpty" --include='*.go' . | grep -v "_test.go" | grep -v "testdata"
(NONE)

$ grep -rn "localpty|LocalPTY|EnableLocalPTY|enable-localpty" --include='*_test.go' .
(NONE)

$ grep -rn "localpty|LocalPTY|enable-localpty" ../mobile/src/ ../scripts/ scripts/
(NONE)
```

**Zero localpty consumers across all surfaces.**

## Shared Primitive Extraction

- `internal/mux/pty_spawn.go`: RETAINED platform-neutral PTY spawn boundary
- Contains `SpawnPTY`, `SpawnPTYWithDir`, `NativeSession` + methods
- Used by: `controlled_pty_adapter.go` (managed) and `session.go::NewSession` (tmux legacy)

## Files Deleted

| File | Reason |
|------|--------|
| `internal/mux/localpty_adapter.go` | Legacy adapter |
| `internal/mux/localpty_adapter_test.go` | Tests |
| `internal/term/localpty_e2e_test.go` | E2E tests |

## Managed Shell Verification

- `controlled_pty_adapter.go` uses `SpawnPTYWithDir` from retained `pty_spawn.go`
- PA4 isolation preserved: no Registry fallback, TerminalTransport intact

## Gates

```
go build ./...                                    exit 0
go vet ./...                                      exit 0
gofmt -d (changed files)                          clean
git diff --check                                  exit 0
go test -race ./... -count=1                      ALL PASS (12 packages)
go test -race ./internal/term -run "TestPA4_" -count=20  PASS
cd mobile && npx tsc --noEmit                     clean
cd mobile && npx jest                             451/451 pass, 34 suites
```
