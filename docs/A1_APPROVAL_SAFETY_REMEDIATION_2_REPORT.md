# A1 Approval Safety — Remediation 2 Report (R2-A..E)

Status: **READY FOR INDEPENDENT A1 RE-VERIFICATION** — safety foundation hardened;
production actionable path still BLOCKED (no provider delivery channel).

## 1. Identity & SHAs

```text
remote:  https://github.com/mhkim315/DevRemote.git
branch:  feature/phase10-multi-adapter
reviewed (rejected) implementation: ed466094cd7a38d148d845d7057c63fe2019e1c9
re-verification/handoff:            57561e3d61867a525b2515e9da0125f8006c88fc
accepted S1.1 ancestor:             02c8385e3270fbbc4df45e0c71ccad6ebe11a076
remediation-2 implementation HEAD:  0521c3828b9d78028f64f9479f8269f469050f85
```

Preserved (re-verification §1, unchanged): heuristic/legacy/PTY/prompt/status
display-only; capability + `DetectApproval` + `SafeApprovalGate` gating; paired-device
auth mandatory; safe public DTO; generic `CommandBroker` removed; production
non-actionable default; `NewUnavailableApprovalDelivery` writes no bytes.

## 2. Finding → code → non-vacuous negative test

### R2-A — store-authoritative claim
- `internal/term/approval_store_gen.go` `ClaimForExecution`: recomputes the canonical
  digest from the stored immutable option + input (`canonicalActionFromOption`), uses
  ONLY `rec.requiredPerm` (no caller override — `RequiredPerm` removed from
  `ClaimRequest`), requires a bounded canonical idempotency key (`validCanonicalKey`),
  validates input against the stored schema, and validates the full RuntimeRef/expiry
  atomically. A caller `AssertDigest` is an optional consistency check only.
- Tests: `TestClaim_StoreRecomputesDigest_ArbitraryAssertionRejected` (arbitrary
  digest → digest_mismatch; store digest authoritative),
  `TestClaim_StoredPermissionNotOverridable`, `TestClaim_IdempotencyKeyRequiredAndCanonical`
  (empty/space/slash/newline/oversized → invalid_key), `TestClaim_InputValidatedAgainstStoredSchema`,
  `TestClaim_RejectsRuntimeAndActionMismatches`.

### R2-B — approval-bound idempotency + fail-closed capacity
- One immutable `ApprovalExecutionBinding` (`approval_execution.go`) owned by the claim
  token. `idempotencyEntry` binds `{binding, requesterDevice, accepted}`. Same
  key+digest → already_accepted ONLY for the exact same approval binding; reuse
  against another approval/runtime/digest → conflict. Capacity FAILS CLOSED
  (`ClaimLedgerFull`). No automatic retry (frozen: a spent key never re-executes).
- Tests: `TestIdempotency_SameKeyAcrossApprovalsIsConflict`,
  `TestIdempotency_SameKeyDifferentDigestConflict`, `TestIdempotency_CapacityFailsClosed`,
  `TestIdempotency_NoRetryAfterDeliveryFailure`.

### R2-C — fully-bound receipt + runtime linearization
- `DeliveryReceipt` (`approval_delivery.go`) carries the full binding + claim token +
  opaque `ReceiptID`. `RecordDelivery` compares EVERY binding field and the token and
  requires a ReceiptID before committing accepted/already_accepted. A `superseded`
  flag + generation high-water (set atomically by `SupersedeRuntime`/`InvalidateSession`
  under the store lock, NO lock across delivery I/O) make an in-flight claim
  non-committable after replacement/correlation-loss/delete. `RuntimeDeliveryGate`
  serializes acceptance against replacement (old generation accepts no bytes).
  Telemetry wires `SupersedeRuntime` on launch replace + stream-generation change.
- Tests: `TestRecordDelivery_RejectsAnyBindingFieldMismatch` (per-field: token,
  approval, session, runtime, digest, idem-key, missing receipt-id),
  `TestRecordDelivery_SupersededDuringDeliveryCannotCommit` (launch/stream/correlation),
  `TestRecordDelivery_DeleteDuringDeliveryUnavailable`,
  `TestDeliveryGate_OldGenerationAcceptsNothing`, `TestClaimDelivery_RaceChurn` (race).

### R2-D — mobile UTF-8 bytes + log safety
- `mobile/src/lib/approvalRequest.ts` `boundedString` uses `TextEncoder().encode().length`
  (UTF-8 bytes), byte-compatible with backend Go `len()`. Mobile idempotency key uses
  the backend canonical grammar. Backend `sanitizeLogID` bounds + strips control/newline
  bytes from session/approval/action ids before logging.
- Tests: `approvalRequest.test.ts` multibyte bounds (Korean 3-byte, emoji 4-byte,
  combining) at/above the byte limit; `TestSanitizeLogID` (newline/CR/tab/NUL/ESC →
  `.`, bounded).

### R2-E — provider path honestly blocked
- `provenActionMapping` returns none; production wires `NewUnavailableApprovalDelivery`;
  the Codex fixture (`testdata/codex/approval_waiting.jsonl`) still only OBSERVES its
  own resolution (`waiting_for_approval` → provider-internal `approval_resolved`) — no
  verified resolution channel, and blind Y/N is prohibited. No controlled, redistributable
  provider mapping + production-wired delivery channel exists.
- Tests: `TestIngest_CodexIngestedNonActionable`, `TestProvenActionMapping_NoneProven`,
  `TestHandler_NonActionableRejectedNoDelivery`. Controlled fixtures prove the mechanism
  only; none is substituted for a production positive path.

## 3. Focused completion evidence (supplement #7)

| category | evidence |
|---|---|
| claim authority | store digest recompute, stored-perm non-override, invalid-key, input-schema, runtime/action-mismatch, one-owner race |
| cross-Approval idempotency | same-key-across-approvals → conflict; same-key/diff-digest → conflict |
| fail-closed capacity | ledger full → ClaimLedgerFull (untracked claim rejected) |
| fully-bound receipt/commit | per-field receipt-mismatch rejection + missing receipt-id |
| runtime replacement linearization | superseded-during-delivery (launch/stream/correlation) + delete + delivery-gate old-gen-accepts-nothing |
| UTF-8 byte bounds | Korean/emoji/combining at-and-above backend byte limit |
| provider action-mapping | NONE proven (`provenActionMapping` empty) — **blocked** |
| production delivery-channel | NONE (`NewUnavailableApprovalDelivery`) — **blocked** |

## 4. Gate (on 0521c3828)

```text
go test -race ./internal/agent/... ./internal/term/... ./internal/transcript/...   OK
SKIP_NATIVE_GATE=1 sh scripts/build-gate.sh:
  Backend build/vet/race/diff  OK
  Mobile typecheck (tsc)  OK    Mobile test (jest, 383)  OK
  Invariants: vendor branch  OK   ID inference  OK
  Security: secret scan  OK
git diff --check  OK
```

Android/Kotlin native gate — **environmentally unavailable, confirmed by inspection**:
`mobile/.gitignore` line 26 ignores `/android/` (the Expo-prebuild project + `gradlew`
are excluded from VCS; running it needs `expo prebuild` + an Android SDK, absent here).
A1 changed ZERO native files. Run with `SKIP_NATIVE_GATE=1`; reported as SKIPPED, not
passed.

## 5. Honest result & scope

- A1 safety foundation is hardened (R2-A..D corrected; every finding maps to code +
  a non-vacuous negative test).
- The production **actionable approval path remains BLOCKED**: no verified Codex
  action mapping and no production delivery channel exist. All production approvals
  stay non-actionable; no test-only actionable fixture substitutes for a production
  positive path, and A1 is NOT claimed complete.
- **N1 remains blocked.** No N1/O1/O2, Task/Dispatch, worker completion, automatic
  policy, generic command execution, CLI redesign, ConPTY/Windows, cloud relay, or
  lock-screen work was done.

```text
REVIEW REQUEST: A1 Approval Safety remediation 2 — 0521c3828b9d78028f64f9479f8269f469050f85
```
