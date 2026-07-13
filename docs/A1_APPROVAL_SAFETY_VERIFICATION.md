# A1 Approval Safety — Independent Verification

Verdict: **REJECT — focused remediation required**

Reviewed report HEAD:
`706750ec46ddc76fbabf15c66013e2e920505513`

Reviewed implementation:
`3b56c4f05d16653b5fd9c494d8f1682135701603`

Accepted S1.1 ancestor:
`02c8385e3270fbbc4df45e0c71ccad6ebe11a076`

## 1. Repository and gate evidence

- canonical remote and branch: `https://github.com/mhkim315/DevRemote.git`,
  `feature/phase10-multi-adapter`;
- reviewed local/remote equality at `706750e`: PASS;
- accepted S1.1 ancestry: PASS;
- reviewed worktree: clean;
- focused `go test -race ./internal/term -count=1`: PASS when re-run outside
  the filesystem/network sandbox;
- full backend build/vet/race/diff and mobile TypeScript/Jest: PASS;
- Android Kotlin compile could not run in the clean verification worktree because
  its environment-local `gradlew` was absent. No A1 native files changed.

The green implemented tests do not cover the authority boundaries below. This is
a structural correctness rejection, not a claim that the existing tests fail.

## 2. Preserved work that should not be redesigned

The implementation correctly:

- removes legacy parser approval creation from production authority;
- gates accepted-adapter detection on `CapApprovalDetection` and authoritative
  source-event provenance;
- keeps `waiting_approval` display-only;
- preserves launch/stream identity on stored records and rejects stale ingestion;
- uses the host-bound paired-device transport when device auth is installed;
- bounds request bodies and rejects unknown JSON fields;
- fails Codex approve closed instead of synthesizing blind `y/n` input;
- keeps Claude/no-capability evidence non-actionable;
- passes existing backend race and mobile suites.

Remediation must retain this accepted foundation and change only the missing A1
claim, delivery, action, lifecycle-race, DTO, and auth boundaries.

## 3. Blocking findings

### B1 — `Reserve` is not the required atomic full-binding claim

`AuthoritativeApprovalStore.Reserve(sessionID, approvalID)` atomically checks only
record existence, expiry, and pending state. Adapter/provider/version,
LaunchGeneration, StreamGeneration, selected ActionDigest, requester context,
permission, and idempotency are checked outside it or not checked at all.
`HandleApprovalAction` performs `LookupRecord`, principal/option checks, a launch-
only check, and only then calls `Reserve`.

This preserves the prohibited check-then-claim window and supplies no opaque
exclusive claim token. A runtime or authorization change between those steps can
still create an execution owner.

Required correction: one linearizable `ClaimForExecution(ClaimRequest)` must
validate the entire authoritative binding and return an opaque claim token.
Claim must be serialized against runtime replacement, stream change,
delete/unlink/termination, and competing approve/reject. Separate pre-claim
lookup remains display-only and carries no execution authority.

### B2 — selected action and requester are not cryptographically bound

The stored `ActionDigest` is `digestOptions` over the whole option set. It omits
the selected action, schema version, normalized arguments/user input, and input
type/placement as one canonical selected action. The request has no idempotency
key. Claim does not bind DeviceID, HostID, BearerSessionID, boot/auth context, or
permission set; the handler only checks that the principal currently contains one
permission before `Reserve`.

Required correction: implement the frozen canonical ActionDigest and
same-key/same-digest idempotency contract. Requester identity must be derived
server-side and captured in the claim; client identity/runtime fields are rejected.

### B3 — generic overwrite queue is still treated as delivery authority

For neutral input, `HandleApprovalAction` calls `h.Cmds.Put` and immediately
commits success. `CommandBroker.Put` is a session-level overwrite map with no
ApprovalID, runtime identity, ActionDigest, idempotency key, or receipt. A later
generic command can overwrite the claimed action before consumption.

There is no approval-specific `DeliveryReceipt`, and the implemented outcome
vocabulary does not contain the required `accepted`, `already_accepted`,
`stale_runtime`, `runtime_mismatch`, `unavailable`, `conflict`, and `rejected`
semantics.

Required correction: introduce the dedicated approval-action delivery boundary
from the authoritative plan. Only a bound `accepted`/`already_accepted` receipt
may permit terminal success. Generic `CommandBroker.Put` cannot implement it.

### B4 — lifecycle replacement during execution can still produce stale success

Runtime identity is checked only before `Reserve`, and only LaunchGeneration is
checked. There is no current StreamGeneration, adapter/provider/version, or
correlation revalidation in the atomic claim. `InvalidateSession` invalidates
pending records only; executing records intentionally remain live. `Clear`
deletes a record, but the handler ignores the return value of `Commit` and still
returns `OutcomeOK` after delivery.

Thus replacement/delete/unlink/termination between reserve and delivery/commit
is not linearized and can deliver to the wrong runtime or return false success.

Required correction: bind delivery to the claimed RuntimeRef and make the
delivery boundary reject stale/mismatched runtime. Commit must consume the exact
claim token and receipt, and a failed/missing commit cannot return success.
Deterministic hooks/race tests must prove every lifecycle interleaving.

### B5 — unproven Codex actions are exposed as actionable buttons

`frozenApprovalOptions("codex")` fabricates Approve/Reject options even though the
implementation report confirms no accepted Codex delivery channel exists.
Approve always becomes `delivery_failed`; Reject is committed locally without
agent delivery. Mobile still renders both buttons.

This conflicts with the frozen rule: an unproven evidence-to-action-to-delivery
mapping is non-actionable display only, with no buttons and no claim path.

Required correction: either provide controlled, redacted evidence for a real
accepted Codex resolution channel and an approval-specific receipt, or expose the
event only as bounded non-actionable intervention information. Do not claim A1
complete while no provider action can traverse the required positive production
path.

### B6 — public DTO is bounded but not the frozen safe allowlist

The public/mobile DTO still accepts and renders `prompt`, option `payload`,
arbitrary labels/placeholders, `source`, confidence, and other provider-shaped
fields. Prompt is allowed up to 4096 characters and rendered directly. This is a
size bound, not the required redacted structural projection. Internal delivery
payloads remain in the public option type.

Required correction: create the safe public DTO from the plan: Pokit-owned or
verified bounded summary/labels, safe option ID, required-input metadata, expiry,
and non-sensitive state only. Raw provider prompt/payload/command/path/token,
claim token, and digest material remain server-side. Backend and mobile must
enforce the same versioned closed shape and UTF-8 byte limits.

### B7 — retry/auth semantics do not match the frozen contract

`delivery_failed` is terminal and cannot be retried with the original key because
no idempotency key exists. Mobile sends no idempotency key and its success decoder
only accepts `ok`. When device auth is absent, `apiPost` falls back to the legacy
token path; tests explicitly preserve that positive fallback. The handler permits
insecure-local execution without any requester principal binding.

Required correction: support bounded manual same-key retry through the approval-
specific boundary, expose `already_accepted`/`conflict` honestly, and require a
server-derived authenticated requester for an authoritative action. The remote
mobile approval path must fail closed without host-bound device auth; local dev
behavior must be explicitly non-authoritative or use a separately authenticated
server context.

### B8 — mandatory acceptance matrix is incomplete

Existing tests do not prove full-binding atomic claim, action substitution,
same-key idempotency, approve-vs-reject ownership, stream/adapter/provider
mismatch at claim, delete/unlink/termination during claim, runtime replacement
between claim and receipt, generic overwrite isolation, safe DTO redaction, or a
positive accepted-provider receipt path.

Required correction: implement all 25 items in
`docs/A1_APPROVAL_SAFETY_PLAN.md` with production-path and race-enabled tests.

## 4. Scope decision

A1 remains the correct milestone. No N1/O1/O2, Task/Dispatch, worker completion,
automatic policy, CLI redesign, ConPTY/Windows, cloud relay, or generic command
work is authorized. Worker execution acknowledgement remains O1; A1 needs only
exact once-only daemon-boundary acceptance.

## 5. Re-verification gate

Re-review must use one stable final implementation SHA and require:

- all B1-B8 corrections;
- the authoritative 25-item A1 acceptance gate;
- focused claim/delivery/lifecycle race suites;
- full backend build/vet/race and mobile TypeScript/Jest;
- Android native compile when the repository environment supplies its wrapper;
- invariant, secret, ID-inference, and diff checks;
- accepted S1.1 ancestry, local/remote equality, and clean worktree.

N1 remains blocked until independent A1 ACCEPT.
