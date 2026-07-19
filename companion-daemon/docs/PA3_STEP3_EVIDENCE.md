# PA3 Step 3 Evidence — Remove legacy API endpoints

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `f997f155f68f68c8602dd0276265ebabe691b425`
Contract SHA: `194f6cd070cd39a8acb6ec0d8cfbf8811f7112ec` (PA3 contract FROZEN)

## Changes

### Query endpoints removed

- `GET /api/sessions?activity=<id>` → 410 Gone (replaced by `GET /api/sessions/{id}/transcript`)
- `GET /api/sessions?history=<id>` → 410 Gone (replaced by `GET /api/sessions/{id}/transcript`)
- Normal `GET /api/sessions` (no query params) unaffected

### Tests added

- `TestStep3_ActivityEndpointReturnsGone` — ?activity= returns 410
- `TestStep3_HistoryEndpointReturnsGone` — ?history= returns 410
- `TestStep3_NormalListUnaffected` — plain GET /api/sessions returns 200

### Tests updated

- `activity_test.go`: expect 410 for ?activity= queries
- `api_golden_test.go`: skip history tests (endpoint removed)
- `fixture_e2e_test.go`: skip UnsupportedCapability test (?history= removed)
- `telemetry.go`: removed `fmt` import (no longer used after history branch removal)

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	16.429s
ok  	devremote/companion-daemon/cmd/devremote	33.424s
```

## Files modified

- `internal/term/telemetry.go` — replaced ?activity=/?history= branches with 410 Gone
- `internal/term/step3_endpoint_test.go` (new) — HTTP route tests
- `internal/term/activity_test.go` — updated expectations
- `internal/term/api_golden_test.go` — skipped history tests
- `internal/term/fixture_e2e_test.go` — skipped ?history= test
