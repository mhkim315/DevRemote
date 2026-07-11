# M3b — Production Evidence Matrix (read-before-code)

Branch: `feature/phase10-multi-adapter`
Baseline: `46e45fe8966ed28666a38324bff60161b8e102f3` (ancestor of HEAD, verified).

This matrix is built from the ACTUAL production code, not from plan prose. Each
row records where the real mobile caller is, the real client function, the real
daemon route/handler, the accepted capability/state gate, the success DTO, the
status-code handling, the current automated coverage, and the **confirmed gap**.

Legend: ✅ present & correct · ⚠ present but wrong for M3b · ❌ missing.

---

## Backend (daemon) — ALREADY ACCEPTED (M2 `eae1e96c9`, wired in `app.go`)

Remote (production, non-`InsecureLocalOnly`) route → permission map,
`cmd/devremote/app.go:262-292`:

| Method / Path | Handler | Permission | Success DTO | Errors |
|---|---|---|---|---|
| `GET /api/sessions` | `HandleSessionsV2` | `sessions:read` | `SessionTelemetry[]` (incl. `adapterCapabilities`) | 401 missing/invalid bearer · 403 no perm |
| `GET /api/session-profiles` | `HandleSessionProfiles` | `sessions:read` | `SessionProfile[]` | 401 · 403 |
| `POST /api/sessions/{id}/stop` | `HandleSessionStop` → `Lifecycle.Stop` | `sessions:stop` | `LifecycleResult{sessionId,action:"stop",state}` | 401·403·404·422 unmanaged·500 terminate-failed(state=stopping) |
| `POST /api/sessions/{id}/kill` | `HandleSessionKill` → `Lifecycle.Kill` | `sessions:kill` | `LifecycleResult{...action:"kill",state}` | 401·403·404·422·500 |
| `DELETE /api/sessions/{id}` | `HandleSessionDelete` → `Lifecycle.Delete` | `history:delete` | `LifecycleResult{...action:"delete",state:exited}` | 401·403·404·422 unmanaged·**409 not-terminal** |
| `DELETE /api/sessions?id=` (query) | `HandleSessionCRUD` (legacy) | `history:delete` | legacy JSON | migration-only; **mobile must not use** |

- `RequirePrincipal` (`internal/devicetrust/auth_middleware.go`): missing bearer →
  **401**, invalid bearer → **401**, missing permission → **403**. → satisfies
  "invalid bearer 401 vs permission 403 distinction" and "member cannot reach
  Stop/Kill/Delete handler".
- Lifecycle status codes (`internal/term/lifecycle_handlers.go:66-84`):
  `ErrLifecycleNotFound`→404, `ErrLifecycleUnsupported`→422 (not a managed
  session), `ErrLifecycleNotTerminal`→**409** (delete on running/stopping),
  terminate-failed→500 with `state:"stopping"`.
- Capability gate is by-capability, never by adapter name
  (`lifecycle_service.go:managedLive/Delete`): the adapter must declare
  `mux.CapManagedLifecycle`. Only `controlled_pty` declares it
  (`controlled_pty_adapter.go:38`). tmux/cmux/localpty → 422.
- `adapterCapabilities` for `controlled_pty` =
  `["observe","liveTerminal","reliableTranscript","input","control","managedLifecycle"]`
  (`mux/adapter_capability.go:57-82`). So `managedLifecycle` AND `input` are both
  independently expressed in `GET /api/sessions`. → satisfies the M3 precondition
  "API expresses managedLifecycle capability and control independently".
- Existing daemon regression coverage (must stay green): `auth_e2e_test.go`
  (member 403 / owner reaches handler / missing-bearer 401 for `/stop`, `/kill`,
  path `DELETE`), `lifecycle_test.go`, `lifecycle_blocker_test.go` (idempotent
  Stop, Kill-while-stopping, delete-only-terminal, cross-session isolation).

**Backend conclusion (REVISED after independent REJECT).** Routes, permissions,
capability gate, terminal-state gate, and action DTOs were already accepted — but
`/api/sessions` did NOT express an authoritative lifecycle state (its `state` is
agent-activity) and dropped terminal sessions when the runtime left the Registry.
So M3b required one **additive** daemon change: a `lifecycleState` field sourced
from the Session Catalog and retention/merge of terminal Catalog rows in the
session list (`internal/term/telemetry.go` `mergeLifecycleState`,
`catalog.go` `List()`). No auth / M2-service / Recorder / Terminal redesign. See
`docs/M3B_IMPLEMENTATION_REPORT.md` §0 for the full 6-blocker remediation.

---

## Mobile — per-operation evidence

### GET /api/sessions (list / refresh — authoritative state source)
- Real caller: `DashboardScreen.fetchSessions` (1.5s poll) and
  `FeedScreen.fetchSession` (poll).
- Client fn: `client.listSessions` → `apiGet('/api/sessions')`.
- Transport: ✅ host-bound device bearer (paired origin only) via
  `apiGet`/`authenticatedFetch`; fail-closed on non-paired origin.
- DTO consumed: `capabilities`, `adapterCapabilities`, `state` (agent-activity),
  `approvals`. `adapterCapabilities.includes('managedLifecycle' | 'input' |
  'liveTerminal')` already read in `FeedScreen`.
- Gap: ❌ no `managedLifecycle` read yet; `state` used for grouping only, not for
  lifecycle-action gating.

### POST /api/sessions/{id}/stop
- Real caller: **none** — no Stop control exists in any mobile screen.
- Client fn: ❌ **missing** (`stopSession` does not exist).
- Confirmed gap: add `stopSession(id)` on the host-bound device transport (POST,
  no body) + a running-managed-gated Stop control with confirmation.

### POST /api/sessions/{id}/kill
- Real caller: **none**.
- Client fn: ❌ **missing** (`killSession` does not exist).
- Confirmed gap: add `killSession(id)` on device transport (POST) + a Force-Kill
  control shown only in `stopping` behind a destructive confirmation.

### DELETE /api/sessions/{id} (delete history)
- Real caller: `DashboardScreen.handleDeleteProfile` (from `AgentProfileModal`).
- Client fn: ⚠ `client.deleteSession` uses **`checkedFetch('/api/sessions?id=...',
  DELETE)`** — the **forbidden query DELETE** on the **legacy token transport**,
  and it fires on **any** session regardless of terminal state.
- Confirmed gaps:
  1. ❌ not using path `DELETE /api/sessions/{id}`.
  2. ❌ not on the host-bound device transport.
  3. ❌ not gated to terminal managed state (violates "delete only in terminal";
     409 handling absent).
- Fix: add device-transport `deleteSessionHistory(id)` → path DELETE; gate it to
  managed terminal state in the UI; render 409 as "Stop the session first" +
  refresh (no retry loop). The legacy `deleteSession` stays only for any
  remaining non-lifecycle callers, but the lifecycle UI must not use it.

### Back / Detach
- Real caller: `FeedScreen` `onBack` (header `←`). ✅ Calls only `onBack`
  (viewer unmount); `useEffect` cleanup only `ctrlRef.current?.cancel()` (ticket
  teardown). No lifecycle endpoint. → satisfies "Back/unmount calls no lifecycle
  endpoint". Must be preserved and proven.

### Terminal input vs Stop
- Real caller: `FeedScreen.send/sendMacro` → WebView binary frame. Stop must
  **not** reuse this path. New Stop/Kill call the dedicated lifecycle endpoints
  only. Input bar is already gated `activeTab==='terminal' && !sessionEnded`;
  M3b additionally disables it on `stopping`/terminal and when `input` capability
  is absent.

---

## Confirmed missing (the only things M3b implements — mobile-only)

1. `stopSession(id)` — device-transport POST `/api/sessions/{id}/stop`.
2. `killSession(id)` — device-transport POST `/api/sessions/{id}/kill`.
3. `deleteSessionHistory(id)` — device-transport **path** DELETE
   `/api/sessions/{id}` (replaces the forbidden query DELETE for lifecycle).
4. A **pure** action-policy reducer mapping (`managedLifecycle`, `input`,
   lifecycle state, in-flight action) → `{canStop, canForceKill, canDelete,
   inputEnabled, label}`, fail-closed to View Only on unknown/missing capability.
5. FeedScreen wiring: Stop / Force Kill / Delete History controls with
   confirmations, duplicate-tap guard, stale-response/session-switch guard, and
   authoritative-state reconciliation (response DTO + next list refresh; WS/EOF
   and network loss are display hints only, never terminal authority).
6. Remove the DashboardScreen edit-modal query-DELETE from the lifecycle path.

Everything else in the plan (pairing, DeviceKey, bearer, WS-ticket, Terminal,
Recorder, daemon auth, M2 lifecycle service) is already accepted and untouched.
