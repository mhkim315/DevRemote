# PA3 Step 4 R1 Evidence — Deterministic session + legacy-absent assertions

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `17a8216fcdde8bad34ab7e748c8b0b6d94bc0da1`
Contract SHA: `194f6cd070cd39a8acb6ec0d8cfbf8811f7112ec` (PA3 contract FROZEN)

## Changes

### TestStep3_NormalList_Returns200 updated

- Produces deterministic session via `mux.Registry` (tmux adapter) + `HandleSessionsV2`
- Asserts required keys PRESENT: `id`, `adapter`
- Asserts all five legacy keys ABSENT: `state`, `load`, `runner`, `runnerColor`, `events`

### File list corrected

- `internal/term/step3_fixture_test.go` only (test fix)
- `managed_catalog.go` from Step 4 (Events removal) — no managed_api.go

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	16.521s
ok  	devremote/companion-daemon/cmd/devremote	33.414s
```
