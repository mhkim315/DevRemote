# PA4.3 Evidence — Terminal Transport Generation-Gated Isolation

**Implementation SHA:** `1c2ee001dbc4b3d0e04ef965f75f10da4acbcd1d`
**PA4.2 Baseline:** `ba675493a` (ACCEPTED)
**PA3 Rollback:** `34d55e950`

## Scope
PA4.3 verifies every managed terminal operation (WriteInput, Resize,
SubscriberFanOut, Retire) is gated by exact-generation TerminalTransport
and never falls back to mux.Registry or legacy adapter session lookup.

## Verified Call Sites (zero production changes needed)

| # | File:Line | Path | Isolation |
|---|-----------|------|-----------|
| 1 | `terminal_transport.go:69` | `WriteInput` → nil writer check → fail-closed | Generation-gated |
| 2 | `terminal_transport.go:80` | `Resize` → gated by retired check | Generation-gated |
| 3 | `terminal_transport.go:58` | `RetireIfGeneration` → mismatched gen rejected | Generation-gated |
| 4 | `terminal_transport.go:94` | `SubscriberFanOut` → exact-generation transport | Generation-gated |
| 5 | `pty.go:204-209` | HandleWS → TerminalTransport preferred path | No Registry fallback for managed |
| 6 | `pty.go:461` | Input routes through TerminalTransport | Generation-gated |
| 7 | `pty.go:363` | Replay through TerminalTransport SubscriberFanOut | Generation-gated |

## 6 PA4.3 Tests (all pass at `-race -count=20`)

| # | Test | Proves |
|---|------|--------|
| 1 | `TestPA4_3_WriteInputGatedByGeneration` | Write silently dropped after retirement |
| 2 | `TestPA4_3_RetireIfGenerationRejectsStaleGen` | Stale generation cannot retire transport |
| 3 | `TestPA4_3_ResizeGatedByGeneration` | Resize is generation-gated |
| 4 | `TestPA4_3_RetireIsIdempotent` | Multiple Retire calls safe |
| 5 | `TestPA4_3_WriteInputFailClosed` | Nil writer silently drops input |
| 6 | `TestPA4_3_NewTerminalTransportAssignsGeneration` | Constructor stores exact generation |

## Gates
```
go build ./... && go vet ./...        → exit 0
gofmt -d (changed files)              → clean
go test -race ./internal/term -run "TestPA4_3_" -count=20 → ok 1.595s
go test -race ./internal/term -count=1                     → ok 16.735s
git diff --check                      → exit 0
HEAD == upstream                      → confirmed externally
git status --short                    → clean
```

## Production Files (Unchanged)
- `terminal_transport.go` — already generation-gated
- `pty.go` — TerminalTransport preferred path for controlled_pty
- `owned_pty_runtime.go` — transport registration with generation
