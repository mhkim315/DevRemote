# PB.5b Evidence — V1 Launcher Wired

**PB.5b IMPL SHA:** `cbfa50b26`
**PA4 ACCEPT SHA:** `74560edd`

## V1 Wiring (SUPERSEDED)

> **Superseded:** The `v1Bridge` adapter, `NewV1FromOld()`, and `v1Handle`
> wrapper were transitional scaffolding removed in PB.5b-T2 (`0f0d57f30`).
> The V1 launcher now directly owns `Spawn` without delegating through
> the old `ManagedPTYLauncher` or wrapping `mux.Session`. Zero mux types
> remain in the V1 interface or its implementation.

**Historical (pre-T2):** `ManagedPTYLauncherV1` (Spawn-only, PTYHandle
5-method interface) bridged to the old `ManagedPTYLauncher` via
`NewV1FromOld()`. The bridge adapter (`v1Bridge`) delegated
`Spawn(cfg)` → `old.CreateSessionAndCapture(opts)` and wrapped
`mux.Session` as a `PTYHandle` (`v1Handle`).

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
go test -race ./... -count=1       ALL PASS 12 packages
```
