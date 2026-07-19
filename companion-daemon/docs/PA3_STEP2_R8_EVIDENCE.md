# PA3 Step 2 R8 Evidence — Production-boundary managed assertions

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `e77261e021949d15e2d0f52abf35d3e0cf1929b8`
Contract SHA: `194f6cd070cd39a8acb6ec0d8cfbf8811f7112ec` (PA3 contract FROZEN)

## Changes (R8)

### Managed API production-boundary test

- Extended `TestManagedREST_ListAndGet_FromOwnedRegistry` with Branch A assertions:
  - `row.Adapter == "codex_app_server"` (Adapter populated)
  - `row.AgentStatus == string(ManagedStatusIdle)` (AgentStatus == NativeStatus)
  - `row.LifecycleState == ""` (absent/empty for managed rows)
  - Raw JSON: `lifecycleState` key absent for managed row

### Removed literal test

- Deleted `TestLifecycleState_ManagedRow_Absent` from `step2_lifecycle_test.go`
  (replaced by real API boundary test above)

### Retained positive controls

- `TestLifecycleState_ControlledPTY_SeededEntry` — seeded CatalogEntry proves non-empty LifecycleState
- 4 Ingest return-value tests — exact-ID atomic admission

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./internal/mux ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	16.509s
ok  	devremote/companion-daemon/internal/mux	6.044s
ok  	devremote/companion-daemon/cmd/devremote	33.412s
```

## Files modified

- `internal/term/managed_api_test.go` — extended with Branch A assertions
- `internal/term/step2_lifecycle_test.go` — removed literal test
