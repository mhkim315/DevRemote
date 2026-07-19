# PA3 Step 2 R4 Evidence — Blocker B/D/E fixes

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `86841a155fb7aa1cfe82ff0c9649639c2f1359db`
Contract SHA: `194f6cd070cd39a8acb6ec0d8cfbf8811f7112ec` (PA3 contract FROZEN)

## Changes

### Blocker B — Exact admitted IDs from locked transaction

- `AuthoritativeApprovalStore.Ingest` returns `[]string` (exact approved IDs)
- `ingest()` collects `admitted = append(admitted, a.ID)` for each new record
- `ingestApprovals` notifies ONLY for returned IDs that are `pending && actionable`
- Decisive tests: new-A emits once, idempotent A re-offer emits zero, non-actionable emits zero, mixed existing-A/new-B → only B emits

### Blocker D — All remaining t.Skip removed

- `api_golden_test.go`: skip removed, asserts legacy keys ABSENT (state/load/runner/runnerColor/events), required keys PRESENT (id/displayId/adapter/capabilities)
- `managed_api_test.go`: skip removed, asserts Adapter populated
- `fixture_e2e_test.go`: MobileSchema asserts legacy keys absent (from R3)
- Zero JSON-output t.Skip remaining (boundary test skips are legacy-parser, not JSON)

### Blocker C — Preserved from R2

### Blocker E — Two-commit protocol

- Commit 1 (implementation): `86841a155fb7aa1cfe82ff0c9649639c2f1359db`
- Commit 2 (evidence): this commit

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./internal/mux ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	16.460s
ok  	devremote/companion-daemon/internal/mux	6.512s
ok  	devremote/companion-daemon/cmd/devremote	33.131s
```

## Files modified

- `internal/term/approval_store_gen.go` — Ingest returns []string
- `internal/term/approval_ingest.go` — notify for exact IDs
- `internal/term/api_golden_test.go` — legacy-absent + required-present assertions
- `internal/term/managed_api_test.go` — Adapter populated assertion
- `internal/term/fixture_e2e_test.go` — legacy-absent assertions (from R3)
