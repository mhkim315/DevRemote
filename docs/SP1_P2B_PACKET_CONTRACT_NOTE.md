# SP1 P2B — Packet Contract Note (atomic capability activation)

Status: **PACKET CONTRACT — P2B ONLY. Implements section 9/P2B of
`docs/SP1_NATIVE_APPROVAL_CONTRACT_NOTE.md`. P3 (authenticated mobile action
path, real `pokit run codex` live turns, full repository gate) is NOT
started.**

Baseline: `2345211ba4f5e8ad22f385614fb237a09854402e` (P2A + R1 independently
ACCEPTED). Executor: SP1 execution agent, canonical checkout
`/Users/mhk/Documents/codex/DevRemote`, branch `feature/phase10-multi-adapter`.

## 1. Authority owner and canonical inputs

- **`ManagedCodexService.InstallApprovalExecution`** is the ONE
  production-owned activation transition. Under a single critical section it
  verifies every precondition and enables actionable ingestion; it returns
  the certified delivery boundary (P2A) and the current-runtime resolver.
  On ANY precondition failure NOTHING changes: ingestion stays
  non-actionable (P1 observation), no endpoint is returned, and the caller
  keeps the capacity-0 gate.
- **Preconditions (all fail closed, no partial state)**: a non-nil approval
  store; the service not shutting down; the pinned `AuthorityVersion`
  exactly equal to the certified `"0.144.1"` and grammar-valid; NO runtime
  created yet (`gen == 0`, empty runtime map) so no epoch can predate
  activation; not already installed (double-install is an error, not an
  idempotent success).
- The composition root (`cmd/devremote/app.go`) performs the wiring as one
  block: install → on success wire `Handlers.ApprovalDelivery` (dispatching
  boundary) + `Handlers.RuntimeOf`; on failure log and keep the capacity-0
  gate with `RuntimeOf` unwired — production then behaves exactly as
  accepted P1 (non-actionable observation).

## 2. Activation semantics

- The per-runtime flag `actionableActive` is copied from the service ONCE at
  create time (before the pump starts): activation is bound to the
  session/epoch at birth and never toggled mid-life. Because installation
  must precede the FIRST runtime, no record ingested by an active service
  ever coexists with a pre-activation epoch of the same service.
- **Actionable ingest (new requests only)**: an exactly-certified
  `requestApproval` observed by an ACTIVE runtime is ingested with
  `Actionable=true`, exactly the two certified options
  (`allow_once`/kind approve, `deny`/kind reject), and daemon-generated
  delivery material per option: the EXACT response bytes
  `{"jsonrpc":"2.0","id":<preserved token>,"result":{"decision":"accept|decline"}}`
  under the certified schema `codex.appserver.decision.v1`. An INACTIVE
  runtime ingests the frozen P1 non-actionable zero-option record.
- **No old record upgrade**: `Actionable` is immutable in the store; a
  re-offer of an existing record ID with a different
  option/material fingerprint is not admitted (frozen rule, re-proven with
  P2B shapes). Activation-order cannot create the case at all (install
  precedes epoch 1).
- **`RuntimeOf`** resolves ONLY a live, installed, current-epoch managed
  runtime: `{codex_app_server, "0.144.1", epoch, 0}`. Uninstalled service,
  unknown session, exited record, or epoch mismatch → not found. Non-managed
  sessions are never resolved (their records are never actionable).
- **Dispatching delivery**: `Handlers.ApprovalDelivery` becomes a dispatcher —
  a binding whose runtime adapter is `codex_app_server` goes to the certified
  P2A boundary; everything else goes to the frozen capacity-0 gate
  (unavailable). There is no queue on the certified path; queue admission
  cannot be reported as success anywhere.

## 3. Deactivation and rollback

- **Replacement/exit/stop/kill**: the pump exit path (P1/P2A) closes input,
  drains every armed waiter deterministically, invalidates the session's
  records and marks the registry record exited → `RuntimeOf` fails,
  `HandleApprovalAction` gets `stale_runtime` at revalidation, the boundary
  refuses at its own runtime revalidation. **Delete** additionally clears the
  records and removes the runtime → `unavailable`.
- **Partial installation**: impossible by construction — the transition is
  one critical section that either commits every field or returns an error
  having changed nothing; the composition root wires the returned endpoints
  only on success. Proven by precondition-failure tests asserting the
  complete OFF state afterwards (non-actionable ingest, RuntimeOf false,
  action POST fails closed with zero provider writes).
- Every other provider, version, and epoch stays capacity zero and
  non-actionable (`provenActionMapping` remains empty; the JSONL observer
  path is untouched).

## 4. Immutable binding table (P2B delta)

| Field | Created/read | Stored | Compared/used | Invalidated/tested |
|---|---|---|---|---|
| activation | InstallApprovalExecution (one critical section) | service `actionable` + per-runtime `actionableActive` (copied at create) | ingest actionability, RuntimeOf gate | precondition failure → never set; never toggled mid-life |
| certified options | activation-gated ingest | store record (frozen copy rules) | claim option lookup, safe DTO options | inactive runtime → zero options |
| delivery material | daemon-generated at ingest from the preserved id token | store record (P2A rules) | claim payload, boundary semantic checks, digests | wrong token/schema/mapping → P2A rejections |
| RuntimeRef | RuntimeOf | — | handler pre-claim + pre-delivery revalidation, claim runtime equality | exit/delete/epoch change → not found |

## 5. Adversarial interleavings (one per critical invariant)

1. **Atomicity**: each failed precondition (nil store, wrong authority
   version, install-after-create, double install, shutdown) leaves BOTH
   capabilities off — a certified wire request afterwards produces a
   non-actionable record (or none), `RuntimeOf` false, and an authenticated
   action POST fails closed with zero provider writes.
2. **New-requests-only**: a record ingested by an inactive runtime stays
   non-actionable and unclaimable forever; an actionable re-offer of the
   same ID is not admitted (no upgrade in place).
3. **No queue-only commit**: with production wiring installed, an action on
   a session whose provider never resolves ends `delivery_failed`
   (ambiguous, non-retryable) — never approved; the fallback gate path
   returns unavailable for non-managed bindings.
4. **End-to-end production shape**: the REAL `HandleApprovalAction` (device
   principal, production store, production dispatcher, production pump)
   commits approved/rejected ONLY after the exact write and the
   pump-observed resolved (deterministic post-written-mark barrier), and a
   deny action provably writes the decline bytes.
5. **Deactivation**: kill/delete after an actionable record exists →
   records invalidated/cleared, action POST fails closed, RuntimeOf false,
   zero further provider writes.
6. **DTO boundary**: the actionable safe DTO exposes exactly the two
   certified options with Pokit-owned labels and no provider bytes, command,
   cwd, amendment, schema identity, or response material.

## 6. Non-goals (P2B)

No P3: no mobile client changes, no live `pokit run codex` provider turns,
no physical-device proof, no full repository gate (reserved for P3 final
HEAD). No N1/A1.2/CP1, no generic provider SDK, no persistence, no
automatic policy. The reviewer's P2A follow-up — proving the REAL provider
delivers resolved-after-written success (the during-write conflict is a
safe false-negative) — is explicitly carried as a P3 mandatory evidence
item.

## 7. P2B gate

Focused race-enabled checkpoint: `go build ./...`, `go vet ./...`,
`go test -race ./internal/term ./cmd/devremote -count=1`, new/updated
suites `-count=20`.

## 8. R1 amendments (reviewer blocker on 4dd2a7d, remediated)

**Blocker**: `SetApprovalStore` could replace the store at any time, and a
pre-installed injected service surviving an install error could mint
actionable records without handler authority (partial activation via store
divergence). Remediation:

1. **Canonical-store immutability**: `SetApprovalStore` now returns an error
   and refuses any change after a store is configured (a DIFFERENT store is
   never accepted; the same store is an idempotent no-op), after the
   activation installed, after the first runtime/generation exists, and
   after shutdown began. `InstallApprovalExecution` additionally requires the
   configured store to be the SAME canonical store it installs — the handler
   store, the runtime ingest store, and the installed delivery authority can
   never diverge. All transitions share the service mutex: concurrent
   configure/install races admit exactly ONE winner (proven under -race).
2. **Composition fail-fast**: `NewAppWithDeps` FAILS (no App is built) when
   the managed service cannot be canonically owned — a configure error
   (foreign store, pre-existing runtime, pre-installed activation) or an
   install error on a service that reports an installed activation. The
   observation-only log fallback remains ONLY for the provably-safe case:
   store canonically owned, activation off (e.g. a non-certified authority
   version, whose observation path also rejects everything — proven
   completely off: zero records, zero claims, zero writes, RuntimeOf false).
3. **Evidence**: pre-created-runtime / foreign-configured / foreign-installed
   `deps.Managed` each fail App creation (fresh service control builds);
   store replacement refused after install and after any runtime, with
   records continuing to land ONLY in the canonical store; concurrent
   configure/install single-winner ownership; a rejected second install
   leaves the first activation fully working and the rejected store empty;
   the degraded (uncertified-version) service is completely off.
