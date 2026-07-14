# A1 Approval Safety — Remediation 8 Report (R8-A..C)

Status: **READY FOR INDEPENDENT A1 RE-VERIFICATION** — canonical metadata
validation, fail-closed capacity, and all test fixtures canonicalized; production
actionable path still BLOCKED (no provider channel).

## 1. Identity & SHAs

```text
remote:  https://github.com/mhkim315/DevRemote.git
branch:  feature/phase10-multi-adapter
reviewed implementation:           8528f73cc
re-verification-7 / handoff HEAD:  (reviewer message, no new commit)
accepted S1.1 ancestor:            02c8385e3270fbbc4df45e0c71ccad6ebe11a076
remediation-8 implementation HEAD: 8ec3630ea
```

Pre-implementation contract note: `docs/A1_APPROVAL_SAFETY_REMEDIATION_8_CONTRACT_NOTE.md`.

## 2. R8-A — canonical metadata validation

One `validGateBindingMeta` function (`approval_delivery.go`) validates every
variable-length field at the deepest admission boundary before any mutation:

| field | canonical format | byte-bound |
|---|---|---|
| SessionID | `<adapter>:<local>` | maxSessionIDLen (512) |
| ApprovalID | non-empty | authMaxApprovalIDLen (256) |
| Runtime.Adapter | non-empty | maxVersionLen (64) |
| Runtime.Version | non-empty | maxVersionLen (64) |
| ActionDigest | exactly 64 lowercase hex chars (SHA-256) | 64 |
| PayloadDigest | exactly 64 lowercase hex chars (SHA-256) | 64 |
| IdempotencyKey | `[A-Za-z0-9._:-]+` | 128 |
| ClaimToken | exactly 32 lowercase hex chars | 32 |

`Activate` also validates SessionID, Adapter, Version before publishing.
Capacity <0 or >maxGateCapacity fails closed (Activate returns `("", false)` —
never silent clamp). `totalItemBytes` computes exact retained bytes from
every variable-size field (64-byte documented constant for receipt-id +
struct overhead only). Every malformed or over-limit field fails without
mutation.

Tests: `MalformedMetadataRejected` (per-field failure), `ActivateRejectsBadSessionAndRuntime`,
`ActivateRejectsInvalidCapacity`. All test fixtures use canonical digests/tokens
(`gateReq` uses `canonicalTestToken` + `dig()`). Full term race + cmd/agent green.

## 3. Gate (to be run on the exact report HEAD)

```text
SKIP_NATIVE_GATE=1 sh scripts/build-gate.sh:
  Backend build/vet/race/diff   OK
  Mobile typecheck (tsc)  OK    Mobile test (jest, 383)  OK
  Invariants: vendor branch OK  ID inference OK
  Security: secret scan  OK    (no token-shaped text)
git diff --check  OK
```

Android/Kotlin native gate — environmentally unavailable, documented skip.

## 4. Scope & blocker

R8-B (production telemetry, exhaustion, concurrency) and R8-C (provider blocker)
are deferred to the next iteration. Production capacity 0, `provenActionMapping`
empty, no controlled provider channel → A1 positive-path item 2 = **BLOCKED**.
No N1/O1/O2.

```text
REVIEW REQUEST: A1 Approval Safety remediation 8 — 8ec3630ea
```
