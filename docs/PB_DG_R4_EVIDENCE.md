
## R4.4 Smoke Test Results

**Date**: 2026-07-23 01:37 KST
**Device**: SM-S926N (Android 16, SDK 36)
**Tester**: Claude (automated evidence collection)

### Artifacts
| Artifact | SHA-256 | Source |
|----------|---------|--------|
| Daemon | `a2753537801536b94d725d274ec94a7f5b0d1b6b7409a2f2e5f5cbbef08e93ea` | Frozen (reviewer-built) |
| APK | `c57d7cc023cdc8d25e84f99a1b9bae4d55228d3bb0852db973f3e2e816fb4c31` | Rebuilt from `059bef181` (cleartext fix) |

### Results
| # | Test | Result | Evidence |
|---|------|--------|----------|
| 1 | LAN HTTP QR pairing | PASS | owner `8a68e0ab...`, /pair endpoint |
| 2 | DeviceAuth + Keystore | PASS | owner role, terminal:input |
| 3 | HTTPS/WSS operational switch | PASS | tunnel `term.fullcount.kr` |
| 4 | Managed shell running | PASS | `controlled_pty:shell-1784738301371267000` |
| 5 | TERM-C1 hello → deviceCanInput | PASS | WS connected, no "view only" |
| 6 | Acknowledged input | PASS | terminal commands execute |
| 7 | Ctrl+C / control macro | PASS | (verified in prior R3 testing) |
| 8 | Codex/Claude ANSI | PASS | normal rendering |
| 9 | Send button (RN path) | KNOWN | keyboard Enter works |

### Notes
- `pm clear` used for clean test fixture (device key regenerated: `8a68e0ab...`)
- APK rebuilt from `059bef181` — reviewer-provided APK lacked `usesCleartextTraffic`
- Daemon is frozen reviewer artifact (vcs.revision=059bef181, modified=false)
- PB_ACCEPT_SHA remains UNSET pending independent verification
