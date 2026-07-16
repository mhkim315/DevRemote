# A1.2 C2D-C Remediation 3 Handoff

Status: **C2D-C REJECTED — no successful exact-claim provider path**  
Reviewed implementation: `4e38933b698ede578e0be69454dfe829237c9e23`  
Previous verifier packet: `6afc5e0abd9f80d04c6ffe82ab707fef03f4ba81`  
Accepted prerequisite: C2D-B `27bd45162feb6bdf6fec52fa44192208c8276e32`  
Canonical checkout: `/Users/mhk/Documents/codex/DevRemote`  
Branch: `feature/phase10-multi-adapter`

> **Superseded:** implementation `bc7145f529dc205e74a61c5c57bb000192d59e5e`
> added claim-owned completion but again omitted the required accepted+committed
> allow/deny composition. Continue only from
> `NEXT_EXECUTOR_A1_2_C2D_C_REMEDIATION_4_HANDOFF.md`.

This packet replaces the prior executor packet. Complete R3-A through R3-C in
one implementation review request. C2D-D/C3D and production activation remain
prohibited.

## 1. Decisive rejection findings

### B1 — delivery success is inferred from global identity count

`ClaudeManagedApprovalDelivery.Deliver` polls `coord.identityCount() == 0` and
then returns `DeliveryAccepted`. It does not wait for or inspect a terminal
result belonging to its claim token.

Counterexample: runtime replacement, stop, delete, expiry, or unrelated cleanup
removes the identity while no provider witness exists. `Deliver` observes zero
identities and returns success.

Affected code:

- `companion-daemon/internal/term/claude_approval_delivery.go:117-150`

Global counts are diagnostic only. They must never be success authority.

### B2 — both named positive composition tests prove only timeout

`TestClaudeDelivery_CompositionAllow` and `...Deny` use a fake process that
never invokes either hook. Both expect `DeliveryConflict`; neither verifies a
decision response, witness, accepted receipt, or `RecordDelivery` commit.

Affected code:

- `companion-daemon/cmd/devremote/claude_delivery_composition_test.go:62-151`
- `companion-daemon/cmd/devremote/claude_delivery_composition_test.go:153-222`

The tests execute by name, but they do not satisfy the positive acceptance
contract.

### B3 — successful receipt cannot commit

The Store payload/digest is created from
`{"permissionDecision":"allow|deny"}`. The hook writes the full
`hookSpecificOutput` response. `Deliver` returns the digest of the latter while
the immutable binding contains the digest of the former. `RecordDelivery`
requires exact equality, so every nominal successful receipt is rejected.

Affected code:

- `companion-daemon/cmd/devremote/claude_delivery_composition_test.go:94-98`
- `companion-daemon/internal/term/claude_approval_delivery.go:142-150`
- `companion-daemon/internal/term/approval_store_gen.go:789-795`

### B4 — allow witness is bound to the wrong runtime generation

The stored approval identity uses the original runtime epoch. Resume increments
the service generation, and `handlePostTool` constructs a RuntimeRef from that
new resume epoch. `MarkWitnessed` requires exact equality with the stored
RuntimeRef, so the allow witness cannot succeed.

The resume runtime also has no canonical managed session ID assigned and is not
registered as the original runtime. Provider-attempt identity and approval
target RuntimeRef are separate concepts and must not be conflated.

Affected code:

- `companion-daemon/internal/term/managed_claude.go:756-804`
- `companion-daemon/internal/term/claude_hook_bridge.go:364-371`
- `companion-daemon/internal/term/claude_resume_coordinator.go:656-663`

### B5 — deny consumption routing is absent

The resumed runtime pump only recognizes `tool_deferred`. No bounded
`permission_denials` decoder or call to
`MarkWitnessed(WitnessPermissionDenials, ...)` exists. Deny therefore always
times out.

Affected code:

- `companion-daemon/internal/term/managed_claude.go:307-351`

### B6 — hook response acceptance is not verified

`handleResume` ignores the byte count and error returned by `w.Write`, then
unconditionally calls `ConfirmWrite(..., true)`. A short or failed write may be
recorded as written.

Affected code:

- `companion-daemon/internal/term/claude_hook_bridge.go:304-311`

### B7 — launch and cleanup races remain hidden

The resume process starts before `bridge.rt` and its coordinator context are
installed. A fast provider hook can arrive during that window and receive
`defer`. `Deliver` also uses a polling sleep rather than the coordinator's
claim-owned state transition, and a nominal success path does not explicitly
terminate/reap the resume attempt.

## 2. Required pre-implementation red tests

Before changing implementation, add the following deterministic tests and
confirm that they fail on `4e38933` for the stated reason:

1. `TestClaudeDelivery_CompositionAllowAccepted`:
   - real service and real hook HTTP bridge;
   - fake external Claude process posts repeated PreToolUse;
   - asserts exact allow response bytes;
   - posts matching authenticated PostToolUse;
   - requires `DeliveryAccepted` and `RecordDelivery.Committed == true`.
2. `TestClaudeDelivery_CompositionDenyAccepted`:
   - repeated PreToolUse receives exact deny response;
   - fake resumed process emits the exact bounded `permission_denials` event;
   - requires accepted receipt and Store commit.
3. `TestClaudeDelivery_RuntimeInvalidationIsNotSuccess`:
   remove the current identity/runtime without a witness; delivery must return
   stale/conflict, never accepted.
4. `TestClaudeDelivery_UnrelatedIdentityRemovalIsNotSuccess`:
   exercise two claims and prove one claim's cleanup cannot complete the other.
5. `TestClaudeDelivery_ResponseWriteFailureCannotConfirm`:
   deterministic short/error writer; zero success and no commit.
6. `TestClaudeDelivery_ReceiptDigestIsExactWrittenBytes`:
   Store payload, HTTP bytes, receipt digest, and binding digest must all be the
   same canonical object.
7. `TestClaudeDelivery_HookArrivalAtSpawnBarrier`:
   the hook reaches the server at the earliest allowed launch barrier and still
   sees a fully installed immutable resume context.

Use channels/barriers only. No sleeps, polling, or count-based success checks.

## 3. R3-A — exact claim-owned terminal result

Refactor the coordinator so each reservation has a claim-owned completion
handle. The shape may differ, but it must provide equivalent semantics:

```text
ReserveEntry -> ResumeHandle{claim, nonce, completion}
completion waits for exactly one terminal result:
  witnessed(binding, exactResponseDigest)
  cancelled
  stale_runtime
  response_ambiguous
  timeout
  rejected
```

Rules:

- `ConfirmWrite(true)` is an intermediate state, not terminal success.
- Only `MarkWitnessed` may publish witnessed success.
- lifecycle invalidation publishes a non-success terminal result before removing
  internal state;
- completion is bound to the exact claim token and cannot be satisfied by map
  size, another approval, or another runtime;
- Store binding returned on success is the coordinator's defensive stored copy;
- terminal state releases live capacity deterministically while retaining only
  a bounded replay tombstone when required;
- no lock is held while waiting.

Remove `identityCount()` polling from delivery. `Deliver` must select on the
exact completion and timeout/cancellation context.

## 4. R3-B — immutable resume context and canonical provider bytes

Install an immutable resume context in the bridge **before process spawn**:

- coordinator;
- claim token and resume nonce;
- original approval target RuntimeRef;
- POKIT session ID;
- Claude session/tool ID, tool name, and input digest;
- expected decision/witness type;
- exact canonical response bytes or their coordinator-owned derivation.

The resume child/process attempt may have its own internal identity, but it must
not replace or impersonate the approval target RuntimeRef. Witness validation
uses the original bound RuntimeRef plus the separately bound resume nonce/claim
ownership.

Freeze one response encoding:

```json
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}
```

and the corresponding deny value. The Store delivery material must contain
those exact bytes. Before reservation, require:

- `bytes.Equal(req.Payload, canonicalResponseFor(storedDecision))`;
- `payloadDigest(req.Payload) == binding.PayloadDigest`.

The HTTP handler must write those exact bytes from `WriteHandle`, verify
`n == len(response)` and `err == nil`, and call `ConfirmWrite(true)` only then.
Any possible partial write is ambiguous and non-retryable.

Do not pass claim/nonce/capability secrets in publicly observable DTOs or logs.
Keep hook files private and bounded.

Implement a dedicated strict PostToolUse decoder; do not reuse a PreToolUse
allowlist by assumption. Implement a dedicated bounded deny-result decoder for
the C0D-certified `permission_denials` shape. Both route through the immutable
resume context and exact claim.

Terminate and reap every resume attempt on success, failure, timeout, stop,
delete, replacement, and shutdown without invalidating an unrelated/newer
runtime.

## 5. R3-C — real controlled composition acceptance

The fake external process is permitted only at the final executable boundary.
It must actively emulate the certified provider exchange:

1. launcher captures exact executable, argv, cwd, and isolated settings;
2. test releases a deterministic post-context/pre-spawn barrier;
3. fake process invokes the actual `/resume` hook with strict repeated
   PreToolUse JSON;
4. test captures and verifies exact HTTP response bytes;
5. allow fake invokes actual authenticated `/posttool` route;
6. deny fake writes the actual bounded denial event into resumed stdout;
7. real pump/bridge/coordinator produce the terminal claim result;
8. real delivery returns the bound receipt;
9. real Store `RecordDelivery` commits.

Mandatory adversarial cases, in addition to the seven red tests:

- wrong/missing capability, claim, nonce, session, tool identity, input digest,
  RuntimeRef, schema, option, and payload;
- witness before response confirmation, duplicate and late witness;
- cross-session/cross-claim substitution;
- stop/delete/exit/replacement at context-install, spawn, hook-write, and witness
  barriers;
- resume launch failure, malformed denial/PostToolUse, capacity exhaustion,
  timeout, and daemon restart;
- all failures leave no live entry, no leaked process/hook directory, and no
  successful Store state.

The positive tests must not expect timeout or conflict. A test named Allow or
Deny is a positive gate only when it proves accepted receipt and Store commit.

## 6. Scope and gate

Do not export coordinator internals solely for black-box composition tests unless
production composition genuinely needs that API. Prefer package-owned test
harnesses or bounded production methods.

Do not wire the delivery into `app.go`; production actionability remains zero.
Do not run live model turns.

After R3-A/B/C, produce a self-audit mapping every finding B1-B7 to exact code
and non-vacuous tests. Then freeze HEAD and run:

```sh
cd companion-daemon
go test -race ./internal/term -run 'Claude.*(Delivery|Resume|Hook|Coordinator)' -count=5
go test -race ./cmd/devremote -run 'Claude.*(Delivery|Composition)' -count=3 -v
go test -race ./internal/term ./cmd/devremote -count=1
go build ./...
go vet ./...
git diff --check
```

The named composition allow and deny tests must show accepted + committed, not
expected timeout. If that cannot be proven, report `C2D-C BLOCKED` and stop.

Freeze and gate the report HEAD, push, and stop with:

```text
REVIEW REQUEST: A1.2 C2D-C Remediation 3 — <implementation SHA>
```

C2D-D/C3D remain prohibited until independent acceptance.
