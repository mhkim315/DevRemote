# PA3 Step 2 R5 Evidence — Ingest return-value tests

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: `ed8cfb714257e0b8849adb64461b7a7d002f6373`
Contract SHA: `194f6cd070cd39a8acb6ec0d8cfbf8811f7112ec` (PA3 contract FROZEN)

## Changes

### Test additions (only)

Four focused tests proving `AuthoritativeApprovalStore.Ingest` returns exact admitted IDs:

1. **TestIngestReturn_NewAdmittedReturnsID** — new actionable approval returns `[AP-NEW]`
2. **TestIngestReturn_IdenticalReofferReturnsEmpty** — idempotent re-offer returns `[]` (empty)
3. **TestIngestReturn_NonActionableAdmittedButNotNotifiable** — non-actionable admitted to store, `Actionable=false`
4. **TestIngestReturn_MixedExistingNew_ReturnsNewOnly** — batch with existing A + new B returns `[AP-B]` only

Notification is dormant (provenActionMapping returns actionable=false for all providers per B5), but the Ingest return value is the authoritative input to any future notification hook.

### managed_api_test.go

- LifecycleState assertion removed — managed Codex/Claude sessions don't have OwnedPTYRuntime catalog entries. LifecycleState is empty for managed providers.

### Two-commit protocol

- Commit 1 (implementation): `ed8cfb714257e0b8849adb64461b7a7d002f6373`
- Commit 2 (evidence): this commit

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./internal/mux ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	16.303s
ok  	devremote/companion-daemon/internal/mux	6.508s
ok  	devremote/companion-daemon/cmd/devremote	33.779s
```

## Files modified

- `internal/term/step2_notification_test.go` (new) — 4 Ingest return-value tests
- `internal/term/managed_api_test.go` — Adapter assertion only
