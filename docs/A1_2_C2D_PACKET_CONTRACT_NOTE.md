# A1.2 C2D — Packet Contract and Binding Table

Status: **C2D-A AMENDED FOR RE-VERIFICATION — NOT IMPLEMENTED — ACTIONABILITY ZERO**

C2D-A is the mandatory pre-implementation authority note required by
`NEXT_EXECUTOR_A1_2_CLAUDE_APPROVAL_HANDOFF.md` §4. It documents the complete
immutable binding table, provider state machine, linearization point, C0D
consumed-decision witnesses, failure modes, adversarial counterexamples,
public/private data classification, and explicit non-goals.

No production code is changed by this note. The note exists so the verifier can
confirm the contract is complete, closed, and independently testable before a
single line of C2D implementation is written.

## 1. Authority owner and canonical stored inputs

The **provider-neutral A1 `AuthoritativeApprovalStore`** is the sole authority
owner. It is the only component that may:

- create a pending Approval with immutable Options and DeliveryMaterial;
- recompute the canonical ActionDigest and PayloadDigest from the stored record;
- bind the server-derived `RequesterAuthContext`;
- issue a claim token owning one `ApprovalExecutionBinding`;
- record a delivery receipt and commit `approved`/`rejected`.

Claude-specific C2D code (C2D-B/C) is a **delivery boundary only**. It
receives a fully-bound claim from the store, delivers one decision through a
one-shot resume, routes the provider-native consumption witness back to the
store via a `DeliveryReceipt`, and never reconstructs provider identity,
options, digests, or authorization from its own state.

**C2D is controlled-composition work, not production activation.** C1D's
production `joinDeferred` path remains `Actionable=false`, with no options or
delivery material, throughout C2D. Test-only composition may create actionable
records to exercise the frozen A1 claim/delivery contract. Production
actionability, mobile CTA wiring, and non-zero delivery capacity remain C3D
work after independent C2D acceptance.

### Canonical stored inputs (created at ingestion)

| Input | Created by | Stored in |
|---|---|---|
| ApprovalID (`claude-<hex>`) | `ManagedClaudeService.joinDeferred` → `genApprovalToken` | `approvalRecord.approval.ID` |
| POKIT SessionID (`claude_headless:claude-*`) | `ManagedClaudeService.CreateDetached` → `genLocalID` | `approvalRecord.approval.SessionID` |
| Provider (`claude_headless`) | hard-coded constant | `approvalRecord.provider` |
| Version (authority version, e.g. `2.1.209`) | `ClaudeEntryConfig.AuthorityVersion` | `approvalRecord.version` |
| LaunchGen (epoch) | `ManagedClaudeService.gen` (monotonic) | `approvalRecord.launchGen` |
| StreamGen (always `0` for Claude) | hard-coded in `joinDeferred` | `approvalRecord.streamGen` |
| Provenance (`provider_hook`) | hard-coded `contract.ProvenanceProviderHook` | `approvalRecord.provenance` |
| Actionable | `false` in the production C1D/C2D path; controlled C2D tests may construct `true` | `approvalRecord.actionable` |
| Options | `nil` in production C1D/C2D; controlled C2D composition supplies `[{id: "allow_once", kind: "approve"}, {id: "deny", kind: "reject"}]` | `approvalRecord.approval.Options` |
| DeliveryMaterial | `nil` in production C1D/C2D; controlled C2D composition supplies per-option `ApprovalDeliveryMaterial{OptionID, SchemaVersion, ResponseBytes}` | `approvalRecord.delivery` |
| RequiredPerm | `""` in production C1D/C2D; controlled C2D composition supplies the frozen stored permission | `approvalRecord.requiredPerm` |
| Claude session_id | `claudePendingObservation.sessionID` from `PreToolUse.session_id` | C2D-B immutable private identity record keyed by `ApprovalID` + `RuntimeRef` |
| tool_use_id | `claudePendingObservation.toolUseID` from `PreToolUse.tool_use_id` | same private identity record; never reconstructed from output ordering |
| tool_name | `claudePendingObservation.toolName` from `PreToolUse.tool_name` | same private identity record |
| input_digest | SHA-256 of canonical JSON of `PreToolUse.tool_input` | same private identity record |

Current C1D deletes `claudePendingObservation` at deferred join and retains only
`ApprovalID` and expiry in `activeApproval`. Therefore C2D-B MUST add the private
identity record above before any delivery implementation. It is not optional
runtime data and cannot be recovered from stream order, command equality, or
provider output. The record is created from the already matched four-field C1D
observation, defensively copied, bounded, and installed under the coordinator
mutex before Store admission. Failed Store admission removes the reservation;
successful admission publishes the ApprovalID association. Termination or
replacement invalidates the reservation under that same mutex. No actionable
claim may resolve an ApprovalID lacking this exact private record.

### C2D claim outputs and authenticated inputs

| Input | Supplied by |
|---|---|
| ClaimToken (32 hex chars) | `AuthoritativeApprovalStore.ClaimForExecution` |
| ApprovalExecutionBinding (all 8 fields) | `AuthoritativeApprovalStore.ClaimForExecution`; stored in `approvalRecord.binding` and the idempotency ledger after grant |
| Canonical delivery payload bytes | `AuthoritativeApprovalStore.ClaimForExecution` |
| RequesterAuthContext | `canonicalRequesterAuth(RequesterContext)` from authenticated principal |

## 2. Complete immutable binding table

Every field below is created at a single point, stored or passed through the
exact chain shown, compared at every boundary, and never inferred from
ordering, timing, command equality, terminal output, or caller-supplied
authority.

### 2.1 Provider-neutral A1 binding (`ApprovalExecutionBinding`)

This is the binding returned by `ClaimForExecution` and carried unchanged
through claim → delivery → receipt → commit. Every field is compared by
`ApprovalExecutionBinding.equal`.

| # | Field | Created at | Stored in | Copied into | Compared at | Tested by |
|---|---|---|---|---|---|---|
| B1 | `ApprovalID` | `genApprovalToken()` → `"claude-" + hex` | `approvalRecord.approval.ID` | `ApprovalExecutionBinding.ApprovalID`, `DeliveryReceipt.Binding.ApprovalID` | `RecordDelivery` (exact match), `validGateBindingMeta` (length ≤ 256) | `TestRecordDelivery_RejectsAnyBindingFieldMismatch` |
| B2 | `SessionID` | `genLocalID("claude")` → `"claude_headless:claude-<nonce>"` | `approvalRecord.approval.SessionID`, session map key | `ApprovalExecutionBinding.SessionID`, `DeliveryReceipt.Binding.SessionID` | `RecordDelivery` (exact match), `validGateBindingMeta` (canonical parse + Validate + ValidateAdapterName), `genEndpoint.sessionID` (cloned) | `TestRecordDelivery_RejectsAnyBindingFieldMismatch` |
| B3 | `Runtime.Adapter` | hard-coded `claude_headless` | `approvalRecord.provider` | `ApprovalExecutionBinding.Runtime.Adapter`, `DeliveryReceipt.Binding.Runtime.Adapter` | `RecordDelivery` (exact match), `validGateBindingMeta` (ValidateAdapterName), C2D delivery boundary (must equal `claude_headless`) | `TestRecordDelivery_RejectsAnyBindingFieldMismatch` |
| B4 | `Runtime.Version` | `ClaudeEntryConfig.AuthorityVersion` (e.g. `2.1.209`) | `approvalRecord.version` | `ApprovalExecutionBinding.Runtime.Version`, `DeliveryReceipt.Binding.Runtime.Version` | `RecordDelivery` (exact match), `validGateBindingMeta` (validVersion grammar), C2D delivery boundary (must equal `rt.authorityVersion` AND `certifiedClaudeAuthorityVersion`) | `TestRecordDelivery_RejectsAnyBindingFieldMismatch` |
| B5 | `Runtime.LaunchGen` | `ManagedClaudeService.gen` (monotonic epoch) | `approvalRecord.launchGen` | `ApprovalExecutionBinding.Runtime.LaunchGen`, `DeliveryReceipt.Binding.Runtime.LaunchGen` | `RecordDelivery` (exact match), C2D delivery boundary (must equal `rt.epoch`), `supersedeLocked` (stale if superseded) | `TestRecordDelivery_RejectsAnyBindingFieldMismatch` |
| B6 | `Runtime.StreamGen` | hard-coded `0` | `approvalRecord.streamGen` | `ApprovalExecutionBinding.Runtime.StreamGen`, `DeliveryReceipt.Binding.Runtime.StreamGen` | `RecordDelivery` (exact match), C2D delivery boundary (must equal `0`) | `TestRecordDelivery_RejectsAnyBindingFieldMismatch` |
| B7 | `ActionDigest` | `CanonicalAction.Digest()` = SHA-256 of SchemaVersion ‖ OptionID ‖ Kind ‖ InputType ‖ InputPlacement ‖ NormalizedInput ‖ DeliverySchemaVersion ‖ DeliveryPayloadDigest | NOT stored (recomputed by `ClaimForExecution`) | `ApprovalExecutionBinding.ActionDigest`, `DeliveryReceipt.Binding.ActionDigest` | `RecordDelivery` (exact match), `validGateBindingMeta` (isHex64), `ClaimForExecution` (AssertDigest check if supplied) | `TestRecordDelivery_RejectsAnyBindingFieldMismatch` |
| B8 | `PayloadDigest` | `payloadDigest(payload)` = SHA-256 of `"a1.payload.v1\x00" ‖ len ‖ payload` | NOT stored (recomputed by `ClaimForExecution`) | `ApprovalExecutionBinding.PayloadDigest`, `DeliveryReceipt.Binding.PayloadDigest` | `RecordDelivery` (exact match), `validGateBindingMeta` (isHex64), `Accept` (must equal `payloadDigest(req.Payload)`) | `TestRecordDelivery_SubstitutedPayloadCannotCommit` |
| B9 | `IdempotencyKey` | client-supplied (mobile), validated by `validCanonicalKey` | `sessionApprovals.idempotency` map key | `ApprovalExecutionBinding.IdempotencyKey`, `DeliveryReceipt.Binding.IdempotencyKey` | `RecordDelivery` (exact match), `validGateBindingMeta` (validCanonicalKey), `ClaimForExecution` (replay by key+binding+auth) | `TestRecordDelivery_RejectsAnyBindingFieldMismatch` |
| B10 | `OptionID` | stored option `opt.ID` selected by the mobile client at claim time | `interactionOptionView.id` | `ApprovalExecutionBinding.OptionID`, `DeliveryReceipt.Binding.OptionID` | `RecordDelivery` (exact match), `validGateBindingMeta` (len ≤ 512), C2D delivery boundary (`certifiedActionDecision[b.OptionID]` must exist) | `TestRecordDelivery_RejectsAnyBindingFieldMismatch` |
| B11 | `DeliverySchema` | `CanonicalAction.DeliverySchemaVersion` (from stored delivery material, empty for legacy) | `storedDeliveryMaterial.schemaVersion` | `ApprovalExecutionBinding.DeliverySchema`, `DeliveryReceipt.Binding.DeliverySchema` | `RecordDelivery` (exact match), `validGateBindingMeta` (len ≤ 64), C2D delivery boundary (must equal `claude.pretooluse.decision.v1`) | `TestRecordDelivery_RejectsAnyBindingFieldMismatch` |

### 2.2 Claude-specific runtime binding (C2D-B private coordinator)

These fields bridge the A1 binding to the concrete Claude resume target. They
are retained in a bounded immutable C2D-B private identity record keyed by
`ApprovalID` and exact `RuntimeRef`; they never enter the provider-neutral
Store or a public DTO.

| # | Field | Created at | Stored in | Compared at | Invalidation |
|---|---|---|---|---|---|
| C1 | Claude `session_id` | `PreToolUse.session_id` from hook bridge | immutable private identity record | deferred join, reservation, repeated PreToolUse, allow/deny witness | epoch replacement, stop, delete, exit, timeout |
| C2 | `tool_use_id` | `PreToolUse.tool_use_id` from hook bridge | immutable private identity record | deferred join, reservation, repeated PreToolUse, PostToolUse hook/deny projection | epoch replacement, stop, delete, exit, timeout |
| C3 | `tool_name` | `PreToolUse.tool_name` from hook bridge | immutable private identity record | deferred join, reservation, repeated PreToolUse, witness (first slice only `Bash`) | epoch replacement, stop, delete, exit, timeout |
| C4 | `input_digest` | SHA-256 of canonical JSON of `PreToolUse.tool_input` | immutable private identity record | deferred join, reservation, repeated PreToolUse; deny projection also binds its digest | epoch replacement, stop, delete, exit, timeout |
| C5 | Hook capability token (98 hex chars) | `newClaudeHookBridge` → `rand.Read(32)` → hex | `handleHook` (must match query param) | resume coordinator (must match the runtime's active bridge) | bridge close, terminate |
| C6 | Resume nonce | C2D-B: `rand.Read(16)` → hex, per-claim | C2D-B coordinator (must match entry, exactly one owner) | repeated PreToolUse hook (must match) | claim completion, timeout, cancellation |
| C7 | Resumed process identity | `execLauncherWithDir.LaunchInDir` for the resume invocation | C2D-B coordinator (epoch + authority version must match claim) | deny-result routing and the separately authenticated PostToolUse hook route | process exit, kill |

### 2.3 Delivery receipt binding

| # | Field | Created at | Compared at | Notes |
|---|---|---|---|---|
| R1 | `ReceiptID` | C2D-C: `newClaudeDeliveryReceiptID()` → `"cldr-" + hex` | `RecordDelivery` (must be non-empty for accepted) | provider-specific opaque ID; entropy failure → `DeliveryConflict` (ambiguous, non-retryable) |
| R2 | `DeliveredPayloadDigest` | `payloadDigest(delivered bytes)` | `RecordDelivery` (must equal `Binding.PayloadDigest`) | substituted bytes cannot commit |
| R3 | `ClaimToken` | copied from `DeliveryReceipt.ClaimToken` | `RecordDelivery` (must equal stored `rec.claimToken`) | claim ownership |
| R4 | `Binding` (all 8 fields) | copied from `DeliveryReceipt.Binding` | `RecordDelivery` (every field compared by `equal`) | full binding identity |
| R5 | Provider consumption witness identity | C2D-C: allow from the production-owned authenticated PostToolUse hook; deny from bounded `permission_denials` projection | `RecordDelivery` (NOT compared by the store — the C2D boundary proves it before returning `accepted`) | both require exact private identity + runtime match; stdout ordering alone is insufficient |

## 3. Provider state machine and linearization

### 3.1 Claude lifecycle (C0D-certified)

```text
INITIAL LAUNCH (C1D, already implemented):
  claude spawn (--settings <isolated>, -p <certified prompt>)
    → PreToolUse hook fires (session_id, tool_use_id, tool_name, tool_input)
    → bridge validates token + strict decode + identity extraction
    → rt.observePreToolUse(...) stores pending observation
    → hook returns defer
    → Claude emits stream-json: {type:"result", stop_reason:"tool_deferred",
       session_id, deferred_tool_use:{id, name, input}}
    → rt.joinDeferred matches the four-field identity
    → C1D: IngestObserved with Actionable=false, Options=nil, DeliveryMaterial=nil
    → Claude process exits (end_turn after defer)

C2D/C3D ACTIONABLE PATH:
  [controlled C2D composition only: the matched private identity is retained,
   and the test fixture creates Actionable=true + Options + DeliveryMaterial]
  → A1 Store creates pending Approval (production C2D remains non-actionable)
  → mobile authenticated decision (allow_once / deny)
  → A1 ClaimForExecution → claim token + ApprovalExecutionBinding + payload

  C2D-B RESUME COORDINATOR:
    → atomic reservation: one claim token → one resume nonce → one owner
    → state: observed_and_joined → decision_reserved → resume_started
    → claude spawn (--resume <session_id>, --settings <same isolated>,
       resume hook pointing at the same bridge with the nonce)
    → repeated PreToolUse fires (must match session_id, tool_use_id,
       tool_name, input_digest exactly)
    → coordinator validates full binding match
    → coordinator writes stored decision ONCE
    → state: repeated_pretooluse_matched → decision_written_once

  C2D-C WITNESS ROUTING:
    → allow: Claude executes tool → production-owned PostToolUse hook fires
              with the same session_id + tool_use_id
            → authenticated hook route validates the exact private identity
            → state: allow_witness → terminal
    → deny:  Claude emits permission_denials with same tool_use_id
            → coordinator observes permission_denials
            → state: deny_witness → terminal
    → bound DeliveryReceipt returned with ReceiptID + DeliveredPayloadDigest
```

### 3.2 Shared linearization point

The **C2D-B coordinator mutex** is the ONE provider-specific linearization
domain. Reservation, provider state transitions, and runtime invalidation all
linearize under it. Under that mutex, reservation:

1. Check: runtime exists, epoch matches, authority version matches, not terminated.
2. Check: no existing reservation for the same claim token (exactly one owner).
3. Check: no existing reservation for the same tool_use_id (exactly one pending resume).
4. Check: capacity not exhausted (bounded coordinator entries).
5. Create entry: `{claimToken, resumeNonce, claudeSessionID, toolUseID, toolName, inputDigest, decision, state: decision_reserved}`.
6. Return the nonce (or fail closed if any check fails).

All subsequent state transitions (resume_started, repeated_pretooluse_matched,
decision_written_once, allow_witness, deny_witness, terminal) and every
stop/delete/exit/epoch-replacement invalidation happen UNDER THE SAME MUTEX.
`atomicTerminated` is an early hint only; a check of it is not authority and
cannot replace the coordinator transition. No lock is held across process
spawn, hook IPC, provider I/O, Store calls, or process wait.

Lock ordering is: runtime lifecycle code snapshots the affected RuntimeRef,
releases `turnMu`, then enters the coordinator invalidation transition. The
coordinator never acquires `turnMu` while holding its mutex. External I/O uses a
generation-bound reservation snapshot; after I/O, completion must reacquire the
coordinator mutex and prove that the same entry, claim token, nonce, RuntimeRef,
and non-invalidated state are still current. A stale completion cannot publish
a witness or accepted receipt.

The repeated PreToolUse hook handler must:

1. Validate its token + nonce against the coordinator entry.
2. Strict-decode and validate the repeated PreToolUse fields against the entry.
3. Claim the write (exactly one write ever).
4. Return the stored decision.
5. Transition to `decision_written_once`.

A duplicate or mismatched repeated PreToolUse, a late hook after timeout, or a
hook arriving after the coordinator entry was already terminated all fail closed
(zero decision delivery; hook returns `defer`).

### 3.3 Closed state vocabulary

```text
observed_and_joined        # C1D observation exists (precondition)
decision_reserved          # coordinator entry created, nonce issued
resume_started             # Claude resume process spawned
repeated_pretooluse_matched # hook validated, decision ready to write
decision_written_once      # hook returned allow/deny exactly once
allow_witness              # PostToolUse result observed, matching tool_use_id
deny_witness               # permission_denials result observed, matching tool_use_id
terminal                   # receipt returned, entry cleaned up

// Non-success terminal states:
stale_epoch                # runtime epoch changed before reservation
duplicate_claim            # same claim token already reserved
capacity_exhausted         # coordinator entry slots full
resume_spawn_failed        # Claude --resume failed to start
resume_exited_early        # Claude exited before repeated PreToolUse
hook_timeout               # repeated PreToolUse didn't arrive within deadline
binding_mismatch           # repeated PreToolUse fields don't match entry
mismatch_reply             # Claude replied with wrong tool_use_id or tool_name
provider_cancelled         # Claude resolved/cancelled the deferred tool before resume
ambiguous_write            # write claimed but witness neither confirmed nor denied
runtime_stopped            # stop/delete while coordinator entry was active
```

## 4. C0D allow and deny consumed-decision witnesses

These are the ONLY accepted consumption witnesses. C0D live evidence proved both
paths with deterministic replay.

### 4.1 Allow witness

**Definition**: an invocation of the separately configured, production-owned
and capability-authenticated `PostToolUse` hook whose `session_id` and
`tool_use_id` exactly equal the private identity record, observed strictly AFTER
the resume PreToolUse hook delivered `allow` and the decision was written.

**C0D evidence**: R3 live probe used
`docs/a1_2_c0d_r5_evidence/harness/hook_posttool.sh`; it did not establish that
the resumed process stdout stream contains a PostToolUse event. The hook
received matching `tool_use_id` after the allow decision and tool execution.

**C2D acceptance criteria**:
- The resumed invocation uses the isolated settings that install both the
  PreToolUse decision hook and the PostToolUse witness hook.
- The PostToolUse route is authenticated to the exact bridge/runtime and strict
  decodes a closed, bounded schema.
- Its `session_id` and `tool_use_id` equal the immutable private identity.
- It is accepted only after `decision_written_once` for the same coordinator
  entry; its hook input proves the provider executed that invocation.

**Rejection**: missing/invalid capability, PostToolUse with a different
session/tool identity, witness before the decision write, duplicate/late hook,
or hook from an invalidated runtime. Stream-json ordering, command equality,
process exit, or side effects cannot substitute for this hook witness.

### 4.2 Deny witness

**Definition**: A `permission_denials` stream-json result containing an entry
where `tool_use_id` equals the bound `tool_use_id`, observed strictly AFTER the
resume hook delivered `deny`.

**C0D evidence**: R5 live probe — `{eventType: "result.permission_denials",
session_id, denied: {tool_name, tool_use_id}}` where `session_id` and
`tool_use_id` exactly match the deferred identity. Denial projection captured in
`denial_projection.json`.

**C2D acceptance criteria**:
- The stream-json output of the resumed Claude process contains a
  `permission_denials` event.
- The event's `session_id` matches the coordinator entry's session.
- At least one denied entry has `tool_use_id` equal to the entry's `toolUseID`.
- The event was observed AFTER `decision_written_once`.

**Rejection**: permission_denials with different `tool_use_id`, permission_denials
observed before the decision, session mismatch, no tool denied.

### 4.3 Non-witnesses (explicitly rejected)

The following are NOT consumption witnesses and must not be accepted as success:

- Queue admission (the hook returned the decision).
- Process exit (Claude exited after the decision).
- Absence of execution (no PostToolUse does NOT prove deny; the tool could have
  been skipped for other reasons).
- Hook response write (the resume hook's HTTP 200 is delivery, not consumption).
- Side effects (a file appeared/disappeared; a process ran).
- Terminal text or JSONL inference.
- Command string equality.
- Event ordering alone (the third event after resume is not necessarily ours).

## 5. Timeout, cancellation, exit, stop/delete, replacement and restart

### 5.1 Resume timeout

The C2D-B coordinator entry has a bounded lifetime (suggested: `30s`
for the repeated PreToolUse to arrive; `120s` total for the resume+decision
+consumption cycle, matching `claudeObservationTimeout`).

- Hook timeout: if repeated PreToolUse does not arrive within the deadline, the
  entry transitions to `hook_timeout` (non-success). The hook is removed.
- Total timeout: if the resumed Claude process does not produce a consumption
  witness within the deadline, the entry transitions to `ambiguous_write`
  (non-success, non-retryable). The process is killed.

### 5.2 Cancellation (provider-side resolution)

Claude may resolve a deferred tool use without a resume (e.g., the model
decides it doesn't need the tool after all). In C0D, this appeared as the
deferred result being resolved/cancelled.

- Before reservation: the coordinator entry cannot be created (no pending
  observation to match).
- After reservation, before resume spawn: the entry is removed, claim fails with
  `DeliveryUnavailable`.
- After resume spawn: the resumed Claude process exits without emitting
  repeated PreToolUse → `hook_timeout`.

### 5.3 Exit

When the managed Claude runtime exits (via `terminate()`):

1. `terminate()` sets `atomicTerminated = true`, `turnClosed = true`, clears
   `pendingObservations`, and calls `InstallRuntimeGeneration(epoch, 1, "terminated")`.
2. After releasing `turnMu`, lifecycle code invokes the coordinator's exact
   RuntimeRef invalidation transition. That transition linearizes under the
   coordinator mutex with reservation and witness publication.
3. Any active private identity/reservation entries for this runtime transition
   to `runtime_stopped`; subsequent hook calls deliver zero decision and stale
   external-I/O completion cannot publish a receipt.
4. The Store's `InstallRuntimeGeneration(StreamGen=1)` ensures any late
   `IngestObserved(StreamGen=0)` is rejected.

Checking `atomicTerminated` before a transition remains a useful fast-path but
is not a correctness boundary: check-then-act without the shared coordinator
transition is prohibited.

### 5.4 Stop/Delete

- **Stop**: `ManagedClaudeService.Stop` calls `rt.terminate()` → same as Exit.
- **Kill**: `ManagedClaudeService.Kill` calls `rt.terminate()` → same as Exit.
- **Delete**: `ManagedClaudeService.Delete` requires `rec.Exited`; then calls
  coordinator cleanup and `approvals.Clear(sessionID)`, which removes all
  in-memory records and tombstones.

### 5.5 Epoch replacement

If a new Claude runtime is created for the same session ID (new epoch), the
Store's `supersedeLocked` invalidates pending records and marks executing/
delivery_failed records as superseded. Runtime publication/replacement must
also invalidate the prior RuntimeRef under the coordinator mutex before the new
runtime can reserve a decision. Per-transition epoch checks supplement, but do
not replace, this linearized invalidation.

### 5.6 Daemon restart

**No pending or executing Claude authority is restored across daemon restart.**
The C2D-B coordinator is an in-memory structure. On restart:

- Pending observations are gone (the Claude process is dead).
- Coordinator entries are gone.
- `AuthoritativeApprovalStore` is in-memory; approval records, tombstones,
  claim tokens, committed records, and the idempotency ledger are all gone.
- An old ApprovalID/idempotency key has no authority after restart and resolves
  as not found/unavailable. It cannot return `already_accepted` or be retried.
- A new managed runtime must observe a fresh provider request and create a new
  Approval before any decision is possible.

Persisting or recovering pending/executing authority is explicitly outside
C2D. Restart zero-state is the required fail-closed behavior.

## 6. Capacity behavior and adversarial counterexamples

### 6.1 Capacity constants

| Resource | Bound | Enforcement | Overflow behavior |
|---|---|---|---|
| Managed Claude sessions | `maxClaudeSessions = 4` | `ManagedSessionRegistry` | `CreateDetached` returns error |
| Pending observations per runtime | `maxPendingClaudeObservations = 4` | `observePreToolUse` turnMu check | observation dropped, `rejects++` |
| Active (ingested) observations per runtime | `maxActiveApprovals = 4` | `joinDeferred` pre-ingest check | join dropped silently |
| C2D-B coordinator entries (global) | TBD (suggested: `maxActiveApprovals * maxClaudeSessions = 16`) | C2D-B reservation mutex | `DeliveryUnavailable` |
| Store sessions | `authMaxApprovalSessions = 1024` | `ingest` eviction check | evict oldest tombstone; fail if none |
| Store records per session | `authMaxApprovalsPerSession = 50` | `ingest` capacity check | item skipped |
| Idempotency keys per session | `authMaxIdempotencyKeys = 256` | `ClaimForExecution` check | `ClaimLedgerFull` |
| Idempotency key length | `maxIdempotencyKeyLen = 128` | `validCanonicalKey` | `ClaimInvalidKey` |
| Delivery material bytes | `maxDeliveryMaterialBytes = 4096` | `validGateBindingMeta` | `DeliveryRejected` |
| Delivery schema version bytes | `maxDeliverySchemaVerBytes = 64` | `validGateBindingMeta` | `DeliveryRejected` |

### 6.2 Adversarial counterexamples

For each critical invariant, one concrete counterexample that the tests must
prove the system rejects.

#### CE-1: Duplicate claim

**Invariant**: Exactly one owner for one runtime/tool-use/claim/resume nonce.

**Counterexample**: Mobile sends two concurrent POST requests with the same
idempotency key. The store's `ClaimForExecution` grants the first and returns
`ClaimAlreadyOwned` for the second. If the coordinator is reached (first claim),
the second claim's delivery attempt must find either:
- coordinator entry already exists for that claim token → `DeliveryConflict`, or
- claim token doesn't match any entry → `DeliveryRejected`.

**Test**: Two goroutines call `ClaimForExecution` with the same key, then both
attempt delivery. Exactly one succeeds or both fail non-success.

#### CE-2: Cross-session tool_use_id substitution

**Invariant**: A decision bound to session A cannot be consumed by session B.

**Counterexample**: Create two Claude runtimes (sessions A and B). Both observe
a PreToolUse. Ingest both. Claim the approval from session A. Attempt to resume
session B with session A's claim. The C2D-B coordinator must reject because
`claudeSessionID` in the coordinator entry (from session A's observation) does
not match the resume target (session B).

**Test**: Create two runtimes, observe both, claim A's approval, attempt delivery
against B's runtime → `DeliveryStaleRuntime` or `DeliveryRejected`.

#### CE-3: Mismatched input digest

**Invariant**: A decision for tool input X cannot execute tool input Y.

**Counterexample**: Observe PreToolUse for `echo hello`. Claim with allow_once.
Before resume, modify the hook script or the repeated PreToolUse payload so the
input is `echo evil`. The C2D-B coordinator must recompute the input digest from
the repeated PreToolUse and reject on mismatch.

**Test**: Valid observation, claim, then resume with altered tool_input →
`binding_mismatch`, hook returns `defer`.

#### CE-4: Late write after timeout

**Invariant**: A hook write after the coordinator timeout must not be accepted.

**Counterexample**: Set a short coordinator timeout. After it fires and the
entry is cleaned up, the hook belatedly receives a repeated PreToolUse and
tries to deliver the decision. The coordinator must reject (entry gone → no
owner → fail closed).

**Test**: Claim, set timeout to 1ms, wait for timeout, deliver repeated
PreToolUse → `hook_timeout` already consumed, hook returns `defer`.

#### CE-5: Stop during resume

**Invariant**: Stop/delete must defeat any in-flight coordinator entry.

**Counterexample**: Claim, spawn resume, while the repeated PreToolUse hook is
about to deliver, call `Stop`. The C2D-B coordinator must detect `rt.terminated`
and transition to `runtime_stopped`. The hook (if it still fires) finds the
entry gone and returns `defer`.

**Test**: Claim, start delivery, call `rt.terminate()` between resume spawn and
hook delivery → `DeliveryStaleRuntime`.

#### CE-6: Capacity exhaustion

**Invariant**: Capacity failure must reject before mutating canonical state.

**Counterexample**: Fill the coordinator to capacity, then attempt one more
claim delivery. Reservation must fail before coordinator mutation. Because the
A1 claim already owns the Store record, the delivery boundary must return a
bound `DeliveryUnavailable` receipt and call `RecordDelivery`, moving the record
to `ApprovalDeliveryFailed` before bounded manual retry is possible.

**Test**: Fill coordinator entries, attempt one more delivery →
`DeliveryUnavailable`; before `RecordDelivery`, replay is `ClaimAlreadyOwned`;
after `RecordDelivery`, same-key/same-binding/auth retry is `ClaimGranted` within
the frozen retry budget. Coordinator capacity and entries remain unchanged.

#### CE-7: Duplicate resume nonce

**Invariant**: One resume nonce = one write opportunity.

**Counterexample**: After the coordinator writes `allow` and transitions to
`decision_written_once`, the resumed Claude process restarts and fires
PreToolUse again with the same `tool_use_id`. The hook handler must find the
entry already in `decision_written_once` state and return `defer` (no second
write).

**Test**: Deterministic hook that fires twice → first returns `allow`, second
returns `defer`, coordinator state stays `decision_written_once`.

#### CE-8: Daemon restart loses pending authority

**Invariant**: After daemon restart, no pending coordinator entry survives.

**Counterexample**: Claim, create coordinator entry, kill the daemon (not
graceful shutdown), restart. Both coordinator and in-memory A1 Store are new and
empty. Replaying the old ApprovalID/key must not recreate or resolve authority.

**Test**: Claim, simulate daemon kill by reconstructing both service and Store,
attempt old ApprovalID/key → not found/unavailable and zero provider write. A
fresh runtime + fresh provider request creates a distinct ApprovalID before a
new claim can be granted.

#### CE-9: Orphan Approval without provider identity

**Invariant**: An ApprovalID without the immutable Claude identity record can
never target a provider invocation.

**Counterexample**: Create an actionable controlled-composition Store record,
then remove or substitute its private identity association before delivery.
The coordinator must reject before resume spawn and write zero decision.

**Test**: Valid A1 claim + missing/mismatched private identity →
`DeliveryRejected` or `DeliveryStaleRuntime`, zero process spawn, zero hook
write, bound non-success receipt recorded.

#### CE-10: False allow witness from process output

**Invariant**: Provider stdout or a fabricated PostToolUse-shaped JSON object is
not an allow consumption witness.

**Counterexample**: After decision write, inject matching `tool_use_id` text or
a PostToolUse-shaped event into resumed stdout without invoking the
capability-authenticated PostToolUse hook.

**Test**: stdout injection produces no `allow_witness`; missing/wrong-capability,
wrong-session, early, duplicate, and post-invalidation PostToolUse hooks all
produce non-success. Only the exact authenticated hook transition can publish
the allow witness.

## 7. Public/private data classification

### 7.1 Private (never enters any public DTO, log, or mobile projection)

| Data | Reason |
|---|---|
| `PreToolUse.tool_input` (raw bytes) | Contains the full Bash command — may include secrets, tokens, paths |
| Canonical tool input digest (`inputDigest`) | Derived from private input; while it's a digest, revealing it enables offline correlation |
| Hook capability token | 98-char hex token — possession grants hook access for the runtime |
| `claudePendingObservation.inputDigest` | Same as tool input digest |
| `ApprovalDeliveryMaterial.ResponseBytes` | The exact daemon-generated decision response — never shown to the mobile user |
| `ClaimRequest.Requester` fields (DeviceID, HostID, BearerSessionID, BootID, PermDigest) | Server-side authentication context |
| `ClaimResult.Token` (claim token) | 32-char hex — possession grants delivery authority |
| C2D-B resume nonce | 32-char hex — one-shot write capability |
| C2D-B coordinator entry state | Internal state machine — exposing it leaks timing/ordering information |
| Claude `session_id` | Internal Claude session identity — not meaningful to the mobile user |
| Delivery payload bytes | The EXACT bytes written to the hook — never projected |
| `DeliveredPayloadDigest` | While a digest, coupled with the binding it reveals delivery identity |
| `DeliveryReceipt.ReceiptID` | Opaque receipt identifier — internal only |
| Raw stream-json from Claude | Contains full model output, prompts, tool results |

### 7.2 Public (safe for mobile DTOs)

| Data | Safety justification |
|---|---|
| `ApprovalID` (`claude-<hex>`) | Opaque identifier, no embedded secrets |
| `SessionID` (`claude_headless:claude-<nonce>`) | Session identity already visible in session list |
| Provider name (`claude`) | Static metadata |
| Version (`2.1.209`) | Static metadata |
| `ApprovalState` (pending/approved/rejected/expired) | Status display |
| `Actionable` boolean | Display gating |
| Options (`allow_once` / `deny` labels) | Static action labels |
| Tool name (e.g. `Bash`) | Certified tool identity — needed for safe review projection |
| Safe review projection (§7.3) | Only a verifier-certified complete canonical representation; otherwise omitted and non-actionable |

### 7.3 Safe review projection (C2D-D)

The frozen public A1 DTO must carry enough information for the user to identify
the action whose digest is claimed. For the certified `Bash` tool, the
projection must:

- Identify the tool as `Bash` (the certified tool name).
- Show a complete, bounded, deterministic canonical command representation for
  a verifier-certified subset, or show no CTA.
- NOT show the raw tool input (may contain secrets, tokens, paths).
- NOT show the hook capability token, claim token, or delivery payload.
- NOT show the Claude session_id or other internal identities.

If truncation or redaction could hide execution meaning (e.g., a very long
command truncated to show only the harmless prefix), the request is
non-actionable. The safe review projection must be fail-closed.

**Feasibility assessment**: not yet proven. A provider/model-supplied
`description` is advisory and can misrepresent the command; it is never review
authority. Truncation, path elision, or secret redaction can change or conceal
execution meaning and therefore cannot produce an actionable projection.

C2D-D may certify only a strict subset for which the complete canonical command
and every execution-relevant argument fit the frozen public bounds without
loss, secret/path disclosure, or ambiguous normalization. If it cannot do so,
all Bash observations remain non-actionable and C2D reports BLOCKED rather than
weakening the display contract. A digest without the full reviewable action is
also insufficient.

## 8. Explicit non-goals

1. **C0H/C0R implementation**: `PermissionRequest` and `--permission-prompt-tool`
   remain BLOCKED. Only the C0D-certified `PreToolUse` defer/resume lifecycle is
   in scope.

2. **Generic provider/hook/plugin SDK**: This is a single Claude-specific delivery
   boundary. No abstraction over providers.

3. **Multiple parallel tool approvals**: Only one tool approval per Claude runtime
   is in scope for the first slice.

4. **Automatic approval policy or allow-always**: Every decision requires an
   authenticated mobile claim.

5. **Claude Agent SDK or Channels**: The managed Claude invocation uses direct
   `claude` CLI spawn with isolated settings.

6. **Interactive PTY/key injection approval**: Approval is delivered through the
   hook bridge, not through terminal input.

7. **Terminal prompt parsing, synthetic keys, or side-effect authority**: Only the
   hook bridge, authenticated PostToolUse hook (allow), and exact deny projection
   are authoritative.

8. **Persistence of pending authority across daemon restart**: No coordinator entry
   or pending observation survives restart.

9. **Multiple Claude versions in the same slice**: Only Claude Code 2.1.209 with
   the pinned SHA-256 digest is supported.

10. **Non-Bash tools**: Only `Bash` is a candidate in the first slice. Every other
    tool (`Read`, `Write`, `Edit`, `Glob`, `Grep`, etc.) remains non-actionable
    until separately certified.

11. **Mobile CTA or action route**: No mobile production change is authorized until
    C3D.

12. **Production installation**: C2D builds and tests the delivery boundary through
    controlled production-composition tests only. The boundary is NOT wired into
    `app.go` handlers. `provenActionMapping` remains empty for Claude.

13. **N1, O1/O2, Executor-Verifier, tmux/cmux cleanup, CLI redesign, cloud relay,
    Windows**: Out of scope.

## 9. C2D staged checkpoint summary

| Checkpoint | Scope | Deliverable |
|---|---|---|
| **C2D-A** (this note) | Contract and code-path audit | This document — binding table, state machine, witnesses, counterexamples, data classification |
| **C2D-B** | Private one-shot resume coordinator | Claude-specific in-memory coordinator with the §3.2 state machine and §5 failure modes |
| **C2D-C** | Uninstalled Claude ApprovalDelivery | `ApprovalDelivery` implementation using C2D-B coordinator, with C0D witness routing, returning `DeliveryReceipt` |
| **C2D-D** | Safe review feasibility + final evidence | Either a verifier-certified complete bounded Bash subset, or an honest BLOCKED result; controlled composition tests and updated evidence report |

Each checkpoint requires independent verification before the next begins.
C2D-B remains prohibited until this amended C2D-A contract is independently
accepted. C3D remains prohibited until independent C2D acceptance.

## 10. Code-path audit: C1D → C2D delta

### 10.1 What C1D provides (frozen, must not regress)

- `ManagedClaudeService.CreateDetached`: spawn, certify, hook bridge, Store reservation, pump.
- `claudeHookBridge.handleHook`: capability token auth, strict PreToolUse decode, `observePreToolUse`.
- `claudeManagedRuntime.observePreToolUse`: pending observation storage, capacity check, duplicate rejection.
- `claudeManagedRuntime.joinDeferred`: four-field identity match, `IngestObserved(non-actionable)`.
- `claudeManagedRuntime.terminate`: bridge close, process kill, hook dir cleanup, Store tombstone, registry mark.
- 39 existing Claude tests + 15 counterexample tests, 10x race PASS.

### 10.2 What C2D-B must add (next checkpoint)

1. **Private identity + resume coordinator**: bounded immutable identity records
   keyed by ApprovalID + exact RuntimeRef, and bounded active resume entries
   under one mutex. Modify deferred join to retain the four matched provider
   fields; failed admission and lifecycle invalidation remove them.
2. **Resume hook mode**: extend `claudeHookBridge` or create a parallel resume
   bridge that can return `allow`/`deny` instead of only `defer`.
3. **Repeated PreToolUse validation**: validate session_id, tool_use_id, tool_name,
   input_digest against the coordinator entry. Fail closed on mismatch.
4. **One-shot write**: exactly one hook response carries the decision. Subsequent
   hooks for the same tool_use_id return `defer`.
5. **Claude resume spawn**: `--resume <session_id>` with the same isolated settings
   and a resume hook script.
6. **Coordinator cleanup**: stop/delete/exit/epoch replacement share the
   coordinator linearization point; timeout and cancellation are terminal.
7. **Deterministic tests**: all adversarial counterexamples from §6.2, plus state
   inspection after reservation, during resume, after write, after witness routing.

### 10.3 What C2D-C must add

1. **`ClaudeManagedApprovalDelivery` struct**: implements `ApprovalDelivery`.
2. **`Deliver` method**: validates `RuntimeRef{Adapter: "claude_headless",
   StreamGen: 0}`, validates `DeliverySchema == "claude.pretooluse.decision.v1"`,
   validates `certifiedActionDecision[OptionID]` exists, resolves the live runtime,
   delegates to C2D-B coordinator, waits for consumption witness, returns
   `DeliveryReceipt`.
3. **Witness routing**: allow uses the authenticated production-owned
   PostToolUse hook; deny parses the bounded permission_denials projection. Both
   require exact private invocation and RuntimeRef identity.
4. **Controlled production-composition tests**: wire the delivery boundary with
   the frozen A1 store and redacted C0D fixtures. Prove all interleavings
   (duplicate decision, allow-vs-deny, stop/delete, epoch replacement, timeout,
   malformed output, result-before/while/after-decision, cross-session
   substitution).
5. **Production composition**: the boundary is NOT installed in `app.go`.
   `Handlers.ApprovalDelivery` stays the capacity-0 gate for Claude.

### 10.4 Files that C2D will touch (audit baseline)

```
companion-daemon/internal/term/
  managed_claude.go           # C1D: READ-ONLY (add coordinator field + resume mode)
  managed_claude_test.go      # C1D: ADD C2D-B coordinator tests
  claude_hook_bridge.go       # C1D: EXTEND (resume decision + PostToolUse witness modes)
  claude_hook_bridge_test.go  # NEW: resume and PostToolUse hook tests
  claude_resume_coordinator.go       # NEW: C2D-B coordinator
  claude_resume_coordinator_test.go  # NEW: C2D-B tests
  claude_approval_delivery.go        # NEW: C2D-C delivery boundary
  claude_approval_delivery_test.go   # NEW: C2D-C tests

companion-daemon/cmd/devremote/
  app.go                      # C1D: READ-ONLY (no Claude delivery installation)
  c2d_composition_test.go     # NEW: controlled composition tests

companion-daemon/docs/
  A1_2_C2D_PACKET_CONTRACT_NOTE.md   # THIS FILE
  A1_2_C2D_EVIDENCE_REPORT.md        # NEW: C2D-D final evidence
```

No mobile, Codex, or provider-neutral A1 files are modified.

## 11. Verification checklist (for independent C2D-A review)

- [ ] Every field in §2.1 (B1–B11) and §2.2 (C1–C7) has a single creation point,
  a single storage location, and is compared at every boundary.
- [ ] No field is inferred from ordering, timing, command equality, terminal
  output, or caller-supplied authority.
- [ ] The state machine in §3.1 matches the C0D live evidence (defer→resume→
  match→decision→witness).
- [ ] The shared linearization point in §3.2 is a single mutex; no lock is held
  across process spawn, hook IPC, or provider I/O.
- [ ] C0D allow and deny witnesses (§4) preserve their proven asymmetric
  sources: authenticated PostToolUse hook for allow, bounded
  permission_denials projection for deny.
- [ ] Non-witnesses (§4.3) are explicitly rejected.
- [ ] Every failure mode in §5 has a defined terminal state and recovery path.
- [ ] Every adversarial counterexample in §6.2 targets one invariant and has a
  concrete test scenario.
- [ ] Public/private data classification (§7) covers every field in the binding
  tables.
- [ ] Safe review projection (§7.3) never trusts provider description or lossy
  truncation/redaction; unproven reviewability remains non-actionable.
- [ ] Explicit non-goals (§8) match the frozen C0D/C1D boundaries and the plan's
  hard exclusions.
- [ ] Code-path audit (§10) identifies every file that C2D will touch and
  confirms no mobile, Codex, or provider-neutral files are modified.
- [ ] The contract does not depend on macOS-specific attestation beyond what
  C1D already provides (pinned digest + realpath check).

## 12. C2D-A independent-review amendments

This revision closes the material mismatches found at
`7cd68e99909ac9aa361987f2af7a47e1bbf86ad2`:

1. Requires a bounded immutable private Claude identity record because C1D
   currently deletes pending identity after deferred join.
2. Restores the evidence-proven asymmetric witnesses: authenticated PostToolUse
   hook for allow and permission_denials projection for deny.
3. Corrects restart semantics to an empty in-memory Store with no restored
   approval or idempotency authority.
4. Places reservation and lifecycle invalidation in one coordinator
   linearization domain; atomic flags are hints, not authority.
5. Corrects capacity failure to flow through a bound non-success receipt and
   `RecordDelivery` before bounded retry.
6. Prohibits provider description and lossy truncation/redaction from creating
   actionable review evidence.
7. Keeps production C2D non-actionable; only controlled composition may exercise
   the delivery contract before C3D.
