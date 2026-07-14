# A1.1 CP0 H0 — Harness Recovery Contract Note (pre-implementation)

Status: **H0 ONLY — harness reliability; no production code, capacity stays ZERO**

Date: 2026-07-14. Executor: Claude Code. Authoritative handoff:
`docs/NEXT_EXECUTOR_A1_1_CP0_HARNESS_RECOVERY_HANDOFF.md`. Baseline tip `3c646d12c`
(rejected implementation `0f67833` is an ancestor). Scope:
`scripts/cp0/appserver_probe.py` + focused CP0 docs/tests only.

## 1. Ownership

- **Run lock** `/tmp/pokit-cp0-<uid>/.probe.lock`: owned by the single process that
  created it with `O_CREAT|O_EXCL` (fd held in `_lock_fd`). Acquired once per probe
  run and released exactly once from **one outer `finally`** (`_with_run_lock`), on
  every normal, error, timeout and exception path. `_release_lock` never unlinks a
  lock this process does not hold (foreign-lock guard); `cmd_clean` refuses when a
  foreign lock is present.
- **Owned process group**: the supervisor child creates its own session+PG via
  `setsid` and reports the PGID in a fixed-size 64-byte READY frame. The parent may
  `killpg` **only an acknowledged PGID**; before READY it may kill only the direct
  supervisor PID. Unrelated Codex processes are never targeted.

## 2. States

idle → lock-owned → supervisor-ready (READY frame parsed; PGID owned by parent) →
child-ready (PID + SCM_RIGHTS FDs received; PGID membership verified) → active
(reader thread + deadline timer) → stopping (`stop()` entered exactly once via
`_stopped` under `_lock`) → cleaned (timer cancelled once, owned PG killed,
descriptors closed once, supervisor reaped, run lock released by the outer finally).

## 3. Linearization points

1. **Response claim**: the provider request id is inserted into `_responded_ids`
   under `AppServer._lock` (`pending_approval`). At most one claimant per request;
   a duplicate respond attempt returns `None`.
2. **Wire publication**: write + flush + seq increment inside `send()` under the
   same `_lock`, acquired only **after** the claim lock has been released — never
   nested. A failed write publishes no seq (fail-closed).
3. **PG-ownership publication**: the parent's successful parse of the fixed READY
   frame. The fixed frame removes the stream-coalescing hazard between the READY
   message and the SCM_RIGHTS PID/FD message.

## 4. Recursive-lock counterexample (confirmed at `0f67833`)

`on_msgs` held `a._lock` while calling `a.send()`, which re-acquires the same
non-reentrant lock → the probe deadlocks forever on the real approval response path;
the provider deadline killed codex while the Python probe, shell and `.probe.lock`
leaked. Regression: `test_response_no_deadlock` re-enters exactly this path through
the `_FAULT_HOLD_LOCK_ACROSS_SEND` known-bad control (which must be observed to
deadlock within the bound) and then proves the production path completes, claims
once, and publishes a monotonic outbound seq. `RLock` is prohibited: it would hide
ownership rather than prove the response transition is linearized.

## 5. Timeout and exception behavior

- **Startup phase 1 (READY)**: bounded by a monotonic deadline; on timeout the
  parent kills the supervisor PID directly (PG unknown), reaps it, and fails.
- **Startup phase 2 (PID/FD handoff)**: bounded; on timeout / garbled / ERROR /
  membership mismatch / truncated ancillary data the parent kills the acknowledged
  PGID, reaps the supervisor, and fails. Total spawn wall-clock ≤ 2 × phase timeout
  + ε, deterministic.
- **Runtime**: deadline timer sets `_timed_out` and kills the owned PG. `send()`
  returns −1 after timeout/close/pipe failure without publishing a seq.
- **Any exception**: every `AppServer` cleanup field is initialized before any
  fallible operation, so `stop()` is idempotent and safe after partial
  construction; probe runs call `stop()` from `finally`, and the run lock is
  released by `_with_run_lock`'s outer `finally`.

## 6. Exact cleanup success evidence

- `_assert_pg_cleaned(pgid)`: bounded poll **after** the deterministic SIGKILL;
  success only when a `ps` scan of the owned PGID shows no live and no zombie
  entries. Zombies that are our direct children are reaped by the harness first and
  are never counted as success while unreaped (`UNREAPED_ZOMBIES` ≠ `LIVE_ESCAPE`).
- A failed `ps`/`pgrep` observation is `OBSERVATION_FAILED` → test failure, never a
  silent pass.
- Unrelated pre-existing Codex PID set is preserved (`before ⊆ after`).
- No `.probe.lock` after every run; a foreign lock is never deleted.
- Every no-model test is armed with a hard outer wall-clock watchdog (`os._exit(124)`).

## 7. Binding table

| Resource | Created at | Published at | Released/invalidated at | Negative test |
| --- | --- | --- | --- | --- |
| run lock | `_acquire_lock` `O_EXCL` | fd in `_lock_fd` | one outer `finally` in `_with_run_lock` | `test_lock` (exception path, foreign-lock guard, re-acquire) |
| owned PGID | supervisor `setsid` | READY-frame parse in parent | `killpg` + reap + `_assert_pg_cleaned` | `test_prehandoff_hang`, `test_pg_cleanup` |
| stdio FDs | supervisor `Popen` | SCM_RIGHTS `recvmsg` | `stop()` close-once under `_lock` | `test_pg_cleanup` (post-stop fields None) |
| deadline timer | `__init__` after successful spawn (field pre-set to None) | field assignment | `stop()` cancel-once via `_stopped` | `test_runtime_timeout` |
| response claim | `pending_approval` under `_lock` | seq in `send()` under `_lock` | n/a (single-shot evidence run) | `test_response_no_deadlock` (duplicate → None; known-bad control) |

## 8. Non-goals (handoff §11)

No production code or non-zero capacity; no change to the frozen A1 store, claim,
receipt, DTO or mobile path; no CP1, N1, A1.2, O1 or O2; no cdhash, code-signing,
EndpointSecurity, Windows or supply-chain attestation; no regeneration or
recertification of provider evidence; no `pokit run`, terminal UI, PTY or generic
provider redesign; no broad shell permissions; no repeated full repository gates; no
real model turn to validate the lock fix.
