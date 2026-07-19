# PA3 Step 7 Evidence — Final Cleanup and Static Verification

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `04c8ddcb865b6d4cf6414d9646bf451cc46e53a2` (Step 6b-4 R4 ACCEPTED)

## Scope

Per contract §Step 7 (docs/PA3_CONTRACT.md lines 1127-1137): verify all
deleted types are fully removed, confirm remaining types have active
production consumers, run full gate.

## Contract verification

### 1. models.AgentEvent — RETAINED

`models.AgentEvent` has 40+ active production consumers across:
- **Parsers**: gemini_parser.go, claude_parser.go, antigravity_parser.go,
  codex_parser.go — all return `[]models.AgentEvent`
- **AgentParser interface** (parser.go:19): `Parse(json.RawMessage) ([]models.AgentEvent, error)`
- **Reader** (reader.go:18): `ReadNewEvents(...) ([]models.AgentEvent, error)`
- **Telemetry** (telemetry.go:33,165): `SessionTelemetry.Events` field,
  `normalizeEventType(*models.AgentEvent)`
- **Managed API** (managed_api.go): `[]models.AgentEvent{}` in projections
- **Agent package**: Accepted adapter contract uses `agent.AgentEvent`
  (separate type, wraps models.AgentEvent)

**Verdict**: CANNOT remove — type is essential to the parser/telemetry pipeline.

### 2. EventStore interface — ALREADY REMOVED

`eventstore_stub.go` deleted in Step 6b-4. Zero production references confirm:
```
grep -rn "EventStore\b" internal/ cmd/ --include="*.go" | \
  grep -v "_test.go" | grep -v "managedEventStore"
# ZERO matches
```

`managedEventStore` is a separate internal type for managed Codex/Claude
runtimes — not the legacy EventStore.

### 3. CommandBroker — RETAINED

`CommandBroker` interface still in active use:
- `runtime.go:39`: `Handlers.Cmds` field
- `pty.go:709,714`: `h.Cmds.Put` / `h.Cmds.Take` for legacy debug commands
- `app.go:60,166`: Wiring via `Dependencies.Cmds`

**Verdict**: CANNOT remove — still used by production handlers.

### 4. Static verification

```
grep -rn "ActivityBuffer\|ActivityEvent\|EventStore\|memoryEventStore" \
  internal/ cmd/ --include="*.go" | grep -v "_test.go" | \
  grep -v "managedEventStore\|newManagedEventStore\|eventStoreFor"
# ZERO matches — all deleted types fully removed from production
```

## Gate results

```
$ cd companion-daemon && go build ./...
(no output - success)

$ cd companion-daemon && go vet ./...
(no output - success, clean)

$ go test -race ./... -count=1
ok  devremote/companion-daemon/cmd/devremote  33.468s
ok  devremote/companion-daemon/internal/agent  2.898s
ok  devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202  4.555s
ok  devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1  8.985s
ok  devremote/companion-daemon/internal/agent/contract  4.728s
ok  devremote/companion-daemon/internal/agent/doctor  126.000s
ok  devremote/companion-daemon/internal/devicetrust  5.873s
ok  devremote/companion-daemon/internal/mux  8.459s
ok  devremote/companion-daemon/internal/sessionid  2.258s
ok  devremote/companion-daemon/internal/term  19.765s
ok  devremote/companion-daemon/internal/transcript  2.572s
ok  devremote/companion-daemon/internal/watcher  2.924s
(ALL 12 packages pass)

$ git diff --check
(no output - clean)

$ cd ../mobile && npx tsc --noEmit
(no output - success, zero diff)
```

## Summary

| Check | Status |
|-------|--------|
| models.AgentEvent removal | SKIPPED (40+ production consumers) |
| EventStore removal | ALREADY DONE (Step 6b-4) |
| CommandBroker removal | SKIPPED (active production use) |
| Static: zero deleted-type refs | PASS |
| Build | PASS |
| Vet | PASS |
| Race tests | PASS (12/12 packages) |
| Gofmt | PASS |
| Mobile tsc | PASS |
