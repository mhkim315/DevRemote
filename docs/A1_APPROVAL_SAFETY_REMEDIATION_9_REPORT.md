# A1 Approval Safety — Remediation 9 Report

Status: **R9-A + R9-B complete and gated; A1 / provider path / N1 remain BLOCKED**

Scope executed: R9-A (canonical identity + separated accounting) and R9-B (real
telemetry-path activation, explicit capacity states, deterministic contested-state
proof). R9-C is a reconfirmation only. No provider-positive path, N1, O1 or O2 work
was performed.

## 0. Repository state

```text
repository:  https://github.com/mhkim315/DevRemote.git
root:        /Users/mhk/Documents/codex/DevRemote
branch:      feature/phase10-multi-adapter
baseline (reviewed reject):     a179d23001c919fd5d42b3ceaba09549cb13fd56
R9-A checkpoint (implementation):1a5fb4b
R9-B checkpoint (implementation):5acc36f31d5e5ac33841bffe609654cffd7e534d
accepted S1.1 ancestor:          02c8385e3270fbbc4df45e0c71ccad6ebe11a076   (ancestor ✓)
reviewed rem-8 implementation:   8ec3630ea                                 (ancestor ✓)
```

Onboarding was performed before editing (pwd, remote, branch, fetch, HEAD==origin,
ancestry, clean worktree). Fast-forward only; no history rewrite, reset, or force-push.
A stale second checkout at `/Users/mhk/.gemini/antigravity/scratch/DevRemote` (at the
older `ebdc5071`) was identified as an ancestor and NOT used.

## 1. R9-A — canonical identity + accounting (checkpoint 1a5fb4b)

Production: `companion-daemon/internal/term/approval_delivery.go`.

| Requirement | Resolution | Evidence |
|---|---|---|
| R9-A1 SessionID canonical | `validSessionID`: `mux.ParseSessionID` + non-empty adapter/local + `Canonical()==input` + `SessionRef.Validate()==nil` + `mux.ValidateAdapterName(adapter)==nil` | production-wired |
| R9-A1 Adapter grammar | `validAdapterID`: `mux.ValidateAdapterName` + ≤64 | production-wired |
| R9-A1 Version grammar | `validVersion`: `[A-Za-z0-9][A-Za-z0-9._-]{0,63}`; rejects slash/backslash/control/whitespace/traversal | production-wired |
| R9-A2 Activate test passes x.session | `TestDeliveryGate_ActivateRejectsBadSessionAndRuntime` now passes `x.session`, asserts no mutation on each rejection, includes a known-bad the old length-only impl accepted | focused proof |
| R9-A3 exact vs conservative accounting | `exactRetainedVariableBytes` (exact) + named `gateItemFixedCharge` (conservative) = `chargedItemBytes`, with an overflow-impossibility proof from per-field bounds | production-wired |

R9-A tests (focused, `-race`): `TestDeliveryGate_ActivateBoundsExactAndOneOver`,
`TestDeliveryGate_AcceptApprovalIDBounds`,
`TestDeliveryGate_AcceptRevalidatesIdentityGrammar`,
`TestDeliveryGate_ChargedItemBoundaryExactAndOneOver`,
`TestDeliveryGate_AggregateExhaustionReachesGlobalBound` (reaches the global
`maxGateTotalQueuedBytes` across retired endpoints; proven non-vacuous by a fresh-gate
accept of the identical item), `TestDeliveryGate_AcceptPayloadNonAliasing`. Every
rejection asserts unchanged `endpoints`, `current`, `order`, per-endpoint `queuedBytes`,
`queue` length, `seq`, and global `totalBytes`.

Note on the local-ID contract: a space or Unicode in the LOCAL id is valid per the
repository's canonical `SessionRef.Validate` (only the adapter is grammar-constrained
and only control chars are rejected in the local id). Whitespace/path rejection is
enforced on the adapter (`ValidateAdapterName`) and on the version (`validVersion`),
matching the canonical boundary the handoff mandated — no stricter rule was invented.

## 2. R9-B — production path + deterministic contested state (checkpoint 5acc36f)

The three reviewer-rejected tests (`TestTelemetry_RepeatedPollPreservesHandle`,
`TestDeliveryGate_ExhaustionAndSafeVictimInvariants`,
`TestDeliveryGate_AcceptActivateReclaimDeterministic`) were **removed**, not relabeled.

| Requirement | Resolution | Evidence |
|---|---|---|
| R9-B1 real telemetry path | `TestDeliveryGate_ProductionTelemetryPathActivation` drives the gate ONLY via `s1cPoll → TelemetryService.processSession → accepted-adapter + managed-launch correlation → deliveryGate.Activate`. No direct `gate.Activate` is used as evidence. | production-wired |
| R9-B1 same-runtime idempotent | 5 repeated polls preserve one handle + endpoint count | production-wired |
| R9-B1 one real gen change → one replacement | production `RegisterOrReplaceLaunch` (adapter-epoch reset + old-gen deactivate) then fresh log content re-establishes correlation → exactly one new handle; next same-gen poll makes none | production-wired |
| R9-B1 correlation loss deactivates | `RemoveLaunch` → next poll deactivates (current handle empty) | production-wired |
| R9-B2 all-active exhaustion | `TestDeliveryGate_CapacityAllActiveFailsUnchanged`: fails, all state unchanged | contract proof |
| R9-B2 retired-nonempty at bound | `TestDeliveryGate_CapacityRetiredNonEmptyFailsBeforeDrain`: fails before drain; exact item/receipt/bytes remain; no ignored `ok`/handle/receipt/drain | contract proof |
| R9-B2 deterministic safe victim | `TestDeliveryGate_CapacitySafeVictimReclaimedDeterministically`: net-zero reclaim; unrelated endpoint + item untouched | contract proof |
| R9-B3 deterministic interleaving | `TestDeliveryGate_DeterministicAcceptVsReplacementInterleaving`: narrow nil-in-production `acceptEntryHook` pauses an accept (bound to gen A) at a named contested point BEFORE the lock; the A→B replacement is driven to completion and the contested current mapping/ownership/queue/bytes inspected; the single-lock re-read of current at commit rejects the stale accept | contract proof (race), 30× stable |
| R9-B3 negative control | `staleGate` reproduces the destructive post-mutation admission (capture-at-entry, write-after-replacement) the atomic gate prevents — not a payload-check-after-drain, no sleeps | contract proof |

## 3. R9-C — provider blocker reconfirmed (no change)

`provenActionMapping` returns `(nil, false)` for every provider; production wires
`NewGatedApprovalDelivery` and activates every endpoint with capacity 0 (no channel),
so `Accept` returns unavailable and no bytes are ever accepted. No heuristic CTA,
claim, or delivery. A1 and N1 remain BLOCKED. No provider research/implementation was
done; the unpushed Codex positive-path plan is not part of this remediation.

## 4. Gate result (full, on frozen HEAD 5acc36f)

`scripts/build-gate.sh` → **ALL GATES PASSED**:

- Backend: `go build ./...` OK; `go vet ./...` OK; `go test -race ./... -count=1` OK
  (incl. `internal/agent/doctor` 120s, `internal/term` 22s); `git diff --check` OK.
- Mobile: `npm run typecheck` OK; `npm test` OK; android kotlin compile OK.
- Invariants: vendor-branch scan OK; ID-inference scan OK.
- Security: secret scan OK. (The R9 diff introduces no secret; only pre-existing
  test fixtures and the doctor's own scanner patterns match a broad grep.)

Evidence levels per `EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md` §11: full-gate proof for
the tree; no physical/M-track work was in scope. Android reported PASS (not skipped).

## 5. Stop condition

Stopping for independent verification. No provider-positive work, N1, O1, or O2 begun.

REVIEW REQUEST: A1 Approval Safety remediation 9 — 5acc36f31d5e5ac33841bffe609654cffd7e534d
