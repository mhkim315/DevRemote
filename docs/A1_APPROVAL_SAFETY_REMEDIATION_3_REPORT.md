# A1 Approval Safety — Remediation 3 Report (R3-A..E)

Status: **READY FOR INDEPENDENT A1 RE-VERIFICATION** — authority + concurrency
defects closed; production actionable path still BLOCKED (no provider channel).

## 1. Identity & SHAs

```text
remote:  https://github.com/mhkim315/DevRemote.git
branch:  feature/phase10-multi-adapter
reviewed (rejected) implementation: 0521c3828b9d78028f64f9479f8269f469050f85
re-verification-2 / handoff:        701518a6740c1798fbfc41c6208cecf46b714ad3
accepted S1.1 ancestor:             02c8385e3270fbbc4df45e0c71ccad6ebe11a076
remediation-3 implementation HEAD:  50e1bcd6576f06e6ea2978f88385cf855a0e9ed9

commits:
  0ac1acf38  R3-A..E code (claim authority, payload binding, gate, retry, logs)
  50e1bcd65  R3 secret-scan hygiene (comment/test literals, false-positive exclusion)
```

Pre-implementation contract note: `docs/A1_APPROVAL_SAFETY_REMEDIATION_3_CONTRACT_NOTE.md`.
Preserved R2 corrections (re-verification-2 §1): digest recompute, stored permission,
cross-approval conflict, capacity fail-closed, receipt field comparison, superseded
non-commit, mobile UTF-8 bytes, production non-actionable + no-bytes delivery.

## 2. Reproduced counterexample → fix → proof

| reproduced bypass | fix | proof (label) |
|---|---|---|
| same DeviceID, different host/bearer/boot, no perm → `already_accepted` | idempotent replay runs BEHIND full authority; immutable `RequesterAuthContext` (device/host/bearer/boot + sorted-perm digest) bound in claim+ledger | `TestClaim_IdempotentReplayRequiresFullCurrentAuthority`, `TestClaim_MutatedCallerSliceDoesNotMatch` — production-wired |
| claim input X, deliver Y, receipt echoes binding → commit | store computes the ONLY canonical payload + domain-separated `PayloadDigest` in the binding; handler uses store payload; commit requires `receipt.DeliveredPayloadDigest == binding.PayloadDigest` | `TestRecordDelivery_SubstitutedPayloadCannotCommit`, `TestRecordDelivery_RejectsAnyBindingFieldMismatch` (incl. pdigest/bad-pbytes) — production-wired |
| `RuntimeDeliveryGate` test-only; check-then-write race | `Accept` is the sole acceptance point: verify current gen-owned endpoint + active + sink AND enqueue exact bytes under ONE lock; production `ApprovalDelivery` = gated boundary (app.go); telemetry drives Activate/Deactivate | `TestDeliveryGate_AcceptAtomicVsReplacement` (barrier + bypass negative control), `TestDeliveryGate_OldGenerationAcceptsNothing` — production-wired (sink **unavailable**) |
| universal "no retry" contradicts frozen plan | bounded manual retry: a non-accepting outcome makes the record re-claimable with the SAME key/binding/auth up to `maxManualRetries=2`, then `retry_exhausted`; superseded/ambiguous non-retryable; no auto-retransmit; restart leaves none | `TestRetry_BoundedManualRetryAfterNonAcceptance`, `TestRetry_SupersededFailureNotRetryable` — production-wired |
| `sanitizeLogID` leaves paths/tokens verbatim | strip control bytes then apply `redactStr` (home paths, sk-/ghp_/xox/Bearer, Authorization, key=value secrets), bounded | `TestHandler_LogRedaction` (actual log capture; Unix+Windows paths, secret/token patterns) — production-wired |

## 3. Invariant-by-invariant audit

| authority field | production code | test | label |
|---|---|---|---|
| ApprovalID/SessionID | `binding` | receipt-mismatch | production-wired |
| Runtime (adapter/version/launch/stream) | claim recompute + commit + pre-delivery revalidate + `superseded` | full-authority replay, receipt-mismatch | production-wired |
| ActionDigest | store recompute | arbitrary-assertion | production-wired |
| PayloadDigest (domain-sep) | binding + `DeliveredPayloadDigest` at commit | substituted-payload | production-wired |
| IdempotencyKey (canonical) | binding + ledger | invalid-key, capacity | production-wired |
| RequesterAuthContext (device/host/bearer/boot/perm-digest) | ledger + record, checked before replay | full-authority replay, mutated-slice | production-wired |
| stored permission | record, every claim | stored-perm-non-override | production-wired |
| claim token | record, commit | receipt-mismatch | production-wired |
| ReceiptID | receipt, required on accept | receipt-mismatch | production-wired |
| supersession | `SupersedeRuntime`/`InvalidateSession`/`Clear` (telemetry) | superseded-during-delivery | production-wired |
| delivery acceptance (atomic) | `RuntimeDeliveryGate.Accept`; `gatedApprovalDelivery` (app.go); telemetry Activate/Deactivate | gate barrier + old-gen + negative control | production-wired; **sink unavailable** |
| bounded manual retry | record `retries` | retry bound/exhaust/superseded | production-wired |
| logs | `sanitizeLogID`+`redactStr` | log-capture | production-wired |
| positive provider path | `provenActionMapping` empty; no sink | non-actionable ingest | **blocked** |

## 4. Gate (frozen HEAD 50e1bcd65)

```text
go test -race ./internal/agent/... ./internal/term/... ./internal/transcript/...   OK
SKIP_NATIVE_GATE=1 sh scripts/build-gate.sh:
  Backend build/vet/race/diff  OK
  Mobile typecheck (tsc)  OK    Mobile test (jest, 383)  OK
  Invariants: vendor branch OK  ID inference OK
  Security: secret scan  OK
git diff --check  OK
```

Android/Kotlin native gate — **environmentally unavailable, confirmed by inspection**:
`mobile/.gitignore` line 26 excludes `/android/` (Expo-prebuild `gradlew`, needs
`expo prebuild` + Android SDK, absent here); A1 changed ZERO native files. Run with
`SKIP_NATIVE_GATE=1`; reported SKIPPED, not passed.

## 5. Positive provider status & scope

`provenActionMapping` yields no options and production registers no delivery sink —
no controlled provider mapping / production delivery channel exists. Mandatory A1
positive-path item 2 remains **BLOCKED**; all production approvals are non-actionable.
No test fixture is substituted for the production positive path and A1 is NOT claimed
complete. No N1/O1/O2, Task/Dispatch, worker completion, automatic policy, generic
command execution, CLI redesign, ConPTY/Windows, cloud relay, or lock-screen work.
N1 remains blocked.

```text
REVIEW REQUEST: A1 Approval Safety remediation 3 — 50e1bcd6576f06e6ea2978f88385cf855a0e9ed9
```
