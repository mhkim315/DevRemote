# S1.1 Runtime Status Hardening — Implementation Report

Milestone: **S1.1 — Runtime Status Hardening** (between accepted S1 and A1)
Repository (canonical): `https://github.com/mhkim315/DevRemote.git`
Absolute top-level: `/Users/mhk/.gemini/antigravity/scratch/DevRemote`
Branch: `feature/phase10-multi-adapter`
Baseline (accepted S1.1 handoff HEAD): `24328aea3b30cfa3587a526e6c965325621e7f71`
Final implementation SHA: `715c22b59320fa80628b394bde1cd4a4a07571e0`
Authoritative execution contract: `docs/NEXT_SESSION_S1_1_RUNTIME_STATUS_HARDENING_HANDOFF.md`

S1.1 hardens runtime evidence traceability (A), exact runtime identity (B), and
recovery/replay behavior (C) WITHOUT changing the frozen T0 authority model or the
public `agentActivity` DTO.

---

## 0. Repository identity + baseline recovery record

Recovery followed handoff §0.1. The session began at `70ef5df` (one commit behind
canonical); `git fetch` + `merge --ff-only` advanced to the accepted baseline
`24328ae` (a clean fast-forward; the only intervening commit was the S1 acceptance
+ S1.1 handoff docs). All accepted-ancestry checks (§0.1) exit 0:

```
70ef5df (S1 report) · b6504bd (S1 impl) · 9ad6f83 (T3) · 8f7c22de (D1)
ef4a162c (T2) · 162266f8 (T1) · 3ce2604b (T0)   — all ancestors of HEAD
```

Worktree clean; local == canonical remote at each checkpoint.

---

## 1. A/B/C commit traceability

| Packet | Commit | Scope |
| --- | --- | --- |
| S1.1-A | `1aae4e5` | bounded winning-evidence Seq bound to the resolved status |
| S1.1-B | `fbc47fe` | real monotonic launch generation + runtime PID/StartedAt identity |
| S1.1-C | `715c22b` | per-epoch winner high-water: replay/recovery/eviction hardening |

Each was committed only after its focused tests passed; the single REVIEW REQUEST
(below) is emitted once, after A+B+C and the full final gate.

Files changed (17; all within allowed scope — `internal/term/`,
`internal/transcript/launch_binding.go`, and the audit-justified
`internal/mux/controlled_pty_adapter.go`):

```
internal/mux/controlled_pty_adapter.go          (+ ProcessProvider: PID+StartedAt)
internal/term/adapter_state.go                  (launchCorrelation +start arg)
internal/term/agent_status_store.go             (WinningSeq, LaunchGen, epoch rule)
internal/term/create.go                         (real launch spec + replacement invalidation)
internal/term/telemetry_service.go              (launchGen + process-identity wiring, InvalidateForLaunch)
internal/transcript/launch_binding.go           (LaunchSpec, monotonic gen, StartedAt, PID+start correlation)
+ 5 new S1.1 test files, 6 accepted test files signature-updated
```

---

## 2. Before/after call graph

**Before (accepted S1):**

```
createFromProfile → RegisterLaunch(id, prov, adapter, ver, pid=0, generation=1)   # constant gen, silent no-op on re-register
processSession → adapterState → callAcceptedAdapter → GetStatus/ResolveStatus
              → statusStore.Update{Generation: streamGen}                          # one generation axis
              → LaunchCorrelation(binding, prov, ver, discoveredPID)               # PID compared only if binding.PID!=0 (always 0 in prod)
AgentActivityRecord{... Generation, Version}                                       # winner not preserved
```

**After (S1.1):**

```
createFromProfile → managedProcessIdentity(reg,id) → (real PID, spawn StartedAt)
                  → RegisterLaunch(LaunchSpec{...PID,StartedAt}) → (gen, replaced)  # registry-assigned monotonic gen; replaces stale binding
                  → if replaced: Telemetry.InvalidateForLaunch(id, gen)             # non-current high-water at new launch, resets ingestion epoch
processSession → launchGen = binding.Generation
              → getProcessIdentity(ctx,sess,snapshots,batch) → (PID, StartedAt)     # batch snapshot, else session ProcessProvider
              → launchCorrelation(binding, prov, PID, StartedAt)                    # exact PID AND start when claimed; PID-reuse fails closed
              → statusStore.Update{LaunchGen, Generation: streamGen, ...}
                 buildStatusEvidence → (evidence, seqs)                             # parallel Seq slice
                 GetStatus/ResolveStatus                                           # frozen, unchanged
                 locateWinningSeq(evidence, seqs, res)                             # winner located from resolver OUTPUT, no re-decide
AgentActivityRecord{... Generation, Version, LaunchGen, WinningSeq, WinnerHighWater}  # internal only; NOT in DTO
```

The frozen `ResolveStatus`/`GetStatus` and the accepted event→status mapping are
untouched; A/B/C add reference metadata, an identity axis, and an epoch rule
*around* them.

---

## 3. Winning-evidence matrix (S1.1-A)

`WinningSeq` = the stable `AgentEvent.Seq` of the exact accepted event that
produced the resolved status, located by matching the resolver's OUTPUT
`(Status, Provenance)` onto the accepted latest-Seq-ordered candidate list.

| Resolution outcome | WinningSeq bound? | Why |
| --- | --- | --- |
| positive status from a native_log event | yes (that event's Seq) | non-advisory; resolver preserves status/provenance so the exact winner is found |
| equal precedence/confidence tie | yes (latest Seq) | reuses accepted B4 latest-Seq ordering |
| unknown / degraded / adapter error | **no** | fail closed — no fabricated identity |
| advisory-terminal claim (ResolveStatus downgrades to unknown) | **no** | resolved status ≠ any positive candidate |
| Revoke / Invalidate / RevokeIfPresent | **no** | authority loss carries no winner |
| no status evidence in the poll | prior preserved | Update leaves the prior record untouched |

`WinningSeq`/`HasWinningSeq` are `int64`/`bool` — no text or maps — and are NOT in
`AgentActivityDTO` (golden bytes unchanged; leak-guard test asserts the field names
never appear in the wire bytes).

---

## 4. Runtime-identity matrix (S1.1-B)

Launch `Generation` is now a registry-assigned process-monotonic token (the
hard-coded `1` is gone). PID+StartedAt are OPTIONAL supporting identity.

| Binding claims | Discovery reports | Correlation |
| --- | --- | --- |
| provider+version, no PID | any | ManagedLaunch (accepted weaker rule preserved) |
| PID (no start) | same PID | ManagedLaunch |
| PID (no start) | different / missing PID | Unavailable |
| PID + StartedAt | same PID + same start | ManagedLaunch |
| PID + StartedAt | same PID + **different** start (reuse) | **Unavailable** |
| PID + StartedAt | same PID + missing start | **Unavailable** (fail closed) |
| provider or version mismatch | — | Unavailable |
| nil binding | — | Unavailable |

Runtime identity NEVER selects agent status — it only gates correlation, which is
one precondition for accepting already-resolved evidence. Process name / CWD /
PTY / screen / prompt are not status authority (unchanged from S1).

Two-generation rule (`staleWrite` + `reconcileEpoch`): `LaunchGen` dominates,
`streamGen` breaks ties within a launch. `LaunchGen==0` (unspecified downgrade)
does not compete on the launch axis and never lowers the stored launch high-water,
so a current observer can downgrade a session whose binding vanished while a
delayed OLDER nonzero-launch write stays rejected.

`controlled_pty` exposes PID (daemon-owned child) + spawn StartedAt through the
EXISTING `mux.ProcessProvider` — no new interface, no ps/lsof scraping.
tmux/cmux/localpty/`NativeSession` are untouched.

---

## 5. Recovery/replay matrix (S1.1-C)

Per-(launch,stream)-epoch `WinnerHighWater` = highest winning Seq accepted in the
epoch; it SURVIVES a downgrade.

| Scenario | Result |
| --- | --- |
| newer Seq wins, then replay of older Seq (same epoch) | rejected; winner unchanged |
| repeated identical batch | idempotent |
| correlation loss → non-current | downgrade; high-water retained |
| after loss, re-deliver SAME pre-loss Seq | rejected (no recovery) |
| after loss, FRESH strictly-higher Seq | recovers positive status |
| loss then repeated incompatible poll | stays unknown+degraded |
| new stream generation (truncation/relink) | high-water reset; low Seq accepted |
| new launch generation (replacement) | high-water reset; low Seq accepted |
| bounded eviction then same-id reuse | no stale winner/high-water |
| Clear (delete) then recreate | no stale winner/high-water |
| daemon restart (fresh in-memory store) | absent until fresh correlated evidence |

No persistence was added; unknown-after-restart is the accepted safe result.

---

## 6. Requirement → production-path negative test matrix

| Requirement (handoff §4) | Production owner | Test |
| --- | --- | --- |
| A: stronger evidence winner identified | `AgentStatusStore.Update`→`locateWinningSeq` | `TestS11A_WinnerSeqIdentifiesExactWinner`, `_StrongerEvidenceUpdatesWinnerSeq` |
| A: equal precedence latest-Seq | same | `TestS11A_EqualPrecedenceBindsLatestSeq` |
| A: order/one-shot/incremental invariance | same | `TestS11A_OrderIndependentWinner` |
| A: cross-session/unknown never win | `filterSessionEvents`/`buildStatusEvidence` | `TestS11A_CrossSessionAndUnknownNeverWin` |
| A: degraded/error/version-conflict no winner | `Update`/`Revoke` | `TestS11A_DegradedAndRevokeCarryNoWinner` |
| A: API/mobile golden bytes unchanged | `AgentActivityDTO` marshal | `TestS11A_DTOGoldenUnchanged_NoWinnerLeak`, `TestS1E_DTOGoldenShape` |
| A: winner preserved on production path | `TelemetryService.processSession` | `TestS11A_ProductionPathPreservesWinner` |
| B: two launches strictly increasing gens | `transcript.RegisterLaunch` | `TestS11B_ReRegisterIsMonotonicAndReplaces` |
| B: binding replacement revokes before new evidence | `create.go`→`InvalidateForLaunch`; store rule | `TestS11B_LaunchReplacementRejectsLateOldLaunchWrite`, `_CreateRegisterReplaceWiring` |
| B: same PID+different start rejected | `LaunchCorrelation` | `TestS11B_CorrelationPIDStartRules`, `_ProductionCorrelationUsesRuntimeIdentity` |
| B: different PID/provider/version rejected | `LaunchCorrelation` | `TestS11B_CorrelationPIDStartRules` |
| B: missing required start fails closed | `LaunchCorrelation` | `TestS11B_CorrelationPIDStartRules` |
| B: delete→recreate cannot inherit gen | `RegisterLaunch` counter | `TestS11B_DeleteRecreateHigherGeneration` |
| B: replacement of A cannot affect B | `AgentStatusStore` | `TestS11B_ReplacementIsolatedPerSession` |
| B: matching identity remains correlated | production path | `TestS11B_ProductionCorrelationUsesRuntimeIdentity` (matching subtest) |
| B: controlled_pty exposes PID+start | `controlledPTYSession.ProcessInfo` | `TestS11B_ControlledPTYExposesProcessIdentity` |
| C: newest Seq wins then older replay rejected | `reconcileEpoch` | `TestS11C_ReplayOfOlderSeqRejected` |
| C: cursor rewind/repeat no regression | `reconcileEpoch` | `TestS11C_RepeatedBatchIdempotent` |
| C: loss → non-current → fresh recovery | `RevokeIfPresent`/`Update` | `TestS11C_CorrelationLossThenFreshRecovery` |
| C: loss then incompatible stays non-current | `RevokeIfPresent` | `TestS11C_LossThenIncompatibleStaysNonCurrent` |
| C: eviction+reuse no old evidence | `store`/`evictOldestLocked` | `TestS11C_EvictionThenReuseNoStaleEvidence` |
| C: Clear+recreate no old evidence | `Clear`/`Update` | `TestS11C_ClearDropsAllEvidence` |
| C: daemon restart begins absent | fresh `TelemetryService` + `processSession` | `TestS11C_RestartBeginsAbsentUntilFreshEvidence` |
| C: race under update/replace/clear/recreate | `AgentStatusStore` | `TestS11C_ConcurrentEpochChurnRace` (`-race`), `TestS1E_StatusStoreRace` |
| C: S1 golden regression | `AgentActivityDTO` | `TestS1E_DTOGoldenShape` |

All accepted S1 tests (`TestS1C_*`, `TestS1E_*`, `TestAgentStatusStore_*`,
`TestProduction_*`) remain PASS with their signatures updated for the added
`LaunchGen` axis and `time.Time` start argument (semantics preserved; `launchGen=0`
in accepted tests falls through to the original streamGen-only rule).

---

## 7. Frozen-contract + DTO confirmation

- `internal/agent/contract/` — **unchanged** (`git diff 24328ae..HEAD` empty).
- `internal/term/telemetry.go` (home of `AgentActivityDTO`) — **unchanged**.
- `mobile/` — **unchanged**.
- Accepted T1/T2 adapter `GetStatus` semantics — **unchanged** (delegate to frozen
  `ResolveStatus`; A only reads their output, never alters the winner).
- `waiting_approval` remains display-only; no ApprovalStore entry / CTA / action
  was created.
- No new public status/provenance/degraded vocabulary; all new fields
  (`LaunchGen`, `WinningSeq`, `WinnerHighWater`) are internal to
  `AgentActivityRecord` and never serialized.

---

## 8. Commands, counts, skips, cleanup

Final gate on `715c22b`:

```sh
cd companion-daemon
gofmt -l (my 17 touched files)        # clean (pre-existing non-S1.1 files still flagged; unchanged by me)
go build ./...                        # OK
go vet ./...                          # OK
go test -race ./internal/agent/... ./internal/term/... \
     ./internal/transcript/... ./internal/mux/... -count=1   # PASS (all 8 packages)
go test ./cmd/... -count=1            # PASS (composition root: create/register/invalidate wiring)
git diff --check                      # clean
sh scripts/build-gate.sh             # Backend OK; Mobile typecheck OK; Mobile jest OK; Android Kotlin FAILED (gradlew absent — M-track)
```

- Focused test counts: S1.1-A 9 tests, S1.1-B 4 (transcript) + 4 (term, one with
  2 subtests), S1.1-C 11 tests; all PASS. Accepted S1 suites PASS.
- **Mobile gate**: `npm run typecheck` **OK** and `npm test` **OK** via
  `build-gate.sh` (no `mobile/` file changed and the public DTO is byte-identical,
  so the accepted S1 mobile validation/golden remains valid and green).
- **Android Kotlin compile**: environment-blocked — `mobile/android/gradlew` is
  absent in this environment (same limitation the accepted S1 report recorded).
  S1.1 changes NO native/product-device path, so this is not an S1.1 regression.
  **M-track.**
- **Physical device smoke**: not run; no native path changed by S1.1. **M-track.**
- Cleanup: only Go build/test caches used; no stage worktrees or generated
  artifacts retained.

---

## 9. Remediation after independent REJECT (verification `07a4bb3`)

The first S1.1 submission (`715c22b`) was independently REJECTed with three
focused correctness blockers
(`docs/S1_1_RUNTIME_STATUS_HARDENING_VERIFICATION.md`). Each is fixed below in a
narrow commit with a matching production negative test; frozen `ResolveStatus`,
the public DTO, T0/T1/T2, and mobile remain unchanged.

| Blocker | Commit | Fix | Key test(s) |
| --- | --- | --- | --- |
| R1 — `WinningSeq` ignored confidence, so it could bind a non-winner when two same-(status,provenance) candidates differ in confidence | `e1651f0` | `locateWinningSeq` also matches normalized confidence via `normConfidence` (same clamp01 the contract applies before selection); no re-derived precedence | `TestS11A_LowerConfidenceLaterEventDoesNotWin` (reviewer counterexample), `_HigherConfidenceLaterEventWins`, `_OutOfRangeConfidenceClampedWinnerMatched`, `_UnequalConfidenceOneShotEqualsIncremental` |
| R2 — `LaunchBinding.Adapter` stored but never verified in correlation | `3108e5e` | `LaunchCorrelation` takes `discoveredAdapter` and requires a non-empty exact match; `processSession` passes the runtime's actual `sess.AdapterName()` | `TestS11B_CorrelationPIDStartRules/{adapter mismatch,empty discovered adapter}`, `_EmptyBindingAdapterFailsClosed`, `_ProductionAdapterMismatchNoStatus` |
| R3 — replacement published the binding before invalidation, leaving a concurrent-poll window (and a stream-stale-write that could reject the later invalidation) | `ef47b55` | one production-owned boundary `TelemetryService.RegisterOrReplaceLaunch`: reserve gen → invalidate old status+ingestion at gen → publish; registry split into `ReserveLaunchGeneration` + `PublishLaunch` | `TestS11R3_InvalidationPrecedesPublish`, `_ConcurrentPollReplacementRace` (`-race`), `_ReplacementIsolatedPerSession`, `_BoundaryMonotonicAcrossDeleteRecreate` |

Remediation baseline: `07a4bb3` (reviewer REJECT doc, docs-only). Remediation
final implementation SHA: `ef47b551e6c9fdb141676b35c0731be8c0e35d14`.

### 9.1 Verified non-blocking product limitation (documented per handoff §5)

The local CLI `pokit run claude` / `pokit run codex` path
(`cmd/devremote/client.go:runClient` → local socket → `createLocalControlled`)
still joins arguments into one command string and executes through the legacy
`bash -c` branch, and does NOT register a recognized launch binding. Therefore:

- `pokit run claude/codex` currently provides **managed lifecycle**, not
  recognized managed-agent authority;
- the recognized PID/StartedAt/LaunchGen correlation path implemented here is the
  accepted **preset HTTP profile** create path (`createFromProfile`);
- arbitrary local commands correctly receive no supported-agent semantic authority.

This is NOT one of the three remediation blockers and does not expand the A/B/C
scope. It is recorded as an O1-readiness boundary: do not describe the current
local CLI path as `recognized managed-agent` or `orchestration-certified`.
Changing the `pokit run` protocol is explicitly deferred.

---

REVIEW REQUEST: S1.1 Runtime Status Hardening remediation — ef47b551e6c9fdb141676b35c0731be8c0e35d14
