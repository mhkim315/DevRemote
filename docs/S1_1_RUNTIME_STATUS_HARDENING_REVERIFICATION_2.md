# S1.1 Runtime Status Hardening — R3-2 Re-verification

Decision: **REJECT — serialization accepted; two bounded/non-bypassable cleanup blockers remain**

Reviewed implementation: `ff4f61affc17b0f60b6fc67c8f41c2605e02ed1f`

Reviewed report HEAD: `4e096bfc9b8af75ca1dea28da15e2a03fc8984ed`

## 1. Accepted remediation evidence

- the same-session reserve/invalidate/publish transition is now serialized;
- invalidation runs before publication without holding the registry map mutex;
- stale generation publication is rejected;
- concurrent first registrations are serialized;
- `RemoveLaunch` is serialized against a same-session transition;
- the deterministic gate test is non-sleep-based and observes contested state;
- focused transcript/term race suites pass;
- stable-HEAD `scripts/build-gate.sh` reports **ALL GATES PASSED**, including
  Android Kotlin compile in the verifier environment;
- accepted R1/R2 and the frozen contract/public DTO remain unchanged.

The prior lower-generation-after-higher-generation defect is closed.

## 2. BLOCKER 1 — per-session gate registry is unbounded

`internal/transcript/launch_binding.go:LaunchRegistry` adds:

```go
gates map[string]*sync.Mutex
```

`sessionGate` creates one entry for every new session ID. `RemoveLaunch` deletes
the binding but never deletes or retires the gate. Since normal session IDs are
unique, repeated create/delete permanently grows `gates` even while `bindings`
and the status store remain bounded.

This violates the original S1.1-B requirement to keep launch registry state
bounded and S1.1-C deterministic bounded cleanup. Existing delete/recreate tests
only inspect bindings/generations; none assert auxiliary gate state after churn.

Required remediation:

- use bounded transition synchronization, such as a fixed-size striped lock set,
  or a correctly reference-counted gate entry with ABA-safe retirement;
- do not delete a gate while a waiter can still hold its pointer;
- add large unique-session create/remove churn proving auxiliary synchronization
  state is constant-bounded or returns to its baseline;
- preserve same-session serialization and race cleanliness.

## 3. BLOCKER 2 — replacement publication still has a nil-invalidation bypass

`RegisterLaunch` remains exported and calls:

```go
globalLaunchRegistry.RegisterOrReplace(spec, nil)
```

It can replace an existing live binding without installing status/ingestion
invalidation. `createFromProfile` also uses `RegisterLaunch` when
`Handlers.Telemetry == nil`. Thus the safe transition algorithm exists, but the
package does not make pre-publication invalidation non-bypassable for a
replacement.

This directly misses the R3-2 handoff requirement to remove, unexport, or
fail-close every replacement-capable convenience path that bypasses the
invariant.

Required remediation:

- make the production publish API one non-bypassable boundary;
- a first-registration-only helper must fail closed if a binding already exists,
  rather than replacing it without invalidation;
- recognized create wiring must not silently take a weaker replacement path when
  telemetry/status wiring is absent;
- migrate tests to an explicit isolated registry/test helper rather than retaining
  a weaker global production API;
- add negative tests for attempted nil-invalidation replacement and the
  `Telemetry == nil` create condition.

## 4. Scope and stop

Do not reopen accepted R1/R2 or the R3 serialization algorithm. Do not change
local CLI behavior, T0/public DTO, A1, N1, O1, O2, terminal certification,
Windows, or SSH. Close only bounded synchronization cleanup and the replacement
bypass, rerun stable-HEAD full gates, and request one more independent S1.1
review.
