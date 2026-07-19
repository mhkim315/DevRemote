# PA3 Step 4 R2 Evidence — Deterministic fixture adapter

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `e90a498dd5b820fa23e5b812f2128b0a76431945`

## Changes

### Deterministic fixture adapter

Replaced `mux.NewTmuxAdapter` (host-dependent) with `step3Adapter` — an in-memory fixture adapter providing a deterministic session row independent of host tmux state or socket permissions.

### Normal-list test assertions

- Nonempty `id`/`adapter` row PRESENT (`step3:test-session`)
- All five legacy keys ABSENT: `state`, `load`, `runner`, `runnerColor`, `events`

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	16.565s
ok  	devremote/companion-daemon/cmd/devremote	33.267s
```
