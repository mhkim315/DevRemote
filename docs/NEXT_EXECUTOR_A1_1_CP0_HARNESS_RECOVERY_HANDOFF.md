# Next Executor Onboarding — A1.1 CP0 Harness Recovery

Status: **RECOVERY PACKET H0 ONLY — `0f67833` NOT ACCEPTED — CP1 UNAUTHORIZED — CAPACITY ZERO**

Date: 2026-07-14

This handoff is the authoritative onboarding document for the replacement execution
agent. It supersedes conflicting CP0 process-image and schema-completeness language in
older handoffs for the current recovery packet. Do not restart the A1.1 research from
the beginning.

## 1. Your role and single immediate objective

You are the **execution agent**, not the independent verifier.

Your only authorized packet is **H0: make the CP0 evidence harness bounded,
deadlock-free and cleanly terminating**. Do not collect new provider evidence, change
production code, enable approval capacity or begin CP1 until H0 is independently
reviewed.

H0 is complete only when the harness:

- cannot recursively deadlock its response path;
- releases its run lock on every normal, error, timeout and exception path;
- owns and terminates only the process group it created;
- returns within a deterministic wall-clock bound when startup or runtime hangs;
- leaves no live owned process and no stale lock;
- has non-vacuous no-model tests for those properties.

## 2. Canonical repository identity

Use only:

```text
checkout: /Users/mhk/Documents/codex/DevRemote
remote:   https://github.com/mhkim315/DevRemote.git
branch:   feature/phase10-multi-adapter
baseline: 0f67833b911079c90490f584481650093951fb6b
```

At startup run:

```sh
cd /Users/mhk/Documents/codex/DevRemote
git fetch origin feature/phase10-multi-adapter
git status --short --branch
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git merge-base --is-ancestor 487f91e25c33fff2fabb870413a59987e93787ef HEAD
git merge-base --is-ancestor 997a697b55b421aa8590aa05a672c604762fd0d2 HEAD
```

Stop if local and remote differ, either ancestry check fails, or the worktree contains
unrelated changes. Do not use another checkout, reset, force-push or rewrite history.
Build on the current canonical tip even though `0f67833` is not accepted.

## 3. Frozen product state

- S1.1 is accepted.
- The provider-neutral A1 safety core is frozen.
- A1.1, A1 and N1 remain BLOCKED.
- `provenActionMapping` remains empty.
- production approval-delivery capacity remains zero.
- no terminal text, prompt, PTY, screen, `waiting_approval`, generic send-text or
  send-key path may become approval authority.
- H0 changes only `scripts/cp0/appserver_probe.py` and focused CP0 documentation/tests.
  Production Go, mobile and adapter code are out of scope.

## 4. Minimal read order

Read only what is needed for H0, in this order:

1. this handoff;
2. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
3. `scripts/cp0/appserver_probe.py` at current HEAD;
4. `docs/A1_1_CP0_EVIDENCE_REPORT_2.md` for already captured evidence;
5. `docs/A1_CODEX_PROVIDER_POSITIVE_PATH_PLAN.md`, CP0 and non-goal sections only;
6. `docs/NEXT_SESSION_A1_1_CODEX_PROVIDER_POSITIVE_HANDOFF.md`, frozen authority
   sections only.

Do not reread every A1 remediation round unless a named invariant cannot be resolved
from these sources. Historical rejection archaeology is not H0 work.

## 5. Confirmed failure at `0f67833`

The independent reviewer reproduced a real production-harness deadlock.

Current response logic does this:

```text
on_msgs acquires a._lock
→ on_msgs calls a.send
→ a.send tries to acquire the same non-reentrant a._lock
→ Python probe deadlocks forever
```

The provider deadline killed node/native app-server, but the Python probe and shell
remained alive and `.probe.lock` remained present. Therefore the reported five tests
did not cover the real approval response path and are not acceptance evidence.

Additional confirmed defects:

- `_approval_trace` does not own lock release through one outer `try/finally`;
- failed `AppServer` initialization can leave fields such as `_deadline_timer`
  undefined while `stop()` assumes they exist;
- `cmd_launchchain()` still reads `a.p.pid` although the supervisor refactor sets
  `a.p = None`;
- `test_startup_hang` currently spawns a normal sleeping child and kills it manually;
  it does not force the supervisor to hang before PID/FD handoff;
- the five reported tests omit the response path where the deadlock occurs.

## 6. Safe takeover and stale-lock rule

Before editing, check:

```sh
LOCK=/tmp/pokit-cp0-$(id -u)/.probe.lock
test -e "$LOCK" && sed -n '1p' "$LOCK" || true
ps -axo pid,ppid,pgid,state,etime,command | \
  rg 'appserver_probe.py|codex app-server|_hang_test.py' || true
```

If the previous executor or probe is still alive, **do not remove the lock and do not
kill it implicitly**. Stop the old execution session or request explicit user approval
to terminate the exact owned process group. Never use a broad `pkill codex`; unrelated
user Codex processes must survive.

Only after the recorded owner is confirmed dead may the stale `.probe.lock` be
removed. Do not delete other `/tmp` files unless they were created by this packet and
their ownership is known.

## 7. Mandatory pre-implementation note

Before changing the harness, add a short H0 contract note covering:

- owner of the run lock and owned process group;
- states: idle → lock-owned → supervisor-ready → child-ready → active → stopping →
  cleaned;
- linearization points for response write and process-group publication;
- exact success evidence for cleanup;
- timeout and exception behavior;
- the recursive-lock counterexample;
- explicit non-goals from section 11.

Keep it short. H0 is harness reliability work, not a new architecture phase.

## 8. Required H0 changes

### H0-A — response locking

- inspect/copy the matching provider request under the state lock;
- release that lock before calling `send()`, or provide one clearly owned internal
  primitive that cannot recursively acquire the same lock;
- keep write + flush + outbound sequence publication under one serialization boundary;
- add a deterministic no-model regression that enters the former recursive path and
  finishes within a short bound;
- include a known-bad control or fault seam proving the test would catch recursive
  locking.

Do not solve this by changing the lock to `RLock`; that would hide unclear ownership
and would not prove the response state transition is linearized correctly.

### H0-B — total cleanup

- acquire the run lock once and release it from one outer `finally`;
- initialize every `AppServer` cleanup field before any operation that can fail;
- make `stop()` idempotent and safe after partial construction;
- close owned descriptors and cancel timers exactly once;
- kill/reap only the acknowledged owned process group;
- preserve unrelated Codex processes;
- distinguish terminated zombies from live escapes without calling zombies success
  when the harness itself is responsible for reaping them.

### H0-C — truthful deterministic tests

- replace or rename `test_startup_hang` so its name matches what it proves;
- add a deterministic seam that blocks **before PID/FD handoff** and prove the parent
  returns by deadline and removes the owned group;
- no sleeps as concurrency proof; bounded polling is allowed only for external process
  disappearance after a deterministic signal/barrier;
- assertions must inspect the contested intermediate state, not only final cleanup;
- a skipped `ps`/`pgrep` observation must not silently pass a cleanup assertion.

### H0-D — supervisor-refactor regressions

- repair `cmd_launchchain()` to use the supervisor-returned PID/PGID contract rather
  than `a.p`;
- preserve the current SCM_RIGHTS ownership boundary;
- do not redesign provider transport or implement product runtime code.

## 9. Focused H0 gate

Do not run the full repository build gate. Run only the harness gate on a frozen HEAD.
At minimum it must include:

```text
1. compile
2. lock acquire/release plus stale-lock exception cleanup
3. response-path no-deadlock regression
4. pre-handoff startup-timeout and owned-PG cleanup regression
5. runtime stop/timeout and unrelated-process isolation regression
6. launch-chain command smoke test with no `None` dereference
```

Every test must have a hard outer wall-clock timeout. Record the command, exit status
and elapsed time. Afterward prove:

```text
no .probe.lock
no process in the packet-owned PGID
no packet-owned Python/node/native child
unrelated pre-existing Codex PID set preserved
git diff --check passes
worktree contains only H0 files
```

Do not run a real model turn merely to validate the lock fix. One real accept and one
real decline trace may be recaptured only after H0 is independently accepted and CP0
evidence collection resumes.

## 10. Risk-proportional CP0 criteria after H0

These rules govern later CP0 work but are **not H0 implementation tasks**.

Required later:

- POKIT-spawned child PID + process-start identity bound to launch and connection
  generation;
- exact supported Codex version and the minimal request/response schema subset used by
  the positive path;
- runtime replacement/reconnect/termination invalidates prior response authority;
- request-bound accept/decline delivery followed by matching provider `resolved` and
  corroborating outcome;
- cancellation, duplicate, timeout and child cleanup fail closed;
- bounded production thread/turn entry path;
- structurally allowlisted redacted evidence with stable per-value pseudonyms.

Deferred hardening, and not a reason to reject CP0/A1.1:

- publisher or full software-supply-chain certification;
- cdhash, EndpointSecurity or private `csops` integration;
- a universal proof that every macOS path replacement is impossible;
- attestation of every Node/package/dynamic-library artifact;
- byte-for-byte reproducibility of all 267 generated schema files;
- Windows attestor implementation.

For schema identity, canonicalize and pin only the protocol/request/response subset
actually consumed by the initial action. Do not reopen full-bundle research in H0.

## 11. Explicit H0 non-goals

Do not:

- add production code or non-zero capacity;
- change the frozen A1 store, claim, receipt, DTO or mobile path;
- begin CP1, N1, A1.2, O1 or O2;
- implement cdhash, code-signing, EndpointSecurity, Windows or supply-chain attestation;
- regenerate or recertify all provider evidence;
- redesign `pokit run`, terminal UI, PTY or generic provider interfaces;
- grant broad shell permissions;
- run repeated full repository gates.

## 12. Commit, report and stop

After the focused H0 gate passes:

1. freeze HEAD;
2. produce a short invariant self-audit mapping each H0 requirement to exact harness
   code and a non-vacuous test;
3. commit and push one focused recovery checkpoint;
4. verify local/remote equality, accepted ancestry and clean worktree;
5. report exact test commands, elapsed times and cleanup evidence;
6. stop for independent H0 verification.

Required marker:

```text
REVIEW REQUEST: A1.1 CP0 Harness Recovery H0 — <implementation SHA>
```

Do not call CP0, A1.1 or A1 complete. Do not begin new provider evidence collection in
the same turn.
