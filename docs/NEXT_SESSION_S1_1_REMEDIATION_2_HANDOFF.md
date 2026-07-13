# Next Session Handoff — S1.1 R3 Atomicity Remediation 2

Status: **SUPERSEDED — R3 SERIALIZATION ACCEPTED; FINAL CLEANUP REQUIRED**

Continue only from `docs/NEXT_SESSION_S1_1_REMEDIATION_3_HANDOFF.md`. This file
remains the historical transaction-remediation contract.

R1 exact winner binding and R2 adapter binding are independently accepted. Do
not redesign or broaden them. Fix only the remaining R3 registry transaction
identified in:

`docs/S1_1_RUNTIME_STATUS_HARDENING_REVERIFICATION.md`.

## 0. Canonical repository identity

```text
remote: https://github.com/mhkim315/DevRemote.git
branch: feature/phase10-multi-adapter
reviewed report HEAD: faa391736637cb59b6b67173550dc504aeabf0cb
reviewed remediation implementation: ef47b551e6c9fdb141676b35c0731be8c0e35d14
prior review docs commit: 07a4bb38479d34c2a321d3e3952f761c5dbeff23
accepted S1 marker: 70ef5df28dede7b0f3025eeaab7826f76a229fbf
```

Before editing, verify top-level, canonical remote, branch, clean worktree,
local/remote equality, and ancestry of `ef47b55`, `715c22b`, and `70ef5df` after
fetch + fast-forward. Never reset, stash, force-push, or reconstruct from chat.

## 1. Read order

1. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
2. this handoff;
3. `docs/S1_1_RUNTIME_STATUS_HARDENING_REVERIFICATION.md`;
4. the prior verification and remediation handoff;
5. `internal/transcript/launch_binding.go` and tests;
6. `internal/term/telemetry_service.go`;
7. `internal/term/s1_1r3_atomic_replacement_test.go`;
8. `internal/term/create.go` and production create tests.

## 2. Exact blocker

The current boundary is a sequence of separately locked operations. Concurrent
replacements can publish a lower reserved generation after a higher generation,
and concurrent first registrations can both skip invalidation.

Required invariant for one canonical session ID:

```text
lock/serialize transition
→ allocate generation
→ determine first registration vs replacement
→ if replacement, install status + ingestion invalidation at that generation
→ publish binding
→ release transition
```

While invalidation runs, `LookupLaunch` may continue to expose the old binding or
block, but it must never expose the new binding before its high-water exists.
Another registration for the same session must not interleave. Publication must
reject a generation that is not strictly newer than the currently published
generation.

## 3. Implementation constraints

- Prefer a single registry-owned registration/replacement operation. If it takes
  a pre-publication invalidation callback, audit lock ordering and prove there is
  no inverse registry/status/telemetry lock acquisition.
- A per-session transition lock is acceptable if bounded cleanup and lock order
  are explicit; a short global transition lock is acceptable if measurements do
  not justify extra complexity.
- Remove, unexport, or fail-close `ReserveLaunchGeneration`, `PublishLaunch`, and
  any replacement-capable convenience path that bypasses the invariant.
- Preserve lifecycle `Clear` versus replacement invalidation semantics.
- Preserve process-wide monotonic generations, delete/recreate behavior, and
  session isolation.
- Do not use sleeps to establish ordering.

## 4. Required deterministic tests

1. Two same-session replacements are paused with barriers after allocation so
   the higher generation attempts to finish first and the lower generation
   attempts to finish last; the binding must never regress.
2. Two concurrent first registrations cannot both take a no-invalidation path.
3. A telemetry lookup/poll cannot observe the new generation before its
   non-current high-water is installed.
4. No old stream evidence can commit under the new launch generation.
5. Different sessions remain independent.
6. Delete/recreate stays strictly monotonic.
7. Repeated `-race` execution passes.

Assertions must inspect the contested state before any final cleanup replacement
can mask an intermediate failure. Remove the unused `lastGen` pattern or assert
the actual publication history monotonically.

## 5. Explicitly out of scope

- R1/R2 redesign;
- local `pokit run` recognized-agent redesign;
- Task/Dispatch, ApprovalStore, notifications, O1/O2;
- public DTO or frozen T0 changes;
- terminal certification, Windows, or SSH expansion.

## 6. Gate and stop

Run focused deterministic tests, repeated race tests, and the complete
`scripts/build-gate.sh` on a stable HEAD. Update the implementation report with
the exact transaction and lock-order proof, test names, full implementation SHA,
and one new review marker. Commit and push without force, verify canonical
local/remote equality and clean worktree, then stop. Do not begin A1.
