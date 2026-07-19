# PA3 Step 4 Evidence — SessionTelemetry DTO update

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `d09eb593724a53a0b087aa72ddff1bd772557fd1`
Contract SHA: `194f6cd070cd39a8acb6ec0d8cfbf8811f7112ec` (PA3 contract FROZEN)

## Changes

### DTO fields removed from JSON output

- `State`, `Load`, `Runner`, `RunnerColor`, `Events` marked `json:"-"` (from Step 2, retained)
- Struct stubs populated with defaults for test compatibility (Snapshot, mergeLifecycleState)
- Full struct field removal deferred to Step 6 (deletion)
- `managed_catalog.go`: removed `Events: []models.AgentEvent{}` from SessionTelemetry literal + unused `models` import

### Test assertions

- `TestAPIGolden_GetSessions`: legacy keys absent (state/load/runner/runnerColor/events), required keys present
- `TestStep3_NormalList_Returns200`: legacy keys absent from JSON

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	17.062s
ok  	devremote/companion-daemon/cmd/devremote	33.190s
```

## Files modified

- `internal/term/managed_catalog.go` — removed Events from literal, unused import
- `internal/term/managed_api.go` — removed Events from literals
