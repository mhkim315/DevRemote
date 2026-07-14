# A1.1 CP0 H0 — Harness Recovery Report (invariant self-audit)

Status: **H0 IMPLEMENTED — awaiting independent verification. CP0 remains IN
PROGRESS; A1.1 / A1 / N1 remain BLOCKED; capacity stays ZERO.**

Date: 2026-07-15. Executor: Claude Code. Authoritative handoff:
`docs/NEXT_EXECUTOR_A1_1_CP0_HARNESS_RECOVERY_HANDOFF.md`. Contract note:
`docs/A1_1_CP0_H0_CONTRACT_NOTE.md` (written before implementation).

Implementation commit: `406de8395abdcc5e2899a5d5096e166b4ed6eb00`
(parent = canonical tip `3c646d12c7078efa0dc542d85ff10d4a1881ec6c`).

## 1. Repository identity (protocol §2)

- remote `https://github.com/mhkim315/DevRemote.git`, branch
  `feature/phase10-multi-adapter`; local == remote at `3c646d12c…` before work;
  worktree clean; ancestry `487f91e25…` OK, `997a697b5…` OK, baseline
  `0f67833b9…` OK.
- Stale-lock/takeover check (handoff §6): no `.probe.lock`, no live probe /
  packet app-server / hang-test process before editing.
- **Checkout deviation, declared**: work was performed in the execution
  session's configured checkout `~/.gemini/antigravity/scratch/DevRemote`
  (same remote/branch/SHA, clean, all ancestry checks passing) rather than
  `~/Documents/codex/DevRemote` named in handoff §2. No reset/force-push;
  history builds on the canonical tip. The Documents checkout was verified to
  sit at the same `3c646d12c` and can fast-forward.
- SCM_RIGHTS viability (mid-execution user directive): H0 was satisfiable
  **without** redesigning the supervisor/SCM_RIGHTS design — all changes are
  local to locking, cleanup, tests and the PID contract. Not BLOCKED.

## 2. Invariant self-audit (H0 requirement → code → non-vacuous test)

All references are `scripts/cp0/appserver_probe.py` at `406de8395`.

| # | H0 requirement | Code | Test (all no-model) |
| --- | --- | --- | --- |
| A1 | inspect/copy matching request under the state lock; send() only after release | `_claim_locked` :454, `pending_approval` :465, `respond_to_approval` :471 | `test_response_no_deadlock` :996 |
| A2 | write+flush+outbound seq under one serialization boundary | `send` :434 (single `_lock` block; fail-closed −1, no seq on failed write) | trace-order assertion (`provider->daemon` seq 1 → `daemon->provider` seq 2) |
| A3 | deterministic regression entering the former recursive path, bounded | production phase of `test_response_no_deadlock` (thread join ≤ 5 s) | same test |
| A4 | known-bad control proving the test catches recursive locking | `_FAULT_HOLD_LOCK_ACROSS_SEND` :353, fault branch :471 | phase 1 must observe the deadlock (join 2 s, thread still alive) or the test fails |
| A5 | no RLock | lock stays `threading.Lock` :375; claim/wire are two explicitly ordered, never-nested acquisitions | docstrings :434/:471; regression test |
| B1 | run lock acquired once, released from one outer finally | `_with_run_lock` :54; `_approval_trace` :681 wraps the whole locked body | `test_lock` :941 exception path (`LOCK_EXCEPTION_CLEANUP`) |
| B2 | never release/unlink a lock we do not own | `_release_lock` ownership guard :37; `cmd_clean` foreign-lock refusal :921; `_acquire_lock` exception cleanup :14 | `test_lock` foreign-guard phase (`LOCK_FOREIGN_GUARD`) |
| B3 | every cleanup field initialized before any fallible op | `AppServer.__init__` :357 (all fields set before `_spawn_with_timeout`) | `test_launchchain_smoke` unspawned instance; `test_response_no_deadlock` `_test_no_spawn` |
| B4 | stop() idempotent, safe after partial construction; timers/FDs exactly once | `stop` :502 (`_stopped` under `_lock`; timer/file handles detached under lock, closed once) | `test_pg_cleanup` double-stop + cleared-fields assertions; `test_runtime_timeout` post-timeout stop |
| B5 | kill/reap only the acknowledged owned PG; bounded even when READY never arrives | `_spawn_with_timeout` :211 (killpg only after READY parse; bare `sv_pid` kill before READY; `_reap_supervisor` :183 bounded reap) | `test_prehandoff_hang`; `test_pg_cleanup` |
| B6 | preserve unrelated Codex processes | before/after `pgrep` set with `before ⊆ after` assertion | `test_prehandoff_hang`, `test_pg_cleanup` (`unrelated_untouched=3` live) |
| B7 | zombies ≠ success when we must reap them; distinguish from live escapes | `_assert_pg_cleaned` :572 (reaps own-child zombies; `UNREAPED_ZOMBIES` vs `LIVE_ESCAPE`, both failures) | used by all four lifecycle tests |
| C1 | rename/replace `test_startup_hang` truthfully | removed; `test_prehandoff_hang` :1061; `test_startup_timeout` renamed `test_runtime_timeout` :1159 (it proves the runtime deadline) | — |
| C2 | deterministic seam blocking BEFORE PID/FD handoff; parent returns by deadline; owned group removed | `_fault_prehandoff_hang` supervisor seam :211 (after READY+spawn, before sendmsg); `_on_ready` parent barrier | `test_prehandoff_hang` (returned in 3.1 s vs 3 s phase deadline; PG cleaned) |
| C3 | no sleeps as concurrency proof; contested intermediate state asserted | READY `threading.Event` barrier → assert owned PG alive + spawn not returned + handoff withheld; bounded polling only after deterministic SIGKILL / for the wall-clock deadline property itself | `test_prehandoff_hang` `INTERMEDIATE_STATE` line; `test_response_no_deadlock` claim-set/trace-order |
| C4 | skipped ps/pgrep must not silently pass | `_pgrep_codex` :528 / `_scan_pg` :544 return None on observation failure; every caller exits 1 with `OBSERVATION_FAILED` | all lifecycle tests |
| C5 | hard outer wall-clock timeout per test | `_arm_test_watchdog` :1234 (`os._exit(124)`), armed in `main()` for every test command | gate ran under it |
| D1 | launch-chain uses supervisor PID/PGID contract, no `a.p` | `_spawned_image_info` :811; `cmd_launchchain` :827 try/finally; `a.p` field deleted entirely | `test_launchchain_smoke` :1186 (live + unspawned branches; old code raised AttributeError) |
| D2 | preserve SCM_RIGHTS ownership boundary | protocol unchanged except the READY frame is fixed 64-byte (`_READY_FRAME_LEN` — removes stream coalescing against the SCM_RIGHTS message); no transport redesign | `test_prehandoff_hang`, `test_pg_cleanup` exercise the full handshake |

Evidence level (protocol §11): **focused proof** for every row above via the
no-model harness gate. No production-path or provider claim is made; no live
provider evidence was collected (per handoff §9 and the user directive).

## 3. Focused H0 gate (frozen tree of `406de8395`)

Runner: `python3 scripts/cp0/appserver_probe.py <test>`; every test armed with
the internal wall-clock watchdog (exit 124 on hang).

| Gate item | Command | Exit | Elapsed |
| --- | --- | --- | --- |
| 1 compile | `test_compile` | 0 | 0.1 s |
| 2 lock acquire/release + exception cleanup + foreign guard | `test_lock` | 0 | 0.0 s |
| 3 response-path no-deadlock (known-bad control + production path) | `test_response_no_deadlock` | 0 | 2.1 s |
| 4 pre-handoff startup-timeout + owned-PG cleanup | `test_prehandoff_hang` | 0 | 3.2 s |
| 5a runtime stop + unrelated-process isolation | `test_pg_cleanup` | 0 | 0.1 s |
| 5b runtime deadline timeout | `test_runtime_timeout` | 0 | 2.2 s |
| 6 launch-chain smoke, no None dereference | `test_launchchain_smoke` | 0 | 0.1 s |

Repeat run of the full suite: 7/7 pass.

Post-gate cleanup proof: no `/tmp/pokit-cp0-<uid>/.probe.lock`; no
packet-owned probe/benign-child process (`ps` scan); pre-existing unrelated
Codex PID set (3 processes) preserved; `git diff --check` clean; worktree
contains only H0 files (`scripts/cp0/appserver_probe.py`,
`docs/A1_1_CP0_H0_CONTRACT_NOTE.md`, this report).

## 4. Explicitly not done (honest scope)

- No new provider evidence; no real accept/decline recapture; no model turn.
- Gates #1/#6/#7 of CP0 remain in their prior states (BLOCKED / NOT DONE / NOT
  PROVEN per `docs/A1_1_CP0_EVIDENCE_REPORT_2.md`).
- `provenActionMapping` empty; production approval-delivery capacity zero; the
  frozen A1 core, production Go, mobile and adapter code untouched.
- CP1 / N1 / A1.2 / O1 / O2 not started.

## 5. Review marker

```text
REVIEW REQUEST: A1.1 CP0 Harness Recovery H0 — 406de8395abdcc5e2899a5d5096e166b4ed6eb00
```
