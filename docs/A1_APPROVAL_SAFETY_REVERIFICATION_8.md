# A1 Approval Safety — Independent Re-verification 8

Verdict: **REJECT — remediation 9 required; A1 and provider path remain BLOCKED**

Reviewed branch: `feature/phase10-multi-adapter`

Reviewed report HEAD: `ebdc507134b022e17f73ff987541e3cc60efb225`

Reviewed implementation: `8ec3630ea`

Accepted S1.1 ancestor: `02c8385e3270fbbc4df45e0c71ccad6ebe11a076`

The remote report and implementation are clean descendants of the prior reviewed
tree and `git diff --check` passes. This review is structural and focused: the
remaining defects are directly visible in the submitted production boundary and
tests, so another full repository gate cannot turn the result into ACCEPT.

## 1. Improvements to preserve

Remediation 8 is directionally correct and closes part of R7-A:

- `RuntimeDeliveryGate.Accept` now calls one `validGateBindingMeta` before
  mutation;
- ActionDigest and PayloadDigest require exactly 64 lowercase hex characters;
- ClaimToken requires exactly 32 lowercase hex characters;
- ApprovalID, SessionID, runtime strings and idempotency key have byte bounds;
- invalid capacity returns `("", false)` rather than silently producing a
  capacity-zero endpoint;
- `Activate` checks identity metadata before entropy allocation/publication;
- test fixtures no longer depend on deliberately non-canonical short digests and
  tokens.

Preserve these properties. Do not revert to handler-only validation, silent
capacity clamping or short test-only authority values.

## 2. Blocking findings

### R9-A1 — identity validation is length-only, not canonical

`validSessionID` checks only total length and the existence of one colon.
`validAdapterID` and `validVersion` check only non-empty length. Consequently
control characters, whitespace, path-shaped strings, invalid adapter grammar and
multiple malformed identity forms can be retained by `Activate` and `Accept`.

This does not satisfy the frozen R7 requirement to use a closed or existing
accepted grammar. The repository already has the relevant canonical boundary in
`companion-daemon/internal/mux/id_parser.go`: `SessionRef.Validate` rejects control characters and
`ValidateAdapterName` enforces `[a-z][a-z0-9_-]*`. Version validation needs an
equally explicit bounded ASCII grammar and must reject control/path/traversal
forms. Do not import the Doctor package merely to reuse a path-oriented helper;
keep the term dependency direction clean.

Minimum correction:

- parse once with `mux.ParseSessionID`, require non-empty adapter/local,
  `ref.Canonical() == input`, `ref.Validate() == nil` and
  `mux.ValidateAdapterName(ref.Adapter) == nil`;
- validate Runtime.Adapter with the accepted adapter/provider identifier grammar;
- validate Runtime.Version with one documented bounded ASCII version grammar;
- reject invalid UTF-8, controls, whitespace/path separators and traversal-shaped
  version values before publication or append;
- keep exact equality against the endpoint RuntimeRef after grammar validation.

### R9-A2 — the new Activate test is vacuous for runtime fields

`TestDeliveryGate_ActivateRejectsBadSessionAndRuntime` defines `x.session` but
calls `g.Activate(x.desc, x.rt, 4)`. Every case therefore fails because the test
description lacks a colon. The adapter-empty and version-empty cases never reach
or prove their intended validators.

Minimum correction: pass `x.session`, verify no endpoint/current/order/accounting
mutation for each case, and include accepted canonical controls. Add a known-bad
identity example that the former length-only implementation accepts.

### R9-A3 — required resource-boundary evidence is incomplete

The submitted tests do not cover the complete frozen matrix:

- exact-limit and one-over SessionID;
- exact-limit and one-over ApprovalID;
- exact-limit and one-over adapter and version;
- control/whitespace/path/traversal identity rejection;
- aggregate metadata exhaustion across multiple otherwise-valid items;
- combined payload-plus-metadata exact/one-over accounting.

`TestDeliveryGate_MetadataCountedAgainstTotalBytes` proves a single oversized
item is rejected by the per-item bound. It does not reach or prove the aggregate
`maxGateTotalQueuedBytes` boundary.

`totalItemBytes` may use a conservative repository-owned fixed overhead, but the
code/report must not call that value “exact” while it includes an estimated struct
overhead. Name and document separately:

```text
retained variable bytes (exact)
+ fixed conservative per-item accounting charge
= charged item bytes
```

Use checked addition or prove from individual field limits that overflow is
impossible before calculating aggregate admission.

### R9-B — production and concurrency evidence is still absent

The remediation 8 report explicitly defers R8-B. Therefore it cannot be marked
ready for full A1 acceptance. The old misleading tests remain:

- `TestTelemetry_RepeatedPollPreservesHandle` directly calls `gate.Activate`; it
  does not call `TelemetryService.processSession`, an accepted adapter, launch
  correlation or real stream-generation handling;
- `TestDeliveryGate_ExhaustionAndSafeVictimInvariants` fills all slots, then
  ignores the failed `sKeep` activation and performs no effective retained-item
  or safe-victim assertions;
- `TestDeliveryGate_AcceptActivateReclaimDeterministic` is sequential, has no
  barrier/channel or contested intermediate state, and its “negative control” is
  only a payload string check after drain.

These tests must be replaced, not relabeled. A passing direct-gate test is not
production-path evidence.

### R9-C — positive provider path remains unavailable

Production capacity is zero and `provenActionMapping` is empty. Preserve that
honest state. No provider research or implementation is authorized by remediation
9. A1 remains BLOCKED after R9 until a separately reviewed positive provider plan
is authorized. N1 remains blocked.

## 3. Required remediation 9 evidence

1. One canonical validator rejects malformed identity at `Activate` and `Accept`
   before mutation.
2. Every bounded field has exact-limit, one-over and invalid-grammar coverage.
3. Aggregate metadata and payload-plus-metadata admission are actually reached in
   tests and leave state unchanged on failure.
4. The Activate tests pass the intended session argument and inspect all retained
   state after failure.
5. The real `TelemetryService.processSession` accepted-adapter path preserves one
   handle for repeated same-runtime polls and performs one replacement for a real
   generation change.
6. All-active and retired-nonempty exhaustion fail before mutation; a drained,
   inactive empty endpoint is reclaimed deterministically without affecting
   another endpoint.
7. A deterministic accept/activate/reclaim interleaving inspects the contested
   mapping, queue, item and byte state. A known-bad control proves the test is not
   vacuous.
8. The exact final report HEAD passes the proportional focused tests and final
   repository gate. Android may be reported only as PASS or a documented
   environmental skip.
9. Provider mapping/capacity remain disabled and A1/N1 remain BLOCKED.

Continue only from
`docs/NEXT_SESSION_A1_APPROVAL_SAFETY_REMEDIATION_9_HANDOFF.md`.
