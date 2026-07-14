# A1.1 CP0 H0-R1 — Lock Replacement Remediation Report

Status: **H0-R1 IMPLEMENTED — awaiting independent verification. CP0 remains
IN PROGRESS; A1.1 / A1 / N1 / CP1 remain BLOCKED; capacity stays ZERO.**

Date: 2026-07-15. Executor: Claude Code. Performed directly in the canonical
checkout `/Users/mhk/Documents/codex/DevRemote` (handoff §2), on reviewer
baseline `cfdccb7e3018d7f847565c40ee4d436cab21fb6f`.

Implementation commit: `31dcd4276dd41e2aaee0c9b142c0b07a66a75022`

## 1. Blocker (from independent H0 review)

`_release_lock` checked only `_lock_fd != None` and then unconditionally
unlinked the current pathname. If the held lock path was deleted and replaced
by another run's lock, our release deleted the foreign lock — violating the
contract-note claim "`_release_lock` never unlinks a lock this process does
not hold" (reviewer counterexample: `different=True foreign_survived=False`).

## 2. Fix (scripts/cp0/appserver_probe.py @ 31dcd4276)

- `_acquire_lock`: after `O_CREAT|O_EXCL` create + token write, records the
  owned inode identity `(st_dev, st_ino)` via `fstat(fd)` and a bounded
  (≤256B) owner token `"<run_id> pid=<pid>"`.
- `_release_lock`: unlinks the pathname **only** when `fstat(fd)` and
  `stat(_LOCK_PATH)` both still equal the recorded identity. On any mismatch
  or stat failure it closes only its own fd and never touches the pathname.
  The `_lock_fd is None` foreign guard is unchanged.

## 3. Regression test (non-vacuous)

`test_lock_replacement`: acquire → delete own path → create a foreign lock at
the same path → `_release_lock`. Asserts the inodes differ (scenario is not
vacuous), the foreign lock survives with intact content, and — positive
control — a subsequent normal own-inode release still unlinks the lock (the
fix is not "never unlink"). Armed with the standard 30s hard watchdog.

Result in this canonical checkout:

```text
own_inode=20218277 foreign_inode=20218278 different=True foreign_survived=True
LOCK_REPLACEMENT_TEST_PASS foreign_preserved=True own_release_ok=True
```

## 4. Focused gate (canonical checkout, frozen tree of 31dcd4276)

| Test | Exit | Elapsed |
| --- | --- | --- |
| test_compile | 0 | 0.1 s |
| test_lock | 0 | 0.0 s |
| test_lock_replacement (new) | 0 | 0.0 s |
| test_response_no_deadlock | 0 | 2.1 s |
| test_prehandoff_hang | 0 | 3.2 s |
| test_pg_cleanup | 0 | 0.1 s |
| test_runtime_timeout | 0 | 2.2 s |
| test_launchchain_smoke | 0 | 0.1 s |

Post-gate: no `.probe.lock`; no packet-owned process; unrelated Codex PIDs
preserved; `git diff --check` clean; only `scripts/cp0/appserver_probe.py`
and this report changed.

Scope honored: no harness redesign, no provider trace recapture, no
cdhash/schema work, no full build gate, no production code.

## 5. Review marker

```text
REVIEW REQUEST: A1.1 CP0 Harness Recovery H0-R1 — 31dcd4276dd41e2aaee0c9b142c0b07a66a75022
```
