# A1.2 C1D — Managed Claude Observation Evidence Report

Status: **C1D R11-F1 IMPLEMENTED — F2 PASS (live proof) — AWAITING ACCEPTANCE**

Implementation SHA: `51d76ea99b2589891950b7daca8044d5dbfba42e`
Accepted C0D evidence: `e42d4c570e64462ce813861017cc635e338e68bf`
Accepted SP1/Codex baseline: `2b940a6fce6e878ffa0da17b5df4d39438af144d`
R11 handoff SHA: `7267080df88d385d5c86273b3f79a1d964e7765c`

## 0. R11-F1: Metadata-only Store generation authority — PASS

### Problem

R11 was rejected because `terminate()` called `IngestObserved` with
`Provenance: "c1d_internal"` and a fake `_c1d_hw_` approval ID. The Store's
`ingest()` function mutated session state (created session, advanced high-water,
superseded records) BEFORE validating item provenance. This meant even an
invalid ingest with non-authoritative provenance could create session metadata
and supersede existing records.

### Fix

**`AuthoritativeApprovalStore.InstallRuntimeGeneration`** (new method):
- Creates session if absent (bounded by `authMaxApprovalSessions`)
- Atomically binds `SessionID + LaunchGeneration + StreamGeneration`
- Supersedes older records at prior generations
- Creates **NO** Approval record — pure metadata transition
- This is the single Store-owned path for termination/replacement to install
  generation authority without an ingest side effect

**`ingest()` restructured** (deferred mutation):
- Session creation and high-water advance are now DEFERRED until AFTER at least
  one item passes ALL validation gates:
  1. Non-empty, bounded ApprovalID
  2. Consistent SessionID
  3. **Authoritative provenance** (`contract.ApprovalAuthoritative`)
  4. Valid delivery material (if present)
  5. Non-conflicting idempotent re-offer
  6. Capacity within `authMaxApprovalsPerSession`
- If no items pass all gates, `ingest()` returns 0 with **zero Store mutation**
- An invalid `IngestObserved` (e.g. `Provenance: "c1d_internal"`) now creates
  NO session, advances NO generation, and supersedes NO records

**`managed_claude.go` terminate()**:
- Replaced the `IngestObserved(c1d_internal)` pattern with a direct call to
  `InstallRuntimeGeneration(sessionID, epoch, 1, "terminated")`

### New tests (6 tests, all PASS)

| Test | What it proves |
| --- | --- |
| `TestInstallRuntimeGeneration_CreatesSessionWithoutRecord` | Session exists, zero records, zero public DTOs |
| `TestInstallRuntimeGeneration_RejectsLateStreamGen0` | `genNewer(epoch,1,epoch,0)` rejects late observation |
| `TestInvalidIngestObserved_NoStoreMutation` | Non-authoritative provenance → session NOT created, zero Len |
| `TestInvalidIngestObserved_DoesNotAdvanceHighWater` | Valid record survives invalid high-gen ingest attempt |
| `TestInstallRuntimeGeneration_SupersedesRecords` | Pending records invalidated when hw advances |
| `TestInstallRuntimeGeneration_IdempotentSameGeneration` | Same/older gen → no-op for hw, no duplicate records |

### Existing tests — all preserved

- All 6 existing `TestInstall*` / `TestInvalid*` tests: PASS
- All 35+ Claude-specific tests (10x race): PASS
- All frozen A1/SP1 regressions: PASS
- Full `go test -race ./...` (12 packages): PASS

## 1. Existing C1D tests — all preserved PASS

| Test | Status |
| --- | --- |
| `TestClaudeCreateDetachedAttestorFails` | PASS |
| `TestClaudeCreateDetachedLauncherError` | PASS |
| `TestClaudeCreateDetachedSuccess` | PASS |
| `TestClaudeDeferredJoinFullIdentityMatch` | PASS |
| `TestClaudeDeferredJoinMismatchedSessionID` | PASS |
| `TestClaudeDeferredJoinMismatchedToolName` | PASS |
| `TestClaudeDeferredJoinMismatchedInputDigest` | PASS |
| `TestClaudeDuplicateToolUseID` | PASS |
| `TestClaudeCapacityExhaustion` | PASS |
| `TestClaudeExitClearsPendingObservations` | PASS |
| `TestClaudeStopIdempotent` | PASS |
| `TestClaudeKillCleanup` | PASS |
| `TestClaudeDeleteTerminalOnly` | PASS |
| `TestClaudeApprovalRecordNonActionable` | PASS |
| `TestClaudeHookBridgeDecodeValid` | PASS |
| `TestClaudeHookBridgeDecodeMalformed` | PASS |
| `TestClaudeHookBridgeDecodeMissingRequired` | PASS |
| `TestClaudeHookBridgeDecodeUnknownField` | PASS |
| `TestClaudeHookBridgeDecodeDuplicateKey` | PASS |
| `TestClaudeHookBridgeDecodeTrailingContent` | PASS |
| `TestClaudeHookBridgeDecodeWrongHookEvent` | PASS |
| `TestClaudeHookBridgeDecodeOversizedFields` | PASS |
| `TestClaudeLaunchArgv` | PASS |
| `TestClaudeShutdownCleansUp` | PASS |
| `TestClaudeStreamDeferredParsing` | PASS |
| `TestClaudeProcessLineSkipsNonResult` | PASS |
| `TestClaudeHookSettingsSchema` | PASS |
| `TestClaudePumpEOFExit` | PASS |
| `TestClaudeAttestorDigestMismatch` | PASS |
| `TestClaudeAttestorDigestMissing` | PASS |
| `TestClaudeAttestorNoPinnedPath` | PASS |
| `TestClaudeTimeoutExpiry` | PASS |
| `TestClaudeActiveApprovalCapacity` | PASS |
| `TestClaudeEntropyFailure` | PASS |
| `TestClaudeStopInvalidatesApprovals` | PASS |
| `TestClaudeIPCComposition` | PASS |
| `TestClaudeIPCUnavailable` | PASS |
| `TestClaudeStopJoinRace` (forward race) | PASS |
| `TestClaudeReverseRace` | PASS |

## 2. Production CLI path — PASS

`pokit run claude` serializes `{"operation":"create","profileId":"claude"}`
through the 0600 Unix socket. `TestClaudeIPCComposition` proves the structured
profile routes to `ManagedClaudeService` and `TestClaudeIPCUnavailable` proves
fail-closed when the service is absent.

## 3. Non-actionable proof — PASS

`TestClaudeApprovalRecordNonActionable` — ingested records have zero options,
zero delivery material. No ClaimForExecution, CTA, or action handler wiring
exists in C1D.

## 4. R11-F1 review blockers — all fixed

### Blocker 1: Older-gen termination no-op — FIXED

`InstallRuntimeGeneration` now only calls `supersedeLocked` when
`genNewer(incoming, current)` is true. An older or equal generation
is a complete no-op.

Test: `TestInstallRuntimeGeneration_OlderGenDoesNotSupersedeCurrentApprovals`
— seeds a record at gen (10, 0), then calls InstallRuntimeGeneration at
gen (5, 1). Verifies the gen-10 record remains `ApprovalPending`.

### Blocker 2: Capacity eviction preserves live authority — FIXED

`evictOldestSessionLocked` skips sessions with pending/executing records
(via new `sessionHasLiveAuthority` helper). `evictOneLocked` only considers
terminal records as eviction victims.

Tests:
- `TestEvictOldestSession_SkipsLiveAuthority` — live session survives
  session-count eviction pressure
- `TestEvictOneLocked_PreservesPendingRecords` — all-pending session at
  capacity survives record eviction

### Blocker 3: Newer gen supersession independent of item admission — FIXED

`ingest()` now tracks `hasAuthoritativeItem` separately from `pending`
admission. A structurally valid newer generation supersedes old records
even when specific items fail duplicate-ID or capacity checks.

Tests:
- `TestIngest_NewerGenSupersedesWithDuplicateIDs` — duplicate ID at
  newer gen still invalidates old-gen records
- `TestIngest_NewerGenSupersedesAtCapacity` — capacity-full newer gen
  still invalidates all old-gen records

## 5. F2: Live production proof — PASS

```text
Environment: macOS arm64
Binary: /Users/mhk/.local/share/claude/versions/2.1.209
Version: 2.1.209 (Claude Code)
SHA-256: 59d2de7f49db2f75d5c33bbb46a6b8f288ad24d40b61e30602a502bb7ddc380c
```

`TestClaudeStartIPCServerIntegration` — PASS (12.62s):

```text
IPC Server listening on /tmp/pokit-c1d-test-*.sock
created session: claude_headless:claude-* state=running via IPC socket
session: provider=claude version=2.1.209 epoch=1 digest=59d2de...
approvals: 1  ← exactly one non-actionable observation
post-stop approvals: 1  ← resolution window
PASS
```

Full production path proven:
1. `pokit run claude` structured request → IPC socket ✓
2. `StartIPCServer` → `ManagedClaudeService.CreateDetached` ✓
3. Attestor: version check (2.1.209) + path match + SHA-256 digest ✓
4. Direct launch: `--verbose --settings <isolated> --setting-sources "" -p <prompt>` ✓
5. Private hook bridge: `PreToolUse` → `defer` → `tool_deferred` join ✓
6. `AuthoritativeApprovalStore.IngestObserved`: exactly 1 non-actionable record ✓
7. Zero options, zero delivery material, zero CTA/claim path ✓
8. Stop → cleanup: hook dir removed, child reaped, socket gone ✓

## 6. Gate results

```text
=== Backend ===
go build ./...                        PASS
go vet ./...                          PASS
go test -race ./... -count=1          PASS (12 packages, 0 failures)
git diff --check                      PASS
=== Invariants ===
agentKind === vendor branch scan      PASS (no hits)
option ID inference scan              PASS (no hits)
=== Security ===
Secret scan (production paths)        PASS (no new secrets)
=== Frozen regressions ===
Claude race tests (10x)               PASS
A1/SP1 regression tests               PASS
=== Live proof ===
TestClaudeStartIPCServerIntegration   PASS (12.62s, 1 observation)
=== Mobile ===
npx tsc --noEmit                      NOT RUN (no mobile changes)
```

## 7. Test counts

| Category | Count | Status |
| --- | --- | --- |
| New F1 counterexample tests | 5 | PASS |
| Existing F1 Store tests | 6 | PASS |
| Claude-specific tests | 39 | PASS |
| Claude race tests (10x) | 390 | PASS |
| Live IPC-to-Store proof | 1 | PASS |
| A1/SP1 frozen regressions | all | PASS |
| Total packages | 12 | PASS |

## 7. C1D non-goals (unchanged)

- C2D/C3D: NOT implemented
- Action options, ClaimForExecution, delivery capacity, mobile CTA: ZERO
- Process-image attestation: deferred to C3D
- macOS code-signing, Windows, generic SDK: NOT in scope
- C0H/C0R: remain BLOCKED
- N1, O1/O2, Agent SDK, Channels, terminal injection: NOT in scope

## 8. Privacy projection

- Raw prompt, command, tool input, CWD, hook capability: absent from DTOs/logs
- Provider payload: never enters public DTOs
- `SafeApprovalDTO`: zero options, zero delivery material
- `ListSafe` returns only bounded, redacted projections

REVIEW REQUEST: A1.2 C1D Managed Claude Observation — `07582490ec618bdf1135757362ef7e59a264e3c1`
