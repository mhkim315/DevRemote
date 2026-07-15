# SP1 Native Approval — Corrected Authority Contract

Status: **AUTHORITATIVE PLAN; IMPLEMENTATION NOT STARTED**

Baseline: `62652633eb56577f0308973f7581ac94498f19fe` (independently accepted
SP0.5 report HEAD). This note supersedes the incorrect first draft in
`a41ebc2e49e6579a8d255441a42a681465f93ddd`.

SP1 is authority and concurrency work. Each implementation packet stops for
independent verification. An intermediate checkpoint is permission to begin the
next packet, not SP1 acceptance.

## 1. Frozen boundary and owners

- `codexManagedRuntime.pump` remains the only app-server stdout reader.
- `AuthoritativeApprovalStore` remains the sole owner of ingestion, claim,
  requester authority, idempotency, expiry, supersession and commit.
- `HandleApprovalAction` remains the only mobile approval-write entry.
- Codex-specific code may project a provider request and deliver a certified
  provider response. It may not create a second claim, ledger or approval store.
- `waiting_approval`, PTY text, screen content, JSONL fallback and generic terminal
  input never create actionable authority.
- Production capacity remains zero until the complete exact-response and
  resolved-consumption path is installed for the current certified runtime.

## 2. Correct provider wire identity

For pinned Codex `0.144.1`, `item/commandExecution/requestApproval` is a JSON-RPC
server request. Its provider request identity is the **top-level JSON-RPC `id`**,
not `params.id`. The following structured fields are also required:

- top-level `id`;
- `params.threadId`;
- `params.turnId`;
- `params.itemId`;
- `params.environmentId`, exactly `"local"`;
- bounded `params.availableDecisions` tags;
- current POKIT `SessionID` and managed epoch.

The native JSON-RPC ID must be decoded without floating-point conversion, bounded,
stored internally, and echoed with the same JSON type and value in the response.
The public `ApprovalID` is a canonical bounded internal string derived from that
identity, but it is not a substitute for the preserved native response ID.

The accepted CP0 evidence shows the advisory decision tags `accept`,
`acceptWithExecpolicyAmendment`, and `cancel`. It does **not** show an
`availableDecisions` list of `[accept, decline]`. CP0 separately proves that a
daemon response with `decision:"decline"` is resolved and prevents the command.
Therefore:

- `availableDecisions` is a bounded certification fingerprint, not action authority;
- the initial certified actions are `allow_once -> accept` and `deny -> decline`,
  based on schema plus the live CP0 accept/decline consumption evidence;
- `acceptWithExecpolicyAmendment` and `cancel` remain non-actionable;
- an unknown/missing field, tag set, response decision or provider version makes
  the request non-actionable.

Raw command, CWD, prompt, exec-policy amendment body and arbitrary provider payload
are never stored in the public approval DTO. Amendment presence may be kept only as
a bounded internal marker.

## 3. Immutable binding

The complete provider-positive binding is:

| Field | Created/read | Stored | Compared/used | Invalidated/tested |
|---|---|---|---|---|
| SessionID | managed registry | approval record and claim | handler, runtime resolver, delivery | delete/stop/kill/epoch change |
| RuntimeRef | managed record | approval record and binding | claim and pre-write revalidation | replacement/exit |
| provider/version | pinned managed config | RuntimeRef | ingest, claim, delivery certification | mismatch/unsupported version |
| native request ID | top-level JSON-RPC `id` | private provider request record | exact response ID and resolved requestId | duplicate/cancel/timeout |
| thread/turn/item | structured params | private provider request record | active request and consumption correlation | turn completion/replacement |
| canonical action | stored option plus internal certified mapping | claim binding | ActionDigest and delivery material selection | action substitution tests |
| exact provider response bytes | daemon-generated only | private immutable delivery material | PayloadDigest, write, receipt | byte substitution tests |
| requester context/key/token | authenticated server/store | existing A1 binding and ledger | claim, replay and commit | auth/replay tests |

One authoritative canonical version string must be used in the managed record,
`ApprovalIngest`, `RuntimeRef`, gate and receipt. The current display value
`"codex-cli 0.144.1"` is not accepted by the gate's version grammar. SP1 must use a
grammar-valid authority value such as `"0.144.1"`; a display version, if retained,
is separate and never compared as authority.

## 4. Required minimal A1-core extension

The frozen core currently computes an empty canonical payload for no-input decision
options. An `ApprovalDelivery` consequently receives no readable selected action and
no exact Codex response bytes. A digest must never be reversed or used as an action
lookup.

SP1 may make one narrow provider-neutral internal extension:

- ingestion supplies, for each certified option, daemon-generated immutable
  delivery material (selected action identity, schema version and exact bounded
  response bytes);
- the store defensively copies it and includes all delivery-semantic fields in the
  canonical action and payload digests;
- `ClaimForExecution` returns the exact stored bytes;
- the same payload digest is carried through delivery receipt and commit;
- none of this material appears in public/mobile DTOs.

This does not authorize generic commands or introduce a provider SDK. It only makes
the existing claim -> delivery -> receipt contract capable of carrying an exact
server-generated provider response.

## 5. Delivery and consumption linearization

Queue admission alone is not delivery success. The existing
`RuntimeDeliveryGate.Accept` returns `accepted` after bounded queue insertion, before
Codex consumption, so `NewGatedApprovalDelivery` must not be used unchanged as the
SP1 success boundary.

The handler-facing `ApprovalDelivery.Deliver` may return `DeliveryAccepted` only
after this sequence:

1. the store grants the exact claim and payload;
2. the current RuntimeRef is revalidated;
3. a bounded per-runtime pending-response entry is armed for the exact native
   request identity;
4. the sole pump writes the exact response through the owned app-server transport;
5. the sole pump observes a matching `serverRequest/resolved` strictly after the
   write and routes it to that pending entry;
6. the delivery returns a fully bound receipt and the store commits it.

The pending table/channel is bounded and epoch-owned. It has one linearization point
for claim ownership, one write at most, and deterministic cancellation. A resolved
event seen before arming/write, a different request/thread, timeout, duplicate,
provider cancellation, process exit, stop, kill, delete or epoch replacement cannot
produce success. No component other than the pump may read provider stdout.

If the current gate cannot enforce this lifecycle, isolate it as internal admission
or replace the smallest unsafe boundary. Do not report gate queue insertion as
provider acceptance.

## 6. Actionability and activation

`Actionable` is immutable in an ingested approval record. Increasing gate capacity
does not upgrade a record created with `Actionable=false`.

- P1 may observe and ingest only non-actionable records with no action options.
- P2 may create actionable records only for **new** requests after the certified
  delivery boundary and current-runtime endpoint have been installed successfully.
- Installing delivery capability and enabling actionable ingestion must be one
  production-owned transition for that session/epoch. On failure both remain off.
- Old non-actionable records are never upgraded in place.
- Every other provider, version and epoch remains capacity zero/non-actionable.

## 7. Restart and invalidation

ApprovalStore, pending response state and delivery endpoints are in memory. A daemon
restart restores no pending, executing or delivered authority and no old approval
record. A new managed epoch starts empty and non-actionable until a new structured
provider request is observed through a currently certified boundary.

Stop, kill, delete, natural exit and runtime replacement cancel pending delivery
waiters, deactivate actionability before further provider writes and prevent a late
resolved event from committing.

## 8. Public/mobile boundary

Use the existing ApprovalStore-backed safe approval DTO and
`HandleApprovalAction`. A managed event may contain a bounded display-only approval
reference, but it must not create a second approval DTO, action list or CTA authority.
The mobile client sends only option ID, bounded optional input and idempotency key
over the existing host-bound authenticated transport.

## 9. Staged implementation and stop gates

### P1 — Structured observation, non-actionable only

- strict top-level JSON-RPC ID preservation and params projection;
- bounded epoch-owned pending provider-request record;
- exact field/version/fingerprint gating;
- safe non-actionable ApprovalStore ingestion with no options;
- capacity remains zero, no provider response write, no mobile CTA.

Stop after a focused race-enabled checkpoint and independent P1 verification.

### P2A — Exact delivery material and certified boundary

- implement the minimal internal A1-core extension in section 4;
- implement the pump-owned resolved router and Codex delivery boundary;
- prove exact write-before-resolved, payload substitution rejection, timeout,
  duplicate, cancel, exit and replacement interleavings with deterministic barriers;
- do not enable production actionability yet.

Stop after independent P2A verification.

### P2B — Atomic capability activation

- install the exact current-runtime delivery endpoint and actionable-ingest switch as
  one production-owned transition;
- only newly observed certified requests receive `allow_once` and `deny` options;
- wire managed `RuntimeOf`; prove no old record upgrade and no queue-only commit;
- retain capacity zero on every mismatch or partial installation.

Stop after independent P2B verification.

### P3 — Authenticated mobile and live production proof

- consume only the existing safe ApprovalStore DTO/action API;
- prove authenticated allow-once and deny using real `pokit run codex` managed turns;
- prove exact provider consumption, command execution/non-execution, duplicate tap,
  stale epoch, deletion and DTO privacy;
- run the authoritative full repository gate on a frozen final HEAD and write the
  final evidence report.

Only independent P3/full-contract review may ACCEPT SP1/A1.1 and unblock N1.

## 10. Non-goals

No N1, Claude/A1.2, generic provider SDK, Task/Dispatch, worker protocol,
Executor-Verifier, automatic policy, PTY/screen approval, generic terminal input,
tmux/cmux cleanup, persistence, Windows expansion or terminal UI redesign.
