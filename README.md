# POKIT

Local-first AI agent observability. See what your AI agents are doing, respond to interactions, and review activity — from your phone.

## Quick start

### Prerequisites

- Go 1.26+
- Node.js 20+
- Android SDK (for Android builds) or Xcode (for iOS builds)

### 1. Install daemon

```sh
sh scripts/install.sh
```

This builds and installs the `devremote` binary to `~/.local/bin`.

### 2. Start daemon

```sh
devremote daemon --insecure-local-only --enable-localpty --enable-agent-detection
```

### 3. Build and run mobile

For local testing (no Supabase login):

```sh
cd mobile
npm ci
EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1 npx expo run:android
```

Or for iOS:

```sh
EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1 npx expo run:ios
```

### 4. Connect

- Enter daemon URL in the app (e.g. `http://localhost:9171` for desktop, `http://10.0.2.2:9171` for Android emulator).
- Tap CONNECT.
- Dashboard loads with active sessions.

## Development

### Full dev setup

```sh
sh scripts/dev-setup.sh
```

### Build gate

```sh
sh scripts/build-gate.sh
```

Runs 8 checks: go build, go vet, go test -race, git diff --check, mobile typecheck, vendor branch scan, ID inference scan, secret scan.

### No-login local test mode

Set `EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1` at build time to skip Supabase auth. The mobile app uses a dev token (`dev-token`) accepted by the daemon in `--insecure-local-only` mode.

Production auth (Supabase JWT) is unchanged. The test flag is a build-time gate, not a runtime toggle.

## Architecture

POKIT has two layers:

- **Terminal Adapter Layer** — where sessions run (tmux, cmux, LocalPTY)
- **Agent Adapter Layer** — what AI agent runs inside (Claude, Codex, Antigravity)

Agents are detected from process evidence and their activity is normalized into a common event contract. Unknown agents, events, and statuses degrade gracefully.

## Project structure

```
companion-daemon/     Go daemon (HTTP/WebSocket API)
mobile/               React Native app (Expo)
scripts/              Build, install, and gate scripts
docs/                 Architecture plans, acceptance docs, reports
```

## Agent support

| Agent | Detect | Parse | Interaction |
|-------|--------|-------|-------------|
| Claude | ✅ | ✅ | ✅ |
| Codex | ✅ | ✅ | ✅ |
| Antigravity | ✅ | ✅ | observe-only |

## License

Proprietary. Not for redistribution.
