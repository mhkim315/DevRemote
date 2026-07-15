# SP0 Native Managed Runtime — Evidence Report

Status: **SP0 P1–P3 + R1 + R2 remediation DELIVERED — resubmitted for
independent review. SP0.5 / SP1 / Cleanup / A1.1 provider-positive delivery /
N1 / O1 / O2 NOT started. Approval capacity ZERO; `provenActionMapping` empty;
frozen A1 contracts untouched.**

Date: 2026-07-15. Executor: Claude Code, canonical checkout
`/Users/mhk/Documents/codex/DevRemote`, branch `feature/phase10-multi-adapter`.

- Start remote HEAD: `1fc0e5a3e2055dca0944d13d5e02118c35ce2977` (handoff commit)
- Accepted-evidence ancestry `42429c8acd4c2840a93e1ab11c53f8e21c34bc19`: PASS
- Contract note: `docs/SP0_NATIVE_RUNTIME_CONTRACT_NOTE.md` (`e8c97c2`)
- Implementation commits: `73b9a5b` (P1), `2b04033` (P2), `654ee8c` (P3),
  `8dcd876` (R1), `2b35f524dc339c24d7c3d5ee4284c967300836c3` (R2 — final)
- Authoritative handoff: `docs/NEXT_EXECUTOR_SP0_NATIVE_RUNTIME_HANDOFF.md`

## 1. Certified production path (handoff §1)

```text
pokit run --detach codex
  -> structured IPC create {operation:create, profileId:codex, detach:true}   [production-wired]
  -> fail-closed pinned Verify (version + realpath + shim/native sha256)      [production-wired]
  -> direct spawn <pinned>/.bin/codex ["app-server","--stdio"], own pgroup    [production-wired]
  -> initialize/initialized/thread/start handshake, bounded watchdog          [production-wired]
  -> canonical codex_app_server:<local> in owned ManagedSessionRegistry       [production-wired]
  -> single server-derived bounded certification turn (fixed constant)        [production-wired]
  -> production event pump: turn/started -> working, turn/completed -> completed [production-wired]
  -> authenticated REST GET /api/sessions + /api/sessions/{id}/native-status  [production-wired]
```

Interactive (non-detach) `pokit run codex` is NOT claimed: terminal attachment
is SP0.5. The managed path has no PTY, no Recorder, no shell.

## 2. Packet P1 — structured launch and owned child

| Requirement | Production code | Proof (test) | Level |
| --- | --- | --- | --- |
| Structured recognized-profile request, no command string | `buildRunCreateRequest` (client.go) | `TestBuildRunCreateRequest_DetachCodexStructured` asserts profileId form + absent `command` key; multi-token/non-detach stay legacy | focused proof |
| Conflicting profile/command/executable fail closed | `validateLaunchInputExclusivity` at IPC handler AND `localCreateSpec.toOptions` (deepest boundary) | `TestManagedIPCCreate_ConflictingInputs_FailClosedBeforeSpawn` — 3 combos, launcher invoked 0 times, registry empty; direct toOptions rejection | contract proof |
| Direct certified exec, exact argv, no shell | `ManagedCodexService.CreateDetached` → `execLauncher` | `TestManagedIPCCreate_StructuredRequest_ExactArgv` — real IPC handler + injected launcher records exe==pinned bin, argv==`[app-server --stdio]`, shell-token scan | contract proof |
| Runtime owns stdin/stdout/wait/reap/framing | `codexManagedRuntime` (send/readNext/pump/stop); `execProcess` pipes + `waitOnce` | pump/rollback tests exercise the owning runtime; no other stdio reader exists (no PTY/Recorder path) | focused proof |
| Initialize failure → kill+reap, no visible session | rollback in `CreateDetached`/`handshake` watchdog | `TestManagedCreate_FailedInitialize_KillsChildNoSession`, `TestManagedCreate_HandshakeDeadline_KillsChildNoSession`, `TestManagedCreate_FailedVerify_NoChildNoSession` | contract proof |
| Constructed by the real composition root when enabled | `NewAppWithDeps` + `Config.EnableManagedCodex` (default false), `--enable-managed-codex` | `TestNewAppWithDeps_ManagedCodexFlag` (on→non-nil, off→nil) | focused proof |

## 3. Packet P2 — owned registry and native status

`ManagedSessionRegistry` (internal/term/managed_registry.go): Register / Get /
List / UpdateNativeStatus(sessionID, epoch, status) / MarkExited(sessionID,
epoch) / Remove / Close. One mutex, value-copy reads, capacity 4 fail-closed,
single epoch per launch, closed vocabulary {idle, working, completed, exited}.
No dependency on `mux.Registry`, discovery, snapshots, screen, JSONL, telemetry.

| Requirement | Proof | Level |
| --- | --- | --- |
| register/get/list defensive copies | `TestManagedRegistry_RegisterGetList_DefensiveCopies` (mutating returned copies never changes state) | contract proof |
| duplicate/capacity fail without replacing | `TestManagedRegistry_DuplicateAndCapacity_FailWithoutReplacing` (exact before/after record equality) | contract proof |
| ordered native events update status | `TestManagedRegistry_OrderedNativeEventsUpdateStatus`; production pump: `TestManagedPump_NativeEventsDriveWorkingThenCompleted` | contract proof |
| old/closed epoch rejected | `TestManagedRegistry_StaleClosedEpochRejected` (stale/future epoch, unknown status, direct-exited injection, post-exit, post-close) | contract proof |
| child exit → non-current/exited | `TestManagedRegistry_ChildExitMarksNonCurrent`; pump-level `TestManagedPump_ChildExitMarksExited_AndLateEventsInert` | contract proof |
| race register/update/close/snapshot | `TestManagedRegistry_ConcurrentAccess` under `-race` (exited never regresses; duplicate never replaces) | contract proof |
| unknown events cannot fabricate status | pump consumes crafted `thread/status/changed`, `managed/status`, wrong-thread `turn/completed` — consumption PROVEN via a narrow pump observer seam, status unchanged (non-vacuous) | contract proof |

Not implemented (per handoff): reconnect, persistence, restart recovery,
general replacement, multi-provider dispatch.

## 4. Packet P3 — authenticated REST proof and observer isolation

| Requirement (§8) | Evidence | Level |
| --- | --- | --- |
| 1. Authenticated REST lists + retrieves the managed session | `TestManagedREST_ListAndGet_FromOwnedRegistry`; auth wiring `TestManagedNativeStatusRoute_Authenticated` (remote anonymous → 401; local dev token → handler) | contract proof |
| 2. Bounded real prompt → observable working → completed | LIVE (below) | physical/live proof |
| 3. Production event pump causes the transitions | pump is the only writer on the live path; deterministic pump tests + live ordered observation | contract + live |
| 4. Contradictory screen/PTY/JSONL/telemetry cannot overwrite | `TestManagedREST_SpoofedDiscoveryCannotOverwrite` (same-ID spoofed row dropped, owned status served, known-bad control included); structurally no write API exists from telemetry/screen/JSONL into the owned registry | contract proof |
| 5. Failing tmux/cmux refresh does not affect managed path | `TestManagedREST_FailingDiscoveryIsolation` (list/get/status/create with erroring adapters) | contract proof |
| 6. Bounded DTOs | `TestManagedREST_DTOBounded` — exact field set {id, provider, version, nativeStatus, launchGen, createdAt, statusChangedAt, exited}; leak scan (prompt text, thread/turn ids, opaque process id, paths) | contract proof |

### Live production proof (ONE pinned turn, executed once)

`POKIT_SP0_LIVE=1 go test ./cmd/devremote -run TestSP0_LiveProductionProof -v`
— full log retained by the executor session; key lines:

```text
SP0-LIVE created managed session codex_app_server:codex-app-1784084632577214000 at 2026-07-15T03:03:52.577379Z
SP0-LIVE authenticated list contains codex_app_server:codex-app-1784084632577214000
SP0-LIVE native-status → working (launchGen=1 exited=false)
SP0-LIVE native-status → completed (launchGen=1 exited=false)
SP0-LIVE ordered transitions: [working@2026-07-15T03:03:52.604844Z completed@2026-07-15T03:03:54.6138Z]
SP0-LIVE managed shutdown clean at 2026-07-15T03:03:54.616413Z
--- PASS: TestSP0_LiveProductionProof (3.44s)
```

The test drives the EXACT structured CLI request bytes over a real 0600 unix
socket through the production `StartIPCServer`, spawns the pinned 0.144.1
app-server via the production launcher, and observes ordered `working →
completed` ONLY through the authenticated REST read path. Post-run process
scan: no orphan pinned/app-server processes.

## 5. Minimal cleanup (handoff §9)

- creation rollback kills + reaps: P1 rollback tests;
- child exit closes the pump and makes status non-current: P2 pump tests;
- daemon shutdown stops/reaps owned children with a bounded fallback:
  `App.Shutdown` → `ManagedCodexService.Shutdown(ctx)`;
  `TestManagedShutdown_KillsChildrenAndClosesRegistry` + live clean shutdown;
- shutdown/close prevents later events from updating state: registry `Close()`
  ordering proven in the same test.

## 6. Scaffold disposition (handoff §2)

The unaccepted CP0 entry-path scaffold (`CodexAppServerRuntime` /
`CodexEntryState` / `StartCertificationTurn` with nil stdio, `Replace`, `Ref`,
`entryPathRecord`) was REPLACED, not annotated: it could not own stdio and was
never wired. Retained (evidence-aligned): `CodexAppServerEntryConfig`,
`PinnedConfig0x144`, fail-closed `Verify()` — extended with the native-artifact
digest (streaming) and an explicit realpath error path. The frozen
`RuntimeDeliveryGate` tests from the prior packet
(`codex_appserver_runtime_test.go`) are untouched and still green.

## 7. Binding-table audit (vs contract note)

All rows implemented as specified. One deviation: OS/Arch are stored in the
record (per contract) but deliberately EXCLUDED from the REST DTO (contract
listed only the storage; SP0 REST needs no platform metadata — narrower than
planned, fail-safe direction). ProcessID never appears in any DTO or log.

## 8. Honest scope and evidence-level disclosure

- **production-wired**: everything in §1's pipeline, both REST routes, both
  auth modes, composition flag, shutdown ordering.
- **test-only**: fake launcher/app-server scripts used for all failure,
  adversarial, and concurrency coverage (per handoff: deterministic fakes for
  failure coverage; one real turn for the production proof).
- **live (once)**: the §4 production proof; it is the only model-spending run.
- **not-run / deferred**: interactive attach, resize, Stop/Kill/Delete REST for
  managed sessions, reconnect/recovery, approvals (SP1), tmux/cmux removal,
  Claude/Windows, process-image attestation beyond pinned digests (explicitly
  NOT claimed as attestation).
- The IPC `StartIPCServer` signature gained a trailing managed parameter; all
  existing callers updated mechanically (nil = disabled), no behavior change
  for non-managed paths (full term/cmd race suites green).
- `certificationPrompt` is a fixed daemon-owned constant; no user input can
  reach `turn/start` in SP0. If a provider ever emitted `requestApproval` on
  this path, SP0 records nothing actionable and never responds (capacity zero).

## 9. Remediation round R1 (reviewer blockers, all fixed)

Reviewer verdict on `8ecbb70`: REJECT with three production-path blockers.
All fixed in implementation commit
`8dcd876b4d4274005c0aee2c10dca145dbcd7953` (`fix(SP0-R1)`):

1. **Silent legacy fallback when managed disabled** — the structured
   `profileId=codex && detach` request now fails closed with an explicit
   `managed codex runtime unavailable` error when the service is nil
   (`internal/term/ipc.go`): no child, no session, no launcher, never the
   controlled_pty authority model. Paired production IPC tests:
   `TestManagedIPCCreate_DisabledFailsClosed_NoLegacyFallback` (exact error,
   empty response id, empty mux registry) and
   `TestManagedIPCCreate_EnabledUsesManagedRuntime`.
2. **CreateDetached vs Shutdown not linearized** — single closing transition:
   `s.mu` guarding `{closing, runtimes}` is the one linearization point. The
   spawned child is published into the runtime map in the SAME critical
   section that re-checks `closing`, BEFORE any post-spawn I/O — so either
   Shutdown's snapshot owns the child (kills + reaps it) or the create
   observes `closing` and rolls back its own child. New creates after
   shutdown fail closed before verify/spawn. No lock is held across external
   I/O. `App.Shutdown` ordering fixed: IPC stops accepting (step 5) BEFORE
   managed shutdown (step 6); an in-flight handler past accept is covered by
   the closing state. Deterministic barrier tests (narrow `createBarrier`
   seam, nil in production): `TestManagedCreate_ShutdownRace_PostSpawn`
   (unpublished window → create rolls back own child),
   `TestManagedCreate_ShutdownRace_PostRegister` (published window →
   shutdown snapshot owns the child; resumed create fails and removes the
   record), `TestManagedCreate_AfterShutdown_FailsBeforeSpawn` (launcher
   never invoked). Non-vacuous: under the previous ordering the post-spawn
   test returns a register-time error instead of the fail-closed message and
   the post-register test's child is absent from the shutdown snapshot.
3. **REST list dependent on observer discovery** — new
   `GET /api/managed-sessions` served ENTIRELY from the owned registry
   (`HandleManagedSessions`), wired in both auth modes with sessions:read.
   It never touches `mux.Registry`, discovery, or telemetry. This is the
   SP0-certified managed list; managed rows appended to `/api/sessions`
   remain a coexistence convenience sharing the legacy snapshot's
   availability. Negative test `TestManagedREST_BoundedWhileDiscoveryHangs`:
   tmux/cmux `ListSessions` BLOCKED on a barrier (hang, not error) while a
   concurrent observer call occupies discovery — managed list/get/create all
   complete within the bound, and a later discovery success does not change
   the managed status.

The R1 live Codex turn was NOT re-run (reviewer direction: avoid cost and
duplication); the §4 live proof remains the recorded production-positive
evidence and the remediation did not alter the pinned launch pipeline's
happy path (P1/P2/P3 focused suites re-run green).

## 9a. Remediation round R2 (remaining blocker, fixed)

Reviewer verdict on `98f2ca5`: REJECT, one remaining blocker — Shutdown could
return while a spawned-but-unpublished child was still alive (the R1 test even
released the create barrier only AFTER Shutdown returned). Fixed in
implementation commit `2b35f524dc339c24d7c3d5ee4284c967300836c3`
(`fix(SP0-R2)`):

- **Service-owned in-flight lease** (`inflightCreate`) registered BEFORE
  verify/spawn; every create exit path releases it. The spawned child is
  handed to the lease before the post-spawn window, so Shutdown's lease
  cancellation kills it directly — verify/launch/handshake receive shutdown
  cancellation without depending on the create goroutine resuming or on the
  30s handshake watchdog.
- **Shutdown drains**: one closing transition under `s.mu` (closing + lease
  snapshot + runtime snapshot), registry closed, every lease cancelled, every
  published child killed, then a ctx-bounded wait until every published child
  is reaped AND the lease set is empty. A nil Shutdown return now guarantees:
  no POKIT-spawned child alive, zero in-flight creates, empty runtime map,
  closed registry (asserted by `assertDrainedShutdownState`).
- **Tests in the reviewer-required order**:
  `TestManagedCreate_ShutdownRace_PostSpawn` and `_PostRegister` now park the
  create at the barrier, call Shutdown, assert the child is killed AND
  Shutdown has NOT returned, resume the create, verify the rollback, and only
  then observe Shutdown return clean.
  `TestManagedShutdown_BoundedWhenCreateNeverResumes`: a pathologically stuck
  create cannot block Shutdown past its ctx — Shutdown returns an honest
  drain error and the child is still killed.
- Non-vacuous: under the R1 code, both race tests fail at "shutdown returned
  while a create was still in flight".

The live Codex turn was again NOT re-run (unchanged happy-path pipeline; §4
live proof remains the production-positive evidence).

## 10. Final gate (frozen remediation HEAD)

Run once on frozen implementation HEAD
`2b35f524dc339c24d7c3d5ee4284c967300836c3` via `sh scripts/build-gate.sh`:

```text
--- Backend ---  go build OK / go vet OK / go test -race OK / git diff --check OK
--- Mobile ---   npm run typecheck OK / npm test OK / android kotlin compile OK
--- Invariants --- vendor branch scan OK / ID inference scan OK
--- Security --- secret scan OK
=== ALL GATES PASSED ===
```

This report commit is docs-only on top of that verified tree.

## 11. Review marker

```text
REVIEW REQUEST: SP0 Native Managed Runtime — 2b35f524dc339c24d7c3d5ee4284c967300836c3
```
