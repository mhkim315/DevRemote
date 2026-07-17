# A1.2 C3D-B — Authenticated API and Mobile Path Contract

Status: **PRE-IMPLEMENTATION R1 — NO CODE CHANGED — ZERO-GAP HYPOTHESIS TO VERIFY**

Parent: `docs/NEXT_EXECUTOR_A1_2_C3D_PRODUCTION_ACTIVATION_HANDOFF.md` §6
Accepted C3D-A-R2: `bbd9bd4a1b41244f4a3daac3124d13a4f40dae47`

C3D-B verifies the existing authenticated mobile approval path for the new
Claude catalog record. Static audit indicates that all seven requirements are
already implemented by the accepted A1/SP1 path, so the expected outcome is a
test-only packet. This remains a hypothesis until the composed remote route and
the mobile presentation behavior below pass. If a test exposes a production
gap, stop and return for contract review; do not silently expand C3D-B.

## 1. Requirement coverage — per-field binding

| # | Handoff §6 requirement | Enforced by (exact, existing) |
|---|---|---|
| 1 | Paired-device server context with `PermTerminalInput` only | Remote route: `devicetrust.RequirePrincipal(sessionMgr, h.HandleApprovalAction, devicetrust.PermTerminalInput)` (`cmd/devremote/app.go:410`). Handler: `PrincipalFromContext` → `requesterFromPrincipal` → `canonicalRequesterAuth` (`approval_handler.go:132`). Store: `requesterAuthorized` requires 4-field server-derived identity + stored permission (`approval_execution.go:76`). |
| 2 | Client body carries NO provider/runtime/requester authority | `HandleApprovalAction` decodes only `{action, input, idempotencyKey}` with `DisallowUnknownFields` (`approval_handler.go:46`). Provider is the stored record's; runtime is resolved via `h.RuntimeOf`; requester is `PrincipalFromContext`. No client-asserted authority field exists. |
| 3 | Exact static summary + two options for pending actionable catalog record only | `projectSafeApproval` selects `catalogSummary(rec.catalogActionID)` = `"Run Claude approval verification probe"` when `validCatalogBinding` passes (`approval_dto.go:82-87`). Non-catalog records carry the generic Pokit-owned summary. Options render ONLY for `rec.actionable == true`. |
| 4 | `waiting_approval`, non-catalog Claude observations, stale DTOs → no CTA | Non-catalog observations: `Actionable: false`, `Options: nil` (`managed_claude.go joinDeferred`). Mobile `ApprovalCard`: `!approval.actionable` → display-only "Respond in the live terminal — no remote action is available." Mobile `approvalActionable()`: requires `state === 'pending' && actionable === true`. |
| 5 | Duplicate tap = same idempotency binding; changed option/digest = conflict | Store: same key+same binding+same auth → `already_accepted`; same key+different binding/auth → `conflict` (`approval_store_gen.go:706-738`). Mobile `keyFor`: one key per (approval, option), preserved across manual retries (`ApprovalCard.tsx:32-37`). |
| 6 | Unsupported/stale/expired/delivery-failed → non-success, UI honest | `claimOutcomeHTTP` maps all non-granted outcomes to distinct HTTP codes (`approval_handler.go:142-175`). Mobile `handleAction`: 401/403 → "Device not authorized", 409 → "No longer current", 410 → "Approval expired", 502 → "Couldn't deliver — resolve in the terminal", 400 → "Action not accepted" (`ApprovalCard.tsx:51-63`). |
| 7 | ZERO raw Claude payload/path/token/digest/description crosses public boundary | `SafeApprovalDTO` fields: ID, SessionID, Summary (Pokit-owned static label), State, Actionable, Options (ID+Label+Kind+requiresInput only), CreatedAt, ExpiresAt. No field carries raw command, description, tool input, hook payload, provider response bytes, digest source, claim token, or delivery material. Backend `projectSafeApproval` EXCLUDES `delivery`, `binding`, `claimToken`, `auth`, `provenance`. Mobile `validateApproval` explicitly enumerates ALLOWED fields and rejects unknown keys (`approvalRequest.ts:21-23`). |

## 2. Expected zero production-code change

Every C3D-B requirement is byte-for-byte satisfied by the accepted
A1 core and SP1. The handler, store, DTO projector, mobile decoder, and
mobile card are provider-neutral by design; C3D-A's single catalog entry
drops into them without any structural change:

- The handler already calls `h.RuntimeOf` which now resolves Claude sessions.
- The handler already dispatches to `h.ApprovalDelivery` which now routes
  Claude bindings to the certified boundary.
- The store already admits the exact catalog tuple (C3D-A's §4 admission).
- The DTO projector already selects the catalog summary for records with
  a valid `catalogActionID`.
- Mobile already validates ONLY the allowlisted `SafeApprovalDTO` fields
  and renders action buttons ONLY for `pending + actionable` records.

**Zero backend and mobile production change is the preferred outcome** per
handoff §6, but only the tests below may establish that conclusion.

## 3. C3D-B implementation = verification tests only

Since no production code changes, C3D-B's "implementation" is a set of
deterministic focused tests proving the composed production route for
Claude catalog records. No live model turn; no `-race` regression on any
accepted suite.

### 3A — Backend: exact mobile DTO wire shape

Prove that `ListSafe` for a pending actionable Claude catalog record
produces exactly the allowed wire shape, and that non-catalog records,
stale records, and delivery-failed records carry zero options:

1. **Actionable catalog record DTO** — `ListSafe` returns exactly the eight
   approval fields `{id, sessionId, summary, state, actionable, options,
   createdAt, expiresAt}`. The values are:
   `summary:"Run Claude approval verification probe"`, `state:"pending"`,
   `actionable:true`, and exactly
   `options:[{id:"allow_once",label:"Approve",kind:"approve",requiresInput:false},
   {id:"deny",label:"Deny",kind:"reject",requiresInput:false}]`.
   Both timestamps must be bounded, calendar-valid RFC3339. No extra field,
   `inputPlaceholder`, raw command, description, payload or token is present.
2. **Non-catalog observation DTO** — `actionable:false`, `options:[]`,
   summary is the generic provider-neutral label.
3. **Stale/expired DTO** — state is non-pending, `actionable` may be
   unchanged but mobile's `approvalActionable` gate evaluates
   `state==='pending'` → no CTA.

### 3B — Backend: composed authenticated claim-and-commit

Build a remote-mode App with `NewAppWithDeps`, the accepted managed Claude
service and its App-owned canonical Store. Send requests through the actual
registered `/api/sessions/{id}/approvals/{approvalId}` route. Direct calls to
`HandleApprovalAction`, manually injected principals, or a separately composed
`http.ServeMux` do not satisfy this packet.

Prove through that composed route that:

4. **Allow claim** — valid principal + `PermTerminalInput` → claim
   granted → delivery → commit `approved`; DTO state reflects committed.
5. **Deny claim** — same path, commit `rejected`.
6. **Duplicate idempotent replay** — same key + same action → `already_accepted`.
7. **Changed option conflict** — same key + different option → `conflict`.
8. **Server-derived requester only** — a valid paired owner bearing
   `PermTerminalInput` succeeds. Missing bearer, member/insufficient permission,
   revoked/foreign bearer, wrong host, wrong boot or otherwise invalid principal
   is rejected with 401/403 before claim and causes zero provider delivery.
9. **Non-actionable record rejected** — `not_actionable`.
10. **Stale runtime rejected** — resolved runtime gone (Stop) →
    `stale_runtime`.
11. **Client authority fields rejected** — independently add each of
    `provider`, `runtime`, `launchGeneration`, `streamGeneration`, `deviceId`,
    `hostId`, `bearerSessionId`, `bootId`, and `permissions` to an otherwise
    valid body. Every request returns 400 through strict decoding, makes no
    claim and writes no provider response.

The allow/deny tests must use the exact `ClaimResult` token, binding and Store
payload returned by the App-owned Store all the way through delivery receipt
and commit. A synthetic binding that merely proves “not capacity-zero” is not
an authenticated claim-and-commit proof.

### 3C — Mobile: TypeScript and Jest gate (unchanged code)

Run the existing mobile gate on the unchanged mobile source. Prove:

12. `npx tsc --noEmit` passes on the unmodified mobile tree.
13. The complete Jest suite passes.
14. The strict `validateApproval` decoder rejects: unknown field,
    missing required field, non-boolean `actionable`, non-array `options`,
    invalid option ID/label/kind, foreign session ID, bad timestamp.
15. `approvalActionable` returns false for: `state!='pending'`,
    `actionable:false`, non-pending states (approved, rejected, expired,
    delivery_failed, invalidated).
16. Exercise the Dashboard-to-`ApprovalCard` presentation boundary (a component
    test or a small extracted pure error classifier, if extraction is required):
    400 displays “Action not accepted”; 401/403 “Device not authorized”; 409
    “No longer current”; 410 “Approval expired”; 502 “Couldn't deliver”. None
    calls `onResolved`. Only strict `accepted` or `already_accepted` calls
    `onResolved`; malformed 2xx remains a visible failure. A non-pending or
    non-actionable DTO produces no CTA at the Dashboard boundary.
17. `waiting_approval` status alone and a non-catalog Claude observation create
    no authoritative Approval and no mobile CTA. This may cite an accepted
    named regression only if that test exercises the same current production
    boundary; otherwise add a focused regression.

### 3D — Privacy: no Claude raw data in public DTO

Prove by JSON serialization grep:

18. Serialized `ListSafe` DTO for a Claude record contains none of the unique
    raw sentinels or forbidden structural keys:
    `"command"`, `"echo pokitclaudeapprovalprobe"`, `"tool_input"`,
    `"permissionDecision"`, `"hookSpecificOutput"`, `"claimToken"`,
    `"ActionDigest"`, `"PayloadDigest"`, `"delivery"`, SHA-256-like
    digest source values, raw path/token/description sentinels, or provider and
    adapter fields. Do **not** globally ban the string `"claude_headless"`:
    the canonical `sessionId` legitimately contains that adapter prefix.
    Assert the exact structural allowlist instead.

## 4. No new live evidence in C3D-B

C3D-C (§7) owns the live allow/deny evidence with the real pinned
Claude 2.1.209 binary. C3D-B is deterministic only — the real
production handler paths are already individually proven by C3D-A
(delivery, dispatch, RuntimeOf) and the A1/SP1 acceptance gate
(authenticated mobile claim).

## 5. C3D-B file plan

No production file changes. Test additions only:

- `internal/term/claude_mobile_dto_test.go` (or the closest existing Store DTO
  suite) — items 1-3 and 18. Keep projector tests pure and deterministic.
- `cmd/devremote/*_test.go` — items 4-11 through `NewAppWithDeps` and the
  composed remote route. Reuse the C3D-A service fixture but do not bypass the
  App-owned Store, route middleware, resolver or dispatcher.
- `mobile/__tests__/approvalRequest.test.ts`,
  `mobile/__tests__/approvalClient.test.ts`, and an ApprovalCard/Dashboard
  presentation test — items 12-17. Production mobile files remain unchanged
  unless the tests demonstrate a real gap; any such gap requires a new review
  before implementation.

Focused gate: backend `go vet` plus `go test -race` for `internal/term` and
`cmd/devremote`; mobile `npx tsc --noEmit` and the complete Jest suite;
`git diff --check`; repository privacy/secret scan. Freeze the exact tested
HEAD before the test/report commit and re-run any gate if that tree changes.

After C3D-B: freeze HEAD, commit tests, push, and stop for independent
review. C3D-C live proof remains prohibited until C3D-B ACCEPT.
