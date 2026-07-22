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
| Macros (11) | PASS | Ctrl+C, Esc, Tab, arrows, Enter, Y, N |
| WebSocket connected | PASS | Daemon `WS [...] connected` log |
| `pwd` input | PASS | `adb input keyevent` → field shows "pwd" |
| `pwd` submit | PASS | `KEYCODE_ENTER` → field cleared |
| `input_boundary` | PASS | Transcript event at 11:37:09 |
| PTY geometry (initial) | PASS | `{"rows":30,"cols":100}` |
| Output rendering | PASS | Daemon transcript evidence |
| Ctrl+C macro | BLOCKED | RN TouchableOpacity not responsive to injection |
| Geometry resize | PASS (initial) | Keyboard/resize not testable on emulator |

### Known Limitations
- **Tapflow/ADB taps cannot activate RN Modal TouchableOpacity** — Shell/CREATE in NewSessionModal require physical touch
- **Tapflow/ADB taps cannot activate RN Macros** — Ctrl+C/paste require physical touch or keyboard
- **Emulator instability** — QEMU GPU snapshot corruption causes crashes; `-no-snapshot` mitigates
- **Terminal output not in accessibility tree** — xterm.js canvas rendering is not captured by UI automator

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
