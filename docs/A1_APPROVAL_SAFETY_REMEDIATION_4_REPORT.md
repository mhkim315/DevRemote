# A1 Approval Safety — Remediation 4 Report (R4-A..E)

Status: **READY FOR INDEPENDENT A1 RE-VERIFICATION** — authority + concurrency +
gate-integrity defects closed; production actionable path still BLOCKED (no provider
channel).

## 1. Identity & SHAs

```text
remote:  https://github.com/mhkim315/DevRemote.git
branch:  feature/phase10-multi-adapter
reviewed (rejected) implementation: 0ac1acf38
re-verification-3 / handoff HEAD:   703ba8cac
accepted S1.1 ancestor:             02c8385e3270fbbc4df45e0c71ccad6ebe11a076
remediation-4 implementation HEAD:  5360ec617
```

Pre-implementation contract note: `docs/A1_APPROVAL_SAFETY_REMEDIATION_4_CONTRACT_NOTE.md`.
Preserved (re-verification-3 §1): idempotent replay behind authority + copied/digested
requester context; canonical action+payload digests + substituted-payload rejection;
bounded manual retry; diagnostic log redaction; production non-actionable (empty
`provenActionMapping`, no sink).

## 2. Reproduced counterexample → fix → proof

| reproduced bypass | fix (code) | proof |
|---|---|---|
| empty HostID → `granted`; empty BootID → `granted` | `RequesterContext.present()` requires DeviceID+HostID+BearerSessionID+BootID; one shared `requesterAuthorized()` validator gates fresh/retry/already_accepted (`approval_execution.go`, `approval_store_gen.go`) | `TestClaim_IncompleteRequesterRejectedAtStore` (each empty field + missing perm → unauthorized; complete → granted) |
| `reconcileSessions(nil)` leaves the endpoint active; old RuntimeRef still accepts | `reconcileSessions` collects pruned IDs under `s.mu`, then clears status/approval stores and `deliveryGate.Deactivate(id)` OUTSIDE `s.mu` (`telemetry_service.go`) | `TestTelemetry_RegistryDisappearanceDeactivatesGate` (observes the gate accepts 0 bytes after prune) |
| gate holds `mu` across arbitrary `DeliverySink.Enqueue` | gate directly owns a concrete bounded in-memory queue; Accept = check+append atomic (no callback); capacity 0 = unavailable; queue-full fails closed; `Drain` snapshots then external I/O runs outside the lock (`approval_delivery.go`) | `TestDeliveryGate_BoundedAcceptAndCapacity`, `TestDeliveryGate_ExternalDrainDoesNotHoldTransitionGate`, `TestDeliveryGate_NegativeControlCheckThenWriteRace` |
| content-based `grep -v` secret-scan bypass | removed the `Risk-proportional` exclusion; the heading was reworded upstream, so the scan passes with no content bypass (`scripts/build-gate.sh`) | secret scan OK on the current tree; a real `sk-`/`ghp_` value is still detected (negative control) |

## 3. Invariant / cleanup-owner audit

Requester fields (all validated inside the store before claim/retry/already_accepted):
DeviceID, HostID, BearerSessionID, BootID, stored permission — **production-wired**
(`requesterAuthorized`). Client identity fields rejected (`DisallowUnknownFields`).

Delivery-gate cleanup owners (each deactivates the endpoint):

| owner | code | label |
|---|---|---|
| registry disappearance | `reconcileSessions` (pruned IDs, outside s.mu) | production-wired (`TestTelemetry_RegistryDisappearanceDeactivatesGate`) |
| explicit delete / unlink | `TelemetryService.Clear` | production-wired (`TestTelemetry_ClearDeactivatesGate`) |
| launch replacement | `invalidateForLaunch` | production-wired |
| stream-generation change | poll stream-change branch | production-wired |
| correlation loss / version conflict | poll ingest else-branch Deactivate | production-wired |

Gate lock/capacity/drain: Accept is an atomic bounded in-memory append under one
lock (no callback); capacity 0 → unavailable; queue-full → non-acceptance; Drain
snapshots under a brief lock; provider/external I/O runs after Drain, outside the
gate — **production-wired mechanism; sink capacity 0 (unavailable)**.

## 4. Gate (frozen HEAD 5360ec617)

```text
go test -race ./internal/agent/... ./internal/term/... ./internal/transcript/...   OK
SKIP_NATIVE_GATE=1 sh scripts/build-gate.sh:
  Backend build/vet/race/diff  OK
  Mobile typecheck (tsc)  OK    Mobile test (jest, 383)  OK
  Invariants: vendor branch OK  ID inference OK
  Security: secret scan  OK   (no content bypass; heading false positive gone)
git diff --check  OK
```

Android/Kotlin native gate — **environmentally unavailable, confirmed by inspection**:
`mobile/.gitignore` line 26 excludes `/android/` (Expo-prebuild `gradlew`; needs
`expo prebuild` + Android SDK, absent here); A1 changed ZERO native files. Run with
`SKIP_NATIVE_GATE=1`; reported SKIPPED, not passed.

## 5. Positive provider status & scope (R4-E)

Reconfirmed from the existing accepted adapters only: no controlled provider
evidence-to-action mapping or production delivery channel exists. `provenActionMapping`
is empty and every gate endpoint is activated with capacity 0 (no sink), so all
production approvals are non-actionable and no bytes are accepted. Controlled tests
prove the machinery only; none is substituted for a production positive path, and A1
is NOT claimed complete. Mandatory positive-path item 2 remains **BLOCKED**. No N1/O1/
O2, Task/Dispatch, worker completion, automatic policy, generic command execution,
CLI redesign, ConPTY/Windows, cloud relay, or lock-screen work. N1 remains blocked.

```text
REVIEW REQUEST: A1 Approval Safety remediation 4 — 5360ec617efd84a6b4ad1d0421452c1fe59acaad
```
