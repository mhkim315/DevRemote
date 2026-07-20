# PB.5a Evidence — Managed PTY Spawn Seam Independence

**PB.5a IMPL SHA:** `2fe4cbedf`
**PA4 ACCEPT SHA:** `74560edd`

## Boundary Design

`ManagedPTYLauncher` is a narrow platform-neutral interface in `internal/term/pty_launcher.go`:

```
type ManagedPTYLauncher interface {
    Name() string
    CreateSessionAndCapture(ctx, opts) (localID, Session, error)
    CreateSession(ctx, opts) (string, error)
    ListSessions(ctx) ([]Session, error)
    CompareAndTerminate(ctx, canonicalID, expected) error
    TerminateSession(ctx, localID) error
}
```

Does NOT expose: registry, discovery, adapter selection, legacy list/get APIs.

## Zero-Registry Proof

Managed spawn paths (Create → createWithCapture) call only ManagedPTYLauncher methods:
- `launcher.CreateSessionAndCapture()` replaces `sci.CreateSessionAndCapture()`
- `launcher.CompareAndTerminate()` replaces `sit.CompareAndTerminate()`
- `launcher.TerminateSession()` replaces `term.TerminateSession()`

`o.spawn` field changed from `mux.Adapter` to `ManagedPTYLauncher`.
All type assertions to mux interfaces removed from owned_pty_runtime.go.

## Gates
```
go build ./...                     exit 0
go vet ./...                       exit 0
go test -race ./... -count=1       ALL PASS (12 packages)
```
