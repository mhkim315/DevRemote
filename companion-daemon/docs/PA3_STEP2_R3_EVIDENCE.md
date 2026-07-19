# PA3 Step 2 R3 Evidence — Blocker B/D/E fixes

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `3b1a3efc67b80607ab5298889e06ef37ab585645`
Contract SHA: `194f6cd070cd39a8acb6ec0d8cfbf8811f7112ec` (PA3 contract FROZEN)

## Changes

### Blocker B — Ingest returns admitted count

- `AuthoritativeApprovalStore.Ingest` signature changed from `func(...)` to `func(...) int`
- Returns `ingest()`'s admitted count (0 for idempotent re-offers)
- `ingestApprovals` notifies only if `admitted > 0`
- One actionable admission emits exactly once; identical re-poll emits zero; non-actionable emits zero

### Blocker D — All t.Skip removed, JSON assertions

- `api_golden_test.go`: skip removed, asserts legacy keys absent (state/load/runner/runnerColor/events), required keys present (id/adapter/capabilities)
- `fixture_e2e_test.go`: skip removed, asserts legacy keys absent + required keys present (id/adapter/capabilities)
- `managed_api_test.go`: skip removed, asserts lifecycle/adapter present
- Zero `t.Skip("PA3 Step 2..."` remaining in any test file

### Blocker C — Preserved from R2 (isAcceptedAdapter gate)

### Blocker E — Two-commit protocol

- Commit 1 (implementation): `3b1a3efc67b80607ab5298889e06ef37ab585645`
- Commit 2 (evidence): this commit

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./internal/mux ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	16.305s
ok  	devremote/companion-daemon/internal/mux	6.567s
ok  	devremote/companion-daemon/cmd/devremote	33.103s
```

## Files modified

- `internal/term/approval_store_gen.go` — Ingest returns int
- `internal/term/approval_ingest.go` — notify only if admitted > 0
- `internal/term/telemetry_service.go` — preserved isAcceptedAdapter gate
- `internal/term/api_golden_test.go` — JSON assertions (legacy absent, required present)
- `internal/term/managed_api_test.go` — lifecycle/adapter assertion
- `internal/term/fixture_e2e_test.go` — MobileSchema with legacy-absent assertions
