# PA3 Step 2 R7 Evidence — Branch A final LifecycleState tests

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `a6bdb4a067a76e4502d8a802327fd20f8a25f78e`
Contract SHA: `194f6cd070cd39a8acb6ec0d8cfbf8811f7112ec` (PA3 contract FROZEN)

## Changes

### TestLifecycleState_ControlledPTY_SeededEntry

- Seeds a running OwnedPTYRuntime CatalogEntry using `seedCatalog()` helper
- Verifies `mergeLifecycleState` returns exact `LifecycleState="running"`
- Proves controlled_pty sessions carry non-empty LifecycleState from the catalog

### TestLifecycleState_ManagedRow_Absent

- Constructs a managed Codex SessionTelemetry row
- Asserts `LifecycleState == ""` (absent/empty)
- Asserts `Adapter == "codex_app_server"` and `AgentStatus == "idle"`
- Per Branch A: LifecycleState owned exclusively by OwnedPTYRuntime

### Removed old trivial tests

- Removed `TestLifecycleState_ControlledPTY_NonEmpty` (no seeding)
- Removed `TestLifecycleState_ManagedRow_Empty` (duplicate — covered by new test)
- Cleaned unused mux/transcript imports from step2_notification_test.go

### Two-commit protocol

- Commit 1 (implementation): `a6bdb4a067a76e4502d8a802327fd20f8a25f78e`
- Commit 2 (evidence): this commit

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./internal/mux ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	17.052s
ok  	devremote/companion-daemon/internal/mux	6.496s
ok  	devremote/companion-daemon/cmd/devremote	35.158s
```

## Files modified

- `internal/term/step2_lifecycle_test.go` (new) — seeded LifecycleState + managed absent tests
- `internal/term/step2_notification_test.go` — removed old trivial tests, cleaned imports
