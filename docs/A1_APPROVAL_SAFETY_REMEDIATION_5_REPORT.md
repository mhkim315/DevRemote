# A1 Approval Safety — Remediation 5 Report (R5-A..E)

Status: **READY FOR INDEPENDENT A1 RE-VERIFICATION** — endpoint-ownership,
queue-binding, resource/entropy, and gate-integrity defects closed; production
actionable path still BLOCKED (no provider channel).

## 1. Identity & SHAs

```text
remote:  https://github.com/mhkim315/DevRemote.git
branch:  feature/phase10-multi-adapter
reviewed (rejected) implementation: 5360ec617
re-verification-4 / handoff HEAD:   0f94a1c8c
accepted S1.1 ancestor:             02c8385e3270fbbc4df45e0c71ccad6ebe11a076
remediation-5 implementation HEAD:  0f95c7c3c
```

Pre-implementation contract note: `docs/A1_APPROVAL_SAFETY_REMEDIATION_5_CONTRACT_NOTE.md`.
Preserved (re-verification-4 §1): complete requester presence + stored permission at
the store for fresh/retry/replay; exact idempotency/action/payload-digest/claim/receipt
comparisons; registry-disappearance + explicit cleanup deactivation; no callback under
the transition lock; queue-full/capacity-zero fail closed; bounded retry, safe DTOs,
device transport, log redaction; production non-actionable (capacity 0, empty mapping).

## 2. Reproduced counterexample → fix → proof

| finding | fix (code) | proof |
|---|---|---|
| R5-A accepted item lost on A→B replacement (`Activate` overwrote the queue; `Drain(sessionID)` mutable lookup) | gate keeps `endpoints[handle]` + `current[session→handle]`; Activate publishes a new endpoint and RETIRES the old (keeps its queue); Accept returns the handle; Drain uses the CAPTURED handle | `TestDeliveryGate_AcceptedItemSurvivesReplacement` (captured-A returns exactly item A; B empty; retired A accepts nothing) |
| R5-B queue was `[][]byte`, unbound to the approval | queue holds `AcceptedDelivery{Binding, ClaimToken, ReceiptID, defensive payload}`; receipt ReceiptID == item ReceiptID; Drain returns typed copies | `TestDeliveryGate_TypedItemBindingAndAliasing` (fully-bound fields, no-payload distinguishable by action, caller-aliasing blocked) |
| R5-C entropy fallback `"n"` + caller-defined capacity | `genToken()` fails closed on entropy failure (Activate publishes nothing, Accept appends nothing); repository-owned bounds on capacity/endpoints/item-bytes/total-bytes; negative/oversized → no-channel | `TestDeliveryGate_EntropyFailureFailsClosed`, `TestDeliveryGate_ResourceBoundsFailClosed` |
| R5-D final report HEAD failed the secret gate | rephrased the token-shaped example text in the R4 docs; no scanner exclusion; the FULL gate is run on the exact final report HEAD | secret scan OK on the final tree (this report + notes included) |

## 3. Queue-item + endpoint audit

Every `AcceptedDelivery` field derives from the claim's canonical binding:
ApprovalID, SessionID, Runtime(adapter/version/launch/stream), ActionDigest,
PayloadDigest, IdempotencyKey (in `Binding`), ClaimToken (opaque ownership), ReceiptID
(endpoint nonce+seq), defensive payload copy — **production-wired**; never in a
public/mobile DTO.

Endpoint identity: opaque handle per Activate; retired endpoints retained (bounded by
`maxGateEndpoints`, oldest-retired eviction) so a success receipt's item is not
silently destroyed; capacity 0 (production) → retired endpoints are always empty;
restart → fresh gate, no residual authority. Linearization: acceptance append,
publication/retirement/deactivation all under one lock; Drain snapshots then external
I/O runs on the copies OUTSIDE the lock — **production-wired mechanism; capacity 0
(unavailable)**.

## 4. Gate (final report HEAD)

Run on the EXACT final tree AFTER this report commit:

```text
go test -race ./internal/agent/... ./internal/term/... ./internal/transcript/...   OK
SKIP_NATIVE_GATE=1 sh scripts/build-gate.sh:
  Backend build/vet/race/diff  OK
  Mobile typecheck (tsc)  OK    Mobile test (jest, 383)  OK
  Invariants: vendor branch OK  ID inference OK
  Security: secret scan  OK   (no scanner exclusion; docs contain no token-shaped example text)
git diff --check  OK
```

Android/Kotlin native gate — **environmentally unavailable, confirmed by inspection**:
`mobile/.gitignore` line 26 excludes `/android/` (Expo-prebuild wrapper; needs
`expo prebuild` + Android SDK, absent here); A1 changed ZERO native files. Run with
`SKIP_NATIVE_GATE=1`; reported SKIPPED, not passed. The report HEAD SHA and gate result
below are stated after the final-tree run.

## 5. Positive provider status & scope (R5-E)

Reconfirmed from the current accepted adapters only: no controlled provider mapping or
delivery channel exists. `provenActionMapping` is empty and every gate endpoint is
activated with capacity 0 (no channel), so all production approvals are non-actionable
and no bytes are accepted. Controlled tests prove the machinery only; none is
substituted for a production positive path, and A1 is NOT claimed complete. Mandatory
positive-path item 2 remains **BLOCKED**. No N1/O1/O2, Task/Dispatch, worker completion,
provider implementation/research, automatic policy, generic command execution, CLI
redesign, ConPTY/Windows, cloud relay, or lock-screen work. N1 remains blocked.

```text
REVIEW REQUEST: A1 Approval Safety remediation 5 — 0f95c7c3cdac6dc1524a363027829782dd09579e
```
