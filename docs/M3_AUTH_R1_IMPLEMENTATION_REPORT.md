# M3-auth-R1 Implementation Report — Authenticated Android M3a REST Wiring

Status: **READY FOR INDEPENDENT VERIFICATION**

Branch: `feature/phase10-multi-adapter`

Baseline (accepted M3-auth-4A): `b34c33528dd50dd6daa8321b358a3fa3e5aca90b`

This report covers the M3-auth-R1 phase: authenticated Android M3a end-to-end
re-verification plus the smallest missing authenticated REST wiring. No iOS,
no M3b lifecycle UX, no Transcript, no new authentication architecture.

## 1. Baseline ancestry

```sh
git merge-base --is-ancestor b34c33528dd50dd6daa8321b358a3fa3e5aca90b HEAD   # exits 0
```

Confirmed: HEAD is a descendant of the accepted M3-auth-4A baseline. The docs
handoff commit (`d36fb1fca`) was fetched and fast-forwarded; the accepted
implementation at `b34c335` is intact and unmodified.

## 2. Confirmed gap (code, not assumption)

The handoff predicted the likely gaps were `GET /api/session-profiles` and
`POST /api/sessions`. Confirmed from source at `b34c335`:

- The daemon (`cmd/devremote/app.go`) already binds **both** routes to the
  host-bound device transport in remote mode:
  - `GET /api/session-profiles` → `RequirePrincipal(..., PermSessionsRead)`
  - `POST /api/sessions` → `RequirePrincipal(..., PermSessionsCreate)`
- The **mobile client** did NOT: `listSessionProfiles` and `createSession`
  (`mobile/src/lib/client.ts`) used `checkedFetch` + `authHeaders(token)`
  (legacy Supabase/dev token), unlike `probeDaemon` / `listSessions` /
  `getSessionHistory` / `getActivityHistory`, which already used the
  host-bound `apiGet`.
- In `paired_device` mode there is no Supabase session, so `App.tsx` passes
  `token = session?.access_token = undefined` down to `DashboardScreen` →
  `NewSessionModal`. The two legacy calls therefore sent **no** Authorization
  header and the daemon replied `401`. M3a New Session could not function on a
  paired device.

The gap was purely client-side transport routing. The accepted daemon
authorization and the accepted create contract were correct and are unchanged.

## 3. Change

`mobile/src/lib/client.ts`:

- Added `apiPost(path, body, legacyToken?)` — the write-side twin of `apiGet`
  with the **same** host-bound, fail-closed contract: when a paired device is
  active, the device bearer (and the request itself) is sent **only** to the
  exact canonical paired origin; any origin/host/port/path/query/fragment/
  userinfo/non-HTTPS variant fails closed with **zero** network requests.
- Routed `listSessionProfiles` through `apiGet('/api/session-profiles', token)`.
- Routed `createSession` through `apiPost('/api/sessions', {profileId,name,cwd}, token)`.

Non-idempotent safety (handoff §6): `apiPost` uses the accepted
`authenticatedFetch`, which proactively refreshes via `getValidToken()`
**before** the request but never re-issues a POST after a 401/403. A rejected
create surfaces a recoverable `AuthError` (`PokitError`) and sends **exactly one**
POST — no duplicate session is ever created. The legacy `explicit_local_dev`
path (no device auth) is preserved unchanged.

No daemon change. No native change. No new dependency.

## 4. Exact REST authentication matrix (paired remote mode)

| Endpoint | Client fn | Transport | Daemon permission |
|---|---|---|---|
| `GET /api/sessions` (probe) | `probeDaemon` | host-bound device bearer (`apiGet`) | `PermSessionsRead` |
| `GET /api/sessions` | `listSessions` | host-bound device bearer (`apiGet`) | `PermSessionsRead` |
| `GET /api/session-profiles` | `listSessionProfiles` | host-bound device bearer (`apiGet`) **← newly wired** | `PermSessionsRead` |
| `POST /api/sessions` | `createSession` | host-bound device bearer (`apiPost`) **← newly wired** | `PermSessionsCreate` |
| `GET /api/sessions?history=` | `getSessionHistory` | host-bound device bearer (`apiGet`) | `PermSessionsRead` |
| `GET /api/sessions?activity=` | `getActivityHistory` | host-bound device bearer (`apiGet`) | `PermSessionsRead` |
| `GET /term/ws` | `getWSTicket` → ticket URL | one-time, session-bound WS ticket | ticket-validated |
| `DELETE /api/sessions` | `deleteSession` | legacy token — **M3b, out of scope** | `PermHistoryDelete` |
| `POST /api/sessions` (edit) | `createOrUpdateSession` | legacy token — **M3b edit, out of scope** | — |
| `POST …/approvals/…` | `resolveApproval` | legacy token — see FOLLOW-UP below | `PermTerminalInput` |
| `/push/register` | `registerPushToken` | legacy token — notifications out of scope | `PermSessionsRead` |

All M3a reads and the M3a create now use the host-bound device transport. The
device bearer is never sent to a non-paired origin.

## 5. Exact create request/response contract

Request (paired device):

```http
POST /api/sessions
Authorization: Bearer <device bearer>
Content-Type: application/json

{"profileId":"shell|codex|claude","name":"","cwd":""}
```

- The body is **exactly** `{profileId, name, cwd}`. `name`/`cwd` are optional and
  sent as empty strings when omitted (the daemon derives the default name from
  the profile label). The client never sends `id`, `runner`, `runnerColor`,
  `command`, `executable`, `adapter`, or `workspaceId`.
- Daemon dispatch: `HandleSessionCRUD` routes any POST carrying `profileId` to
  `createFromProfile` (preset-only, server-resolved executable). `profileId:"custom"`
  → `403`; unknown/uninstalled profile → `400`.

Success response (200), returned **only after the Recorder is proven ready**:

```json
{"id":"controlled_pty:<generated>","adapter":"controlled_pty","profileId":"shell","name":"Shell","state":"running"}
```

Client gating (`isRunnable`): navigation to the Terminal happens only when the
response has a non-empty canonical `id`, `adapter === "controlled_pty"`, and
`state === "running"`. Any other 2xx shape leaves the New Session form open and
adds no phantom card.

Failure mapping:
- `401`/`403` → recoverable `PokitError(AuthError)`, no retry, no duplicate.
- network/timeout → `PokitError(NetworkUnreachable|Timeout)`, no session.
- daemon startup failure → `500` with `state:"failed"`, not runnable.

## 6. Automated proofs

New: `mobile/__tests__/m3aAuthWiring.test.ts` — 8 tests, all pass, using the
real production `listSessionProfiles`/`createSession`/`isRunnable`:

- M3a profile list — host-bound device transport
  - `sends the device bearer to the paired origin, never the Supabase token`
  - `fails closed on a non-paired origin — zero network requests`
- M3a session create — host-bound device transport
  - `owner create sends exactly {profileId,name,cwd} + device bearer, returns running controlled_pty`
  - `member/read-only 403 → recoverable AuthError, exactly one POST (no duplicate)`
  - `401 create is NOT auto-replayed (non-idempotent) → exactly one POST`
  - `non-paired origin → device bearer never sent, ZERO network requests`
  - `network failure → NetworkUnreachable (no session created)`
  - `malformed 2xx (missing running/id) is not runnable → no navigation, no phantom card`

Regression: the accepted host-bound reads, ticket Terminal path, fail-closed
variants, and daemon Recorder/ticket proofs are unchanged and still green
(`deviceAuthTransport.test.ts`, `terminalGeneratedScript.test.ts`,
`wsTicket.test.ts`, `internal/term` Go suites).

## 7. Full gate result

`sh scripts/build-gate.sh` → **ALL GATES PASSED**:

```
--- Backend ---
  go build ... OK
  go vet ... OK
  go test -race ... OK
  git diff --check ... OK
--- Mobile ---
  npm run typecheck ... OK
  npm test ... OK                         (17 suites, 209 tests; was 201 at 4A + 8 new)
  android module kotlin compile ... OK
--- Invariants ---
  vendor branch scan ... OK
  ID inference scan ... OK
--- Security ---
  secret scan ... OK
```

Note: the M3-auth-4A verifier could not independently rerun the Android Kotlin
compile (no generated `android/gradlew` in their clean checkout). This
environment has the generated project, so **the `android module kotlin compile`
gate was independently run and passed** this phase, closing that noted gap.

## 8. Physical Android gate — honest deferral

An emulator (`emulator-5554`) is connected, but the physical M3a **product**
release smoke (§8 of the handoff) is **NOT claimed as passed**:

- `scripts/android-native-gate.sh` runs the M3-auth-1A AndroidKeyStore
  *instrumentation*, which validates the native DeviceKey module — **unchanged**
  by this phase — and regenerates the `android/` project via
  `expo prebuild --clean`. It does not exercise the JS create/profile flow this
  phase changed, so it was not run here to avoid working-tree churn for no
  coverage of the change.
- The end-to-end product smoke (install release APK → LAN QR pair → switch to
  HTTPS tunnel/LTE → cold start without Supabase → create Shell/Codex/Claude →
  open ticket Terminal → bidirectional IO → background/foreground reconnect →
  geometry mirror → natural exit/history) requires a live daemon, an HTTPS
  origin (the client's `canonicalOrigin` rejects non-HTTPS), and manual QR
  pairing. It is a release-gate activity and is deferred. It has not been run.

Recommended before release: run the §8 checklist on a Samsung device (or the
connected emulator behind an HTTPS tunnel) and record APK hash/build, daemon
commit, device model, and Android version.

## 9. Worktree

Clean after commit. Changes in this phase:

- `mobile/src/lib/client.ts` (add `apiPost`; route profiles+create through the
  host-bound transport)
- `mobile/__tests__/m3aAuthWiring.test.ts` (new proofs)
- `docs/M3_AUTH_R1_IMPLEMENTATION_REPORT.md` (this report)

## 10. FOLLOW-UP (non-blocking)

- `resolveApproval` (approval resolution) still uses the legacy token transport.
  It is not on the M3a create/open path and was legacy at the accepted baseline;
  in remote mode its daemon route already enforces `PermTerminalInput`. Consider
  routing it through `apiPost` in the M3b/interaction phase for consistency.
- Physical §8 product smoke, as above.

## 11. Verifier prompt

> You are independently verifying M3-auth-R1 on branch
> `feature/phase10-multi-adapter`. Confirm
> `git merge-base --is-ancestor b34c33528dd50dd6daa8321b358a3fa3e5aca90b HEAD`
> exits 0. In `mobile/src/lib/client.ts`, confirm `listSessionProfiles` and
> `createSession` route through the host-bound `apiGet`/`apiPost` (not
> `checkedFetch`), that `apiPost` fails closed on any origin variant, and that a
> non-idempotent 401/403 create is never retried (exactly one POST). Confirm the
> POST body is exactly `{profileId,name,cwd}` with no `id`/`runner`/`command`/
> `executable`. Independently read `deviceAuthTransport.test.ts` and
> `m3aAuthWiring.test.ts` and confirm the device bearer is never sent to a
> non-paired origin and never co-exists with a Supabase token. Run
> `sh scripts/build-gate.sh` and confirm ALL GATES PASSED. Treat the physical
> §8 product smoke as an explicit release gate (deferred, not claimed). Report a
> BLOCKER only for: bearer to wrong origin, Supabase/dev token authorizing a
> remote M3a op, duplicate create, wrong create payload/capability, navigation
> before a Recorder-ready running response, phantom session on failure, Recorder
> ownership violation, or Terminal bearer exposure / ticket replay.
