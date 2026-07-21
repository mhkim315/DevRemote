# PB.5b Evidence — SUPERSEDED

> **THIS DOCUMENT IS SUPERSEDED.** It describes the `v1Bridge` transitional
> scaffolding (`cbfa50b26`) which was a rejected intermediate implementation.
> The accepted lineage is:
> - **PB.5a** (`4ed0d3dc3`): V1 launcher cutover — `docs/PB_5a_EVIDENCE.md`
> - **PB.5b-T2** (`0f0d57f30`): Consumer migration, zero mux imports — `docs/PB_5b_EVIDENCE.md` (see branch commits)
> - **PB.5b-T3** (`c2c0f542a`): Physical mux directory deletion — `docs/PB5_TASK3_EVIDENCE.md`
>
> The interface design and gate results below are retained as historical
> reference only. They do NOT describe the accepted production implementation.

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
