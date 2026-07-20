# PA4 Mobile Live Acceptance — Agent Handoff

**Date**: 2026-07-20
**Current HEAD**: `79d285227`
**Branch**: `feature/phase10-multi-adapter`

## Current State

PA4 mobile live acceptance **PASS** on Samsung SM-S926N (Android 16).

### Running Services
| Service | Status | Command |
|---------|--------|---------|
| Daemon | Running (production mode) | `/tmp/devremote_fresh daemon` (PID varies) |
| Metro | Running | `npx expo start --dev-client --localhost -a` (port 8081) |
| Cloudflare tunnel | Running | `cloudflared tunnel run` → `term.fullcount.kr` → `127.0.0.1:9171` |
| Device | Connected USB | `adb devices` → `R3CX106PTFD` (SM-S926N, Android 16) |

### Key Paths
| What | Path |
|------|------|
| Daemon binary | `/tmp/devremote_fresh` (build from `companion-daemon/cmd/devremote`) |
| Daemon log | `/tmp/devremote.log` |
| IPC socket | `/tmp/pokit.sock` |
| Pairing QR PNG | `/var/folders/_t/.../pokit-pair-*.png` |
| Mobile project | `/Users/mhk/orca/DevRemote/mobile` |
| Device store | `/Users/mhk/.pokit/devices.json` |

## What Was Done

### 10 Bugs Found & Fixed
1. QR renderer ANSI leak → `pair.go` renderQRANSI()
2. Android cleartext HTTP blocked → `usesCleartextTraffic`
3. Host proof double SHA-256 → removed `sha256()` wrapper in pairingClient.ts
4. Pair command auto-reject in background → `--auto-approve` flag
5. **`--insecure-local-only` breaks device auth** → MUST use production mode
6. `selectAppRoute` pairing_required bypass → removed (fail-closed)
7. ConnectScreen restructured → SET DAEMON URL + operational URL separation
8. USB forward loss on adb restart → use tunnel instead
9. Managed PTY empty terminal → pre-existing
10. WebView keyboard input → pre-existing (focus issue)

### Quick Restart Commands
```sh
# Build daemon
cd /Users/mhk/orca/DevRemote/companion-daemon
go build -o /tmp/devremote_fresh ./cmd/devremote

# Start daemon (PRODUCTION MODE - no --insecure-local-only!)
kill $(pgrep devremote) 2>/dev/null; sleep 1
/tmp/devremote_fresh daemon > /tmp/devremote.log 2>&1 &

# USB forwards
adb -s R3CX106PTFD reverse tcp:9171 tcp:9171
adb -s R3CX106PTFD reverse tcp:8081 tcp:8081

# Start Metro
cd /Users/mhk/orca/DevRemote/mobile
lsof -ti :8081 | xargs kill -9 2>/dev/null; sleep 1
npx expo start --dev-client --localhost -a &

# Generate QR for pairing
/tmp/devremote_fresh pair --duration 10m --auto-approve &
# Then forward the pairing port:
PAIR_PORT=$(lsof -i -P -n | grep devremote | grep LISTEN | grep -v 9171 | tail -1 | awk '{print $9}' | sed 's/.*://')
adb -s R3CX106PTFD reverse tcp:$PAIR_PORT tcp:$PAIR_PORT
# Open QR PNG:
ls -t /var/folders/_t/zxzx_2zj7n72xf_nlb4wnj400000gn/T/pokit-pair-*.png | head -1 | xargs open

# Create managed shell session (for testing after pairing):
echo '{"version":1,"operation":"create","profileId":"shell","name":"Shell"}' | nc -U /tmp/pokit.sock
```

## Critical Gotchas

1. **ALWAYS use production mode** (`daemon` without `--insecure-local-only`). The insecure flag breaks device auth after pairing.
2. **adb reverse clears on adb restart** — re-establish forwards after any daemon/adb restart.
3. **Pairing QR uses HTTP LAN endpoint** — phone must be on same WiFi (192.168.219.x) or have port forwarded via USB.
4. **ConnectScreen**: User must SET DAEMON URL (`https://term.fullcount.kr`) before scanning QR. The SET button does NOT connect — it only stores the URL for pairing.
5. **After pairing**: App saves device identity, creates TokenManager, gets bearer token, then connects. Full flow works in production mode.
6. **App data clear**: `adb shell pm clear com.pokit.mobile` resets all pairing/auth state.

## What's Remaining (NOT DONE)

1. **HTTPS QR pairing through tunnel**: Currently pairing uses LAN HTTP endpoint. Need daemon `/pair` routes on main HTTP server (port 9171) so tunnel can serve `https://term.fullcount.kr/pair`.
2. **Managed shell terminal input**: WebView keyboard focus issue — pre-existing, not PA4.
3. **iOS integration test cleanup**: 5 tests updated but need review for new PairFromScanDeps interface.
4. **PA4.5 Evidence**: Update with final SHA `79d285227` and production mode result.
5. **Mobile `npx tsc --noEmit`**: Should pass (Jest 451/451 passes).

## Coordinator Handle
The verification agent is at `term_8ca20c76-b656-4e0a-85d6-1750515b05d2`. Send `worker_done` there.

## Key Files Modified (since PA4 baseline)
- `mobile/src/lib/authMode.ts` — pairing_required fail-closed
- `mobile/src/screens/ConnectScreen.tsx` — SET DAEMON URL + operational URL
- `mobile/src/lib/connectPairing.ts` — PairFromScanDeps refactor, empty URL rejection
- `mobile/src/lib/pairingClient.ts` — sha256 fix
- `mobile/__tests__/authMode.test.ts` — 4 new selectAppRoute tests
- `mobile/__tests__/m3aAuthIOSIntegration.test.ts` — updated for new interface
- `companion-daemon/cmd/devremote/pair.go` — QR renderer, auto-approve
- `companion-daemon/cmd/devremote/pair_test.go` — QR renderer test
- `companion-daemon/internal/devicetrust/pairing.go` — debug logging, ServeHTTP
- `companion-daemon/docs/PA4_MOBILE_BUG_REPORT.md` — final bug report
