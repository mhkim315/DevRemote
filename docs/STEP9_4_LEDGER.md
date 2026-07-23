# Step 9.4 — Secure Accountless Onboarding Ledger

**Status:** IN PROGRESS
**Branch:** `feature/canonical-timeline-foundation`
**Started:** 2026-07-24
**CONTRACT SHA:** `609127e29`

## Round Count Ledger

| # | Phase | Worker | Attempt | Dispatch | Reported SHA | Verified SHA | Pre-gate | Verdict | Blocker | Notes |
|---|-------|--------|---------|----------|-------------|-------------|----------|---------|---------|-------|
| 1 | contract | T2 | 1 | DONE | `609127e29` | `609127e29` | PASS | — | — | 5 packets, epoch model frozen |
| 2 | impl-9.4-A | T1 | 1 | DONE | `3931c07e1` | `3931c07e1` | PASS | V1_REJECT | 6 IMPL_REJECT | CLI routing, plist, upgrade, idempotency, build-tag, doctor |
| 3 | impl-9.4-A-R2 | T1 | 2 | FIX | 대기중 | — | — | — | same 6 from R2 | ping-pong: T1 fix all 6 before re-review |

## Operational Rules

**Dynamic task splitting:** Ping-pong first (same worker fixes). If both T1 and T2 fail on the same blocker → Coordinator splits task by package boundary. Record SPLIT in round ledger with subtask IDs. Never allow a 3rd blind retry with the same approach.

**Split recording:** Every SPLIT_REQUIRED or coordinator-initiated split gets a row with Dispatch=SPLIT, recording: reason, original task, subtasks, worker assignment.

## Cumulative Counts

| Phase | Rounds | Details |
|-------|--------|---------|
| Contract | 1 | T2 single-shot |
| Implementation | 2 | T1 R1 (REJECT) + R2 fix (진행중) |
| Pre-gate returns | 0 | |
| V1 REJECT | 1 | 6 IMPL_REJECT (routing, plist, upgrade, idempotency, build-tag, doctor) |
| V1 ACCEPT | 0 | |
| Evidence | 0 | |
| V2 Audit | 0 | |
| Context Guardian | 0 | |
| **Total** | **3** | |

## Defect Classification

| Category | Count | % |
|----------|-------|---|
| CONTRACT_REJECT | 0 | — |
| IMPL_REJECT | 6 | 100% |
| PRE_GATE_BLOCKED | 0 | — |
| TECH_EVID_BLOCKED | 0 | — |
| EVID_SYNC_BLOCKED | 0 | — |
| CONTEXT_DRIFT_BLOCKED | 0 | — |

## V1 REJECT Detail — Round 2

| Blocker | Category | Description | Root Cause |
|---------|----------|-------------|------------|
| B1 | IMPL_REJECT | daemon subcommands unreachable, always enters foreground serve | main.go routing: daemon not excluded from subcommand dispatch |
| B2 | IMPL_REJECT | .plist is JSON, not valid LaunchAgent property list | Wrong format; need XML plist per launchd.plist(5) |
| B3 | IMPL_REJECT | upgrade/rollback absent: no backup, no restore, plist short-circuits | Missing feature implementation |
| B4 | IMPL_REJECT | idempotency/security: loaded detection incomplete, .tmp race, no 0700 repair | Incomplete state checking + TOCTOU |
| B5 | IMPL_REJECT | GOOS=linux build fails on Darwin-only symbols | Build tag separation incomplete |
| B6 | IMPL_REJECT | doctor: no CLI version, false trust/auth diagnostics | Wrong schema fields, revoked counted active, non-200 treated OK |

## Decision Log

| Round | Decision | Rationale |
|-------|----------|-----------|
| 1 | CONTRACT to T2 (Codex) | Combined contract+impl capability; architecture-first approach |
| 2 | 9.4-A → T1 (DeepSeek) | Backend Go work (CLI, LaunchAgent); T2 context limits |
| 3 | V1_REJECT → T1 fix (ping-pong) | First rejection — same worker gets fix chance. If T1 fails again with same blockers → escalate to T2 or split |
| — | Coordinator never self-accepts | Accept only via V1/V2 verdict. Coordinator does not judge implementation quality |

## Update Log

| Date | Event | Detail |
|------|-------|--------|
| 2026-07-24 | CONTRACT ACCEPT | `609127e29` — STEP9_4_CONTRACT.md covering A-E |
| 2026-07-24 | 9.4-A dispatched | T1 daemon bootstrap interface (CLI + LaunchAgent) |
| 2026-07-24 | 9.4-A R1 DONE | `3931c07e1` — gofmt/build/vet/test 17/17 PASS |
| 2026-07-24 | V1 REJECT | 6 IMPL_REJECT (B1-B6): routing, plist, upgrade, idempotency, build-tag, doctor |
| 2026-07-24 | 9.4-A R2 dispatched | T1 fix round — all 6 blockers, ping-pong |
