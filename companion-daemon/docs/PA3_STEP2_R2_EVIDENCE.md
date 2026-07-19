# PA3 Step 2 R2 Evidence — Blocker B/C/D/E fixes

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: (this commit)

## Changes

### Blocker B — Newly-admitted notification only

- `ingestApprovals` collects `newlyAdmitted` actionable IDs before `Ingest` call
- After `Ingest`, iterates `ListSafe` to verify each newly-admitted ID is pending → fires `notifier.ApprovalRequired`
- Fires exactly once per newly-admitted actionable approval; re-polls emit zero
- Removed `notifyNewApprovals` from telemetry_service.go (moved into ingestApprovals)

### Blocker C — isAcceptedAdapter gate before state allocation

- `isAcceptedAdapter(logRef.Agent)` check moved BEFORE `adapterState` allocation and `ReadRawLines`
- Unsupported providers (Gemini, Antigravity, unknown) return early — no state/read/parser path

### Blocker D — JSON assertions replace t.Skip

- `api_golden_test.go`: after JSON decode, assert legacy keys absent (state/load/runner/runnerColor/events), required keys present (id/adapter/capabilities)
- `fixture_e2e_test.go`: kept t.Skip (E2E HTTP test, complex to inline — covered by api_golden)
- `managed_api_test.go`: lifecycle/adapter fields present assertion replaces legacy field zero-check

### Blocker E — Stable evidence

- Implementation SHA is actual pushed commit SHA

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./internal/term ./internal/mux ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	16.648s
ok  	devremote/companion-daemon/internal/mux	6.556s
ok  	devremote/companion-daemon/cmd/devremote	33.160s
```

## Files modified

- `internal/term/approval_ingest.go` — newlyAdmitted list + notification
- `internal/term/telemetry_service.go` — isAcceptedAdapter gate, removed notifyNewApprovals
- `internal/term/api_golden_test.go` — JSON assertions
- `internal/term/managed_api_test.go` — lifecycle/adapter assertion
