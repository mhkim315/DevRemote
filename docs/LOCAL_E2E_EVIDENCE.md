# Local E2E Connection Mode — Implementation & Evidence

**Branch**: feature/phase10-multi-adapter  
**Date**: 2026-07-22  
**Device**: Android Emulator (Pixel 9, API 37) + SM-S926N (physical)

## Commits

| SHA | Description |
|-----|-------------|
| `554e1c67a` | `canonicalLocalTestOrigin()` — localhost HTTP for emulator |
| `647fa031d` | Absolute daemon URL for terminal WebView |
| `ad12fdb52` | Remove temporary diagnostic logging |
| `57e75fe08` | **Add `'input'` to adapterCapabilities** (view-only root cause) |

## Implementation

### canonicalLocalTestOrigin() (authMode.ts)
Permits `http://localhost:<port>` and `http://127.0.0.1:<port>` ONLY when ALL conditions are true:
- Debug build (`EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1`)
- `explicit_local_dev` connection mode
- Hostname exactly `localhost` or `127.0.0.1`
- Explicit port present

Rejects: LAN IPs, `10.0.2.2`, arbitrary domains, missing port, HTTPS, wrong mode.

### ConnectScreen localTest prop
When `localTest={NO_LOGIN}`:
- Default URL: `http://localhost:9172`
- Validation uses `canonicalLocalTestOrigin()`
- SET DAEMON URL triggers `connect()` probe

### TermURI absolute URL
`deriveTerminalAuth` uses `authCtx.baseURL` prefix for absolute daemon URL on emulator (Metro :8081 ≠ daemon :9172).

### input capability (ROOT CAUSE)
`readCapabilities()` checks `adapterCapabilities.includes('input')` → `inputCapable`.
`computeActionPolicy` uses `inputCapable` → `inputEnabled`.
Without `'input'` in adapterCapabilities: `inputEnabled=false` → "view only" bar.

**Fix**: Added `'input'` to `adapterCapabilities` in:
- `telemetry.go` (live rows + retained rows)
- `managed_catalog.go` (ManagedCapabilities)

## Test Results

### Automated
- **Mobile**: 537/537 (TSC + Jest, 35 suites)
- **Daemon**: all pass (race detector)

### Manual (Emulator + Tapflow)

| Test | Result | Evidence |
|------|--------|----------|
| localhost HTTP accepted | PASS | `canonicalLocalTestOrigin()` 18 tests |
| HTTPS unchanged | PASS | `canonicalOrigin()` existing tests |
| DAEMON CONNECTED | PASS | Tapflow UI tree |
| Session creation | PASS | IPC socket + daemon `RECORDER` log |
| Terminal WebView loaded | PASS | 47-element UI tree |
| **"view only" resolved** | PASS | `'input'` capability → Send + macros enabled |
| Send button | PASS | `enabled=true` |
| Macro controls visible/enabled | PASS | 11 TouchableOpacity elements, enabled=true |
| WebSocket connected | PASS | Daemon `WS [...] connected` log |
| `pwd` input | PASS | `adb input keyevent` → field shows "pwd" |
| `pwd` submit | PASS | `KEYCODE_ENTER` → field cleared |
| `input_boundary` | PASS | Transcript event at 11:37:09 |
| PTY geometry (initial) | PASS | `{"rows":30,"cols":100}` |
| PTY output production | PASS | Daemon transcript `input_boundary` |
| xterm.js rendering | NOT TESTED | Canvas output not in accessibility tree; requires screenshot or xterm buffer read |
| Macro controls visible/enabled | PASS | 11 macro TouchableOpacity elements, `enabled=true` |
| Macro native press → PTY delivery | NOT TESTED | RN TouchableOpacity not responsive to Tapflow/ADB injection; requires Maestro/Detox or physical touch |
| Ctrl+C native press | NOT TESTED | Same injection limitation |
| Output rendering (visual) | NOT TESTED | Requires Samsung WebView or screenshot diff |

### Known Limitations
- **Tapflow/ADB taps cannot activate RN `TouchableOpacity.onPress`** — Shell/CREATE in Modal, macro buttons, Send button all require native UI automation (Maestro/Detox) or physical touch
- **Emulator instability** — QEMU GPU snapshot corruption causes crashes; `-no-snapshot` mitigates
- **Terminal output not in accessibility tree** — xterm.js canvas rendering requires screenshot diff or xterm buffer read
- **`explicit_local_dev` ≠ production auth** — dev-token + HTTP localhost does not cover QR pairing, DeviceAuth, WSS tickets, Keystore identity

## Physical Device Gates (require SM-S926N)
- QR pairing (host LAN IP)
- Device authentication (challenge/verify)  
- Production WSS tickets
- Android Keystore identity
- Ctrl+C, paste, macro touch activation
- Geometry resize (keyboard/window)
- Complex commands with spaces/quotes
- Regression SHA vs HEAD comparison

## Release-Negative Evidence
- `canonicalOrigin()` unchanged — HTTPS still required for production
- Release builds: `NO_LOGIN=false` → `localTest=false` → HTTP localhost rejected
- Android `usesCleartextTraffic` in debug manifest only (pre-existing)

## Revision History

| SHA | Date | Changes |
|-----|------|---------|
| `b1caf9795` | 2026-07-22 | LOCAL-E2E-R1: evidence corrections, testIDs, daemon test |
| `5c50f9199` | 2026-07-22 | LOCAL-E2E-R2: Detox scaffold, 10 testIDs |
| `5c50f9199` | 2026-07-22 | Detox status: SCAFFOLD READY — NOT EXECUTED |

### Detox Status: SCAFFOLD

Test file `mobile/e2e/terminal-macros.test.ts` serves as automation contract spec.
Actual execution requires APK rebuild with Metro bundle baked in (NO_LOGIN mode).
Detox native tap execution is deferred to CI/build pipeline or SM-S926N gate.

Assertions to complete before Detox PASS claim:
- Enter macro tap via `terminal-macro-enter` testID → PTY delivery confirmed
- Ctrl+C tap via `terminal-macro-ctrl-c` → 0x03 byte → process interrupt confirmed
- Paste → exact PTY bytes confirmed
- Two-frame Text+Enter → both ACKs confirmed
- Reconnect → capability+geometry+pending ACK state confirmed
