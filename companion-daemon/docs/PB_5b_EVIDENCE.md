# PB.5b Evidence — V1 Launcher Wired

**PB.5b IMPL SHA:** TBD (fill after commit)
**PA4 ACCEPT SHA:** `74560edd`

## V1 Wiring

`ManagedPTYLauncherV1` (Spawn-only, PTYHandle 5-method interface) bridges to
the old `ManagedPTYLauncher` via `NewV1FromOld()`. The bridge adapter
(`v1Bridge`) delegates `Spawn(cfg)` → `old.CreateSessionAndCapture(opts)`
and wraps `mux.Session` as a `PTYHandle` (`v1Handle`).

## Interface Design

```
type ManagedPTYLauncherV1 interface {
    Spawn(ctx, cfg SpawnConfig) (PTYHandle, error)
}

type PTYHandle interface {
    Resize(rows, cols int) error
    Write(p []byte) (int, error)
    Wait() error
    Signal(sig syscall.Signal) error
    Close() error
}
```

Zero mux types in the V1 interface. Bridge implementation uses mux internally.

## Gates

```
go build ./...                     exit 0
go vet ./...                       exit 0
gofmt -d .                         clean
go test -race ./... -count=1       ALL PASS (11 packages)
```
