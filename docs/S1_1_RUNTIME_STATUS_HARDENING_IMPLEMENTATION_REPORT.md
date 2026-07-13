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
| R3 (first attempt — later REJECTed, see §9.2) | `ef47b55` | `RegisterOrReplaceLaunch` composed `ReserveLaunchGeneration` + a separate invalidate + `PublishLaunch` — but the three steps were separately locked, so concurrent replacements could publish a lower generation last, and the test used a non-deterministic `select/default` that masked it | superseded by §9.2 |

Remediation baseline: `07a4bb3` (reviewer REJECT doc, docs-only). First-remediation
implementation SHA: `ef47b551e6c9fdb141676b35c0731be8c0e35d14` (R1/R2 accepted; R3 rejected).

### 9.2 R3 remediation 2 — one serialized registry transaction (re-verification `5896603`)

The first R3 attempt was independently REJECTed
(`docs/S1_1_RUNTIME_STATUS_HARDENING_REVERIFICATION.md`): reserve / invalidate /
publish were three separately-locked steps, so two concurrent replacements could
interleave and let a lower reserved generation publish AFTER a higher one
(launch identity regression), and two concurrent first registrations could both
skip invalidation. `PublishLaunch` also did not reject a stale generation. The
concurrency test recorded `lastGen` but never asserted it and let a final
replacement mask any intermediate regression.

Final fix — one registry-owned, serialized per-session transition
`LaunchRegistry.RegisterOrReplace(spec, invalidate)` (commit at the marker SHA):

```text
gate := per-session transition mutex        // different sessions stay concurrent
gate.Lock()
  r.mu.Lock();  nextGen++; gen := nextGen; existed := bindings[id] present;  r.mu.Unlock()
  if existed { invalidate(gen) }            // pre-publication high-water, NO r.mu held
  r.mu.Lock()
    if cur present && gen <= cur.Generation { return cur.Generation }  // strict monotonic: never regress
    bindings[id] = {…, Generation: gen}     // publish
  r.mu.Unlock()
gate.Unlock()
```

- The whole reserve→invalidate→publish transition is serialized per session by the
  gate, so the "lower generation publishes last" window cannot exist.
- Publication rejects any `gen <= currentPublished` — a stale transition never
  regresses the binding.
- Two concurrent first registrations are serialized by the gate; the second
  observes the first's binding and takes the replacement (invalidating) path, so
  they cannot both skip invalidation.
- The invalidation callback runs while the gate is held but the registry map mutex
  is NOT — the audited lock order forbids deadlock (see below).
- The unsafe split API (`ReserveLaunchGeneration`/`PublishLaunch`) is removed;
  `RegisterLaunch` now routes through the same transition with a nil callback
  (first-registration/test only) and the strict monotonic check still applies, so
  no path can publish a replacement without the transition. Lifecycle `Clear`
  (delete) remains a distinct full removal.

**Lock-order proof (no inverse acquisition, no deadlock).** Audited mutation paths
and the locks each takes, in order:

| Path | Lock order | Holds a lock across a registry call? |
| --- | --- | --- |
| poll `processSession` | `telemetry.s.mu` (acquired then RELEASED) → `LookupLaunch`(`registry.mu`) → `statusStore.mu` | No — released before registry/store calls; sequential |
| `RegisterOrReplace` transition | `gate` → [`registry.mu` acquire/release] → `invalidate` cb → [`registry.mu` acquire/release] | callback runs with NO `registry.mu` held |
| `invalidate` cb (`invalidateForLaunch`) | `telemetry.s.mu` (acquire/release) → `statusStore.mu` (acquire/release) | No |
| `statusStore.*` | `statusStore.mu` only (leaf) | No |
| launch registry Lookup/Remove | `registry.mu` only (leaf — never calls into term/status) | N/A |

The gate is acquired ONLY by transitions; the poll and the status store never
acquire it, and the registry map mutex is never held across a term/status call.
So the global order is `gate → {registry.mu | telemetry.s.mu | statusStore.mu}`
with no cycle. No path takes a status/adapter lock before the launch registry.

Deterministic tests (non-vacuous — verified by a negative control that disables
the gate and observes the serialization test FAIL with the reviewer's exact
"B published gen 3 while A held the gate"):

- `transcript.TestS11R3_Registry_GateSerializesReserveThenPublish` — a
  `launchGateWaitHook` seam holds transition A inside its invalidation callback
  (gate held, generation allocated) and proves transition B, on arriving at the
  gate, has NOT allocated its generation and cannot publish until A completes;
- `transcript.TestS11R3_Registry_StrictMonotonicPublishRejectsStale` — a lower
  generation cannot overwrite a higher published one;
- `transcript.TestS11R3_Registry_ConcurrentReplacementsMonotonic` — snapshots the
  binding after every completion; it never drops below a just-published generation;
- term `TestS11R3_LookupNeverAheadOfHighWater`, `_ConcurrentFirstRegistrationsSerialized`,
  `_DifferentSessionsConcurrent`, `_DeleteRecreateMonotonic`, `_HeavyConcurrentRace`
  (`-race`) — production-boundary wiring, isolation, monotonicity, and race safety.

R3-2 baseline: `5896603` (re-verification doc, docs-only). R3-2 implementation
SHA: `ff4f61affc17b0f60b6fc67c8f41c2605e02ed1f` (serialization accepted; two cleanup
blockers remained).

### 9.3 R3 cleanup — bounded gates + non-bypassable replacement (re-verification-2 `e396a11`)

R3-2's serialization algorithm was accepted; two cleanup blockers remained
(`docs/S1_1_RUNTIME_STATUS_HARDENING_REVERIFICATION_2.md`):

**C1 — unbounded per-session gate map.** The transition gate was a
`map[string]*sync.Mutex` that grew one entry per historical session ID forever
(`RemoveLaunch` deleted the binding but never the gate). Fix: a FIXED-SIZE striped
lock array `gates [launchGateStripes]sync.Mutex` (256 stripes) selected by an
FNV-1a hash of the canonical session ID. The synchronization state is now
constant regardless of how many sessions are created/deleted; stripe collisions
only serialize unrelated sessions (never weaken correctness), so no
gate-retirement/ABA protocol is needed. The stripe pointer is stable for the
process lifetime, so a waiter holds it safely. Same session ID → same stripe, so
its transitions and `RemoveLaunch` stay mutually serialized.

**C2 — nil-invalidation replacement bypass.** `RegisterLaunch(spec)` routed to
`RegisterOrReplace(spec, nil)` and could replace a live binding with no
invalidation; `createFromProfile` used it when `Handlers.Telemetry == nil`. Fix:
`RegisterOrReplace` now returns `(published, replaced, ok)` and FAILS CLOSED on a
replacement with a nil callback (existing binding untouched, `ok=false`) — a nil
callback is never permission to replace. The exported API is split:
`RegisterFirstLaunch` (first-registration-only; fails closed if a binding exists)
and `RegisterOrReplaceLaunch(spec, invalidate)` (the term boundary supplies the
callback). `createFromProfile` uses the telemetry boundary in production and
`RegisterFirstLaunch` (fail-closed) only when telemetry is absent — it never takes
a weaker replacement path. Tests use `RegisterFirstLaunch` / an explicit callback,
not a production bypass.

Negative tests (non-vacuous — a negative control that removes the C2 guard makes
`TestS11R3_C2_NilInvalidationReplacementRefused` FAIL):

- `TestS11R3_C1_UniqueChurnBoundedSyncState` — 5000 unique register/remove cycles
  leave bindings at 0 and the stripe set at its constant size;
- `TestS11R3_C1_StripeCollisionStillCorrect` — two IDs colliding on one stripe both
  register/remove correctly;
- `TestS11R3_C2_NilInvalidationReplacementRefused` — a nil-callback replacement
  leaves the binding unchanged and reports `ok=false`;
- `TestS11R3_C2_FirstLaunchFailsClosedOnExisting` — `RegisterFirstLaunch` refuses to
  replace an existing binding;
- `TestS11R3_C2_CallbackReplacementInvalidatesBeforePublish` — a normal
  callback-backed replacement still invalidates (callback sees the OLD binding)
  before publishing the new generation.

All prior R1/R2/R3 tests remain green; frozen `ResolveStatus`, public DTO, T0/T1/T2,
and mobile remain unchanged.

R3-cleanup baseline: `e396a11` (re-verification-2 doc, docs-only). Final
implementation SHA: `02c8385e3270fbbc4df45e0c71ccad6ebe11a076`.



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

REVIEW REQUEST: S1.1 Runtime Status Hardening remediation — 02c8385e3270fbbc4df45e0c71ccad6ebe11a076
