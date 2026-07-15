# SP1 Native Approval — Pre-Implementation Contract Note (Authority)

Status: pre-implementation note (protocol §5 full discipline). Baseline
`62652633eb56577f0308973f7581ac94498f19fe`; accepted SP0.5 core (managed
runtime, lifecycle, event/IO surface) and frozen A1 safety core (store, gate,
binding, handler, DTO) untouched in shape. Authoritative handoff: none
(reviewer directly authorized SP1 after SP0.5 ACCEPT).

Risk class: **authority + concurrency** — execution-authority claim, delivery,
commit, and resolved-consumption boundary.

## 1. Owners

- **Approval observation**: `codexManagedRuntime.pump` — the ONLY native-event
  consumer (unchanged sole stdout reader). It projects the exact
  `item/commandExecution/requestApproval` into a provider-neutral
  `ApprovalIngest` and writes it into the `AuthoritativeApprovalStore`.
- **Ingestion / claim / commit**: `AuthoritativeApprovalStore` (frozen A1
  core — unchanged).
- **Delivery**: a NEW `codexApprovalDelivery` implementing the frozen
  `ApprovalDelivery` interface: writes the store-derived canonical payload
  (a server-generated `{"result":{"decision":"accept"}}` JSON-RPC response)
  through the owned app-server transport, and reads the `serverRequest/resolved`
  notification to prove consumption.
- **Mobile actions**: the existing `HandleApprovalAction` handler +
  `RuntimeDeliveryGate` + `AuthoritativeApprovalStore` (frozen chain — the
  SP1 delivery bridge feeds it exactly the same store-generated
  approval/binding/payload path that the handler already drives, but with a
  REAL provider-connected sink behind the gate, capacity activated for the
  exact certified tuple).
- **ApprovalDecision**: server-derived canonical decision payload (`accept`
  or `decline`); the response JSON is constructed by the daemon, never by
  the mobile client. The mobile client selects only the option ID; the store
  recomputes the digest and payload.

## 2. Exact provider approval shape (CP0 evidence, frozen)

`item/commandExecution/requestApproval` notification structure (pinned 0.144.1):

```json
{
  "method": "item/commandExecution/requestApproval",
  "params": {
    "id": <int, outer JSON-RPC request id>,
    "threadId": "<string>",
    "turnId": "<string>",
    "itemId": "<string>",
    "item": { "command": "<field present, always redacted>" },
    "environmentId": "local",
    "availableDecisions": [{"label":"Accept","tag":"accept"},{"label":"Decline","tag":"decline"}],
    "proposedExecpolicyAmendment": "<field present in real evidence, may be null; kept as bounded INTERNAL provider data>"
  }
}
```

Observe-only (non-actionable for now): `proposedExecpolicyAmendment`.
Actionable: `accept`, `decline` mapped to `allow_once` / `deny` canonical
actions (frozen A1 vocabulary). `cancel` and `acceptWithExecpolicyAmendment`
are NOT actionable and are excluded from the interaction option set.

Provider response:
```json
{"jsonrpc":"2.0","id":<match outer request id>,"result":{"decision":"accept"}}
```
Written **strictly after claim**; the store owns the canonical payload. The
response goes through `rt.send` on the exact app-server transport — no second
process, no PTY wrapper.

Consumption proof: `serverRequest/resolved {requestId,threadId}` arriving
**after** our write — the `requestId` must equal the outer request id we
responded to. Resolution before our write is provider-side resolution, NOT
delivery success. Duplicate same-id responses are silently ignored by the
provider (CP0 finding — POKIT claim/ordering is the sole duplicate defense).
Late post-cancel responses are silently ignored.

## 3. Ingestion binding (pump → AuthoritativeApprovalStore)

The pump observes `item/commandExecution/requestApproval` on the bound thread:

- `AgentApproval.ID` = `params.id` (outer JSON-RPC request id, as string)
- `SessionID` = runtime's `codex_app_server:<local>` canonical id
- `Kind` = `"approval"`
- `Options` = `[{ID:"allow_once",Kind:"allow_once",Label:"Accept"},
  {ID:"deny",Kind:"deny",Label:"Decline"}]` — FIXED set for Codex; input
  is no-input (payload-only); the existing `allow_onceActionDigest` test
  constant already covers `allow_once`
- `environmentId`: required EXACTLY `"local"` — any other value or absent →
  non-actionable (recorded for fingerprint; ingestion withheld)
- `availableDecisions`: presence required with exactly `[accept,decline]`
  tags (order-insensitive) — mismatch → non-actionable
- `proposedExecpolicyAmendment`: recorded as bounded presence marker only
  (non-null → metadata field `amendment_present:true`), never forwarded to
  the action set or the client DTO
- `commandActions`: always REDACTED (`<REDACTED-CMD>`) per CP0; present in
  the provider payload but NEVER exposed
- `ApprovalIngest`: `Provider="codex"`, `Version="codex-cli 0.144.1"`,
  `LaunchGen=rt.epoch`, `StreamGen=0`, `Provenance=contract.Runtime`,
  `Actionable` = the field-gating rules above AND capacity-zero → false
  (SP1 activates capacity for the certified tuple in P2)

Runtime identity for `ApprovalIngest`: `RuntimeOf(sessionID)` resolves
`RuntimeRef{Adapter:"codex_app_server", Version:"codex-cli-0.144.1",
LaunchGen:epoch, StreamGen:0}` — evidence-exact from SP0/SP0.5.

The **exact** agent-kind/status from the existing telemetry path are NOT used
for the managed session (managed session identity is provider-protocol-native,
not agent-detection-derived). The managed session already writes
`AgentKind:"codex"` on the session row — the approval is ingested directly,
so it shares that session identity.

## 4. Delivery bridge linearization (P2)

One delivery attempt per claim token + immutable binding. At most one
provider write (`rt.send`) per deliver call — never retried.

1. Store `ClaimForExecution` grants a claim token + canonical payload (the
   store's own `{"result":{"decision":"X"}}`).
2. **Pre-write revalidation**: `RuntimeOf(sessionID)` must still equal the
   binding's `RuntimeRef` (same adapter + version + LaunchGen) — stale
   runtime → `DeliverStaleRuntime` without a provider write.
3. Write the store's canonical payload through `rt.send`.
4. Await `serverRequest/resolved {requestId == binding.ApprovalID}` —
   bounded (CP0: observed within seconds; SP1 uses a 10s subject-to-review
   bound). The `requestId` must be the exact outer JSON-RPC request id we
   wrote to (== `binding.ApprovalID`). A `resolved` with a different id, a
   resolved that arrived BEFORE step 3, or no resolved within the bound
   all fail closed.
5. `RecordDelivery` with the outcome — committed only when `resolved`
   appeared strictly after the write with the matching id.

Capacity: activated ONLY for the exact certified tuple
`(codex_app_server, codex-cli-0.144.1, epoch==current)` — the gate
`Activate` is called with capacity 1 (bounded single-item queue) when the
pump observes the first actionable approval for a sprint. Superseded when
`Stop/Kill` terminates the runtime (`Deactivate`). Capacity zero for every
other provider, version, and epoch. `provenActionMapping` stays empty (P2
does not wire a public mapping — the store's record IS the binding).

## 5. Mobile action path (P3, unchanged handler chain)

The existing `HandleApprovalAction` handler remains the single mobile action
entry:
- `h.RuntimeOf` resolves from the managed service (new wiring in `NewAppWithDeps` — replaces the nil `RuntimeOf` that SP0/SP0.5 left unwired)
- The gate endpoint is activated with capacity 1 when an actionable approval
  is observed (the P2 activation).
- The handler's claim → delivery → commit chain is already correct and
  provider-neutral — SP1 only wires the codex-specific `RuntimeOf` and
  connected gate sink. The store already computes the canonical payload; the
  delivery bridge feeds it through the real provider transport.

## 6. Duplicate / stale / timeout / cancel semantics

- **Duplicate same-id response** (provider ignores second): POKIT's own
  response is idempotent — `RecordDelivery` already enforces the claim-id
  match under `{claimToken, binding}`; the idempotency ledger prevents
  double-commit. The delivery bridge makes at most one write per claim
  (no-retry).
- **Stale RuntimeRef**: `RuntimeOf` revalidation before every write +
  `RecordDelivery`'s built-in superseded detection are two layers.
- **Timeout**: no `resolved` within the delivery bound → `DeliveryUnavailable`
  outcome, record delivery-failed, retryable (bounded).
- **Cancel**: `turn/interrupt` BEFORE our response write makes the turn
  terminal (`turn/completed` arrives with provider-side resolution —
  NOT our success). The delivery bridge sends no response for a turn that
  has already terminated.
- **Natural child exit**: the delivery bound is tied to the pump — when the
  pump closes (exited), any outstanding await on `serverRequest/resolved`
  fails (EOF). Deliver fails closed; the store record is delivery-failed.

## 7. Reconnect / daemon restart

Approval delivery state is ephemeral (in-memory gate, in-memory delivery
await, no persistence). Daemon restart = all pending approvals lost (the
record stays delivery-failed; a new launch-generation invalidates it via
`SupersedeRuntime`). This is acceptable for SP1 — approval persistence and
restart recovery belong to later work (N1 notifications / orchestration).

## 8. Capacity

- Ingest: `authMaxApprovalsPerSession` per session (50, frozen)
- Gate: endpoint capacity = 1 (one item queued at a time)
- Exactly ONE certified tuple (provider="codex", version="codex-cli-0.144.1",
  LaunchGen matching the current runtime) — gate activation is session-scoped,
  capacity 1
- `provenActionMapping` stays empty — the A1 store IS the authority

## 9. Public DTO allowlist

Managed event DTO: adds kind `approval_required {approvalId, promptSummary,
options by ID only (label/kind/no payload)}` — bounded, no raw request body,
no paths, no tokens. The existing `/api/sessions` approval DTO and the mobile
`HandleApprovalAction` endpoint are used unchanged (the handler already
strips raw data).

## 10. Explicit non-goals

Notifications (N1), multi-option input forwarding, arbitrary approval
commands, `acceptWithExecpolicyAmendment`, `cancel` actionable, persistence/
restart-recovery, Claude/Windows, provider SDK generalization, Cleanup
(tmux/cmux removal).
