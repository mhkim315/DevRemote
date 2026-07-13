# Next Session Handoff — S1.1 Final Registry Cleanup

Status: **COMPLETE — SUPERSEDED BY S1.1 FINAL ACCEPTANCE**

Accepted implementation: `02c8385e3270fbbc4df45e0c71ccad6ebe11a076`.
Continue from `docs/NEXT_SESSION_A1_APPROVAL_SAFETY_HANDOFF.md`.

The R1 winner binding, R2 adapter binding, and R3 same-session transaction are
independently accepted. Do not redesign them. Close only the two findings in:

`docs/S1_1_RUNTIME_STATUS_HARDENING_REVERIFICATION_2.md`.

## 0. Canonical identity

```text
remote: https://github.com/mhkim315/DevRemote.git
branch: feature/phase10-multi-adapter
reviewed report HEAD: 4e096bfc9b8af75ca1dea28da15e2a03fc8984ed
reviewed implementation: ff4f61affc17b0f60b6fc67c8f41c2605e02ed1f
prior review docs: 5896603b84f4d39ffc931075fcb15474e0dd5ed0
accepted S1 marker: 70ef5df28dede7b0f3025eeaab7826f76a229fbf
```

Fetch and fast-forward the canonical branch, verify full local/remote SHA
equality, clean worktree, correct remote, and ancestry of `ff4f61a`, `ef47b55`,
and `70ef5df`. Never reset, stash, force-push, or reconstruct from chat.

## 1. Packet C1 — bounded transition synchronization

Current defect: `LaunchRegistry.gates` retains one mutex for every historical
session ID forever.

Preferred smallest design: replace the dynamic map with a fixed-size striped
gate array selected by a stable hash of canonical session ID. Hash collisions may
serialize unrelated sessions but cannot weaken correctness; the memory bound is
constant and no gate-retirement/ABA protocol is needed. A reference-counted map is
acceptable only with a clear waiter lifetime proof and race tests.

Required invariants:

- same-session transitions and removal use the same gate;
- different session IDs need not always run concurrently if stripes collide;
- generation counter and binding map semantics remain unchanged;
- no dynamic synchronization state grows with historical session count;
- no sleeps or unsafe pointer retirement.

Required tests:

- thousands of unique register/remove operations leave synchronization state at
  a fixed bound;
- same-session deterministic serialization still passes;
- delete racing replacement stays safe;
- repeated race suite passes.

## 2. Packet C2 — non-bypassable replacement boundary

Current defect: exported `RegisterLaunch(spec)` can replace with a nil
invalidation callback, and recognized create falls back to it when telemetry is
nil.

Required behavior:

- expose one production registration/replacement API whose replacement branch
  cannot publish until required invalidation succeeds;
- if a helper is retained for first registration, it must detect an existing
  binding and fail closed without publishing;
- do not treat a nil callback as permission to replace;
- `createFromProfile` with missing required telemetry/status wiring must either
  fail closed/clean up or use an equally safe owned boundary; it must not silently
  weaken identity replacement;
- test fixtures should use isolated registry instances or explicit safe helpers,
  not a production bypass.

Required negative tests:

- nil-invalidation replacement leaves the existing binding unchanged and reports
  failure;
- missing telemetry create cannot replace recognized identity unsafely;
- normal first registration succeeds;
- normal production replacement still invalidates before publish;
- all prior R1/R2/R3 tests remain green.

Do not solve this by clearing status or removing the monotonic generation check.

## 3. Final gate and report

Run focused transcript/term tests repeatedly with `-race`, then the complete
stable-HEAD `scripts/build-gate.sh`. Update the implementation report with the
bounded synchronization design, bypass closure, exact negative tests, full SHA,
and a new review marker. Commit and push without force; verify canonical
local/remote equality, ancestry, and clean worktree; then stop. Do not begin A1.

## 4. Explicitly deferred

- local `pokit run` recognized-agent redesign;
- R1/R2 or public DTO changes;
- ApprovalStore, notifications, Task/Dispatch, O1/O2;
- terminal/OS certification, Windows, or SSH.
