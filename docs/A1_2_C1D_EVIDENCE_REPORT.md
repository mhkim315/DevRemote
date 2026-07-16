# A1.2 C1D — Managed Claude Observation Evidence Report

Status: **C1D R11-F1 IMPLEMENTED — F2 BLOCKED (environment) — AWAITING ACCEPTANCE**

Implementation SHA: `3e3ed584377ad38199088a74aa5ff625fa582378`
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

## 4. F2: Live production proof — BLOCKED

```text
Environment: macOS arm64, claude 2.1.210 (not 2.1.209)
Pinned path: ~/.local/share/claude/versions/2.1.209/claude (does not exist)
Actual binary: ~/.local/bin/claude (version 2.1.210)
POKIT_CLAUDE_DIGEST: NOT SET
```

The live production proof (`TestClaudeStartIPCServerIntegration`) requires:
- Exact Claude Code 2.1.209 at the pinned path
- `POKIT_CLAUDE_DIGEST` set to the verified SHA-256
- Ambient authentication for a harmless bounded provider turn

Installed version is 2.1.210, pinned 2.1.209 is not available. Per the handoff
section 4: "If pinned 2.1.209, ambient authentication or a harmless bounded
provider turn is unavailable, report C1D BLOCKED. Do not substitute fixtures,
fake launchers or a skipped test."

**F2 status: BLOCKED** — awaiting 2.1.209 binary availability.

The existing test `TestClaudeStartIPCServerIntegration` correctly SKIPs when
`POKIT_CLAUDE_DIGEST` is not set and will exercise the full production path
when the environment is available.

## 5. Gate results

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
=== Mobile ===
npx tsc --noEmit                      NOT RUN (no mobile changes)
```

## 6. Test counts

| Package | Tests |
| --- | --- |
| `internal/term` | 45 (2 skipped: live proof, integration) |
| `internal/agent` | all contracts |
| `internal/agent/adapters/claude/v2_1_202` | PASS |
| `internal/agent/adapters/codex/v0_144_1` | PASS |
| `internal/agent/contract` | PASS |
| `internal/mux` | PASS |
| `cmd/devremote` | PASS |
| Remaining packages | PASS |

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

REVIEW REQUEST: A1.2 C1D Managed Claude Observation — `3e3ed584377ad38199088a74aa5ff625fa582378`
