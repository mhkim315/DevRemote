# PA3 Step 3 R2 Evidence — Production-path fixture: Transcript API + 410 negative controls

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `bfee44fa7d6b05fbc2729e501c7e3bc663381c8f`
Contract SHA: `194f6cd070cd39a8acb6ec0d8cfbf8811f7112ec` (PA3 contract FROZEN)

## Changes

### Negative controls (410 Gone)

- `TestStep3_ActivityEndpoint_Returns410` — `?activity=` → 410 via `HandleSessionsV2`
- `TestStep3_HistoryEndpoint_Returns410` — `?history=` → 410 via `HandleSessionsV2`
- `TestStep3_NormalList_Returns200` — query-free list unaffected

### Positive controls (Transcript API proves history available)

- `TestStep3_TranscriptAPI_HistoryAvailable` — feeds bytes via `FeedBytes` + `FlushBytes`, queries `HandleTranscript`, asserts `semantic` segments + `primarySource=byte_stream`
- `TestStep3_TranscriptAPI_HistoryAvailable_AfterOutput` — enables queue, feeds multiple chunks, closes queue, queries with limit, asserts >=2 segments

### Cleanup

- Removed `step3_endpoint_test.go` (consolidated into `step3_fixture_test.go`)
- Zero t.Skip in all Step 3 tests
- Zero production code changes

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	16.208s
ok  	devremote/companion-daemon/cmd/devremote	33.362s
```

## Files modified

- `internal/term/step3_fixture_test.go` (new) — negative 410 + positive Transcript API tests
- `internal/term/step3_endpoint_test.go` (deleted) — consolidated
