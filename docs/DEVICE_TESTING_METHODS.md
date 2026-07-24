# POKIT Device Testing Methods

How to set up, instrument, run, and collect evidence for emulator and
physical-device testing. Written 2026-07-24 from the SM-S926N device gate.

## Environment Setup

### Terminals (iTerm2 — 3 minimum)

| Terminal | Purpose | Command |
|----------|---------|---------|
| T1 | Execution agent | `cd /path/to/DevRemote` — code changes, ADB, rebuilds |
| T2 | Daemon log | `tail -f /tmp/daemon.log` — captured daemon stdout |
| T3 | Android logcat | `adb logcat -v threadtime \| tee android.log` |

### Physical Device (SM-S926N)

```sh
export ANDROID_SERIAL="R3CX106PTFD"
adb kill-server && adb start-server
adb devices -l  # verify: device state, model SM-S926N
adb reverse tcp:8081 tcp:8081  # Metro only — never daemon port
```

### Emulator (Pixel_9)

```sh
emulator -avd Pixel_9 -no-window -no-audio -no-snapshot -no-boot-anim -verbose \
  > /tmp/pokit-emulator/emulator.log 2>&1 &
adb wait-for-device
adb reverse tcp:8081 tcp:8081   # Metro
adb reverse tcp:9172 tcp:9172   # dev daemon
```

Common emulator issues:
- **Duplicate AVD**: Never run same AVD twice. Kill all `qemu-system-*` processes first.
- **Snapshot corruption**: Use `-no-snapshot`.
- **GPU crash**: Add `-gpu swiftshader_indirect` if graphics fail.

### Dev Daemon (emulator only)

```sh
/tmp/devremote_fresh daemon --insecure-local-only --listen-addr 127.0.0.1:9172 \
  > /tmp/daemon.log 2>&1 &
```

### Production Daemon + Tunnel (physical device)

```sh
/tmp/devremote_fresh daemon > /tmp/daemon.log 2>&1 &
# Tunnel starts automatically as child process.
# Wait: curl -s -o /dev/null -w "%{http_code}\n" https://term.fullcount.kr/api/sessions
# Expect HTTP 401 (auth required = tunnel working).
```

---

## Evidence Collection

### Evidence Directory

```sh
PB_DIR="$HOME/Desktop/PB_DEVICE_R<version>_$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$PB_DIR"
```

### Daemon Log Capture

```sh
/tmp/devremote_fresh daemon > "$PB_DIR/daemon.log" 2>&1 &
```

Key daemon log patterns:
- `pairing: device X approved` — pairing success
- `RECORDER start session=Y` — session created
- `WS [Y]: Z connected` — WebSocket connected (Z = source IP)
- `WARN: empty token allowed` — insecure mode auth bypass

### Android Logcat

```sh
adb logcat -c                                    # clear
adb logcat -v threadtime > "$PB_DIR/android.log" 2>&1 &
```

Key logcat patterns:
- `ReactNativeJS:` — JS console.log output
- `PokitError:` — API errors
- `com.pokit.mobile` — app process

### Screenshots

```sh
adb exec-out screencap -p > "$PB_DIR/step-N-description.png"
```

### Device State

```sh
adb shell dumpsys activity activities | grep -E "topResumedActivity|com.pokit"
adb shell dumpsys package com.pokit.mobile | grep -E "versionName|lastUpdateTime"
adb shell getprop ro.product.model
adb shell getprop ro.build.version.release
```

### Daemon State

```sh
/tmp/devremote_fresh devices         # paired device registry
/tmp/devremote_fresh audit --limit 5 # recent security events
lsof -i :9171                        # daemon + tunnel connections
```

### Transcript (insecure daemon only)

```sh
SESSION="controlled_pty:shell-..."
curl -s "http://127.0.0.1:9173/api/sessions/$SESSION/transcript" | python3 -m json.tool
```

Key transcript events:
- `terminal_output` — PTY bytes sent to client
- `input_boundary` — user input received
- `degraded` / `byteStreamSuppressed` — T3 suppression active

### PTY Geometry

```sh
curl -s "http://127.0.0.1:9173/term/size?session=$SESSION"
# → {"rows":30,"cols":100}
```

---

## Artifact Identity

### Daemon Provenance

```sh
go version -m /tmp/devremote_fresh | grep "vcs."
# Expected: vcs.revision=<sha>, vcs.modified=false
shasum -a 256 /tmp/devremote_fresh
```

### APK Provenance

```sh
# Built-in identity (must match candidate + daemon):
unzip -p app-release.apk assets/PB_DEVICE_CANDIDATE_SHA.txt
unzip -p app-release.apk assets/DAEMON_SHA256.txt

# Cleartext check:
aapt2 dump xmltree app-release.apk --file AndroidManifest.xml | grep cleartext

# Full hash:
shasum -a 256 app-release.apk
```

### Build Injection

Before building APK, set correct SHA files (gitignored, injected at build time):

```sh
mkdir -p mobile/android/app/src/main/assets
echo "<candidate-sha>" > mobile/android/app/src/main/assets/PB_DEVICE_CANDIDATE_SHA.txt
echo "<daemon-sha>" > mobile/android/app/src/main/assets/DAEMON_SHA256.txt
```

### Installed APK Verification

```sh
APK_PATH=$(adb shell pm path com.pokit.mobile | cut -d: -f2)
adb pull "$APK_PATH" /tmp/installed.apk
shasum -a 256 /tmp/installed.apk
# Must match frozen APK hash.
```

### Artifact Bundle (for independent verification)

```sh
ARTIFACTS="/tmp/pokit-pb-device-artifacts"
ls "$ARTIFACTS"/
# Expected:
#   pokit-daemon              — daemon binary
#   pokit-app-release.apk     — release APK
#   PB_DEVICE_CANDIDATE_SHA.txt  — embedded in APK
#   DAEMON_SHA256.txt            — embedded in APK
#   APK_SHA256.txt               — full APK hash
#   APK_EXTRACTED_CANDIDATE.txt  — extracted from APK (confirm match)
#   APK_EXTRACTED_DAEMON.txt     — extracted from APK (confirm match)
```

---

## Mobile Diagnostics (PB-DIAG)

Add temporary console.log in FeedScreen.tsx to trace the 3 input gate conditions:

### fetchSession (around line 340)
```typescript
console.log("[PB-DIAG] fetchSession: rowFound=" + !!sess +
  " adapterCaps=" + JSON.stringify(sess?.adapterCapabilities) +
  " lifecycleState=" + (sess?.lifecycleState || "none"));
```

### hello handler (around line 610)
```typescript
console.log("[PB-DIAG] hello: sessionMatch=" + (data.sessionId === session) +
  " gen=" + data.generation + " caps=" + JSON.stringify(nextCaps));
```

### inputGate (around line 247)
```typescript
console.log("[PB-DIAG] inputGate: sessionCanInput=" + actionPolicy.inputEnabled +
  " deviceCanInput=" + deviceCanInput +
  " lifecycleState=" + lifecycleState +
  " managed=" + managed + " inputCapable=" + inputCapable);
```

Read in logcat: `adb logcat -d | grep "PB-DIAG"`

Expected output when working:
```
fetchSession: rowFound=true adapterCaps=["liveTerminal","live_stream","history","managedLifecycle","input"] lifecycleState=running
inputGate: sessionCanInput=true deviceCanInput=true lifecycleState=running managed=true inputCapable=true
```

Expected output when broken:
```
inputGate: sessionCanInput=true deviceCanInput=false lifecycleState=running managed=true inputCapable=true
                                        ^^^^^^^^^^^^^^^^
                                        hello not reaching FeedScreen
```

Always remove diagnostics before committing. Use `git checkout -- <file>` to revert.

---

## Tapflow (Emulator UI Automation)

### Setup

```sh
cd /path/to/DevRemote
npm install @tapflowio/mcp-server
npx tapflow start > /tmp/tapflow.log 2>&1 &
```

### Admin + PAT Creation

```sh
# Create admin (first time):
curl -s -X POST http://localhost:4000/api/v1/auth/init \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@tapflow.local","password":"testpass123"}'

# Login:
curl -s -X POST http://localhost:4000/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@tapflow.local","password":"testpass123"}' \
  -c /tmp/cookies.txt

# Create PAT (1 day):
curl -s -X POST http://localhost:4000/api/v1/tokens \
  -H "Content-Type: application/json" \
  -b /tmp/cookies.txt \
  -d '{"name":"POKIT Test","expiresIn":86400}'
```

### Direct MCP Client (Node.js)

```javascript
import { TapflowClient } from '@tapflowio/mcp-server/dist/client.js';
const c = new TapflowClient('ws://localhost:4000', '<pat>');
await c.connect();

// List devices
const sessions = await c.listDevices();
const android = sessions.find(s => s.platform === 'android');
const pixel = android?.devices.find(d => d.name?.includes('Pixel'));
const sid = pixel.sessionId;

// Connect
await c.connectDevice(sid);

// Screenshot (REST API, avoid stdout buffer issues)
// Use ADB screencap instead — Tapflow screenshot has relay buffer issues.

// UI tree (REST API — works reliably)
const tree = await c.queryUITree(sid);
// tree[n] = { role, label, identifier, frame: { x, y, width, height }, enabled }

// Tap (WebSocket)
await c.tap(sid, normalizedX, normalizedY);

// Type text (WebSocket)
await c.typeText(sid, 'http://localhost:8081');

// Press key (WebSocket)
await c.pressKey(sid, 'enter');
await c.pressKey(sid, 'backspace');

c.disconnect();
```

### ADB Alternative (when Tapflow tap fails on RN elements)

Tapflow's `tap()` and ADB's `input tap` both fail on React Native `TouchableOpacity`
inside `<Modal>`. The Dashboard's non-Modal buttons work with ADB:

```sh
# Dashboard buttons work:
adb shell input tap <px_x> <px_y>

# Modal buttons do NOT work — requires physical touch.

# Text input via keyevents works:
adb shell input keyevent KEYCODE_P
adb shell input keyevent KEYCODE_ENTER
```

### ADB Deep Link (skip DevLauncher)

```sh
adb shell am start -a android.intent.action.VIEW \
  -d "exp+pokit://expo-development-client/?url=http://127.0.0.1:8081" \
  com.pokit.mobile
```

---

## Common Operations

### Owner Recovery

```sh
echo "<first-8-chars>" | /tmp/devremote_fresh devices recover-owner \
  --from <old-owner-id> --to <new-device-id>
```

### Session Creation via IPC (bypass Modal)

```sh
echo '{"version":1,"operation":"create","profileId":"shell","name":"test","cwd":""}' \
  | nc -U /tmp/pokit.sock
```

### Dev Daemon on Separate Port

```sh
/tmp/devremote_fresh daemon --insecure-local-only --listen-addr 127.0.0.1:9173 \
  > /tmp/daemon-dev.log 2>&1 &
# Test: curl http://127.0.0.1:9173/api/sessions
```

### Clear App Data + Registry for Clean Test

```sh
# 1. Revoke active owners to prevent orphaned devices:
OWNER=$(/tmp/pokit-daemon devices | grep "state=active" | awk '{print $1}')
/tmp/pokit-daemon devices revoke "$OWNER"

# 2. Clear app (destroys pairing data + Keystore key = new device ID):
adb shell pm clear com.pokit.mobile

# 3. Verify:
/tmp/pokit-daemon devices | grep active  # should be empty
adb shell pm list packages pokit         # still installed
```

---

## Device Gate Evidence Template

```markdown
## PB-DG-R<N> — Physical Device Smoke

**Date**: YYYY-MM-DD HH:MM TZ
**Device**: SM-S926N (Android V, SDK N)
**Status**: N/N PASS/FAIL

### Candidate Identity
| Item | Value |
|------|-------|
| Source SHA | `<sha>` |
| Daemon SHA-256 | `<sha>` |
| APK SHA-256 | `<sha>` |
| APK embedded candidate | `<sha>` (matches) |
| APK embedded daemon | `<sha>` (matches) |
| vcs.revision | `<sha>` |
| vcs.modified | false |
| Cleartext | usesCleartextTraffic=true |

### Smoke Matrix
| # | Test | Result | Evidence |
|---|------|--------|----------|
| 1 | QR pairing | PASS | owner `<id>`, /pair LAN HTTP |
| 2 | DeviceAuth | PASS | owner role, terminal:input |
| 3 | HTTPS/WSS switch | PASS | WS from 127.0.0.1 (tunnel) |
| 4 | Managed shell | PASS | session ID, RECORDER log |
| 5 | TERM-C1 hello | PASS | no "view only", deviceCanInput=true |
| 6 | Acknowledged input | PASS | command → PTY output |
| 7 | Ctrl+C | PASS | process interrupted |
| 8 | Cleartext-negative | PASS | LAN /pair only, ops via HTTPS/WSS |

### Evidence
- Screenshot: `/tmp/r4.x-terminal.png`
- Daemon log: `/tmp/daemon-r4.x.log`
- Android logcat: `/tmp/r4.x-smoke.log`
- APK: `/tmp/pokit-pb-device-artifacts/pokit-app-release.apk`
- Daemon: `/tmp/pokit-pb-device-artifacts/pokit-daemon`
```
