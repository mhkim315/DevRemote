# PA3 Step 3 R1 Evidence — Replace t.Skip with 410 Gone assertions

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `2bf09c26af76e8616da96fbc01f80dce0e1ea3f6`
Contract SHA: `194f6cd070cd39a8acb6ec0d8cfbf8811f7112ec` (PA3 contract FROZEN)

## Changes

### t.Skip replaced with production-path 410 assertions

- `api_golden_test.go`: `TestAPIGolden_GetSessionsHistory_NoEvents` — asserts 410 Gone for history-capable session
- `api_golden_test.go`: `TestAPIGolden_GetSessionsHistory_ScreenFallback` — asserts 410 Gone for screen-capable session
- `fixture_e2e_test.go`: `TestFixtureE2E_UnsupportedCapability` — asserts 410 Gone for unsupported session
- Removed unused `models` import from api_golden_test.go
- **Zero t.Skip** in all scoped test files (api_golden_test.go, fixture_e2e_test.go, step3_endpoint_test.go)

### Retained negative controls

- `TestStep3_NormalListUnaffected` — query-free GET /api/sessions → 200 (unchanged)

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	16.349s
ok  	devremote/companion-daemon/cmd/devremote	33.334s
```

## Files modified

- `internal/term/api_golden_test.go` — replaced skips with 410 assertions, removed unused import
- `internal/term/fixture_e2e_test.go` — replaced skip with 410 assertion
