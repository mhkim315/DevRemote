# S1 Runtime Status — Implementation Report

Phase: **S1 — Rich Agent Runtime Status Model**
Branch: `feature/phase10-multi-adapter`
Code baseline: `9ad6f834f70e88b800e60124c8e408d38bca9d2b` (accepted T3/S1 baseline)
Doc baseline: `bbf2fdccf5c369e92fcff11bda6fe04f0697fe06` (this report is authored on top of it)
Authoritative execution contract: `docs/NEXT_SESSION_S1_RUNTIME_STATUS_HANDOFF.md`

This report is the **S1-A** deliverable: a frozen status-path audit, an authority matrix,
the additive DTO design, an AgentEvent→StatusEvidence transition table, and a rollback plan.
No runtime behavior is changed in S1-A.

---

## 0. Baseline recovery record (transparency)

A prior execution operated on a **stale local checkout** (remote-tracking ref at `9ad6f83`
while canonical had advanced to `bbf2fdc`), did not `git fetch` first, and wrongly concluded
this handoff was "lost". It then implemented a superficial `runtimeStatus` envelope that
**packaged the legacy PTY heuristic (`AgentStatus = mapLegacyState(sd.State)`) as the agent
`activity`** — a direct violation of handoff §165/§385. That work was never on canonical; it is
preserved only in the local backup branch `backup/s1b-attempt-2cec83c` and is **abandoned**.
Recovery: fetched origin, `reset --hard` local branch to canonical `bbf2fdc`, verified all
accepted ancestry (`9ad6f83`, D1 `8f7c22de`, R2 `6f940b03`, T2 `ef4a162c`, T1 `162266f8`,
T0 `3ce2604b`) are ancestors of HEAD, worktree clean, local == remote. S1 restarts here at
S1-A per the authoritative handoff.

---

## 1. Status-field audit — producer / consumer / authority

`SessionTelemetry` (`companion-daemon/internal/term/telemetry.go:18-45`) carries every
status-like field. None of the following may be renamed or cross-copied (handoff §146-147).

| Field (JSON) | Producer (file:line) | Write authority | Consumer | Dimension |
| --- | --- | --- | --- | --- |
| `state` | `evaluateState()` `telemetry.go:186-254`, called `telemetry_service.go:312` | **PTY/screen heuristic + legacy parser last-event** (non-authoritative) | mobile `AgentCard` color/anim, `DashboardScreen` grouping | agent activity (heuristic) |
| `lifecycleState` | Catalog `Add/beginStop/requestKill/finalize` `catalog.go:39-135`; merged `mergeLifecycleState` `telemetry.go:55-95` | **daemon-authoritative** (Session Catalog / LifecycleService) | mobile `FeedScreen` → `reconcileState`/`computeActionPolicy` (Stop/Kill/Delete) | daemon lifecycle |
| `agentStatus` | prod: `mapLegacyState(sd.State)` `telemetry_service.go:402-404`; fallback: `DetectAgent` `telemetry.go:401-406` | **derived from `state` heuristic** (prod) or detector (fallback) — NOT the T0 contract | mobile `AgentCard` → `formatAgentStatus` label | agent activity (heuristic today) |
| `agentKind` | `TermAgentDetector.DetectAgent` `bridge.go:26-47` | best-of-three detector confidence | `AgentCard`/`isDegraded` | detection metadata |
| `agentConfidence` | `TermAgentDetector.DetectAgent` `bridge.go:26-47` | detector confidence | `isDegraded` (`<0.5`) | detection metadata |
| `stale` | `snap.LastError != nil` `telemetry_service.go:357`, `telemetry.go:379` | **Registry adapter snapshot** (poll health) | serialized, not consumed by mobile | connectivity/health |
| `lastError` | `snap.LastError.Error()` `telemetry_service.go:354-356` | Registry adapter snapshot | not consumed by mobile | connectivity/health |
| `lastSuccessAt` | `snap.LastSuccessAt` `telemetry_service.go:357` | Registry adapter snapshot | not consumed by mobile | connectivity/health |
| `approvals` | `s.approvals.List` `telemetry_service.go:408-410`, populated by legacy parser `:224-246` | ApprovalStore | mobile `ApprovalCard` | approval action state (A1, not S1) |

**Key finding.** The accepted T1/T2 adapters run in production (`callAcceptedAdapter`
`telemetry_service.go:530-581`) but their output is projected **only to Transcript**
(`ProjectAgentEvents`), and their frozen `GetStatus` is **never called**. Production
`agentStatus` is the legacy heuristic renamed. Closing this — without letting the heuristic
masquerade as accepted authority — is the core of S1.

---

## 2. Frozen authority matrix (four independent dimensions — never collapsed)

| Dimension | Values | Authority | S1 role |
| --- | --- | --- | --- |
| daemon lifecycle | starting, running, stopping, exited, killed, failed | Session Catalog / LifecycleService (`lifecycle.go:28-39`) | **input to the response envelope only**, never an input to `GetStatus`; unchanged by S1 |
| agent activity | unknown, idle, thinking, working, waiting_input, waiting_approval, completed, failed, interrupted, degraded (`models.go:24-33`) | frozen T0 `ResolveStatus` over accepted `StatusEvidence` | **the new S1 product path** |
| connectivity/health | fresh, stale, adapter_error, unavailable | Registry/telemetry poll health (`stale`/`lastError`) | separate envelope field; never a status |
| approval action state | pending, approved, rejected, expired | A1/ApprovalStore | **out of scope for S1**; `waiting_approval` is display-only activity |

The word `failed` exists in both lifecycle and activity and MUST stay namespaced
(`lifecycle.state=failed` ≠ `activity.status=failed`).

---

## 3. Frozen T0 status resolution (must be reused verbatim — handoff §158-165)

Single entry point: `contract.ResolveStatus([]StatusEvidence) StatusResult`
(`internal/agent/contract/validate.go:272`). Both accepted adapters' `GetStatus`
(`claude/v2_1_202/claude_adapter.go:582`, `codex/v0_144_1/codex_adapter.go:524`) delegate to it
(empty evidence → `unknown` + `Degrade("no status evidence")`). Both advertise `CapStatus`.

- **Provenance rank** (`contract.go:70-101`): runtime 70 > provider_protocol 60 > provider_hook 50
  > native_log 40 > pty_structural 30 > heuristic 20 > prompt_hint 10 > unknown 0.
- **Advisory** = heuristic / prompt_hint / unknown (`contract.go:114-120`).
- **Terminal statuses** = completed / failed / interrupted (`validate.go:99-101`).
- **Advisory + terminal claim → downgraded** to `unknown`, `Degraded`, confidence ≤
  `AdvisoryStatusConfidenceCeiling = 0.5` (`validate.go:288-295`). Advisory + non-terminal →
  confidence capped at 0.5. All confidence `clamp01`ed.
- Unknown status/provenance evidence is **dropped**; empty → `unknown`/`ProvenanceUnknown`;
  ties break by rank then confidence then first-seen (`validate.go:304-309`).

S1 must not fork this vocabulary, ranking, ceiling, or downgrade.

---

## 4. Production read-path hook point (for S1-C, established here)

In `TelemetryService.processSession` (`telemetry_service.go:104`), the accepted-adapter path
(`:170-208`) runs only when `s.transcript != nil && isAcceptedAdapter(logRef.Agent)`
(codex/claude). `callAcceptedAdapter` returns `(events, discoveredVersion, nextCursor, degraded)`
with records tagged `ProvenanceNativeLog`. `adapterState` (`adapter_state.go`) enforces a bounded
full prefix (2000 records / 4 MiB), stream generation, exact accepted-version gating
(`isAcceptedVersion`), version-conflict, opaque cursor, and launch correlation.

**Hook point = `telemetry_service.go:205`**, inside
`if corr == contract.CorrelationManagedLaunch { if len(acceptedEvents) > 0 { … } }`.
At that line the events are simultaneously: accepted-adapter, session-bound (`SessionContext{id}`),
not-overflowed, version-gated (`versionValid`, no `versionConflict`), and correlation-gated
(`CorrelationManagedLaunch`). This is the only place S1 evidence may be sourced. Per-session state
lives in `TelemetryService.sessions map[string]*sessionStateData` under `s.mu`, cleared by
`Clear(sessionID)` (`:416-420`) on session removal — the natural home/lifecycle for the S1 store.

**No second reader.** S1 must reuse this exact batch/cursor/version/correlation; it must not open a
new log reader, cursor, poll goroutine, or correlation implementation (handoff §245-249, §434).

---

## 5. Versioned, additive DTO design

### 5.1 Internal session-owned record (S1-B)

One immutable current result per canonical session ID, stored beside telemetry per-session state,
atomically replaced on generation change, cleared on delete:

```go
type AgentActivityRecord struct {
    SessionID      string              // canonical <adapter>:<local-id>
    Status         agent.AgentStatus   // resolved via adapter.GetStatus → ResolveStatus
    Provenance     contract.Provenance // winning provenance (never upgraded)
    Confidence     float64             // clamp01, advisory-ceilinged by ResolveStatus
    Degraded       bool
    DegradedReason string              // bounded (<=200 chars), no prompts/paths
    ObservedAt     time.Time           // set from the poll clock at resolution
    Generation     int                 // adapterState.streamGen binding
    Version        string              // accepted provider version binding
}
```
Stale is a **derived read-time policy** over `ObservedAt`, never a stored status and never a way to
manufacture a new activity value (handoff §223-224).

### 5.2 Product DTO (S1-D) — additive, nested, never overloading `state`

Add one nested object to the `GET /api/sessions` row (kept separate from lifecycle/health):

```text
agentActivity {
  contractVersion : "t0.1"          // contract.ContractVersion
  status          : AgentStatus     // frozen vocabulary
  provenance      : Provenance
  confidence      : 0.0–1.0
  degraded        : bool
  observedAt      : RFC3339
  stale           : bool            // derived; health, not a status
}
lifecycle   { state }               // EXISTING daemon value, unchanged, separate
observation { stale }               // EXISTING health, separate
```

Existing `state` / `lifecycleState` / `agentStatus` / `stale` fields remain **byte-for-byte
unchanged**. Never expose raw `lastError`, evidence, prompts, metadata, private paths, or adapter
records (handoff §299). Mobile gets strict DTO validation mirroring `validateTranscriptResponse`
(`client.ts`), which is the repo's precedent for a validated read DTO (contrast `listSessions`,
which currently validates nothing).

---

## 6. AgentEvent → StatusEvidence transition table (closed & conservative)

Evidence is built **only** from accepted, correlation-gated events at the §4 hook point. Each
event contributes `StatusEvidence{Status, Provenance = event.Provenance (native_log for the accepted
path), Confidence = event.Confidence (never upgraded)}`. `ResolveStatus` then decides the winner.

| Accepted AgentEvent (`models.go:51-63`) | → StatusEvidence.Status | Rationale |
| --- | --- | --- |
| `thinking` | `thinking` | structured reasoning marker |
| `tool_call_started` | `working` | tool execution in progress |
| `waiting_input` | `waiting_input` | authoritative input-wait |
| `approval_requested` (bound) | `waiting_approval` | **display-only**; no CTA/action in S1 (§162-163) |
| `completed` | `completed` | strong terminal; native_log is non-advisory so not downgraded |
| `failed` | `failed` | strong terminal |
| `interrupted` | `interrupted` | strong terminal |

**Explicit NON-mappings (produce NO status evidence; state stays whatever prior strong evidence
resolved, else `unknown`):**

| Event | Why no evidence |
| --- | --- |
| `agent_started` | startup marker, not a durable activity state |
| `user_message` | input already delivered; does not establish agent activity |
| `assistant_message` | a message was emitted; not a durable state (avoid inferring idle) |
| `tool_call_finished` | next state ambiguous; must not infer idle/completed |
| `approval_resolved` | approval bookkeeping (A1), not activity |
| `unknown` | unknown/malformed → contributes nothing → resolves to `unknown`/`degraded` |

No status may come from PTY/Transcript/screen/prompt/CWD/process-name (handoff §155-156). Provenance
and confidence are preserved from the accepted event and never upgraded (§273).

---

## 7. Rollback

S1 is purely additive and removable without touching lifecycle, T3, Recorder, or adapters:

1. Remove the S1-D `agentActivity` field from the DTO and the mobile consumer component + its
   import in dashboard/FeedScreen (existing labels revert to `state`/`agentStatus`).
2. Remove the S1-C evidence build + `GetStatus` call at `telemetry_service.go:205` (the accepted
   path continues projecting to Transcript exactly as before).
3. Remove the S1-B session-owned store and its `Clear` hook.

No frozen contract, lifecycle transition, Recorder byte path, or adapter semantics is modified, so
rollback is deletion-only and leaves `9ad6f83` behavior intact.

---

## 8. Checkpoint answers (handoff §78-87)

- **Daemon lifecycle authority:** Session Catalog / LifecycleService (`lifecycleState`); unchanged by S1.
- **Agent-activity authority:** frozen T0 `ResolveStatus` over accepted `StatusEvidence` from
  correlation-gated T1/T2 events; nothing else may create it.
- **Connectivity/health:** Registry poll `stale`/`lastError`; a separate envelope field, never a status.
- **Evidence rejected/degraded:** cross-session, unknown provenance/status, version-conflict,
  uncorrelated, overflow, and advisory-terminal claims → dropped or `unknown`+`degraded`.
- **Production caller (after S1-C):** `TelemetryService.processSession` at the §4 hook, reusing the
  existing accepted-adapter batch/cursor/version/correlation — no new reader.
- **Contracts unchanged:** T0 interface/harness, T1/T2 semantics, D1, T3 arbitration remain
  byte-for-byte/behaviorally unchanged; S1 adds only a new consumer + additive DTO field.

---

## 9. Staged plan (this report tracks all five; single review after S1-E)

- **S1-A** (this report) — audit + authority matrix + DTO design + transition table + rollback.
- **S1-B** — session-owned bounded status store delegating to `GetStatus`/`ResolveStatus`; 8 negative
  authority tests (advisory-terminal→unknown+degraded, cross-session reject, unknown→unknown, old
  generation cannot overwrite, adapter failure isolates, lifecycle exited + activity working stay
  separate, agent completed + lifecycle running stay separate, delete clears + recreate cannot inherit).
- **S1-C** — wire `processSession` §4 hook → bounded evidence → adapter `GetStatus` → store;
  production-path tests calling `processSession`.
- **S1-D** — additive authenticated (`sessions:read`) DTO + strict server/mobile validation + real
  mobile component rendering activity separately from lifecycle.
- **S1-E** — full regression matrix + final-tree gate + push + single REVIEW REQUEST.

---

## 10. S1 completion record (S1-A → S1-E)

### 10.1 Commit / SHA traceability

| Stage | Commit | Scope |
| --- | --- | --- |
| S1-A | `8d73a4a` | status-path audit + authority matrix (this report, doc-only) |
| S1-B | `33c1fed` | session-owned bounded status store + 8 authority negatives |
| S1-C | `e8d53d0` | `processSession` accepted-adapter → store wiring (no second reader) |
| S1-B/C hardening | `2f3fdfc` | reviewer blockers B1–B6 (provenance/session/approval/tie/bound/lifecycle) |
| S1-D | `0b8e14a`→`3db66c9`→`17553e0` | authed `agentActivity` DTO + strict mobile validator + card render decision |
| S1-E | this stage | E1 generation invalidation, E2 connectivity/restart freshness, E3 cleanup evidence, E4 golden+race |

Accepted midpoint: S1-B/C `2f3fdfc`, S1-D `17553e0` (both independently accepted).

### 10.2 Requirement → production-path test matrix

| Requirement | Production owner | Test |
| --- | --- | --- |
| accepted evidence → store via GetStatus | `TelemetryService.processSession` | `TestS1C_*` (codex/claude/version-conflict/uncorrelated) |
| E1 generation change invalidates prior positive status | `processSession` gen-change → `statusStore.Clear` | `TestS1E_GenerationChangeInvalidatesPriorStatus`, `TestS1E_TruncationGenerationChangeInvalidates` |
| older generation cannot overwrite newer | `AgentStatusStore.store` | `TestAgentStatusStore_OldGenerationCannotOverwrite` |
| E2 connectivity/freshness → non-current | `deriveCardActivity` + `isConnectionStale` + `DashboardScreen` | Jest `connection stale → forced non-current`, `isConnectionStale` |
| E2 restart: accepted session, no DTO → no legacy authority | `deriveCardActivity` | Jest `no DTO + accepted session → Unavailable` |
| E2 reconnect commits only new response | `sessionPoller` singleflight + mounted | `sessionPoller.test.ts` (delayed/unmount) |
| E3 lifecycle/history delete clears store | `LifecycleService.Delete` (real route) | `TestS1D_RealDeleteRouteRemovesActivity_APIObserved` |
| E3 registry disappearance clears store | `TelemetryService.reconcileSessions` | `TestS1E_RegistryDisappearanceClears` |
| E3 link/unlink/relink clear store | `HandleLinksAPI` → `Telemetry.Clear`; gen-change for relink | `TestS1E_LinkUnlinkClearStatus`, `TestS1E_GenerationChange*` |
| E3 same-id recreation no inheritance | store `Clear` + fresh `Update` | `TestS1D_RealDeleteRoute*`, `TestAgentStatusStore_DeleteClearsAndNoInherit` |
| E4 cross-language DTO golden (t0.1 + shape) | `AgentActivityDTO` marshal ↔ mobile validator | `TestS1E_DTOGoldenShape`, `agentActivityGolden.test.ts` |
| E4 race under update/gen/clear/recreate | `AgentStatusStore` | `TestS1E_StatusStoreRace` (`-race`) |
| lifecycle/approval never derive from activity | `computeActionPolicy`, `sessionNeedsApproval` | Jest `agent activity NEVER drives ...` |
| unsupported/future version fails closed | mobile validator | Jest `rejects wrong contract version`, golden drift |

### 10.3 Confirmed reuse (no new subsystem)

- Ordering/cursor/generation: `ReadRawLines.GenerationChanged`, `adapterState.streamGen`, the accepted
  adapter opaque cursor, and event `Seq` (tie-break) — no second cursor/poller/correlation was added.
- Cleanup: `TelemetryService.Clear`, `TelemetryService.reconcileSessions`, `LifecycleService.Delete`,
  and the existing `mux.MigrateLegacyID` — no new cleanup registry.
- Resolution: the frozen `contract.ResolveStatus` via accepted-adapter `GetStatus`; `contract.ContractVersion`
  (`t0.1`) is the single shared version constant, mirrored (not code-generated) into the mobile validator.

### 10.4 Behavior summary

- **Restart**: the in-memory store is empty by design; an accepted session with no current DTO renders
  `Unavailable` and never the legacy heuristic as accepted authority.
- **Reconnect / freshness**: a failed poll or an expired last-success horizon forces a previously-fresh
  activity to non-current (`Stale activity`); only a post-reconnect successful response is committed
  (singleflight + mounted guard).
- **Generation**: any stream-generation change immediately clears the prior positive status; a new
  generation with no status event leaves the session absent until fresh correlated evidence arrives.
- **Unsupported version**: the mobile validator fails closed (null → `Unavailable`), never a legacy fallback.
- **Approval**: `waiting_approval` is display-only; it creates no ApprovalStore entry and authorizes nothing.

### 10.5 Commands, counts, skips, cleanup

```sh
cd companion-daemon
gofmt -l ./internal/agent ./internal/term    # (pre-existing non-S1 files only; all S1 files clean)
go build ./... && go vet ./...               # OK
go test -race ./internal/agent/... ./internal/term/... -count=1   # PASS
cd ../mobile && npm run typecheck && npm test # tsc OK; Jest 26 suites / 354 tests PASS
cd .. && git diff --check                     # OK
sh scripts/build-gate.sh                      # ALL GATES PASSED (native gate below)
```

Skips / limitations (honest evidence level):

- **Native Android Kotlin compile**: environment-blocked — `mobile/android/gradlew` is absent in this
  environment. Run with the generated `android/` project before P1. **M-track.**
- **RN component mount render**: `react-test-renderer` is not available offline (react 19.2.3), so the
  AgentCard render is proven via its imported production render-decision function (`deriveCardActivity`)
  plus rg import-chain evidence, not a mounted component. **M-track** (add a mount test where the render
  lib is available).
- **Physical device smoke**: not run here. **M-track.**
- Temporary Go caches under `/tmp/devremote-s1-go-*` only; no stage worktrees or generated candidates retained.

---

REVIEW REQUEST: S1 Rich Agent Runtime Status — c8c9a14fd3d52c45ec5278b57cb91b5b89f728a4
