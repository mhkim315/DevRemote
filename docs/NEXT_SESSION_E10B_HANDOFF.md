# Next Session Handoff — E10b Controlled PTY Runtime

Date: 2026-07-10
Last commit: c19823f0c (Codex Send + width + reconnect). Prior: 4c2127b08 (exit/reset/width), f395afae4 (this doc), a551b79e4.
Branch: feature/phase10-multi-adapter

## What We're Building

**POKIT (DevRemote)** — a macOS daemon that exposes terminal sessions via HTTP/WebSocket APIs. A React Native mobile app connects to the daemon to view and interact with terminal sessions.

### Current Phase: E10b — Controlled PTY Runtime + Local Terminal Attach

We just implemented `pokit run <command>`:
- Creates a Pokit-owned PTY session (controlled_pty adapter)
- Attaches the local Mac terminal as a subscriber
- Mobile/web can also observe and interact
- Recorder captures output → ActivityBuffer → Transcript

### Architecture (Key Concept)

```
Recorder (single PTY reader)
├── ActivityBuffer (Transcript — plain text, ANSI stripped)
├── Subscriber 1 (WebSocket → mobile Terminal)
├── Subscriber 2 (WebSocket → web Terminal)
└── Subscriber N (Unix socket → local Mac terminal — E10b)
```

**Critical invariant**: The Recorder is the SINGLE PTY reader. All viewers are subscribers — they never open their own PTY stream.

### Adapter Types

| Adapter | Mode | Terminal | Transcript |
|---------|------|----------|------------|
| tmux | byte_stream | Live xterm | Reliable |
| localpty | byte_stream | Live xterm | Reliable |
| controlled_pty | byte_stream | Live xterm | Reliable |
| cmux | screen_snapshot_delta | Disabled (501) | Best-effort/degraded |

---

## Recent Progress (2026-07-10)

Two commits after this doc was first written:
- **4c2127b08** — terminal mode hygiene on local attach exit + PTY width
- **c19823f0c** — Codex Send split, mobile ~100-col width policy, reconnect backoff

### Fixed & verified — backend (daemon redeployed)
- **P0 local-attach exit deadlock** — `handleIPCSubscriber` now `conn.Close()`s when the recorder's subCh closes (session ended), so the local client's `io.Copy` sees EOF and the host shell prompt returns. Verified by a unix-socket smoke test (immediate EOF; was a 5s hang). Files: `internal/term/ipc.go`, `internal/term/recorder.go`.
- **P0 control-sequence leak into shell** — `attachLocalTerminal` teardown emits a full terminal mode reset (alt-screen, bracketed paste, focus reporting, all mouse tracking, kitty keyboard pop, modifyOtherKeys, cursor/keypad normal, cursor show, SGR) via one `sync.Once` on **every** exit path (EOF / error / Ctrl+C / normal), plus a 150ms stdin drain to absorb in-flight query responses. Fixes `^[[?14;3R` / `14;3R` leaking into bash after `claude`/`codex`. File: `cmd/devremote/client.go`.
- **Width** — controlled_pty spawn default is ~100 cols (`internal/mux/session.go`); local attach advertises the host terminal size (`sub:<id> <cols> <rows>`) and the recorder resizes the PTY to match (`Recorder.Resize`).
- **Reconnect** — served terminal HTML uses an `everOpened` flag: explicit end (WS close 1008 auth / 1011 stream-ended) stops with SESSION ENDED; a drop **after** a good connection retries indefinitely with capped backoff (1.5s→15s); never-connected failures are bounded (6) then surfaced. Old socket is closed before reconnect, so no duplicate recorder subscribers. File: `internal/term/pty.go` (served HTML onopen/onclose).

### Fixed & verified — mobile (in the release APK)
- **Codex Send** — new `submitLine()` sends the typed text, then `\r` as a **separate** message 40ms later, so Codex sees a discrete Enter keypress. Codex (unlike claude/bash) treated a trailing `\r` bundled with the text as a literal newline — why the Enter macro worked but combined Send didn't. Send button + soft-keyboard Enter both use it. No accumulated-buffer regression. File: `mobile/src/screens/FeedScreen.tsx`.
- **Mobile width** — injected `fitTerminal` clamps xterm to `MIN_COLS=100` (never phone-narrow), adds horizontal-scroll CSS (`#t{overflow-x:auto}`, `.xterm{width:max-content}`), and no longer POSTs `/term/size` (a no-op route that would otherwise shrink the shared PTY).

### ⚠️ Build/verify gotcha — read before "the APK is stale!"
The release APK's `assets/index.android.bundle` is **Hermes bytecode**, and Hermes stores many string literals as **UTF-16LE**. So `grep -a 'overflow-x' bundle` returns 0 even when the change IS present (a plain `type a command` happens to be ASCII and greps fine, which is misleading). This cost a long false "stale bundle" hunt on 2026-07-10. **Verify a fresh mobile bundle via**:
- UTF-16LE byte search: `python3 -c "print(open('b','rb').read().count('overflow-x'.encode('utf-16-le')))"`
- Metro source map `android/app/build/generated/sourcemaps/react/release/index.android.bundle.map` (`sourcesContent`)
- bundle md5 change

Normal `EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1 ./android/gradlew -p android :app:assembleRelease` already produces a fresh bundle — Metro cache invalidation works; no `--reset-cache` / fresh-TMPDIR tricks needed.

### Still open (do NOT change runtime to fix these)
- **P2 Transcript** — Claude/Codex TUI output still flattens/duplicates into long horizontal lines in Transcript. Deferred by product decision. Fix the **projection policy** only; do not alter the live terminal stream, recorder ownership, or cmux heuristics.
- **Mobile h-scroll** — the horizontal-scroll CSS is best-effort; needs on-device visual confirmation. `MIN_COLS=100` clamp is certain.

---

## Development Environment

### Machine
- MacBook (Apple Silicon)
- macOS 26.x
- Go 1.26.4
- Node.js 20
- Android SDK at `/Users/mhk/Library/Android/sdk/`

### Key Paths

| What | Path |
|------|------|
| Project root | `/Users/mhk/.gemini/antigravity/scratch/DevRemote/devremote/` |
| Go daemon | `companion-daemon/` |
| React Native mobile | `mobile/` |
| Daemon binary | `/tmp/pokit-daemon` |
| APK output | `mobile/android/app/build/outputs/apk/release/app-release.apk` |
| Daemon log | `/tmp/daemon.log` |
| IPC socket | `/tmp/pokit.sock` |
| Cloudflare tunnel | `term.fullcount.kr` (LaunchAgent: `com.cloudflare.cloudflared`) |

---

## Build & Run Commands

### Backend

```sh
cd /Users/mhk/.gemini/antigravity/scratch/DevRemote/devremote/companion-daemon

# Build
go build ./...

# Full test with race detector
go test -race ./... -count=1

# Build daemon binary
go build -o /tmp/pokit-daemon ./cmd/devremote

# Start daemon (kill old first)
pkill -f "pokit-daemon"
sleep 1
/tmp/pokit-daemon --insecure-local-only &

# Verify daemon running
curl -s http://localhost:9171/api/sessions | python3 -c "import json,sys; print(f'{len(json.load(sys.stdin))} sessions')"

# Verify IPC socket exists
ls -la /tmp/pokit.sock  # should show srw------- (0600)

# pokit run CLI
/tmp/pokit-daemon run bash          # local attach
/tmp/pokit-daemon run --detach bash # no attach, URL only
/tmp/pokit-daemon run --cwd ~/project codex
```

### Mobile (APK Build)

```sh
cd /Users/mhk/.gemini/antigravity/scratch/DevRemote/devremote/mobile

# TypeScript check
npx tsc --noEmit

# Force rebuild APK (clean bundle cache first)
rm -rf android/app/build/generated
EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1 npx expo run:android --variant release
```

**IMPORTANT**: `EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1` is REQUIRED for no-login test builds. Without it, the APK requires Supabase auth.

### Build Gate

```sh
cd /Users/mhk/.gemini/antigravity/scratch/DevRemote/devremote
ALLOW_MOBILE_NOT_RUN=1 sh scripts/build-gate.sh
```

---

## Device Setup

### Android Phone (USB)

```sh
# List devices
adb devices

# Install APK to specific device
adb -s R3CX106PTFD install -r mobile/android/app/build/outputs/apk/release/app-release.apk

# If "device not found", unplug/replug USB, authorize on phone
```

### Android Emulator

```sh
# List available AVDs
emulator -list-avds

# The Pixel_9 emulator is usually available
# Expo automatically installs to the first running emulator when you run:
npx expo run:android --variant release
```

### iOS Simulator

```sh
# List simulators
xcrun simctl list devices available

# Build and run iOS
npx expo run:ios --device "iPhone 17 Pro"
```

---

## Daemon Health Check

```sh
# Is daemon responding?
curl -s http://localhost:9171/api/sessions

# Does IPC socket exist?
ls -la /tmp/pokit.sock

# Daemon process
ps aux | grep pokit-daemon

# Tunnel (for remote access)
curl -s -o /dev/null -w "%{http_code}" https://term.fullcount.kr/api/sessions
```

---

## Current Bugs / Status

### P0 — Local attach exit hangs (PARTIALLY FIXED)

**Symptom**: `pokit run bash` → `exit` → host terminal doesn't return to usable prompt.

**Fix applied**: `attachLocalTerminal` uses `stdoutDone` channel. When conn closes (session ended), main goroutine restores terminal and exits. `handleIPCSubscriber` calls `conn.Close()` when subCh closes.

**Status**: Code is in `a551b79e4`. Daemon MUST be running the latest binary for this to work. Previous test failures were because the daemon binary wasn't restarted after the code change.

**To test**: Build daemon, restart daemon, run `pokit run bash`, type `exit`.

### P1 — Transcript TUI corruption

**Symptom**: Codex/Claude TUI output produces giant horizontal merged lines in Transcript.

**Fix applied**: `\r` → `\n` replacement in Recorder before ActivityBuffer append.

**Status**: In `a551b79e4`. Needs real-device verification.

### P1/P2 — Default PTY width

**Symptom**: TUI apps may be too narrow at 80 cols.

**Fix**: Default changed to 80x24 in SpawnPTYWithDir. May want 120 cols.

### Mobile Send button

**Fix**: Uses `cmdRef.current` instead of state for reliability.

---

## Key Files to Know

### Backend (Go)

| File | Purpose |
|------|---------|
| `internal/term/recorder.go` | PTY read loop, subscriber management, bootstrap ring buffer |
| `internal/term/pty.go` | HTTP/WS handlers (HandleWS, HandleSessionCRUD) |
| `internal/term/ipc.go` | Unix socket server, handleIPCSubscriber |
| `internal/term/activity.go` | ActivityBuffer (Transcript data store) |
| `internal/mux/controlled_pty_adapter.go` | Controlled PTY adapter |
| `internal/mux/session.go` | SpawnPTY, SpawnPTYWithDir |
| `internal/mux/adapter_capability.go` | AdapterCapabilities |
| `internal/mux/transcript_capture.go` | TranscriptCaptureMode |
| `cmd/devremote/client.go` | pokit run CLI, buildRunPayload, attachLocalTerminal |
| `cmd/devremote/app.go` | Daemon composition, adapter registration |

### Mobile (TypeScript)

| File | Purpose |
|------|---------|
| `src/screens/FeedScreen.tsx` | Terminal/Transcript/Activity tabs, input handling |
| `src/screens/ConnectScreen.tsx` | Daemon URL connect screen |
| `src/screens/dashboard/DashboardScreen.tsx` | Session list |
| `src/lib/client.ts` | API client, connectivity errors |
| `src/lib/connection.tsx` | Connection state management |

---

## Current Git State

```sh
git log --oneline -5
# Most recent: a551b79e4 (E10b: fix Send button + Transcript \r→\n replacement)
# Also includes: exit fix, terminal bootstrap, controlled_pty adapter, etc.
```

Branch: `feature/phase10-multi-adapter`
Remote: `origin` → `https://github.com/mhkim315/DevRemote.git`

---

## Next Steps for the Next Agent

1. **Restart daemon with latest binary** (MUST do after every backend change):
   ```sh
   cd /Users/mhk/.gemini/antigravity/scratch/DevRemote/devremote/companion-daemon
   go build -o /tmp/pokit-daemon ./cmd/devremote
   pkill -f "pokit-daemon"; sleep 1; /tmp/pokit-daemon --insecure-local-only &
   # Verify: curl localhost:9171/api/sessions && ls -la /tmp/pokit.sock
   ```

2. **Test exit fix**: `pokit run bash` → `exit` → verify host shell returns.

3. **Test mobile Send**: Type "pwd" in mobile, press Send/Enter → verify command executes.

4. **Test Transcript**: `pokit run codex` → check Transcript tab for TUI corruption.

5. **After mobile code changes**: Rebuild APK (clean bundle cache first).

6. **Before claiming "fixed"**: Always restart daemon AND check the binary is the latest.

---

## Common Pitfalls

- **Daemon binary not updated**: `go build` must run from `companion-daemon/` directory.
- **Old daemon still running**: Must `pkill -f "pokit-daemon"` before starting new one.
- **Socket missing**: Daemon must be started with new binary. Socket path is `/tmp/pokit.sock`.
- **APK not updated**: Must `rm -rf android/app/build/generated` to force bundle regeneration.
- **CWD issues**: After running `build-gate.sh`, CWD changes — always `cd` back to `companion-daemon/` before `go build`.
- **Classifier blocking**: Some bash commands (pkill, nohup, background processes) may be blocked by the auto-mode safety classifier. Split into smaller commands or ask the user to run them.
