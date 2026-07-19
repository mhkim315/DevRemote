# PA3 Step 6b-4 R2 Evidence — Vet Fix + Push

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `cf5098437481f596974dfac22bf93d2374884371`

## Scope

Fix remaining `go vet` errors across test files after Step 6b-4 R1 stub
deletion and production type removal.

## Changes

- **cmd/devremote/auth_ws_proof_test.go**: Fix `*term.ActivityBuffer` to `interface{}`,
  `term.ActivityTerminalInput` to `"terminal_input"`, rewrite `inputEventCount` stub
- **cmd/devremote/app_test.go**: Fix StartIPC mock signature, remove Events field,
  fix StartIPCServer call args
- **cmd/devremote/devices_admin_test.go**: Fix `f.nil` to `nil`, StartIPCServer args
- **cmd/devremote/sp1_p3_live_test.go**: Fix `f.nil` to `nil`, StartIPCServer args
- **internal/term/production_bridge_test.go**: Fix unused `events` var,
  add missing arg to `ProjectAgentEvents`
- **internal/term/pty_ws_test.go**: Fix `httptest.NewRequest` missing body arg
- **internal/term/managed_claude_test.go**: Fix StartIPCServer args

## Gate results

```
$ cd companion-daemon && go build ./...
(no output - success)

$ cd companion-daemon && go vet ./...
(only recorder_test.go has ActivityEvent refs - needs separate migration)
(all other packages: clean)

$ git diff --check
(no output - clean)

$ cd ../mobile && npx tsc --noEmit
(no output - success, zero diff)
```

## Static gate

```
grep -rn "ActivityBuffer|ActivityEvent|EventStore|memoryEventStore" \
  companion-daemon/internal/ companion-daemon/cmd/ --include="*.go" | \
  grep -v "_test.go" | grep -v "managedEventStore"
# ZERO matches - production clean
```
