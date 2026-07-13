# Next Session Handoff — S1.1 Runtime Status Hardening

Status: **SUPERSEDED AFTER INDEPENDENT REJECT**

The first A/B/C execution from this packet produced implementation `715c22b` and
was independently rejected at report HEAD `1547c93`. Do not execute this packet
again. Continue only from `docs/NEXT_SESSION_S1_1_REMEDIATION_HANDOFF.md` and its
three focused blockers. This document remains the frozen original acceptance
contract and historical scope record.

This is the authoritative execution packet after independent S1 ACCEPT. It is
written for a fresh Claude Code execution session backed by DeepSeek V4 Pro, but
the repository and safety rules apply to every executor.

Do not begin A1, O1, O2, Notifications, another adapter, Windows, or distribution
work. Do not emit an S1.1 review request until S1.1-A, S1.1-B, and S1.1-C all pass
on one final tree.

## 0. Repository identity — do this before reading source

The canonical identity is the remote URL plus branch plus full accepted SHA. A
filesystem directory name is not repository identity.

```text
canonical remote: https://github.com/mhkim315/DevRemote.git
canonical branch: feature/phase10-multi-adapter
accepted S1 implementation: b6504bd7d5c634f0c0459ae87503b82d17c1537b
accepted S1/report HEAD: 70ef5df28dede7b0f3025eeaab7826f76a229fbf
accepted T3: 9ad6f834f70e88b800e60124c8e408d38bca9d2b
accepted D1: 8f7c22def81abf0b932f6dbbacc07325ae2bb12e
accepted T2: ef4a162c7f9a5644fd52d89501e97f4e62301dfa
accepted T1: 162266f830caaf07bf701d9a1294557432855769
accepted T0: 3ce2604bd333dcb63142b5b1710185a823162efa
```

`/Users/mhk/Documents/codex/DevRemote` was the reviewer's local path when this
handoff was written. It is **not** a path instruction for another machine or
shell. Never assume that `/root`, a scratch directory, the parent `codex`
directory, or the first directory named `DevRemote` is canonical.

### 0.1 Existing checkout recovery

From the directory the executor believes is the repository, run exactly:

```sh
pwd
git rev-parse --show-toplevel
git remote -v
git branch --show-current
git status --short
```

Proceed in that checkout only when `git rev-parse --show-toplevel` identifies the
intended DevRemote repository and the worktree is clean. Then:

```sh
git remote set-url origin https://github.com/mhkim315/DevRemote.git
git fetch origin feature/phase10-multi-adapter
git switch feature/phase10-multi-adapter
git merge --ff-only origin/feature/phase10-multi-adapter
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git status --short
```

The two full SHAs must match. Then run:

```sh
git merge-base --is-ancestor 70ef5df28dede7b0f3025eeaab7826f76a229fbf HEAD
git merge-base --is-ancestor b6504bd7d5c634f0c0459ae87503b82d17c1537b HEAD
git merge-base --is-ancestor 9ad6f834f70e88b800e60124c8e408d38bca9d2b HEAD
git merge-base --is-ancestor 8f7c22def81abf0b932f6dbbacc07325ae2bb12e HEAD
git merge-base --is-ancestor ef4a162c7f9a5644fd52d89501e97f4e62301dfa HEAD
git merge-base --is-ancestor 162266f830caaf07bf701d9a1294557432855769 HEAD
git merge-base --is-ancestor 3ce2604bd333dcb63142b5b1710185a823162efa HEAD
```

Every command must exit 0. Do not rely on an abbreviated SHA for recovery.

### 0.2 Wrong directory, dirty checkout, or missing commit

- If `git rev-parse` fails, this is not a Git checkout. Locate the intended
  checkout or clone the canonical URL into a new, clearly named directory.
- If the remote URL names another repository, do not edit it. Change directory
  or make a fresh clone.
- If the intended checkout is dirty, do not reset, stash, clean, or discard
  another agent's work. Use a separate fresh clone and report the dirty checkout.
- If `70ef5df...` is not visible, fetch the canonical branch before claiming the
  commit or handoff is missing.
- If it is still absent after fetch, stop and report the local/remote full SHAs.
  Do not reconstruct the handoff from chat.
- Never use `git reset --hard`, force-push, or rewrite reviewed history.

A safe fresh-clone form is:

```sh
git clone --branch feature/phase10-multi-adapter \
  https://github.com/mhkim315/DevRemote.git DevRemote-s1-1
cd DevRemote-s1-1
git rev-parse HEAD
git status --short
```

Before editing, report the canonical URL, absolute top-level path, branch, local
full SHA, remote full SHA, ancestry results, and clean worktree state.

## 1. Read-before-edit order

1. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
2. this handoff;
3. `docs/S1_FINAL_ACCEPTANCE.md`;
4. `docs/S1_1_RUNTIME_STATUS_HARDENING_PLAN.md`;
5. `docs/S1_RUNTIME_STATUS_IMPLEMENTATION_REPORT.md`;
6. frozen T0 contract: `internal/agent/contract/`;
7. accepted T1/T2 adapters and their fixed conformance tests;
8. `internal/term/agent_status_store.go`, `adapter_state.go`,
   `telemetry_service.go`, `create.go`, and their tests;
9. `internal/transcript/launch_binding.go` and production callers;
10. `internal/models/process.go`, mux `ProcessProvider` implementations, and
    process snapshot collection;
11. accepted S1 mobile DTO/poller tests only to prove they remain unchanged.

Before changing code, write a short audit table containing the exact production
caller, current identity/evidence fields, missing binding, proposed smallest
change, and negative test for each packet below.

## 2. Confirmed baseline facts — do not rediscover by guesswork

At accepted S1 HEAD:

- `AgentActivityRecord` already stores status, provenance, confidence,
  `ObservedAt`, bounded degraded reason, `Generation`, and provider `Version`;
- equal-precedence resolution orders validated evidence by `AgentEvent.Seq`, then
  delegates authority to frozen `ResolveStatus`;
- `adapterState.streamGen`, accepted adapter cursor, and S1-E `Invalidate` already
  prevent cross-stream-generation resurrection;
- `LaunchBinding` already contains provider, adapter, accepted version, process
  name, PID, and generation;
- production create currently calls `RegisterLaunch(..., pid=0, generation=1)`;
- `RegisterLaunch` ignores a second registration for the same session ID;
- `LaunchCorrelation` compares provider/version and PID only when PID is nonzero;
- `models.ProcessInfo` already contains both `PID` and `StartedAt`, and mux
  providers populate them where available;
- the public S1 `agentActivity` DTO intentionally does not expose launch identity,
  cursor, raw evidence, or degraded reason.

Do not add duplicate fields for facts already present. Do not treat PID alone as
identity. Do not make process, CWD, PTY, prompt, screen, or Transcript evidence a
status-authority source.

## 3. Frozen authority and privacy invariants

- T0 status vocabulary, provenance ranking, confidence ceilings,
  `SafeApprovalGate`, and `ResolveStatus` remain frozen.
- Accepted T1/T2 `GetStatus` remains the status-resolution authority.
- Winning-evidence metadata may explain which accepted event won; it cannot alter
  the winner or independently synthesize a status.
- `waiting_approval` is display-only. S1.1 must not create an ApprovalStore entry,
  CTA, action, or authorization path.
- No raw event text, prompt, thinking, tool input/output, command, path, token, or
  opaque cursor may enter the status record, API, log, or report.
- Lifecycle remains Catalog/LifecycleService authority.
- Recovery may return absent/unknown/degraded; it must not confidently return
  evidence bound to a replaced runtime.
- The public S1 DTO remains unchanged unless a separately reviewed consumer and
  contract revision are approved. This handoff grants no such approval.

## 4. Sequential task packets

Complete these in order. A focused checkpoint commit after each packet is
recommended, but continue only after its focused tests pass. Request independent
acceptance once, after A+B+C and the full final gate.

### S1.1-A — Bounded winning-evidence metadata

Production path:

```text
accepted AgentEvents
→ buildStatusEvidence (ordered by existing Seq rule)
→ accepted adapter GetStatus / frozen ResolveStatus
→ AgentStatusStore record
```

Required behavior:

- preserve the minimum bounded internal reference needed by S1.1-C to reject
  replay/regression. Winning `Seq` is the existing candidate; add event ID only
  if the audit demonstrates an exact dedupe/traceability need that Seq cannot
  satisfy;
- reuse the exact ordered candidate list passed to `GetStatus`; do not copy or
  reimplement provenance precedence;
- when multiple candidates have the same resolved status/provenance/confidence,
  bind to the first candidate under the already accepted ordering/tie rule;
- unknown/degraded results without an accepted winning event carry no fabricated
  event identity;
- bound and validate any stored identifier; never store text or metadata maps;
- keep adapter cursor in `adapterState`, not the status store or public DTO.

Required negative tests:

- stronger authoritative evidence wins and the chosen bounded revision fields
  identify that exact winner;
- equal precedence/confidence uses the accepted latest-Seq rule;
- input order and one-shot/incremental presentation do not change the winning
  reference;
- cross-session and unknown-provenance events cannot become a winning reference;
- if event ID is retained, empty/oversized IDs fail closed and are never copied;
- degraded/adapter-error/version-conflict results do not fabricate a winner;
- API/mobile golden bytes remain unchanged.

If preserving the winner would require modifying frozen T0 types or duplicating
`ResolveStatus`, stop and report the conflict. Do not silently fork the contract.

### S1.1-B — Runtime identity binding

Production path:

```text
session creation controller
→ managed launch registration
→ process snapshot (PID + StartedAt where available)
→ LaunchCorrelation
→ TelemetryService status update/revoke
```

Required behavior:

- replace the hard-coded production generation `1` with a real monotonic launch
  generation owned by the launch-registration boundary;
- a process replacement, same-ID recreation, relink, adapter/provider/version
  change, or launch-binding replacement atomically invalidates incompatible prior
  status before new positive evidence can be accepted;
- bind process identity using PID plus nonzero process start time (or a proven
  equivalent non-reusable token) when both are available;
- PID reuse with a different start time fails correlation;
- an unavailable required identity fails closed; it is not treated as a wildcard;
- test-only registration must not create a weaker production bypass;
- keep the registry bounded and ensure delete/removal cleans active binding state
  without allowing a later generation to regress.

Audit before implementation: determine how the freshly created
`controlled_pty` session can expose `ProcessInfo` after creation. Reuse
`models.ProcessInfo.StartedAt` and existing mux providers; do not add arbitrary
`ps`/`lsof` process scraping merely to satisfy a test.

Required negative/production tests:

- two launches of the same canonical ID receive strictly increasing generations;
- binding replacement revokes the old status before accepting new evidence;
- same PID + different start time is rejected;
- different PID, provider, adapter, or accepted version is rejected;
- missing required start identity fails closed when the binding claims one;
- delete → recreate cannot inherit the old generation/evidence;
- replacement of session A cannot affect session B;
- current matching PID+start/provider/adapter/version remains correlated.

### S1.1-C — Recovery, replay, and bounded-state hardening

Required behavior:

- same-generation replay cannot move a winning evidence revision backwards;
- an adapter cursor regression or repeated batch cannot restore older positive
  status or duplicate winning-evidence authority;
- correlation loss immediately makes the result non-current; recovery requires
  fresh compatible evidence for the current launch generation;
- bounded status-store eviction removes all associated internal evidence state;
- daemon restart begins absent/unknown and cannot reconstruct a confident current
  result from stale process/session identity alone;
- concurrent update, invalidate, binding replacement, clear, eviction, and
  recreate remain race-free;
- accepted S1 polling, DTO, lifecycle, and approval separation remain unchanged.

Required tests:

- newest Seq wins, then replay of an older Seq is rejected;
- cursor rewind/repeated records do not regress the winner;
- correlation loss → non-current → compatible recovery with fresh evidence;
- loss followed by incompatible recovery remains non-current;
- eviction followed by same-ID reuse carries no old evidence metadata;
- daemon restart and bounded churn;
- repeated `-race` test over update/replacement/clear/recreate;
- T0/T1/T2 conformance and S1 golden regression.

Do not introduce persistence solely for restart recovery. Unknown after restart is
the accepted safe result.

## 5. Allowed and prohibited scope

Allowed production scope is limited to the smallest required files under:

- `companion-daemon/internal/term/`;
- `companion-daemon/internal/transcript/launch_binding.go`;
- existing mux/process identity plumbing when the audit proves it necessary;
- S1.1 tests and implementation report;
- planning/status documentation after the implementation passes.

Prohibited:

- changing frozen T0 contract/harness or T1/T2 adapter semantics;
- changing the public mobile/backend `agentActivity` DTO;
- new public status/provenance/degraded vocabulary;
- status inference from process/CWD/PTY/screen/prompt/Transcript;
- A1 approval UX, ApprovalStore authority changes, or action execution;
- O1/O2 broker/orchestrator/developer-verifier work;
- D1 activation, Git/worktree automation, merge/rework loops;
- another adapter, Notifications, Windows, or distribution work;
- force-push or history rewrite.

If an implementation appears to require a prohibited change, stop and document
the exact call graph and blocker rather than broadening scope.

## 6. Model-specific execution discipline

For Claude Code + DeepSeek V4 Pro:

1. Do not implement all three packets from a prose summary in one pass.
2. At each packet, restate exact caller, files to touch, forbidden files, and
   failing test before editing.
3. Run focused tests after every material change; inspect failure output instead
   of repeatedly patching by intuition.
4. Prefer one existing owner and one monotonic identity. Do not create parallel
   registries, cursor models, or status resolvers.
5. Use exact equality and closed validation; no fuzzy correlation or heuristic
   authority.
6. Inspect the production caller after adding a helper. Helper-only completion is
   a failure.
7. Do not weaken assertions, skip tests, or convert a blocker into a follow-up.
8. Keep commits narrow and additive on reviewed history. Never amend a pushed
   review commit.
9. After long Go/race runs, remove only stage-specific temporary caches you
   created and report retained large artifacts. Do not delete shared/user data.

## 7. Focused and final gates

Focused commands should include the exact changed packages and named regression
tests. On the final tree run at minimum:

```sh
cd companion-daemon
gofmt -l ./internal/agent ./internal/term ./internal/transcript ./internal/mux
go build ./...
go vet ./...
go test -race ./internal/agent/... ./internal/term/... ./internal/transcript/... ./internal/mux/... -count=1

cd ../mobile
npm run typecheck
npm test -- --runInBand

cd ..
git diff --check
sh scripts/build-gate.sh
git status --short
```

Skipped required tests, zero executed tests, tests run only before the final
change, or an environment skip reported as PASS are failures. Physical-device
work may remain M-track only if S1.1 does not change a native/product-device path.

## 8. Report, commit, push, and stop condition

Create `docs/S1_1_RUNTIME_STATUS_HARDENING_IMPLEMENTATION_REPORT.md` containing:

- repository absolute top-level path, canonical remote, branch, baseline and
  final full SHAs;
- A/B/C commit traceability;
- before/after call graph;
- winning-evidence and runtime-identity matrices;
- every requirement mapped to a production-path negative test;
- confirmation that T0/T1/T2 and the public S1 DTO did not change;
- exact commands, test counts, skips, resource cleanup, and limitations;
- one final marker:

```text
REVIEW REQUEST: S1.1 Runtime Status Hardening — <full implementation SHA>
```

Before push, fetch the canonical branch. Rebase only if it is a clean
fast-forward-compatible update; never force-push. After push report:

```sh
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git status --short
git merge-base --is-ancestor 70ef5df28dede7b0f3025eeaab7826f76a229fbf HEAD
```

Local and remote full SHA must match, ancestry must exit 0, and worktree must be
clean. Then stop for independent S1.1 verification. Do not start A1.
