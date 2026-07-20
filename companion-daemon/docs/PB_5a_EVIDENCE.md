# PB.5a Evidence — Managed PTY Spawn Seam Independence

**PB.5a IMPL SHA:** `981b74e81`
**PA4 ACCEPT SHA:** `74560edd`

## 5a/5b Split

| Wave | Scope | Status |
|------|-------|--------|
| PB.5a | V1 launcher boundary (`pty_launcher_v1.go`) | COMPLETE |
| PB.5b | OwnedPTYRuntime rewrite + old launcher deletion | PENDING |

PB.5a delivers the mux-free launcher design. PB.5b integrates it into OwnedPTYRuntime.

## V1 Launcher Boundary (`pty_launcher_v1.go`)

### ManagedPTYLauncherV1 — Spawn only (line 37-41)

```
type ManagedPTYLauncherV1 interface {
    Spawn(ctx context.Context, cfg SpawnConfig) (PTYHandle, error)
}
```

Single method. No Name, no List, no Terminate, no CompareAndTerminate. Zero mux types.

### PTYHandle — platform-neutral interface (line 28-35)

```
type PTYHandle interface {
    Resize(rows, cols int) error
    Write(p []byte) (int, error)
    Wait() error
    Signal(sig syscall.Signal) error
    Close() error
}
```

Pure Go interface. No mux.Session, no adapter name, no registry.

### SpawnConfig (line 16-27)

```
type SpawnConfig struct {
    Name, Command string
    Args          []string
    Executable    string
    CWD           string
    Env           []string
    Rows, Cols    int
}
```

Plain struct. No mux types.

## Zero Mux Imports

```
$ grep -c "mux\." internal/term/pty_launcher_v1.go
0
```

Zero mux references in the V1 launcher file.

## Transitional State

The old `pty_launcher.go` (with mux wrapper) still serves OwnedPTYRuntime during transition. It will be deleted in PB.5b when OwnedPTYRuntime is rewritten to use ManagedPTYLauncherV1 + PTYHandle directly.

## Gates

```
go build ./...                     exit 0
go vet ./...                       exit 0
gofmt -d .                         clean
go test -race ./... -count=1       ALL PASS (11 packages)
go test -race ./internal/term -run "TestPA4_" -count=20  PASS
cd mobile && npx tsc --noEmit      clean
cd mobile && npx jest              451/451 pass, 34 suites
```
