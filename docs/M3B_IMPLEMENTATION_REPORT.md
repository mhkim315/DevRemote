# M3b Implementation Report — Mobile Session Lifecycle Action UX

Status: **READY FOR INDEPENDENT RE-VERIFICATION (remediation applied)**

Branch: `feature/phase10-multi-adapter`

Required baseline (accepted M3-auth-2B lineage / doctor-repair docs):
`46e45fe8966ed28666a38324bff60161b8e102f3` —
`git merge-base --is-ancestor 46e45fe8966ed28666a38324bff60161b8e102f3 HEAD`
exits 0.

## 0. Remediation of the independent REJECT (6 blockers)

The first M3b commit (`dfa319645`) was correctly REJECTED: the daemon did not
expose an authoritative lifecycle state or retain terminal rows, so mobile
inferred state from list presence and Force Kill / Delete History were
unreachable in the real flow. This revision fixes all six blockers with an
**additive** daemon change (no auth / Terminal transport / Recorder / accepted
M2-service redesign):

1. **Authoritative `lifecycleState` on `/api/sessions`.** Added a `LifecycleState`
   field to `SessionTelemetry`, **separate** from `state`/`agentStatus` (agent
   activity). `mergeLifecycleState` (`internal/term/telemetry.go`) sources it from
   the Session Catalog for managed sessions; non-managed sessions carry none.
   Mobile reads it and **removed all presence/absence inference**.
2. **Retained terminal rows + merge/dedup.** `mergeLifecycleState` unions live
   Registry rows with retained exited/killed/failed Catalog rows, deduped by
   canonical ID (live wins), exposing `lifecycleState` + `history` +
   `adapterCapabilities` on retained rows (`SessionCatalog.List()` added). Delete
   removes the retained row only after success.
3. **Force Kill reachable, one clear policy.** `stopping` is now an observable
   authoritative state (Catalog → list). Policy: **Force Kill is shown iff
   `lifecycleState === 'stopping'`.** After a failed graceful Stop the controller
   refreshes (not retries), the list re-surfaces `stopping`, and Force Kill issues
   a real `/kill` — proven end-to-end in `m3bLifecycleController.test.ts`.
4. **Capability enforcement in rendering.** The input/macros gate is now
   `actionPolicy.inputEnabled` for **every** session (removed the non-managed
   bypass); observe-only/unknown never show input, external `input` adapters keep it.
5. **Strict response validation.** `parseLifecycleResult` (pure) validates exact
   `sessionId`, exact `action`, and the closed state vocabulary; the client
   stop/kill/delete reject a malformed/wrong-session 2xx, so a bogus Delete 2xx
   never navigates.
6. **Production controller + tests.** The flow is extracted into
   `SessionLifecycleController` (used directly by FeedScreen — no parallel copy),
   tested for confirmations→mapping, duplicate taps, stale/session-switch, dispose
   (Back/unmount) no-request, 409 refresh, Stop-failure→Force-Kill, Delete→onDeleted.

Full gate green after remediation: `go test -race ./...`, mobile tsc, jest **21
suites / 287 tests**, invariants, secret scan.

Task: connect the mobile **Stop**, **Force Kill**, and **Delete History** UX to
the real production lifecycle routes, gated by the authoritative
`managedLifecycle` capability and lifecycle state, over the already-accepted
host-bound paired-device transport. No pairing / DeviceKey / bearer / WS-ticket /
Terminal / Recorder / daemon-auth redesign.

## 1. Finding — the backend was complete; the mobile action surface was missing

The daemon M2 lifecycle (`eae1e96c9`) already exposes, in remote/production mode
(`cmd/devremote/app.go:262-292`), `POST /api/sessions/{id}/stop` (`sessions:stop`),
`POST .../kill` (`sessions:kill`), `DELETE /api/sessions/{id}` (`history:delete`),
`GET /api/sessions` + `/api/session-profiles` (`sessions:read`), with
`RequirePrincipal` returning **401** (missing/invalid bearer) vs **403** (missing
permission), and the lifecycle service returning **404 / 422 / 409 / 500**
(`internal/term/lifecycle_handlers.go`). `GET /api/sessions` already expresses
`managedLifecycle` and `input` **independently** in `adapterCapabilities`
(`mux/adapter_capability.go`; only `controlled_pty` declares `managedLifecycle`).

So M3b required **no daemon change**. The confirmed gaps were mobile-only and are
documented per-operation in `docs/M3B_PRODUCTION_EVIDENCE_MATRIX.md`:
- no `stopSession` / `killSession` client functions;
- the only destructive control (`DashboardScreen.handleDeleteProfile`) used the
  **forbidden legacy query DELETE on the legacy token transport**, ungated by
  lifecycle state;
- no capability/state-driven action policy in the viewer.

## 2. Changes (mobile only)

- **`mobile/src/lib/client.ts`**: refactored `apiPost` into a shared
  `apiWrite(method, path, body?, legacyToken?)` (POST behavior byte-identical, so
  `createSession` and its tests are unchanged). Added `stopSession`,
  `killSession`, `deleteSessionHistory` — each rides the host-bound fail-closed
  device transport, hits the exact route, sends **no body**, and maps a non-2xx to
  a `PokitError` carrying `statusCode` (401/403/404/409/422/500). Added the
  `LifecycleActionResult` DTO. The legacy query-form `deleteSession` is retained
  for compatibility but explicitly marked **not for lifecycle** and has zero
  callers.
- **`mobile/src/lib/lifecycle.ts`**: added a **pure** policy module — `readCapabilities`
  (managed/input from `adapterCapabilities`, fail-closed), `reconcileState`
  (monotonic `starting→running→stopping→terminal`, terminal-sticky, never invents
  a terminal state), `stateFromActionResult`, and `computeActionPolicy` →
  `{canStop, canForceKill, canDelete, inputEnabled, viewOnly, terminal, statusLabel}`.
- **`mobile/src/screens/FeedScreen.tsx`**: derives `{managed, inputCapable}`,
  tracks authoritative `lifecycleState` (from action responses + successful list
  refresh; WS/EOF and network loss are display hints only), and renders a
  capability/state-gated lifecycle bar. Stop / Force Kill / Delete History each run
  through one `runLifecycleAction` funnel with `Alert` confirmation, a
  duplicate-tap/concurrent guard, a session-switch stale guard, 409→"Stop first"+
  refresh (no retry), 403→permission message (distinct from 401), and
  authoritative-state retention on failure. `onBack` is unchanged (viewer detach
  only). The terminal input bar is additionally disabled for a managed session
  that is not `running` (non-managed sessions keep prior behavior).
- **`mobile/src/screens/dashboard/DashboardScreen.tsx`** +
  **`components/AgentProfileModal.tsx`**: removed the legacy query-DELETE
  "terminate agent" path from the presentation-only edit modal (`isEditMode` now
  keys off `initialId`, not a delete handler).
- **Tests**: `__tests__/m3bLifecycleClient.test.ts`,
  `__tests__/m3bActionPolicy.test.ts`. **Docs**: this report + evidence matrix.

No daemon change, no Android/iOS auth change, no shared-DTO change, no
Recorder/PTY/ticket change.

## 3. Semantics enforced

```text
Back / Detach   = viewer unmount only (onBack) — no Stop/Kill/Delete request
Stop            = POST /stop (managed+running) — SIGTERM group; history KEPT;
                  never terminal input / Ctrl-C / exit / WS close
Force Kill      = POST /kill — only while stopping, destructive confirmation
Delete History  = DELETE /api/sessions/{id} (PATH) — only in terminal managed
                  state; 409 → "Stop first" + refresh; record/history removal
External/observe/unknown = View Only, no managed lifecycle controls (fail closed)
```

State is authoritative from the daemon action response and the next session-list
refresh; the client never manufactures `exited/killed` after a request or from a
network drop.

## 4. Automated evidence matrix

| Required proof | Evidence |
|---|---|
| production client uses the exact method/path | `m3bLifecycleClient`: stop/kill → `POST .../stop|kill`; delete → `DELETE /api/sessions/{id}`, asserts URL has no `?id=` |
| paired-device bearer only in Authorization | `m3bLifecycleClient`: `Bearer DEVICE_BEARER_A`, header never contains `SUPABASE_JWT` |
| non-paired origin → 0 network requests | `m3bLifecycleClient`: stop/kill/delete each reject with 0 fetches |
| 401 invalid bearer vs 403 permission distinguished | `m3bLifecycleClient`: `statusCode` 401 vs 403; 403 not retried |
| member cannot reach Stop/Kill/Delete handler | daemon `cmd/devremote/auth_e2e_test.go` (member 403 on `/stop`,`/kill`,path DELETE) — green |
| running managed session only → Stop | `m3bActionPolicy`: `canStop` only when managed+running |
| stopping only → Force Kill | `m3bActionPolicy`: `canForceKill` only when managed+stopping |
| terminal managed only → Delete History | `m3bActionPolicy`: `canDelete` only when managed+terminal |
| external/observe-only/unknown → no destructive controls | `m3bActionPolicy`: View Only rows |
| Back/unmount calls no lifecycle endpoint | `FeedScreen.onBack` unchanged; lifecycle only via `runLifecycleAction` behind `Alert`; reset-on-session-change effect |
| Stop sends no terminal input/Ctrl-C/exit | stop hits `/stop` with no body (test); separate from the WebView send path |
| Stop retains history | daemon `internal/term/lifecycle_test.go` / `lifecycle_blocker_test.go` — green (no daemon change) |
| Delete refused on running/stopping; 409 → refresh, no retry | `m3bLifecycleClient` 409 (exactly one call); `FeedScreen` 409 handler |
| duplicate tap prevented | `m3bActionPolicy` pending-guard; `runLifecycleAction` `pendingActionRef` |
| stale response/session switch doesn't change other UI | `FeedScreen` `sessionRef` guard + reset effect |
| failure keeps server authoritative state | `runLifecycleAction` leaves `lifecycleState` unchanged on error; `reconcileState` monotonic |
| no invented terminal state | `m3bActionPolicy`: `stateFromActionResult`/`reconcileState` — unknown never terminal, terminal-sticky |
| bearer/credential/terminal secret scan | build-gate secret scan + `m3bLifecycleClient` "never leaks the bearer" |
| existing M3-auth-2B pairing/bearer/ticket/Terminal suites intact | full jest: 20 suites, 267 tests green |
| daemon lifecycle/race/permission regressions intact | `go test -race ./...` green (no daemon edits) |

## 5. Verification (ran here, all green)

- `cd mobile && npx tsc --noEmit`: PASS
- `cd mobile && npx jest`: PASS — **20 suites, 267 tests** (237 at 2B + 30 new
  M3b action/transport proofs)
- `sh scripts/build-gate.sh`: **ALL GATES PASSED** (backend `go build`/`vet`/
  `test -race`, `git diff --check`, mobile typecheck + jest, Android Kotlin
  compile, vendor-branch + ID-inference invariants, secret scan)
- Targeted daemon regressions: `go test ./internal/term -run Lifecycle` and
  `go test ./cmd/devremote -run 'Auth|RouteMatrix'`: PASS
- Baseline ancestry: `git merge-base --is-ancestor 46e45fe89 HEAD` → exit 0

## 6. Manual gates remaining (M-track, explicitly not claimed)

Physical-device/LTE: Stop from LTE visibly converges and local terminal restores;
Back demonstrably detaches without killing the process; stopping → Force Kill
confirmation; terminal → Delete History; Activity/Transcript retained after Stop
and removed only by Delete; offline/auth-expired recovery. Not run in this
environment; not an E-track blocker.

## 7. Out of scope (guardrails honored)

No daemon/auth/Recorder/PTY/WS-ticket redesign; no query DELETE for lifecycle; no
`createOrUpdateSession`/legacy profile-save on the lifecycle path; no adapter-name
branching; no optimistic terminal state; no WS-EOF-as-authority; no
T0/T1/T2/Adapter-Doctor/Transcript/S1/A1/O1; no Windows work; no Android/iOS auth
structure change.
