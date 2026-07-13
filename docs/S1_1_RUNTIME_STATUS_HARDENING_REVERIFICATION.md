# S1.1 Runtime Status Hardening — Remediation Re-verification

Decision: **REJECT — R1/R2 accepted; one R3 concurrency blocker remains**

Reviewed remediation implementation:
`ef47b551e6c9fdb141676b35c0731be8c0e35d14`

Reviewed report HEAD:
`faa391736637cb59b6b67173550dc504aeabf0cb`

Prior independent decision:
`docs/S1_1_RUNTIME_STATUS_HARDENING_VERIFICATION.md`

## 1. Repository and gate evidence

- canonical remote and branch match the prior review;
- local and remote review HEAD matched at
  `faa391736637cb59b6b67173550dc504aeabf0cb`;
- reviewed implementation `715c22b` and accepted S1 marker `70ef5df` are
  ancestors;
- review-start worktree was clean;
- focused transcript/term `go test -race`: pass;
- stable-HEAD `sh scripts/build-gate.sh`: **ALL GATES PASSED**, including Android
  Kotlin compile in the verifier environment.

## 2. R1 — ACCEPT

`locateWinningSeq` now matches status, provenance, and confidence after the same
clamp normalization used by frozen `ResolveStatus`. The unequal-confidence
counterexample is covered, true ties retain latest-Seq ordering, and the frozen
contract/public DTO remain unchanged.

## 3. R2 — ACCEPT

The actual `sess.AdapterName()` now reaches `LaunchCorrelation`, which requires
a non-empty exact match with `LaunchBinding.Adapter`. Direct and production-path
mismatch tests are present; controlled_pty's valid path remains accepted.

## 4. R3 — REJECT: replacement is still not one atomic transition

Current production implementation in
`internal/term/telemetry_service.go:RegisterOrReplaceLaunch` performs:

```text
ReserveLaunchGeneration()
LookupLaunch(sessionID)
invalidateForLaunchLocked(...)
PublishLaunch(...)
```

Each registry operation takes and releases its mutex independently. The complete
reserve/invalidate/publish transition is therefore not serialized against
another replacement for the same session.

Deterministic failing interleaving:

```text
replacement A reserves generation 2
replacement B reserves generation 3
B invalidates generation 3 and publishes generation 3
A's generation-2 invalidation is rejected as stale
A publishes generation 2 over generation 3
```

`PublishLaunch` does not reject a generation older than the currently published
binding, so the active launch identity regresses. The same issue exists when two
first registrations race: both may observe no binding, and the lower reserved
generation may publish last without the required replacement invalidation.

The new `TestS11R3_ConcurrentPollReplacementRace` does not prove the requested
property:

- it has no barrier that forces the unsafe interleaving;
- it records `lastGen` but never asserts it;
- a final replacement after all goroutines finish overwrites any intermediate
  regression before assertions run.

The split exported `ReserveLaunchGeneration`/`PublishLaunch` API and the
replacement-capable `RegisterLaunch` convenience also leave publication paths
that do not enforce pre-publication invalidation as a non-bypassable invariant.

## 5. Required final remediation

- make one registry-owned, serialized per-session (or safely global) transition
  own generation allocation, replacement detection, pre-publication
  invalidation, and publication;
- ensure no lower/equal generation can replace a higher published binding;
- ensure concurrent first registrations cannot both bypass replacement
  invalidation;
- remove or fail-close split/public convenience paths that can publish a
  replacement without the transition;
- add a deterministic barrier-controlled test that forces reverse completion
  order and observes the binding/high-water before any cleanup or final
  replacement;
- retain poll/write race, per-session isolation, delete/recreate monotonicity,
  and full race gates.

Do not change accepted R1/R2, the frozen T0 contract, the public DTO, local CLI
behavior, A1, N1, O1, or O2.
