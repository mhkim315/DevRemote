# Step 9.4 — Secure Accountless Onboarding Ledger

**Status:** 9.4-A FROZEN, 9.4-B + 9.4-C in progress
**Branch:** `feature/canonical-timeline-foundation`
**Started:** 2026-07-24

## Key Identities

| Packet | Status | SHA |
|--------|--------|-----|
| CONTRACT | ACCEPTED | `609127e29` |
| 9.4-A Daemon Bootstrap | **FROZEN** | `4b55d33e8` |
| 9.4-B QR Pairing | In progress | T2 |
| 9.4-C Android Keys | In progress | T1 |
| 9.4-A Homebrew | Pending | — |
| 9.4-D Revoke/Recovery | Pending | — |
| 9.4-E Physical Device | Pending | — |

## 9.4-A Round Summary

33 total rounds, 22 V1 reviews before ACCEPT.

| Phase | Rounds | V1 REJECT |
|-------|--------|-----------|
| Contract | 1 | 0 |
| Implementation | 32 | 21 |
| **Total** | **33** | **21** |

### Blocker Evolution
6 (R1) → 5 (R2) → 4 (R3-R6) → 3 (R7-R14) → 2 (R15-R24) → 1 (R25-R32) → **ACCEPT (R33)**

### Key Decisions
- R1: CONTRACT → T2 (Codex)
- R2: 9.4-A → T1 (DeepSeek)
- R4: T1 blind-retry → switch to T2
- R5-6: T1+T2 both stuck → SPLIT by concern
- R9: log.Fatalf crisis → lifecycle API void→error (scope expansion)
- R10-13: T2 version path confusion (4 attempts) → T1 fresh eyes
- R24: Crash-consistency → transaction Phase field
- R29: macOS launchctl "not found" ≠ error → 3-way return
- R33: **V1_ACCEPT** — all edge cases resolved

### Design Evolution
1. Basic CLI + LaunchAgent (R1-R3)
2. Transactional upgrade/rollback (R4-R13)
3. Crash-consistent state machine with Phase (R14-R24)
4. macOS-specific launchctl behavior (R25-R32)

## Operational Rules
- Ping-pong first → then model switch → then split
- Coordinator never self-accepts
- Pre-gate before every V1 dispatch
- Blind-retry: same signature + same approach > 2 → escalate

## Update Log
| Date | Event |
|------|-------|
| 2026-07-24 | CONTRACT ACCEPTED at 609127e29 |
| 2026-07-24 | 9.4-A FROZEN at 4b55d33e8 (33 rounds) |
| 2026-07-24 | 9.4-B + 9.4-C dispatched in parallel |
