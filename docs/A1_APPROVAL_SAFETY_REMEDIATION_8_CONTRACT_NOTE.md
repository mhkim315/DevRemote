# A1 Approval Safety — Remediation 8 pre-implementation contract note

Baseline `8528f73` (R7). Fixes only re-verification-7 findings.

## Canonical format + byte bound for every stored field

| field | canonical format | byte-bound | validated where |
|---|---|---|---|
| SessionID | `<adapter>:<local>` (non-empty adapter, colon, non-empty local) | maxSessionIDLen (512) | `validSessionID`, Accept + Activate |
| ApprovalID | non-empty | authMaxApprovalIDLen (256) | `validApprovalID`, Accept |
| Adapter (Runtime.Adapter) | non-empty | maxVersionLen (64) | `validAdapterID`, Accept + Activate |
| ProviderVersion (Runtime.Version) | non-empty | maxVersionLen (64) | `validVersion`, Accept + Activate |
| ActionDigest | exactly 64 lowercase hex chars (SHA-256) | 64 | `isHex64`, Accept |
| PayloadDigest | exactly 64 lowercase hex chars (SHA-256) | 64 | `isHex64`, Accept |
| IdempotencyKey | `[A-Za-z0-9._:-]+` | 128 | `validCanonicalKey`, Accept |
| ClaimToken | exactly 32 lowercase hex chars (newClaimToken) | 32 | `validClaimToken`, Accept |
| ReceiptID | `nonce(16) + "-" + seq` (~27 chars) | ~64 | produced by Accept |
| Runtime.LaunchGen, Runtime.StreamGen | integer | — | compared exactly |
| SessionID (endpoint) | same as above | validated at Activate | |
| RuntimeRef (endpoint) | adapter+version+launch+stream | validated at Activate | |

Deepest admission boundary: `RuntimeDeliveryGate.Accept` validates every field via
`validGateBindingMeta` before ANY mutation. `Activate` validates SessionID and
RuntimeRef fields before publishing; entropy failure fails closed; capacity <0 or
>maxGateCapacity fails closed (returns `("", false)`, NO silent clamp to 0, NO
successful endpoint published with a disguised no-channel cap).

`totalItemBytes` counts ALL variable-size fields exactly; a documented constant
of 64 accounts for the ReceiptID + struct overhead only (nonce hex + dash + seq
digits + internal pointers bound, verified safe for any plausible receipt id).

## Failure behavior

- malformed metadata → Accept returns `("", "", false)`, no mutation, no receipt.
- invalid capacity → Activate returns `("", false)`, no endpoint published, current
  unchanged.
- endpoint exhaustion + no safe victim → Activate returns `("", false)`, no mutation.
- payload/digest mismatch → Accept false, no append.
- concurrent activation/reclamation → under one lock, deterministic ordering.

## Test fixture migration

ALL test helpers MUST use canonical digests (64 hex chars), canonical claim tokens
(32 hex chars), and canonical idempotency keys. The existing helpers `gateReq`,
`digestOf`, and `acceptReceipt` are updated; any test with a short digest or a
non-hex token is corrected. This is intentional: a non-canonical fixture was testing
the absence of a validator, not valid behavior.
