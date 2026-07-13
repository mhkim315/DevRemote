# Next Session Handoff — S1.1 Focused Remediation

Status: **SUPERSEDED — R1/R2 ACCEPTED, R3 REMEDIATION 2 REQUIRED**

Do not execute this packet again. Continue from
`docs/NEXT_SESSION_S1_1_REMEDIATION_2_HANDOFF.md`. This file remains the
historical first-remediation contract.

Independent decision:
`docs/S1_1_RUNTIME_STATUS_HARDENING_VERIFICATION.md` — **REJECT**

Do not start A1, N1, O1, O2, worker protocols, terminal certification, Windows,
or a redesign of `pokit run`. Fix only the three independently verified S1.1
contract defects.

## 0. Canonical repository identity

```text
remote: https://github.com/mhkim315/DevRemote.git
branch: feature/phase10-multi-adapter
reviewed report HEAD: 1547c934113eb21a8dcc0e1ff47eb037493b3125
reviewed implementation: 715c22b59320fa80628b394bde1cd4a4a07571e0
accepted S1: b6504bd7d5c634f0c0459ae87503b82d17c1537b
accepted S1 marker: 70ef5df28dede7b0f3025eeaab7826f76a229fbf
```

A directory name is not repository identity. Before editing:

```sh
pwd
git rev-parse --show-toplevel
git remote -v
git branch --show-current
git status --short
git fetch origin feature/phase10-multi-adapter
git merge --ff-only origin/feature/phase10-multi-adapter
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git merge-base --is-ancestor 715c22b59320fa80628b394bde1cd4a4a07571e0 HEAD
git merge-base --is-ancestor 70ef5df28dede7b0f3025eeaab7826f76a229fbf HEAD
```

The two tips must match, both ancestry checks must exit 0, and the worktree must
be clean. Do not reset, stash, force-push, or reconstruct missing work from chat.

## 1. Read order

1. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
2. this handoff;
3. `docs/S1_1_RUNTIME_STATUS_HARDENING_VERIFICATION.md`;
4. `docs/NEXT_SESSION_S1_1_RUNTIME_STATUS_HARDENING_HANDOFF.md`;
5. `docs/S1_1_RUNTIME_STATUS_HARDENING_IMPLEMENTATION_REPORT.md`;
6. frozen `internal/agent/contract/validate.go` and contract tests;
7. `internal/term/agent_status_store.go`, `adapter_state.go`,
   `telemetry_service.go`, and S1.1 tests;
8. `internal/transcript/launch_binding.go` and its tests;
9. `internal/term/create.go` and the actual production create tests.

Before editing, report a three-row audit: blocker, exact current function,
smallest repair, and production negative test.

## 2. Packet R1 — exact winning-evidence binding

Problem: `locateWinningSeq` matches `(status, provenance)` but frozen
`ResolveStatus` also selects on normalized confidence.

Required:

- preserve frozen `ResolveStatus` unchanged;
- map the resolver output to the exact candidate using status, provenance, and
  normalized confidence;
- on a true exact tie, retain the already accepted latest-Seq ordering;
- degraded/unknown results still carry no winner;
- add tests where the later event has lower confidence than the older event,
  confidence is outside `[0,1]`, and one-shot/incremental input converges.

Do not add event text, metadata, cursor, or public DTO fields.

Checkpoint only after focused term tests pass.

## 3. Packet R2 — exact adapter binding

Problem: `LaunchBinding.Adapter` is never evaluated by `LaunchCorrelation`.

Required:

- pass the runtime's actual `sess.AdapterName()` through the production
  correlation path;
- require non-empty exact equality with `LaunchBinding.Adapter`;
- keep provider/version/PID/start checks fail closed;
- add direct correlation and `TelemetryService.processSession` mismatch tests;
- confirm controlled_pty matching still correlates and tmux/cmux/localpty gain no
  new semantic authority.

Do not introduce a generic capability engine or change adapter interfaces.

Checkpoint only after transcript and term focused tests pass.

## 4. Packet R3 — atomic replacement publication

Problem: `RegisterLaunch` publishes the replacement before
`InvalidateForLaunch` runs, leaving a concurrent telemetry window.

Required invariant:

```text
reserve strictly newer launch generation
→ invalidate old status and prior adapter ingestion state at that generation
→ publish replacement binding
→ only now may telemetry correlate new evidence
```

Implement this as one production-owned replacement boundary. Do not expose a
test-only bypass that can publish first. Preserve first-registration behavior,
bounded registry state, monotonic generation, removal semantics, and lifecycle
`Clear` separation.

Required tests:

- barrier-controlled poll/register race: new binding is unobservable before the
  invalidation high-water exists;
- old stream evidence cannot be committed under the new launch generation;
- delayed old-launch update and revoke remain rejected;
- session A replacement cannot affect B;
- duplicate registration and delete/recreate remain monotonic;
- repeated race run is clean.

Do not solve this by sleeps or by clearing the status record.

## 5. Explicitly deferred

- changing the local CLI `pokit run <args>` protocol;
- classifying command text or process names as agent authority;
- Task, Dispatch, acknowledgement, completion, ApprovalStore, notifications;
- terminal/OS compatibility certification;
- public runtime-status DTO changes;
- Windows or SSH expansion.

Record this verified limitation in the report: local `pokit run claude/codex`
remains managed-lifecycle only; recognized managed-agent authority is currently
available only through the accepted preset profile path.

## 6. Final evidence and stop

Run at minimum:

```sh
cd companion-daemon
gofmt -l <touched-go-files>
go build ./...
go vet ./...
go test -race ./internal/agent/... ./internal/transcript/... ./internal/mux/... ./internal/term/... -count=1
cd ..
sh scripts/build-gate.sh
git diff --check
```

Update `docs/S1_1_RUNTIME_STATUS_HARDENING_IMPLEMENTATION_REPORT.md` with a
remediation section, exact test matrix, skips, local/remote full SHAs, and:

```text
REVIEW REQUEST: S1.1 Runtime Status Hardening remediation — <full implementation SHA>
```

Commit, fetch, fast-forward/rebase safely if needed, push without force, verify
local HEAD equals the canonical remote and the worktree is clean, then stop for
independent S1.1 re-verification. Do not begin A1.
