# Step 9.4 — Secure Accountless Onboarding Ledger

**Status:** INTEGRATED — V1_ACCEPT at `5632868c6`
**Branch:** `feature/canonical-timeline-foundation`
**Started:** 2026-07-24
**Completed:** 2026-07-24

## Key Identities

| Packet | Status | Final SHA | Rounds |
|--------|--------|-----------|--------|
| CONTRACT | ACCEPTED | `609127e29` (+ Amendment 1) | 1 |
| 9.4-A Daemon Bootstrap | FROZEN | `4b55d33e8` | 33 |
| 9.4-B QR Pairing | ACCEPTED | `d91ee9975` | 36 |
| 9.4-C Android Keys | ACCEPTED | `0afbbfac5` | 3 |
| 9.4-A Homebrew | ACCEPTED | `dd5b7033c` | 1 |
| 9.4-D Revoke/Recovery | ACCEPTED | `5632868c6` | 44 |
| INTEGRATED | V1_ACCEPT | `5632868c6` | — |

## Total: 118 rounds, 73 V1 REJECTs

## Operational Rules (validated)
- Ping-pong first → model switch @ 2 failures → split @ both stuck
- T1: backend state machines, constraint compliance, weak on concurrent tests
- T2: architecture, structural design, preserve for V2/CG/emergency
- Coordinator never self-accepts
- T2 budget: 7 calls (D3-A:2, B/C:1, IPC:1, LINEARIZATION:1, ref:1, V1 integ:1)

## Known Alpha Limitations
- Homebrew: source-build, not signed artifact (signing infra deferred)
- Pairing: bridge.Verify after WaitForCandidate (V1 accepted, V2 noted)

## Update Log
| Date | Event |
|------|-------|
| 2026-07-24 | CONTRACT ACCEPTED at 609127e29 |
| 2026-07-24 | Amendment 1 (QR mediation seam) |
| 2026-07-24 | 9.4-A FROZEN at 4b55d33e8 (33 rounds) |
| 2026-07-24 | 9.4-B ACCEPTED at d91ee9975 (36 rounds) |
| 2026-07-24 | 9.4-C ACCEPTED at 0afbbfac5 (3 rounds) |
| 2026-07-24 | Homebrew ACCEPTED at dd5b7033c |
| 2026-07-24 | 9.4-D ACCEPTED at 5632868c6 (44 rounds) |
| 2026-07-24 | V1 Integrated ACCEPT at 5632868c6 |
| 2026-07-24 | V2 in progress — pairing order + ledger + artifact signing |
