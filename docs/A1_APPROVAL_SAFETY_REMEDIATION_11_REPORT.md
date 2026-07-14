# A1 Approval Safety — Remediation 11 Report

Status: **R11 complete and gated; A1 / provider path / N1 remain BLOCKED**

Remediation 11 closes the two test-evidence blockers from the R10 re-verification. The
changes are TEST-ONLY (`internal/term`); no production behavior changed. No
provider-positive path, N1, O1, or O2 work was done.

## 0. Repository state

```text
repository:  https://github.com/mhkim315/DevRemote.git   root: /Users/mhk/Documents/codex/DevRemote
branch:      feature/phase10-multi-adapter
R10 baseline (rejected): 549786dadf0df18190db98511a93146a1a4a53d5
R11 implementation:      d5a965cad4d9b499a5c3bc132be2aecd64f91e86
accepted S1.1 ancestor:  02c8385e3270fbbc4df45e0c71ccad6ebe11a076  (ancestor ✓)
R10 impl 2e70512:        (ancestor ✓)
```

Onboarded (fetch, HEAD==origin, ancestry, clean) before editing; fast-forward only.

## 1. Blocker resolutions

### Blocker 1 — deep gate snapshot omitted queue CONTENT (RESOLVED)

`gateSnap`/`snapshotGate` recorded only each endpoint's queue length and byte total, so
a rejected `Activate`/`Accept` that mutated an existing queued item in place (at equal
length/bytes) could pass `assertUnchanged`. The snapshot now defensively deep-copies
every queued `AcceptedDelivery` into an `itemSnap` — `Binding` (value; strings compared
by content), `ClaimToken`, `ReceiptID`, and `Payload` stored as an immutable string
copy (`string(it.Payload)`), so a later in-place mutation of the live payload bytes
cannot alias the snapshot. `assertUnchanged` (`reflect.DeepEqual` over the whole
`gateSnap`) therefore now compares the full content of every queued item.

The retired-non-empty rejection test (`TestDeliveryGate_CapacityRetiredNonEmptyFailsBeforeDrain`)
and the aggregate-exhaustion rejection test
(`TestDeliveryGate_AggregateExhaustionReachesGlobalBound`) both take `before :=
snapshotGate(g)` and call `before.assertUnchanged(...)`, so they now assert the retained
queue content is byte-for-byte identical after the rejected operation. All R9-A
rejection tests share this deep comparison.

### Blocker 2 — production replacement checked only order length (RESOLVED)

`TestDeliveryGate_ProductionTelemetryPathActivation` previously checked `len(order) ==
count`, which a wrong state (removed `h1` still in `order`, current `h2`) could satisfy
at equal length. It now asserts EXACT ownership under the gate lock:

- after the launch-generation replacement: `order == [h2]`, `current == {sid: h2}`,
  `endpoints == {h2}`, and `endpoints[h1] == nil` (old generation reclaimed);
- the same-generation poll leaves that exact state (`order==[h2]`, single endpoint,
  `current[sid]==h2`);
- after correlation loss (`RemoveLaunch` + poll): `current` is empty, `order == [h2]`
  and `endpoints == {h2}` retained but inactive, and a fully-canonical `Accept` for the
  session fails (acceptance genuinely disabled).

## 2. Provider blocker (unchanged)

`companion-daemon/internal/term/telemetry_service.go:331` still activates every endpoint
with capacity 0; `provenActionMapping` returns `(nil, false)`; no heuristic CTA/claim/
delivery. A1 and N1 remain BLOCKED. No provider-positive work performed.

## 3. Gate result (full, on frozen HEAD d5a965c)

`scripts/build-gate.sh` → **ALL GATES PASSED**: backend build/vet/`-race`/diff-check;
mobile typecheck/test/kotlin; vendor-branch + ID-inference invariants; secret scan. The
R11 diff is test-only and introduces no secret. Interleaving and production-path tests
stable at 20×. Full-gate proof per `EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md` §11;
Android reported PASS.

## 4. Stop condition

Stopping for independent verification. No provider-positive work, N1, O1, or O2 begun.

REVIEW REQUEST: A1 Approval Safety remediation 11 — d5a965cad4d9b499a5c3bc132be2aecd64f91e86
