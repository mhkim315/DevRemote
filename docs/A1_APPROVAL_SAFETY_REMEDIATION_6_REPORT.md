# A1 Approval Safety — Remediation 6 Report (R6-A..E)

Status: **READY FOR INDEPENDENT A1 RE-VERIFICATION** — pre-append payload
validation, safe non-destructive eviction, and idempotent activation defects closed;
production actionable path still BLOCKED (no provider channel).

## 1. Identity & SHAs

```text
remote:  https://github.com/mhkim315/DevRemote.git
branch:  feature/phase10-multi-adapter
reviewed (rejected) implementation: 0f95c7c3cdac6dc1524a363027829782dd09579e
re-verification-5 / handoff HEAD:   a8966d9dfaefec000d27a36e1a407d686322641a
accepted S1.1 ancestor:             02c8385e3270fbbc4df45e0c71ccad6ebe11a076
remediation-6 implementation HEAD:  17dd248e0
```

Pre-implementation contract note: `docs/A1_APPROVAL_SAFETY_REMEDIATION_6_CONTRACT_NOTE.md`.

## 2. Reproduced counterexample → fix → proof

| finding | fix | proof |
|---|---|---|
| R6-A: binding owns digest("authorised"), request carries "substituted" → gate.Accept returns ok=true (daemon-level acceptance of unverified bytes) | `Accept` validates the complete item before append: non-empty fields + canonical key + claim token; Binding.SessionID==endpoint session; Binding.Runtime==endpoint runtime; Binding.PayloadDigest==payloadDigest(req.Payload) | `TestDeliveryGate_SubstitutedPayloadRejectedBeforeAcceptance` (binding owns one digest, request carries different bytes → non-acceptance, captured-drain empty, correct payload still accepted) + `TestDeliveryGate_MalformedBindingRejected` table |
| R6-B: create an accepted-item endpoint → fill to maxGateEndpoints → item silently evicted (drain returns empty) | `evictLocked` removed; `findSafeVictimLocked` only returns retired+inactive+non-current+empty-queue+zero-bytes endpoints. Activate is all-or-nothing: computes projected count PRE-mutation, fails with ("",false) if no safe victim and bound exceeded. Never evicts active or retired-nonempty endpoints. | `TestDeliveryGate_SafeEvictionNeverDestroysAcceptedItems` (deactivated+nonempty survives, deactivated+empty reclaimed) |
| R6-C: telemetry calls Activate on every poll → new handle+Entropy allocated each time, infinite endpoint growth | Activate now idempotent: identical SessionID+RuntimeRef+effective capacity returns the existing handle with NO entropy allocation, NO order growth, NO retirement | `TestDeliveryGate_IdempotentSameRuntimeActivation` (same-runtime idempotent; genuine gen-change is a real replacement preserving A-items for captured-handle drain) |

## 3. Invariant audit

Pre-append validation: every `Accept` checks ApprovalID, SessionID, ActionDigest,
PayloadDigest, IdempotencyKey, ClaimToken, endpoint session, endpoint runtime, and
exact payload-digest equality before appending — **production-wired**.

Safe eviction: only retired, inactive, non-current, empty-queue, zero-byte endpoints
are candidates for reclamation. Activation fails closed (no mutation) when bounds are
exhausted and no safe victim exists. Empty retired endpoints ARE reclaimed;
non-empty ones are NEVER evicted — **production-wired**.

Idempotent activation: same SessionID+RuntimeRef+capacity returns the existing handle
with no growth. A genuine RuntimeRef/capacity change is a true serialized replacement.
Production `processSession` calls `deliveryGate.Activate` on every correlated poll, so
the same runtime preserves one handle across repeated polls — **production-wired**.

Production capacity 0; `provenActionMapping` empty; no controlled provider channel
exists → all production approvals non-actionable. A1 positive-path item 2 = **BLOCKED**.

## 4. Gate (to be run on the exact report HEAD)

```text
go test -race ./internal/agent/... ./internal/term/... ./internal/transcript/...  OK
SKIP_NATIVE_GATE=1 sh scripts/build-gate.sh:
  Backend build/vet/race/diff  OK
  Mobile typecheck (tsc)  OK    Mobile test (jest, 383)  OK
  Invariants: vendor branch OK  ID inference OK
  Security: secret scan  OK
git diff --check  OK
```

Android/Kotlin native gate — **environmentally unavailable, confirmed by inspection**:
`mobile/.gitignore` line 26 excludes `/android/` (Expo-prebuild wrapper; absent here);
A1 changed ZERO native files. Run with `SKIP_NATIVE_GATE=1`; reported SKIPPED, not
passed. Gate result confirmed on the exact final report HEAD below.

## 5. Scope

No N1 notifications, provider implementation/research, Task/Dispatch, worker ACK,
automatic policy, generic commands, CLI redesign, ConPTY/Windows, cloud relay,
lock-screen actions, O1 or O2. N1 remains blocked.

```text
REVIEW REQUEST: A1 Approval Safety remediation 6 — 17dd248e002613f2d10d67b159a1d8ebe9bcd7bc
```
