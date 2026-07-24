# Step 9.4 — Secure Accountless Onboarding Ledger

**Status:** IMPLEMENTATION ACCEPTED — PHYSICAL GATE + DOCS PENDING
**Branch:** `feature/canonical-timeline-foundation`
**Started:** 2026-07-24
**Last update:** V2 final-ready (ledger cleanup SHA 694eb73ee)

## Key Identities

| Packet | Status | Final SHA | Rounds |
|--------|--------|-----------|--------|
| CONTRACT | ACCEPTED | `609127e29` (+ Amendment 1, alpha signing deferral at `8680d36ca`) | 1 |
| 9.4-A Daemon Bootstrap | FROZEN | `4b55d33e8` | 33 |
| 9.4-B QR Pairing | ACCEPTED | `f76042ed3` (BootstrapToken-gated pre-proof Verify) | 36 |
| 9.4-C Android Keys | ACCEPTED | `0afbbfac5` | 3 |
| 9.4-A Homebrew | ACCEPTED | `dd5b7033c` | 1 |
| 9.4-D Revoke/Recovery | ACCEPTED | `5632868c6` | 44 |
| Integrated V1 | ACCEPTED | `5632868c6` | — |
| Pairing order fix | V1_ACCEPT | `f76042ed3` | 2 (T2) |
| Contract + ledger sync | Ready for V2 final | `1a00c8c52` | — |

### Pairing verification order (corrected at f76042ed3)
handleCandidate: BootstrapToken validation → bridge.Verify (pre-proof) → device proof → host proof. Mismatch fails closed before challenged state.

## Total: 118 rounds, 73 V1 REJECTs

## Operational Rules (validated)
- Ping-pong first → model switch @ 2 failures → split @ both stuck
- T1: backend state machines, constraint compliance, weak on concurrent tests
- T2: architecture, structural design, preserve for V2/CG/emergency
- Coordinator never self-accepts
- T2 budget: 7 calls (D3-A:2, B/C:1, IPC:1, LINEARIZATION:1, ref:1, V1 integ:1)

## Known Alpha Limitations
- Homebrew: source-build, not signed artifact (signing infra deferred in contract at 8680d36ca)

## Update Log
| Date | Event |
|------|-------|
| 2026-07-24 | CONTRACT ACCEPTED at 609127e29 |
| 2026-07-24 | Amendment 1 (QR mediation seam) |
| 2026-07-24 | 9.4-A FROZEN at 4b55d33e8 (33 rounds) |
| 2026-07-24 | 9.4-B ACCEPTED (36 rounds); pairing order fix at f76042ed3 |
| 2026-07-24 | 9.4-C ACCEPTED at 0afbbfac5 (3 rounds) |
| 2026-07-24 | Homebrew ACCEPTED at dd5b7033c |
| 2026-07-24 | 9.4-D ACCEPTED at 5632868c6 (44 rounds) |
| 2026-07-24 | V1 Integrated ACCEPT at 5632868c6 |
| 2026-07-24 | V1 pairing order fix ACCEPT at f76042ed3 |
| 2026-07-24 | Ledger cleanup at 694eb73ee |
| 2026-07-24 | V2 + evidence sync ready for V2 final (eeb95a223) |
