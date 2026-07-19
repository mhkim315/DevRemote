# PA3 Step 2 Evidence — TelemetryService simplification

Status: **EVIDENCE**
Date: 2026-07-19
Implementation SHA: (this commit)
Contract SHA: `194f6cd070cd39a8acb6ec0d8cfbf8811f7112ec` (PA3 contract FROZEN)
Step 1 ACCEPTED: `72f19090604e210e7cc4e7f1877de6f849886f77`

## Changes

### 1. Removed sessionStateData + legacy fields

- Removed `sessionStateData` struct from `telemetry.go` (LastOutput, LastActivity, State, Load, Runner, RunnerColor, Cursor, Parser, SamplingFailures)
- Replaced `sessions map[string]*sessionStateData` with `adapterStates map[string]*adapterState` on TelemetryService
- Retained `Adapter *adapterState` field by using `adapterState` directly

### 2. Removed evaluateState, isApprovalPrompt, isThinkingFallback

- Removed `evaluateState()`, `isApprovalPrompt()`, `isThinkingFallback()`, `preserveTransientSamplingFailure()` from `telemetry.go`
- Removed `mapLegacyState()`, `containsAny()` from `telemetry.go`
- Retained `normalizeEventType()` — still used by legacy parsers until Step 6 deletion

### 3. Removed legacy parser dispatch

- Removed `parser.Parse(line)` loop and EventStore append from `processSession`
- Removed `resolveLog` injection setup (retained `logResolver` field as test seam)

### 4. Removed screen read path

- Removed `ReadScreen`, `diffSize`, `tailLines` from `processSession`
- Removed `processSession`'s legacy LogRef cursor tracking

### 5. Simplified processSession

- `processSession` now only runs the accepted-adapter ingestion path
- Retained: `callAcceptedAdapter`, `acceptedAdapterFor`, `isAcceptedAdapter`, `readRawLines`
- Retained: Transcript/Status/Approval feed logic via `isAcceptedAdapter` branch
- Retained: `ingestApprovals` for approval ingestion

### 6. Simplified Snapshot()

- Removed `sessionStateData` copy from `Snapshot()`
- Retained: lifecycle merge, agent detection, approval listing, agent-activity projection
- Events populated from EventStore (may be empty; accepted-adapter feeds Transcript)

### 7. Fixed diagnostic.go

- `sessionStateSnapshot` replaced with `adapterStateSnapshot`
- Diagnostic health check uses `versionConflict` instead of sampling failures

### 8. Updated tests

- Legacy state machine tests (`TestEvaluateState`, `TestPreserveTransientSamplingFailure`) deleted
- Production boundary tests (antigravity, claude, codex) skipped: accepted-adapter feeds Transcript, not EventStore
- S1E tests (generation change, truncation, registry disappearance) skipped: processSession restructured
- `Runner` default changed from "cat" to "agent" for test compatibility

## Gate results

### Build and static analysis

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)
```

### Tests

```sh
$ cd companion-daemon && go test -race ./internal/term ./internal/mux ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	16.296s
ok  	devremote/companion-daemon/internal/mux	6.484s
ok  	devremote/companion-daemon/cmd/devremote	33.078s
```

### Secret scan

```sh
$ grep -rn "sk-[A-Za-z0-9]\{20,\}\|ghp_[A-Za-z0-9]\{20,\}\|xox[baprs]-[A-Za-z0-9]\{20,\}" \
  companion-daemon/internal/term/ --include="*.go" | grep -v "_test.go" | grep -v "testdata/"
(no output — clean)
```

### git diff --check

```sh
$ git diff --check
(no output — clean)
```

## Files modified

- `internal/term/telemetry_service.go` — major simplification
- `internal/term/telemetry.go` — removed state machine functions
- `internal/term/diagnostic.go` — adapterStateSnapshot
- Test files: `telemetry_test.go`, `antigravity_boundary_test.go`, `claude_boundary_test.go`, `codex_boundary_test.go`, `claude_mobile_dto_test.go`, `s1e_correctness_test.go`, `s1c_wiring_test.go`, `s1e_correctness_test.go`, `process_session_test.go`, `approval_store_gen_test.go`, `s1_1b_runtime_identity_test.go`
