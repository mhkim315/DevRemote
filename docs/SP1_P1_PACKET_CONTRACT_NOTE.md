# SP1 P1 — Packet Contract Note (structured observation, non-actionable only)

Status: **PACKET CONTRACT — P1 ONLY. Implements section 9/P1 of
`docs/SP1_NATIVE_APPROVAL_CONTRACT_NOTE.md`. No delivery capacity change, no
actionable options, no mobile CTA, no provider response write.**

Baseline: `f8a69c6c1a359270eb31b8e51cfd5890fb4f0de5` (SP1 corrected contract +
handoff HEAD). Executor: new SP1 execution agent, canonical checkout
`/Users/mhk/Documents/codex/DevRemote`, branch `feature/phase10-multi-adapter`.

## 1. Authority owner and canonical inputs

- **Sole provider stdout reader**: `codexManagedRuntime.pump` (unchanged). P1
  adds two new pump cases (`item/commandExecution/requestApproval`,
  `serverRequest/resolved`) consumed by the SAME single pump goroutine. No new
  reader, no second consumer of provider stdout.
- **Sole approval record owner**: the frozen `AuthoritativeApprovalStore`. P1
  ingests through the EXISTING `Ingest` API only; the store itself is not
  modified. (The §4 minimal delivery-material extension is P2A, not P1.)
- **Canonical inputs** of one P1 observation:
  1. the exact raw JSONL line the pump read (for lossless top-level `id`);
  2. the runtime's bound identity: `sessionID`, `epoch`, `threadID`,
     `currentTurn` (bound at turn/start-response time by JSON-RPC id
     correlation — SP0.5 R1 mechanism, unchanged);
  3. the pinned managed config's NEW canonical authority version
     (`AuthorityVersion`, grammar-valid `"0.144.1"`), separate from the
     display `Version` (`"codex-cli 0.144.1"`), which is never compared as
     authority.
- Mobile-supplied data, PTY text, screen content, JSONL fallback and
  `waiting_approval` contribute NOTHING to this path.

## 2. Correct wire identity (evidence-exact, pinned 0.144.1)

From the accepted CP0 redacted traces (`wire_accept_pinned.jsonl` seq 24):

```json
{"jsonrpc":"2.0","id":<int>,"method":"item/commandExecution/requestApproval",
 "params":{"threadId":..,"turnId":..,"itemId":..,"command":..,"cwd":..,
           "environmentId":"local",
           "availableDecisions":["accept",{"acceptWithExecpolicyAmendment":..},"cancel"]}}
```

- Provider request identity is the **top-level JSON-RPC `id`**, never
  `params.id`. P1 extracts it losslessly as the exact JSON token via
  `json.RawMessage` (no float64 conversion anywhere on this path), bounds it
  (≤ 20 bytes), and certifies ONLY the integer form observed in CP0 evidence
  (`^-?[0-9]+$`, parseable as int64). A string/fractional/exponent/oversized
  id token is an uncertified shape → rejected.
- `availableDecisions` is a bounded certification fingerprint, not action
  authority. Tag extraction: string element → the string; single-key object
  element → the key (payload NEVER read or stored; presence kept only as a
  bounded internal boolean marker). Any other element shape → reject. The
  certified fingerprint is the exact ordered sequence
  `["accept","acceptWithExecpolicyAmendment","cancel"]`.
- `command`, `cwd`, prompt text, amendment bodies and every other params field
  are deliberately not decoded, not stored, not logged, and never enter any
  DTO.

## 3. Immutable binding table (P1 subset)

| Field | Created/read | Stored | Compared/used | Invalidated/tested |
|---|---|---|---|---|
| SessionID | managed create | pending record + approval record | store lookup, DTO scoping | delete (Clear), exit (InvalidateSession) |
| epoch (LaunchGen) | managed create | pending record (runtime-owned) + approval record LaunchGen (StreamGen 0) | store generation rule | stale-generation ingest dropped by store hw rule |
| provider | `codex_app_server` constant | ApprovalIngest.Provider / record provider | future RuntimeRef.Adapter (grammar-valid) | — |
| authority version | pinned config `AuthorityVersion` | ApprovalIngest.Version / record version | exact equality with certified `"0.144.1"` + `validVersion` grammar at projection | mismatch → reject (no state) |
| native request ID | top-level JSON-RPC `id` raw token | private pending record ONLY (never a DTO) | duplicate detection; future exact echo (P2A) | resolved / turn completion / exit / bound exhaustion |
| threadId | structured params | pending record | exact equality with runtime `threadID` | mismatch → reject |
| turnId | structured params | pending record | exact equality with `currentTurn` (bound from turn/start response) | no active turn / completed turn / mismatch → reject |
| itemId | structured params | pending record | non-empty bounded | oversize/missing → reject |
| environmentId | structured params | not stored | exact equality `"local"` | mismatch → reject |
| decision fingerprint | structured params | pending record (tags only) | exact ordered certified sequence | mismatch → reject |
| ApprovalID | daemon-derived `codexas-<epoch>-<idToken>` | approval record + pending record | store idempotency by ID | duplicate id → no second record |

## 4. Transition and linearization point

- The pump is the single writer of the runtime's pending-approval state; the
  state is guarded by the existing `turnMu` (same lock that owns
  turn-identity binding), so "active turn at observation time" and "arm the
  pending entry" are one critical section. There is no second linearization
  point and no lock held across I/O.
- Store admission remains linearized inside `AuthoritativeApprovalStore.mu`
  (unchanged frozen semantics: generation high-water, duplicate-ID
  idempotency, provenance gate, bounds).
- Order per observation: validate structurally (raw re-decode) → under
  `turnMu` check `turnClosed`/thread/turn binding + duplicate + capacity and
  arm the pending entry → release → `store.Ingest` (non-actionable). A
  rejected observation arms nothing and ingests nothing.

## 5. Success evidence and invalidation paths

P1 "success" is ONLY: one non-actionable approval record (Actionable=false,
zero options) + one bounded runtime-owned pending record for the exactly
certified projection. There is no delivery, no claim path, no resolved-commit.

Invalidation:

- `serverRequest/resolved` for the bound thread with a matching requestId
  token → the pending entry is removed (provider-side resolution observed).
  The store record is NOT committed/resolved by this (P1 has no consumption
  authority); it ages out under the frozen 5-minute TTL or session
  invalidation. A resolved for an unknown/foreign requestId or foreign thread
  is inert.
- Exact `turn/completed` for the current turn → pending entries of that turn
  removed.
- Pump exit (child EOF/kill/stop) → all pending entries dropped,
  `InvalidateSession(sessionID)` on the store (pending records →
  invalidated).
- Managed `Delete` → `store.Clear(sessionID)` (records dropped with the
  session).
- Daemon restart: everything is in memory; nothing is restored (§7 of the
  corrected contract).

## 6. Capacity behavior

- `provenActionMapping` stays empty; `Actionable=false` at ingest; the safe
  DTO exposes `actionable:false, options:[]`.
- The `RuntimeDeliveryGate` is not referenced by any managed code; no
  `Activate` call is added; production capacity stays zero for every
  provider, version and epoch.
- `Handlers.RuntimeOf` stays unwired. `HandleApprovalAction` on a P1 record
  fails at the `!snap.Actionable` check (409) BEFORE any runtime resolution
  or delivery — zero provider writes.
- Pending-record capacity: at most 4 armed pending provider requests per
  runtime; the 5th is rejected with no store record (bounded-state exhaustion
  fails closed).

## 7. Adversarial interleavings / counterexamples (one per critical invariant)

1. **params.id substitution**: a request with no top-level `id` but
   `params.id=7` → structural reject; zero pending, zero records. Control: the
   same message WITH top-level id creates exactly one.
2. **Duplicate authority record**: the same top-level id sent twice while
   pending → second observation rejected at the pending-duplicate check; store
   also holds exactly one record (same ApprovalID). No second authority
   record.
3. **Completed-turn request**: `turn/completed(turn-1)` consumed, then
   `requestApproval(turnId=turn-1)` → `currentTurn` is empty → reject. A
   fabricated `turnId=turn-2` without a bound turn also rejects.
4. **Resolved-before-request** (out-of-order provider): a
   `serverRequest/resolved` for id N arriving before any request N is inert
   (no pending entry exists, nothing is created from a resolved event).
5. **Exit race**: stop/kill sets `turnClosed` under `turnMu`; an observation
   after that point rejects inside the same lock — a late requestApproval
   cannot arm state on a closing runtime. Pump end then drops all pending and
   invalidates the session's records.
6. **Byte/privacy leak**: the crafted request carries a distinctive command,
   cwd, and amendment payload; tests marshal the safe DTO, the public list,
   the managed rows and capture the process log during consumption — none of
   the distinctive bytes (nor `jsonrpc`) may appear.
7. **Fingerprint drift**: `availableDecisions=["accept","decline"]` (or a
   two-key object element, or a non-list) → reject; zero records — an
   unproven decision surface can never reach display as a certified request.
8. **Stale generation**: an ingest carrying an older LaunchGen than the
   session's high-water is dropped by the frozen store rule (proven at store
   level with managed-shaped inputs).

## 8. Non-goals (P1)

No delivery material, no pump-owned resolved router for responses, no
provider response write, no `allow_once`/`deny` options, no actionable
records, no gate capacity change, no `RuntimeOf` wiring, no mobile changes
(the closed managed-event kind vocabulary is NOT extended; display reuses the
existing ApprovalStore-backed safe DTO on the managed telemetry rows), no
store schema change, no persistence, no N1/A1.2/CP1. P2A/P2B/P3 start only
after independent P1 acceptance.

## 9. P1 gate

Focused race-enabled checkpoint (per handoff §4): `go build ./...`,
`go vet ./...`, `go test -race ./internal/term -count=1` plus targeted new
tests; the full repository gate runs at P3 finalization only.
