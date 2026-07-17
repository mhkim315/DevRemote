# A1.2 C3D — Production Activation Contract

Status: **PRE-IMPLEMENTATION R1 — NO CODE CHANGED — C3D-A PROHIBITED UNTIL R1 IS ACCEPTED**

Parent: `docs/NEXT_EXECUTOR_A1_2_C3D_PRODUCTION_ACTIVATION_HANDOFF.md`
Accepted C2D: `91160409c9fc7e41a0c60b1c97a6ec6a7c4cffb4`
(`docs/A1_2_C2D_CATALOG_FINAL_ACCEPTANCE.md`)
Base HEAD for this note: `63d7042f9e28a240c39ecbc59beae67599b25a04`
R0 of this note: `69124d830c27897751914fd0f84c8c46bf3a1667` — independent
verdict: ACCEPT WITH REQUIRED CHANGES. R1 applies exactly the four required
changes: (1) §12 no longer asks to accept the pre-launch digest check as the
non-reusable launch binding — it freezes a per-incarnation launch
certification tuple built from the existing launcher/registry identity,
produced at the initial launch AND at every resume launch and bound to that
RuntimeRef; (2) the platform precondition is the exact certified
`darwin/arm64` tuple behind an explicit OS-neutral certification seam, not a
bare GOOS check; (3) §1/§2 rollback wording is corrected — the observation
store binding may remain after a failed install; only actionability, delivery
and RuntimeOf stay off; (4) §8 separates witness AUTHORITY (PostToolUse /
permission_denials) from stdout-marker corroboration.

This note freezes the exact C3D activation design before any production code is
written. C3D activates exactly one catalog action
(`claude.bash.approval_probe.v1`) for exactly one certified tuple
(`claude_headless` / `2.1.209` / `Bash` / `echo pokitclaudeapprovalprobe`).
It is not arbitrary Claude Bash approval. C0H/C0R remain BLOCKED and D1 remains
REJECTED. If independent review rejects any item below, C3D-A does not start.

## 1. Single production install owner and linearization point

One new transition, modeled byte-for-byte on the accepted Codex SP1-P2B
`ManagedCodexService.InstallApprovalExecution`
(`internal/term/managed_approval_activation.go:62`):

```go
func (s *ManagedClaudeService) InstallApprovalExecution(store *AuthoritativeApprovalStore)
    (ApprovalDelivery, func(string) (RuntimeRef, bool), error)
```

- **Sole caller:** `cmd/devremote/app.go` `NewAppWithDeps`, inside the existing
  `if cfg.EnableManagedClaude` block, immediately after
  `managedClaude.SetApprovalStore(approvals)` and before any create surface
  (IPC/HTTP) is reachable. No other production call site exists. Tests
  compose their own service instances.
- **Linearization point:** the single `s.mu` critical section inside
  `InstallApprovalExecution`. Every precondition is verified under `s.mu`;
  the ONLY mutations are `s.approvals = store` (when unset) and
  `s.actionable = true`, committed together at the end of the same critical
  section. The transition races with `CreateDetached` / `Shutdown` only
  through `s.mu`, so exactly one of {install-then-create, create-then-install}
  is observable; the latter fails closed (precondition below).
- **All-or-nothing:** any precondition failure returns an error with ZERO
  activation state change — `s.actionable` stays false, no delivery and no
  resolver are returned, and no ingest path becomes actionable. Precisely
  because `app.go` calls `SetApprovalStore(approvals)` BEFORE the install,
  the canonical observation-store binding MAY already exist and REMAINS in
  place after a failed install; that is the accepted C1D observation-only
  state, not a partial activation. `app.go` mirrors the accepted Codex
  tolerance rule (`app.go:295-304`): on install failure with
  `ApprovalExecutionInstalled() == false` the app runs observation-only —
  the handler keeps the capacity-0 gate, `RuntimeOf` composition excludes
  Claude, and every prior and subsequent Claude observation stays
  non-actionable; on an installed-but-unowned activation the App is NOT
  built.

Preconditions verified under `s.mu` (each → distinct error, nothing mutated):

1. `store != nil`;
2. `!s.closing`;
3. `s.approvals == nil || s.approvals == store` (canonical-store rule, same as
   `SetApprovalStore`, `managed_claude.go:864`);
4. `s.gen == 0 && len(s.runtimes) == 0` — install precedes the FIRST epoch;
   install after any runtime ever existed fails (no record may predate
   activation, so no upgrade path can exist);
5. `!s.actionable` — a second install is an error, never idempotent success;
6. exact certified authority: `s.cfg.AuthorityVersion == "2.1.209"` and
   `validVersion` passes; `s.cfg.Version == "2.1.209"`;
7. launch-certification seam configured and complete: `s.cfg.PinnedPath != ""`,
   `s.cfg.PinnedDigest` is exactly 64 lowercase hex, and the compiled attestor
   kind/version (§12) is the supported one. The per-spawn
   `productionClaudeAttestor.Certify` (`claude_attestor.go:77`) stays
   fail-closed on the initial spawn and on every resume spawn, and §12 binds
   every spawn to a per-incarnation launch-certification tuple;
8. supported platform tuple: `{runtime.GOOS, runtime.GOARCH}` must be a member
   of the compiled closed set `certifiedClaudePlatforms`, whose single entry is
   `{darwin, arm64}` — the exact C0D-certified OS/architecture. Any other
   OS/arch combination fails install; observation still works there. Adding a
   platform entry requires a separate contract review (same rule as adding a
   catalog entry).

On success it returns `(NewClaudeManagedApprovalDelivery(s), s.RuntimeOf, nil)`
where both components are bound to the SAME service and therefore the same
coordinator and the same canonical store.

`SimulateGracefulExit` (`managed_claude.go:1387`) remains test-only and is
never called by any production path; C3D-A adds no production caller.

## 2. Canonical store ownership and rollback

- The ONE `AuthoritativeApprovalStore` is created once in `app.go`
  (`term.NewApprovalStore()`, `app.go:184`) and is the same object handed to
  Codex install, Claude `SetApprovalStore`, Claude install, and
  `term.Handlers.Approvals`. Install re-verifies pointer identity
  (precondition 3); a diverging handler/ingest store pair cannot be built.
- Store immutability after first runtime is already enforced by
  `SetApprovalStore` (`s.gen != 0 || len(s.runtimes) != 0` → error); install
  precondition 4 extends the same rule to activation.
- Rollback: because the transition mutates nothing until all preconditions
  pass under one lock, "rollback" is the absence of activation mutation.
  There is no partially-installed state to unwind. A store binding created
  earlier by `SetApprovalStore` is NOT unwound by a failed install — it is the
  frozen observation-only configuration and keeps working non-actionably.
  The existing create-path rollbacks are
  unchanged: reserved Store slot cleared via `s.approvals.Clear(id)` on any
  post-reservation create failure (`managed_claude.go:973-979,1021-1034`),
  coordinator identity removed on failed Store admission
  (`joinDeferred`, `managed_claude.go:484-527`).

## 3. Actionable ingestion — subsequently created matching runtimes only

Codex pattern reused exactly (`managed_codex.go:932`):

- `claudeManagedRuntime` gains `actionableActive bool`, copied from
  `s.actionable` under `s.mu` inside `CreateDetached` at runtime construction
  (`managed_claude.go:1004-1019`), immutable for the runtime's life.
- `joinDeferred` ingests ACTIONABLE **iff**
  `rt.actionableActive && pending.catalogActionID != ""`:
  - `Options`: exactly `[{ID:"allow_once",Label:"Allow once",Kind:"approve"},
    {ID:"deny",Label:"Deny",Kind:"reject"}]`;
  - `DeliveryMaterial`: exactly two entries, daemon-generated at the ingest
    site: `{OptionID:"allow_once", SchemaVersion: claudeDecisionSchemaV1,
    ResponseBytes: claudeHookResponseBytes("allow")}` and the `deny`/`"deny"`
    counterpart (`claude_resume_coordinator.go:58`);
  - `Actionable: true`, `RequiredPerm: devicetrust.PermTerminalInput`,
    `CatalogActionID: pending.catalogActionID`.
- Every other case (inactive runtime, empty catalogActionID, resume runtimes —
  whose `rt.approvals` is nil by construction in `ResumeForApproval`) keeps
  the frozen C1D non-actionable zero-option ingest byte-for-byte.
- No existing record is ever upgraded: records are immutable after admission;
  a re-offer with a different option/material fingerprint is dropped by the
  frozen `optionFprint` rule (`approval_store_gen.go:390-400`), and install
  precedes the first epoch so no pre-activation record can exist in a
  production process.

## 4. Deepest Store admission boundary

`provenActionMapping` (`approval_ingest.go:27`) is NOT touched; the telemetry
ingest path stays zero-actionable for every provider. Activation flows only
through the managed-service ingest above, and the Store re-verifies it.

New always-on admission rule inside `AuthoritativeApprovalStore.ingest`
(admission = record creation; claim/receipt authority is untouched and gains
no provider branch). It is a data-driven policy keyed by `in.Provider`:

- `in.Provider == "claude_headless"` and `item.Actionable == true` admits ONLY
  when ALL of the following hold — otherwise the item is dropped at the same
  validation stage as the existing `validateDeliveryMaterial` failure, i.e.
  BEFORE it can count as a structurally valid item: a rejected forged item
  neither creates a session nor advances the generation high-water, and it
  never supersedes existing authority:
  1. `in.StreamGen == 0`;
  2. `item.CatalogActionID` resolves via `lookupCatalogEntry` to an entry with
     `entry.Provider == in.Provider && entry.Version == in.Version` (this
     transitively pins tool `Bash` and the exact catalog command — the Store
     never sees or needs raw command bytes);
  3. the (bounded-copied) option set is EXACTLY the two certified options
     above — IDs, labels, kinds, no input schema, order-insensitive,
     count == 2;
  4. `item.DeliveryMaterial` has exactly the two entries above and each entry's
     `SchemaVersion` equals `claudeDecisionSchemaV1` and its `ResponseBytes`
     are byte-equal to the Store-side RECOMPUTED
     `claudeHookResponseBytes(certifiedClaudeDecision[optionID])`;
  5. `item.RequiredPerm == devicetrust.PermTerminalInput`;
  6. `item.Provenance == contract.ProvenanceProviderHook`.
- A forged `Actionable`, unknown/mismatched catalog ID, third/renamed option,
  wrong schema, or substituted material byte is rejected here even when a
  trusted internal caller supplies it. The public summary cannot be forged at
  all: it is never stored — `projectSafeApproval` (`approval_dto.go:82`)
  selects the compiled `catalogSummary(rec.catalogActionID)` at projection
  time and `boundCatalogID` (`approval_store_gen.go:213`) already zeroes an
  invalid ID binding.
- Non-actionable `claude_headless` items keep the frozen C1D/P2B admission
  unchanged. `codex_app_server` admission is byte-for-byte the accepted SP1
  behavior. Other providers are unchanged (and remain non-actionable via
  `provenActionMapping`).
- **Test migration (explicit, for review):** the eight accepted non-catalog
  controlled fixtures in `cmd/devremote/claude_delivery_composition_test.go`
  (`makeSetup`-based, `{"command":"echo hello"}` + `Actionable:true`) violate
  the new always-on admission by design. C3D-A migrates their SETUP to the
  catalog probe input (as `catalogMakeSetup` already does) while preserving
  every assertion unchanged. No accepted assertion is weakened or deleted;
  the migration diff is part of the C3D-A review packet. All other accepted
  P1/P2A/P2B/P3 and Codex suites run unmodified.

## 5. Complete immutable tuple — field-by-field binding table

| # | Field | Created by (exact site) | Carried in | Verified at (every consumer) | Public? | Invalidated by |
|---|---|---|---|---|---|---|
| 1 | ApprovalID | `genApprovalToken` → `"claude-"+32hex`, `joinDeferred` (`managed_claude.go:452-457`) | coordinator `identities` key; `approvalRecord` key; `binding.ApprovalID` | `ReserveEntry` identity lookup; `RecordDelivery` binding equality | yes, opaque (`SafeApprovalDTO.ID`) | timeout, stop/kill/delete, exit-without-join, witness commit |
| 2 | POKIT SessionID | `CreateDetached` `claude_headless:<genLocalID>` (`managed_claude.go:950`) | record, identity `pokitSessionID`, binding, resume ctx | `validSessionID` everywhere; `ReserveEntry` B1 (`id.pokitSessionID == binding.SessionID`); `Accept`/`Deliver` meta validation | yes | Delete → `Clear(sessionID)` |
| 3 | RuntimeRef | `joinDeferred` `{claude_headless, s.cfg.AuthorityVersion, rt.epoch, 0}` (`managed_claude.go:463`) | identity `runtime`; record gens; `binding.Runtime`; `resumeContext.originalRuntime` | `ReserveIdentity` (adapter/version grammar + catalog match); `ClaimForExecution` exact-gen; `Deliver` adapter+StreamGen==0; `ReserveEntry` equal; `MarkWitnessed` `entry.runtime.equal(rt)` — always the ORIGINAL ref, never the resume epoch | no | epoch supersede (`InstallRuntimeGeneration` on terminate), `ClearRuntime` |
| 4 | Claude session_id | hook bridge strict decode (`claude_hook_bridge.go:240`) | pending obs → identity `sessionID` → `resumeContext.claudeSessionID` → `--resume` argv | `joinDeferred` equality; `ClaimWrite` exact; deny decoder top-level `session_id` vs `ctx.claudeSessionID` (`routeDenial`); PostToolUse witness equality in `MarkWitnessed` | never | coordinator identity lifecycle |
| 5 | tool_use_id | hook bridge strict decode | pending obs → identity → entry | `joinDeferred`; `ClaimWrite`; `routeDenial` single-match rule (duplicate bound id → fail closed); `MarkWitnessed` | never | same as 4 |
| 6 | tool_name | hook bridge strict decode | pending obs → identity → entry | catalog entry `ToolName` at `ReserveIdentity`; `ClaimWrite`; `routeDenial` cross-binding check; `MarkWitnessed` | never | same as 4 |
| 7 | input digest | `canonicalJSON`+`sha256Hex` in `handleHook` (`claude_hook_bridge.go:251-256`), cross-checked against classifier digest by `selectCatalogActionID` | pending obs → identity → entry | `joinDeferred` recompute-equal; `ClaimWrite` exact; witness digest RECOMPUTED from the event's own tool_input (denial decoder `decodeDenialEntry`; posttool handler) and compared in `MarkWitnessed` — never `ctx.inputDigest` passthrough | never (raw input discarded at classification) | same as 4 |
| 8 | CatalogActionID | P1 classifier at `handleHook` (`claude_catalog_classifier.go:184`) | pending obs → identity (validated) → `ApprovalIngestItem.CatalogActionID` → `record.catalogActionID` | `ReserveIdentity` catalog/provider/version/tool match; §4 admission rules; `boundCatalogID`; summary selection at projection | no — only its compiled static label is public | record lifecycle |
| 9 | OptionID (action) | compiled certified options at ingest; selected by store in `ClaimForExecution` (`findStoredOption`) | `binding.OptionID`; idempotency ledger | §4 admission (closed set); `deriveDecision` via `certifiedClaudeDecision` (allow_once→allow, deny→deny; anything else rejected); `RecordDelivery` binding equality | yes, closed vocabulary (`SafeOptionDTO`) | terminal state |
| 10 | Delivery schema | `claudeDecisionSchemaV1` bound into material at ingest; folded into action by `ClaimForExecution` (`approval_store_gen.go:665-668`) | `binding.DeliverySchema` | §4 admission; `Deliver` (`claude_approval_delivery.go:47`); `deriveDecision`; `RecordDelivery` equality | never | — |
| 11 | Payload / PayloadDigest | Store copies EXACT stored material bytes at claim; digest via `payloadDigest` | `ClaimResult.Payload`; `binding.PayloadDigest` | `Deliver`: `bytesEqual(req.Payload, claudeHookResponseBytes(decision))` AND `payloadDigest(req.Payload)==b.PayloadDigest`; receipt `DeliveredPayloadDigest` is recomputed by `MarkWitnessed` from the decision actually written; `RecordDelivery` requires digest equality — substituted bytes never commit | never | — |
| 12 | ActionDigest | `CanonicalAction.Digest()` at claim (includes option, kind, schema version, delivery schema + payload digest) | `binding.ActionDigest` | claim `AssertDigest` (optional); binding equality at reserve/accept/commit | never | — |
| 13 | Claim token | `newClaimToken` 32-hex at `ClaimForExecution` | `ClaimResult.Token`; coordinator `entries` key; receipt | `validClaimToken`; `ReserveEntry` dup-reject; `ClaimWrite`/`ConfirmWrite`/`MarkWitnessed` key; `RecordDelivery` `receipt.ClaimToken == rec.claimToken` | never | entry lifecycle |
| 14 | Resume nonce | `generateCoordNonce` inside `ReserveEntry` | `ResumeHandle`; resume hook URL query | `handleResume` query vs `resumeContext`; `ClaimWrite` `entry.resumeNonce` exact | never | one-shot: consumed by state transition |
| 15 | Idempotency key | mobile `ApprovalCard.keyFor` (one key per approval+option, reused across retries) | request body → `ClaimRequest` → binding → ledger | `validCanonicalKey`; ledger equality (same key+binding+auth → `already_accepted`; same key+different binding/auth → `conflict`) | client-generated, bounded grammar | ledger capacity |
| 16 | Requester auth | `devicetrust.PrincipalFromContext` → `requesterFromPrincipal` → `canonicalRequesterAuth` | claim `rec.auth`; ledger `auth` | `requesterAuthorized` (4 server-derived fields present + stored `PermTerminalInput`) on EVERY path incl. replay | never | — |
| 17 | Receipt ID | `newClaudeReceiptID` `"cldr-"+32hex` pre-allocated in `Deliver` | `DeliveryReceipt.ReceiptID` | `RecordDelivery` requires non-empty on success | never | — |
| 18 | Resume attempt | one coordinator entry per claim token (`ReserveEntry`), states reserved→writeClaimed→decisionWritten→terminal | coordinator entry | duplicate reserve → false; duplicate `ClaimWrite` → `outcomeDuplicate`; one-shot completion channel | never | cancel/timeout/ClearRuntime/witness |
| 19 | Launch certification | §12 tuple at each spawn: attestor kind/version + OS/arch + artifact identity + `proc.OpaqueID()`/PID/SpawnedAt + SessionID/Epoch + result/reason | initial: immutable `ManagedSessionRecord` fields; resume: recorded with the delivery attempt | create/resume fail closed on non-certified result; `RuntimeOf` condition 5; install precondition §1.7-8 | never | per-incarnation — dies with its epoch |

The chain is: hook classification (1×, raw bytes discarded) → pending
observation → `ReserveIdentity` → Store admission (§4) → authenticated claim →
`ReserveEntry` → resume spawn → repeated-hook `ClaimWrite`/`ConfirmWrite` →
provider-native witness (PostToolUse for allow; strict `permission_denials`
decode for deny) → `MarkWitnessed` → `DeliveryReceipt` → `RecordDelivery`
commit. Every arrow compares the fields listed above; no field is ever
regenerated downstream from display state.

## 6. Current-runtime resolution (`RuntimeOf`) and invalidation

`ManagedClaudeService.RuntimeOf(sessionID) (RuntimeRef, bool)` — new, modeled
on Codex (`managed_approval_activation.go:97`) with one Claude-specific rule
for the C0D deferred-exit lifecycle (the initial process EXITS after
`tool_deferred`; the decision is delivered to a `--resume` spawn):

Resolve `{claude_headless, rt.authorityVersion, rt.epoch, 0}` iff ALL hold:

1. service installed (`s.actionable`);
2. `rt := s.runtimes[sessionID]` exists (Delete removes it);
3. `rec := s.reg.Get(sessionID)` exists and `rec.Epoch == rt.epoch`;
4. EITHER `!rec.Exited` (live pre-defer window), OR
   `rec.Exited && rt.deferredExit && coordinator.HasIdentityForRuntime(sessionID, rt.epoch)`
   (the expected joined-deferred exit window — B4 semantics; a small new
   read-only coordinator accessor);
5. the epoch's launch-certification result (§12) is `certified` — an
   uncertified incarnation can never resolve (creation already fails closed
   on certification failure; this condition makes the binding independently
   testable at the resolver).

Invalidation matrix (each row = deterministic C3D-A test, §5 handoff item 6):

| Event | Mechanism already in tree | RuntimeOf after | Stale completion defeated by |
|---|---|---|---|
| exit without join | `terminate` → `ClearRuntime(sid, epoch)` (`managed_claude.go:744`) | false (no identity) | identity gone → `ReserveEntry` false |
| joined-deferred exit | `deferredExit` preserves identity | TRUE (claim window) | this is the certified path |
| observation timeout | `clearStaleObservations` → `InvalidateRecord` + `ClearForApproval` | false | record invalidated + identity gone |
| Stop/Kill (live or after deferred exit) | `terminate` or the `rec.Exited` branch → `ClearRuntime` | false | `TerminalStaleRuntime`/`TerminalCancelled` on active entries |
| Delete | runtimes-map removal + `reg.Remove` + `approvals.Clear` + `ClearRuntime` | false | store record gone |
| replacement | resume spawn has its OWN epoch and is never in `s.runtimes`; `ClearRuntime(sid, resumeEpoch)` on its exit cannot match the original identity's LaunchGen | original binding unaffected | `MarkWitnessed` requires the ORIGINAL RuntimeRef |
| daemon restart | store/coordinator are in-memory only | false (nothing restored) | frozen §6 rule: recovery is unknown/non-actionable only |
| epoch supersede | `InstallRuntimeGeneration(sid, epoch, 1, "terminated")` on terminate | n/a | late `IngestObserved(StreamGen=0)` rejected |

The handler additionally re-resolves `RuntimeOf` immediately before delivery
(`approval_handler.go:107-112`) and the claim binds the exact generation, so a
resolution that goes stale between claim and delivery records
`DeliveryStaleRuntime` and never commits.

**Composition:** `app.go` currently wires the single `h.RuntimeOf` from Codex
install. C3D-A adds one combined resolver at the composition boundary:
`mux.ParseSessionID(sessionID).Adapter` → `codex_app_server` → Codex resolver,
`claude_headless` → Claude resolver, anything else → `(RuntimeRef{}, false)`.
Codex-only, Claude-only and both-installed compositions all yield exactly the
accepted Codex behavior for Codex sessions. No generic authority gains a
provider branch — this is boundary dispatch.

**Delivery dispatch:** the accepted `dispatchingApprovalDelivery` is NOT
modified. Claude is composed into the FALLBACK position:
`NewDispatchingApprovalDelivery(codexDelivery, claudeDispatch)` where
`claudeDispatch` routes `Binding.Runtime.Adapter == claude_headless` (and only
that, re-verified again inside `Deliver`) to `ClaudeManagedApprovalDelivery`
and everything else to the frozen capacity-0 gate (`unavailable`). Codex
routing bytes are unchanged; unknown adapters still terminate at the
capacity-zero gate; cross-provider substitution fails in `Deliver`'s adapter
check before any provider write.

## 7. Authenticated requester derivation and mobile idempotency

Unchanged, reused exactly (C3D-B proves it end-to-end through the composed
production route; prefer zero mobile production change):

- Route: `POST /api/sessions/{id}/approvals/{approvalId}` behind
  `devicetrust.RequirePrincipal(sessionMgr, h.HandleApprovalAction,
  devicetrust.PermTerminalInput)` (`app.go:380`). Only a paired-device bearer
  session on this host yields a principal; the requester context is derived
  ONLY via `PrincipalFromContext` → `requesterFromPrincipal`
  (`approval_handler.go:132`). The client body is
  `{action, input?, idempotencyKey}` with `DisallowUnknownFields` — no
  provider/runtime/requester/actionable field is accepted from the client;
  unknown fields reject the body.
- Store enforces the complete server-derived 4-field requester presence +
  stored `PermTerminalInput` on EVERY path including replay
  (`requesterAuthorized`, `approval_execution.go:76`).
- Mobile: strict `SafeApproval` decoder (`approvalRequest.ts`) renders the
  static summary `"Run Claude approval verification probe"` and the two
  options ONLY for a validated pending+actionable record; `ApprovalCard.keyFor`
  creates one idempotency key per (approval, option) and reuses it across
  manual retries; `resolveApproval` is host-bound device-auth only and treats
  only `accepted`/`already_accepted` as success. Same key + same digest →
  `already_accepted`; changed option/digest under the same key → `conflict`
  (409). No mobile file changes unless C3D-B demonstrates a contract gap.

## 8. Live success evidence (C3D-C) and complete failure outcomes

Exactly two live positives on the pinned `2.1.209` artifact (recorded
identity: `PinnedPath` realpath + SHA-256 in the evidence report; ambient
auth; owned temporary workspace as cwd; bounded/redacted stream captures):

1. **allow once** — fresh `CreateDetached`; the REAL initial process emits
   `PreToolUse` for the exact probe; classifier match; deferred join; pending
   actionable record with the static summary; authenticated device API claim
   `allow_once`; one resume spawn; repeated-hook `ClaimWrite` returns the
   exact allow response bytes; **success AUTHORITY = the matching
   `PostToolUse`** (same session/tool_use_id/name/recomputed digest,
   `MarkWitnessed`); **corroboration (non-authority)** = the probe's stdout
   token `pokitclaudeapprovalprobe` present exactly once in the bounded
   captured tool result, evidencing one harmless execution; receipt commit
   `approved`; duplicate `RecordDelivery` non-commit; clean exit; no
   settings/auth mutation outside the isolated hook dir.
2. **deny** — same fresh setup; authenticated claim `deny`; exact deny
   response bytes; **success AUTHORITY = the exact bound
   `permission_denials` witness** (strictly decoded entry; raw entry input
   re-digested, equal to the observation digest); **corroboration
   (non-authority)** = no matching `PostToolUse` and the probe token absent
   from the captured stream, evidencing zero execution; receipt commit
   `rejected`; clean exit.

Authority and corroboration are disjoint: only the provider-native witness
routed through `MarkWitnessed` may produce `accepted`; stdout presence or
absence can never create, substitute for, or veto a witness — it is recorded
in the evidence report as execution corroboration only. The catalog command
is an `echo` and leaves no filesystem marker; no new catalog entry or
filesystem side effect is added for evidence purposes.

Negative coverage stays deterministic (no live turns): duplicate tap
(`already_accepted` / `conflict`), stale epoch, replacement, timeout,
stop/delete, daemon restart (fresh store restores nothing), unsupported
version, cross-session/request substitution, forged-admission rejections (§4),
wrong-witness-kind rejections.

Complete closed failure surface (every path lands in one of these; none is
success):

- claim: `not_found`, `not_actionable`, `unknown_action`, `input_rejected`,
  `invalid_key`, `digest_mismatch`, `expired`, `already_owned`,
  `retry_exhausted`, `stale_runtime`, `runtime_mismatch`, `unauthorized`,
  `ledger_full`, `conflict` (HTTP mapping `claimOutcomeHTTP`);
- delivery: `unavailable` (no service/coordinator, identity gone, resume spawn
  failure), `rejected` (binding/schema/option/payload/digest mismatch),
  `runtime_mismatch` (wrong adapter / StreamGen), `conflict` (receipt-ID
  entropy failure, delivery timeout, non-witnessed terminal:
  cancelled/ambiguous/stale/timeout), `stale_runtime` (pre-delivery
  revalidation) — committed as `delivery_failed` with the frozen bounded
  manual retry (`maxManualRetries = 2`; ambiguous and superseded outcomes are
  marked non-retryable);
- witness: early/duplicate/mismatched/wrong-kind witnesses leave the entry
  unchanged or fail it closed (`routeDenial` cross-binding + duplicate-id
  rules; PostToolUse never proves deny); a malformed denial-shaped line
  cancels the active entry (`failClosedDenial`).

## 9. Capacity behavior

All bounds are pre-mutation rejections (nothing canonical is changed on a
capacity failure): `maxClaudeSessions = 4`, `maxPendingClaudeObservations = 4`,
`maxActiveApprovals = 4`, `maxCoordinatorIdentities = 16`,
`maxCoordinatorEntries = 16`, `authMaxApprovalsPerSession = 50`,
`authMaxApprovalSessions = 1024` (live-authority sessions are never evicted),
`authMaxIdempotencyKeys = 256` (`ledger_full`), `maxManualRetries = 2`,
observation/coordinator timeouts 120 s (witness window 240 s), delivery poll
timeout 120 s. C3D changes none of these constants.

## 10. Explicit non-goals

Everything in handoff §8 verbatim: no arbitrary Bash, no additional catalog
entries, no allow-always; no PermissionRequest / permission-prompt-tool /
Agent SDK / Channels / PTY key injection; no public DTO expansion or raw
provider text; no A1 Store/claim/receipt weakening; no Codex protocol change;
no N1/O1/O2/tmux/cmux cleanup/canonical timeline/Grok/Navigator; no
daemon-restart restoration of pending approval authority; no Windows work; no
generic provider SDK extraction. Additionally: no change to
`provenActionMapping`, no second catalog summary, no new public API fields,
no `SimulateGracefulExit` production use.

## 11. Known-bad counterexamples (become C3D-A tests)

1. **Partial install (KB-1):** a hypothetical install that (a) sets
   `s.actionable = true` before verifying the pinned digest, or (b) is called
   after `CreateDetached` already produced epoch 1. Consequence: (a) a
   non-certified binary becomes deliverable, (b) records created before
   activation coexist with actionable ingest under the same store session and
   an operator cannot distinguish them — the exact upgrade hazard §4 of the
   handoff prohibits. C3D-A proves the real transition rejects both: install
   failure mutates nothing (returned delivery/resolver are nil, `actionable`
   stays false, a subsequent create ingests non-actionable), and
   install-after-create fails with the runtime-exists error while the
   pre-existing record remains zero-option forever.
2. **Provider-wide actionability (KB-2):** flipping
   `provenActionMapping("claude_headless")` to return options (or ingesting
   `Actionable:true` with a fabricated option/material set and no catalog ID)
   would make EVERY Claude approval observation actionable on provider name
   alone — precisely the rejected D1 shape. C3D-A proves the §4 admission
   boundary drops such an item even when supplied by an internal caller with
   authoritative provenance: no record, no CTA, no claim; and that a
   catalog-ID-bearing item with one substituted material byte is equally
   dropped.

## 12. Non-reusable launch binding — per-incarnation certification tuple

`A1_2_CLAUDE_APPROVAL_EXTENSION_PLAN.md` §4 requires, before actionability, a
platform-specific spawned-process identity or an equivalent non-reusable
launch binding. R0 asked to accept the bare pre-launch digest check; the
verdict rejected that (the check certifies a FILE, not a launch incarnation).
R1 freezes the following instead — built from identity the daemon already
owns, no macOS code-signing work:

**`ClaudeLaunchCertification`** — one tuple produced at EVERY spawn site
(`CreateDetached` AND every `ResumeForApproval`), wrapped around the spawn:
the existing pre-exec artifact certification plus the launcher-derived
incarnation identity, evaluated through one OS-neutral seam.

| Field | Source (exact, existing where noted) |
|---|---|
| Attestor kind/version | compiled constant `pokit.claude.prelaunch.v1` — the versioned identity of the attestor implementation |
| OS / Arch | `runtime.GOOS` / `runtime.GOARCH`; must be a member of `certifiedClaudePlatforms` (§1.8, single entry `{darwin, arm64}`) |
| Artifact identity | `cfg.Version` + `cfg.PinnedDigest` (64-hex), verified fail-closed by `productionClaudeAttestor.Certify` immediately before exec (`claude_attestor.go:77-117`) |
| Opaque launch identity | `proc.OpaqueID()` — existing `"proc-<pid>-<spawnUnixNano>"` (`managed_codex.go:124`) |
| PID / SpawnedAt | `proc.PID()`, `clockNow()` at spawn |
| POKIT SessionID / Epoch | the session ID and the `s.gen` epoch allocated for THIS incarnation |
| Result / Reason | `certified`, or the exact failure reason — any non-certified result fails the create/resume closed |

- **Non-reusable:** the tuple is keyed by
  `(PokitSessionID, Epoch, ProcessID)`. Epochs are unique per service
  (`s.gen` is monotonic under `s.mu`) and `OpaqueID` embeds the incarnation's
  PID and spawn timestamp, so a tuple from one incarnation can never certify
  another incarnation, another session, or another epoch — and none of it is
  reusable as a generic cross-provider contract.
- **Initial-launch binding:** the tuple is bound into the immutable
  `ManagedSessionRecord` identity (`ProcessID`, `OS`, `Arch`, `CreatedAt`,
  `CertifiedDigest`, `PID` already exist and are immutable after `Register`,
  `managed_registry.go:33`; C3D-A adds the attestor kind/version and
  certification result/reason as equally immutable fields — additive,
  zero-valued and inert for Codex records). `RuntimeOf` (§6) resolves only
  when the CURRENT epoch's record carries a certified result, in both the
  live and the joined-deferred windows.
- **Resume-launch binding:** `ResumeForApproval` produces its OWN tuple for
  its own epoch and `OpaqueID` before returning the runtime; `Deliver`
  proceeds past the resume spawn only when that tuple is certified. Witness
  AUTHORITY still validates against the ORIGINAL RuntimeRef (§5 row 3) — the
  resume tuple identifies the delivery incarnation and is recorded with the
  attempt; it never substitutes for the approval's bound runtime.
- **OS-neutral certification seam:** the observable contract is exactly the
  provider-neutral tuple `{OS, arch, attestor kind/version, opaque artifact
  identity, opaque launch identity, certification result/reason}`
  (extension plan §4). The macOS specifics — `--version` probe,
  `EvalSymlinks` realpath pinning, file SHA-256 — live ONLY inside the
  attestor/launcher implementations. A future Windows implementation supplies
  the same observable tuple through the same seam; no common contract code
  depends on macOS paths, code-signing, or process APIs.
- **Explicitly not claimed:** this tuple is not process-image attestation (no
  post-exec image digest, no code-signing evaluation) and is never called
  that. It is the launch-incarnation binding the R0 verdict specified: exact
  artifact/version certification + OS/arch + daemon-owned launch identity +
  session/epoch binding + result/reason, generated per spawn and bound to
  that incarnation's RuntimeRef.

## 13. C3D-A file plan (expected, subject to review)

- `internal/term/managed_claude_activation.go` (new): install transition,
  `RuntimeOf`, Claude fallback dispatcher, combined resolver helper.
- `internal/term/claude_attestor.go` (or a new
  `claude_launch_certification.go`): §12 per-incarnation certification tuple,
  the compiled attestor kind/version constant, and the closed
  `certifiedClaudePlatforms` set — all behind the OS-neutral seam.
- `internal/term/managed_registry.go`: additive immutable
  certification-result/attestor-identity fields on `ManagedSessionRecord`
  (zero-valued and inert for Codex records; no Codex behavior change).
- `internal/term/managed_claude.go`: `actionableActive` copy in
  `CreateDetached`; actionable branch in `joinDeferred` (§3); produce and
  bind the §12 tuple at BOTH spawn sites (`CreateDetached`,
  `ResumeForApproval`).
- `internal/term/claude_resume_coordinator.go`: read-only
  `HasIdentityForRuntime`.
- `internal/term/approval_store_gen.go`: §4 admission policy at the single
  `ingest` admission site.
- `cmd/devremote/app.go`: one owned composition step (§1) + combined
  `RuntimeOf` + dispatcher wiring.
- Tests: new `*_activation_test.go` (install/§5 handoff items 1-6, KB-1,
  KB-2, invalidation matrix, §12 certification binding + uncertified-spawn
  fail-closed), admission negatives, dispatcher routing, and the
  §4 fixture migration in `claude_delivery_composition_test.go`.
  No live turn, no mobile change in C3D-A.

After C3D-A: freeze HEAD, run focused race + full backend gate, commit, push,
stop for independent review. C3D-B and C3D-C follow their handoff sections
only after the preceding ACCEPT.
