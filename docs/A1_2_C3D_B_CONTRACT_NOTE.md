# A1.2 C3D-B — Authenticated API and Mobile Path Contract

Status: **PRE-IMPLEMENTATION — NO CODE CHANGED — ZERO-GAP ASSESSMENT**

Parent: `docs/NEXT_EXECUTOR_A1_2_C3D_PRODUCTION_ACTIVATION_HANDOFF.md` §6
Accepted C3D-A-R2: `bbd9bd4a1b41244f4a3daac3124d13a4f40dae47`

C3D-B verifies the existing authenticated mobile approval path for the new
Claude catalog record. The core finding: **all seven C3D-B requirements are
already enforced by the accepted A1/SP1 production path.** No backend or
mobile production code needs to change. C3D-B is a verification packet only.

## 1. Requirement coverage — per-field binding

| # | Handoff §6 requirement | Enforced by (exact, existing) |
|---|---|---|
| 1 | Paired-device server context with `PermTerminalInput` only | Route: `devicetrust.RequirePrincipal(sessionMgr, h.HandleApprovalAction, devicetrust.PermTerminalInput)` (`cmd/devremote/app.go:380`). Handler: `PrincipalFromContext` → `requesterFromPrincipal` → `canonicalRequesterAuth` (`approval_handler.go:132`). Store: `requesterAuthorized` requires 4-field server-derived identity + stored permission (`approval_execution.go:76`). |
| 2 | Client body carries NO provider/runtime/requester authority | `HandleApprovalAction` decodes only `{action, input, idempotencyKey}` with `DisallowUnknownFields` (`approval_handler.go:46`). Provider is the stored record's; runtime is resolved via `h.RuntimeOf`; requester is `PrincipalFromContext`. No client-asserted authority field exists. |
| 3 | Exact static summary + two options for pending actionable catalog record only | `projectSafeApproval` selects `catalogSummary(rec.catalogActionID)` = `"Run Claude approval verification probe"` when `validCatalogBinding` passes (`approval_dto.go:82-87`). Non-catalog records carry the generic Pokit-owned summary. Options render ONLY for `rec.actionable == true`. |
| 4 | `waiting_approval`, non-catalog Claude observations, stale DTOs → no CTA | Non-catalog observations: `Actionable: false`, `Options: nil` (`managed_claude.go joinDeferred`). Mobile `ApprovalCard`: `!approval.actionable` → display-only "Respond in the live terminal — no remote action is available." Mobile `approvalActionable()`: requires `state === 'pending' && actionable === true`. |
| 5 | Duplicate tap = same idempotency binding; changed option/digest = conflict | Store: same key+same binding+same auth → `already_accepted`; same key+different binding/auth → `conflict` (`approval_store_gen.go:706-738`). Mobile `keyFor`: one key per (approval, option), preserved across manual retries (`ApprovalCard.tsx:32-37`). |
| 6 | Unsupported/stale/expired/delivery-failed → non-success, UI honest | `claimOutcomeHTTP` maps all non-granted outcomes to distinct HTTP codes (`approval_handler.go:142-175`). Mobile `handleAction`: 401/403 → "Device not authorized", 409 → "No longer current", 410 → "Approval expired", 502 → "Couldn't deliver — resolve in the terminal", 400 → "Action not accepted" (`ApprovalCard.tsx:51-63`). |
| 7 | ZERO raw Claude payload/path/token/digest/description crosses public boundary | `SafeApprovalDTO` fields: ID, SessionID, Summary (Pokit-owned static label), State, Actionable, Options (ID+Label+Kind+requiresInput only), CreatedAt, ExpiresAt. No field carries raw command, description, tool input, hook payload, provider response bytes, digest source, claim token, or delivery material. Backend `projectSafeApproval` EXCLUDES `delivery`, `binding`, `claimToken`, `auth`, `provenance`. Mobile `validateApproval` explicitly enumerates ALLOWED fields and rejects unknown keys (`approvalRequest.ts:21-23`). |

## 2. No code changes needed

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

**Zero mobile production change is the preferred outcome** per handoff §6.

## 3. C3D-B implementation = verification tests only

Since no production code changes, C3D-B's "implementation" is a set of
deterministic focused tests proving the composed production route for
Claude catalog records. No live model turn; no `-race` regression on any
accepted suite.

### 3A — Backend: exact mobile DTO wire shape

Prove that `ListSafe` for a pending actionable Claude catalog record
produces exactly the allowed wire shape, and that non-catalog records,
stale records, and delivery-failed records carry zero options:

1. **Actionable catalog record DTO** — `ListSafe` returns exactly
   `{id, sessionId, summary:"Run Claude approval verification probe",
    state:"pending", actionable:true,
    options:[{id:"allow_once",label:"Approve",kind:"approve"},
             {id:"deny",label:"Deny",kind:"reject"}]}`.
   No extra fields; no raw command, description, payload, token in the
   serialized JSON.
2. **Non-catalog observation DTO** — `actionable:false`, `options:[]`,
   summary is the generic provider-neutral label.
3. **Stale/expired DTO** — state is non-pending, `actionable` may be
   unchanged but mobile's `approvalActionable` gate evaluates
   `state==='pending'` → no CTA.

### 3B — Backend: authenticated claim-and-commit for Claude record

Prove through `HandleApprovalAction` (the real production handler) that:

4. **Allow claim** — valid principal + `PermTerminalInput` → claim
   granted → delivery → commit `approved`; DTO state reflects committed.
5. **Deny claim** — same path, commit `rejected`.
6. **Duplicate idempotent replay** — same key + same action → `already_accepted`.
7. **Changed option conflict** — same key + different option → `conflict`.
8. **Missing/invalid principal rejected** — 401/403 before claim.
9. **Non-actionable record rejected** — `not_actionable`.
10. **Stale runtime rejected** — resolved runtime gone (Stop) →
    `stale_runtime`.

### 3C — Mobile: TypeScript and Jest gate (unchanged code)

Run the existing mobile gate on the unchanged mobile source. Prove:

11. `npx tsc --noEmit` passes on the unmodified mobile tree.
12. Jest tests (if any approval or DTO tests exist) pass.
13. The strict `validateApproval` decoder rejects: unknown field,
    missing required field, non-boolean `actionable`, non-array `options`,
    invalid option ID/label/kind, foreign session ID, bad timestamp.
14. `approvalActionable` returns false for: `state!='pending'`,
    `actionable:false`, non-pending states (approved, rejected, expired,
    delivery_failed, invalidated).

### 3D — Privacy: no Claude raw data in public DTO

Prove by JSON serialization grep:

15. Serialized `ListSafe` DTO for a Claude record contains none of:
    `"command"`, `"echo pokitclaudeapprovalprobe"`, `"tool_input"`,
    `"permissionDecision"`, `"hookSpecificOutput"`, `"claimToken"`,
    `"ActionDigest"`, `"PayloadDigest"`, `"delivery"`, SHA-256-like
    hex strings (64-char lowercase hex), `"claude_headless"`.

## 4. No new live evidence in C3D-B

C3D-C (§7) owns the live allow/deny evidence with the real pinned
Claude 2.1.209 binary. C3D-B is deterministic only — the real
production handler paths are already individually proven by C3D-A
(delivery, dispatch, RuntimeOf) and the A1/SP1 acceptance gate
(authenticated mobile claim).

## 5. C3D-B file plan

No production file changes. Test additions only:

- `internal/term/approval_handler_test.go` or a new
  `claude_mobile_dto_test.go` — C3D-B items 1-3 (DTO shape), items
  4-10 (authenticated claim via real handler), item 15 (privacy grep).
- Mobile: `npx tsc --noEmit` + existing Jest suite — prove no
  regression in the unmodified mobile tree (items 11-14).

After C3D-B: freeze HEAD, commit tests, push, and stop for independent
review. C3D-C live proof remains prohibited until C3D-B ACCEPT.
