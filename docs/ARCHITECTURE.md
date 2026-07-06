# POKIT Architecture

## Overview

POKIT is a mobile-native AI agent remote cockpit. It consists of two parts:

```
┌─────────────────────────────┐     ┌──────────────────────────────┐
│  macOS Daemon (Go)          │     │  Mobile App (React Native)    │
│                             │◄────│                               │
│  • REST API (:9171)         │     │  • Dashboard / Feed / History │
│  • WebSocket (terminal)     │────►│  • QR Code connect            │
│  • Unix Socket IPC          │     │  • Push notifications         │
│                             │     │                               │
│  • tmux session management  │     │  Expo SDK 56                  │
│  • cmux surface aggregation │     │  React Native 0.85            │
│  • Agent log parsing        │     │                               │
└─────────────────────────────┘     └──────────────────────────────┘
```

## 1. Companion Daemon (Go)

### 1.1 Package Structure

```
companion-daemon/
├── cmd/devremote/          Entry point (main.go, app.go)
│   ├── main.go             Flag parsing, CLI subcommands
│   ├── app.go              Composition root (App, NewApp, Run, Shutdown)
│   ├── client.go           CLI client (pokit run, link, unlink)
│   └── hook.go             Shell hook script generator
├── internal/
│   ├── mux/                Multi-adapter session multiplexer
│   │   ├── adapter.go      Adapter/Session interfaces + capability contracts
│   │   ├── registry.go     Session registry with singleflight caching
│   │   ├── session.go      Native PTY session + test helpers
│   │   ├── cmux_adapter.go cmux backend (surface discovery, tree parsing)
│   │   └── tmux_adapter.go tmux backend (tmux ls, session management)
│   ├── term/               HTTP handlers + domain logic
│   │   ├── runtime.go      Handlers struct, AuthMiddleware, context injection
│   │   ├── pty.go          WebSocket handler, session CRUD, xterm.js HTML
│   │   ├── verifier.go     Supabase JWT verifier (RS256/ES256 JWKS)
│   │   ├── telemetry.go    Session state machine (idle→thinking→working→waiting)
│   │   ├── telemetry_service.go  TelemetryService (owns loop + cache)
│   │   ├── eventstore.go   Agent event storage (in-memory, 500 cap)
│   │   ├── linkstore.go    Session link persistence (atomic temp-file + rename)
│   │   ├── linker.go       Link/unlink HTTP + IPC handlers
│   │   ├── commandbroker.go Pending command storage (one-shot Put/Take)
│   │   ├── notifier.go     Push notification interface
│   │   ├── ipc.go          Unix domain socket server
│   │   ├── parser.go       JSONL log parsing infrastructure
│   │   ├── reader.go       Log file tailing + cursor tracking
│   │   ├── resolver.go     Agent process detection (Claude/Codex/Gemini)
│   │   ├── claude_parser.go    Claude JSONL format parser
│   │   ├── codex_parser.go     Codex JSONL format parser
│   │   └── gemini_parser.go    Gemini JSONL format parser
│   ├── models/
│   │   ├── events.go       AgentEvent struct (no runtime state)
│   │   └── process.go      ProcessInfo struct
│   └── watcher/
│       └── tailer.go       JSONL directory watcher (fsnotify)
```

### 1.2 Key Technology Choices

| Component | Technology | Rationale |
|---|---|---|
| Language | Go 1.26 | Single binary, no runtime dependency, fast startup |
| HTTP server | `net/http` (private ServeMux) | Standard library, no framework overhead |
| WebSocket | `gorilla/websocket` | Mature, production-tested upgrade + binary frames |
| JWT verification | `golang-jwt/jwt/v5` | RS256/ES256 via Supabase JWKS, HS256 dev fallback |
| PTY management | `creack/pty` | Cross-platform pseudo-terminal, resize support |
| File watching | `fsnotify` | Cross-platform inotify/FSEvents for JSONL tailing |
| Singleflight | `golang.org/x/sync/singleflight` | Deduplicate concurrent adapter refreshes |
| Terminal parsing | `x/term`, `charmbracelet/x/vt` | Raw terminal mode, VT escape sequence handling |
| QR generation | `qrterminal` | Terminal QR code for pairing |
| IPC | Unix domain socket (`net.Listen("unix")`) | Local CLI↔daemon communication, mode 0600 |
| Process management | `os/exec` | tmux attach, cmux subprocess, cloudflared tunnel |

### 1.3 Core Design Patterns

**Composition Root** (`app.go`): `App` struct owns every runtime dependency. `NewApp(cfg)` creates and wires everything explicitly — no package globals, no `init()` registration, no singleton patterns. Tests use `NewAppWithDeps(cfg, Dependencies{...})` to inject fakes.

**Dependency Injection via Interfaces**: 5 injectable interfaces allow full test isolation:

```go
type Dependencies struct {
    Verifier     term.TokenVerifier  // JWT auth
    Events       term.EventStore     // agent events
    Links        term.LinkStore      // session links
    Cmds         term.CommandBroker  // pending commands
    StartWatcher func() (watcherResource, error)
    StartIPC     func(path, reg, events, telemetry) (ipcResource, error)
    StartTunnel  func() tunnelResource
}
```

**Shutdown Order**: All resources stopped in deterministic order with deadline:
HTTP → Telemetry → Watcher → Tunnel → IPC socket.

**Multi-Adapter Session Model**: `Registry` aggregates sessions from multiple backends (tmux, cmux) with canonical IDs (`tmux:<name>`, `cmux:surface:<id>`), singleflight-cached snapshots, and stale-cache tolerance.

### 1.4 REST API

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/sessions` | List all sessions with telemetry state |
| POST | `/api/sessions` | Create new tmux session |
| DELETE | `/api/sessions?id=...` | Terminate session |
| GET | `/api/sessions?history=...` | Session history / terminal output |
| GET/POST/DELETE | `/api/v2/links` | Session link CRUD |
| WS | `/term/ws?session=...` | Live terminal WebSocket |
| GET | `/term/?session=...` | xterm.js web terminal |
| GET | `/debug/cmd?session=...` | Pending command polling |
| POST | `/debug/cmd?session=...` | Send command (approval Y/N) |
| POST | `/push/register?token=...` | Expo push token registration |

## 2. Mobile App (React Native / Expo)

### 2.1 Technology Stack

| Layer | Technology |
|---|---|
| Framework | React Native 0.85 + Expo SDK 56 |
| Language | TypeScript 5 |
| Navigation | `@react-navigation/native` + bottom tabs + native stack |
| Auth | `@supabase/supabase-js` (email/password + JWT) |
| Camera | `expo-camera` (QR code scanning) |
| Notifications | `expo-notifications` (Expo Push) |
| Storage | `@react-native-async-storage/async-storage` |
| WebView | `react-native-webview` (terminal rendering) |
| Clipboard | `expo-clipboard` |

### 2.2 Component Tree

```
App.tsx
├─ ConnectionProvider              ← baseURL, isConnected, connect/disconnect
│  └─ AppContent
│     ├─ AuthScreen                ← Supabase email/password login
│     ├─ ConnectScreen             ← QR camera scan + manual URL input
│     └─ RootTabs (token)
│        ├─ DashboardScreen        ← Agent cards grid, approval cards, CRUD
│        │  ├─ AgentCard           ← Session card (state animation, runner icon)
│        │  ├─ AgentProfileModal   ← Create/edit/delete agent
│        │  └─ ApprovalCard        ← Y/N approval buttons
│        ├─ FeedScreen             ← WebView terminal + event bubbles
│        │  └─ EventBubble         ← AI event timeline
│        ├─ GlobalFeedScreen       ← Cross-session event feed
│        └─ SnippetsScreen         ← Copy-paste snippets
```

### 2.3 API Client (`src/lib/client.ts`)

Centralized HTTP client replacing scattered `fetch()` calls:

```typescript
listSessions(token): Promise<any[]>
createOrUpdateSession(id, runner, color, token)
deleteSession(id, token)
getSessionHistory(sessionID, token)
sendDebugCommand(sessionID, command, token)
registerPushToken(token, pushToken)
terminalURL(sessionID, token): string
terminalWebSocketURL(sessionID, token): string
```

All requests go through `checkedFetch()` which enforces `res.ok` checking.

### 2.4 Connection State (`src/lib/connection.tsx`)

Single `ConnectionProvider` React context owns all connection state:

```typescript
{ baseURL, isConnected, loading, connect(url), disconnect() }
```

`AsyncStorage.getItem('BASE_URL')` is called exactly once — in the provider's mount effect. No other component touches AsyncStorage for connection state.

## 3. Communication Flow

```
Mobile App                          macOS Daemon
─────────                          ────────────
                                   
QR Scan / Manual URL               
  │                                
  ├─ connect(url)                  
  │  └─ AsyncStorage.setItem       
  │                                
  ├─ GET /api/sessions ──────────► Registry.Sessions(ctx)
  │  ◄───────────────────────────  [cmux:surface:1, tmux:ai, ...]
  │                                
  ├─ WS /term/ws?session=... ────► HandleWS
  │  ◄══ binary frames ═════════  stream.Read() loop
  │  ══ text input ═════════════► stream.Write()
  │                                
  ├─ POST /debug/cmd ────────────► CommandBroker.Put()
  │  (Y/N approval)               
  │                                
  ├─ GET /push/register ─────────► pushNotifier.SetToken()
  │                                
  │                    telemetry loop (every 2s)
  │                    ├─ Registry.Sessions()
  │                    ├─ ProcessSnapshot() (batch)
  │                    ├─ ReadNewEvents() (JSONL tail)
  │                    ├─ evaluateState()
  │                    └─ Notifier.ApprovalRequired()
  │                         └─ POST expo push ◄──────────────
```

## 4. Authentication

```
Mobile App                          macOS Daemon
─────────                          ────────────
                                   
Supabase Auth                       
  ├─ email/password login           
  ├─ session.access_token (JWT)     
  │                                 
  ├─ GET /api/sessions ──────────► AuthMiddleware
  │  Authorization: Bearer <JWT>    │
  │                                 ├─ ExtractToken()
  │                                 ├─ Verifier.Verify(ctx, token)
  │                                 │  ├─ Insecure mode: accept empty
  │                                 │  ├─ ParseUnverified (dev mode)
  │                                 │  └─ Production:
  │                                 │     ├─ JWKS fetch (RS256/ES256)
  │                                 │     ├─ Issuer check
  │                                 │     ├─ Audience check
  │                                 │     └─ Owner UUID check
  │                                 └─ 401 if rejected
```

## 5. Refactoring Summary (Phase 1-7)

11 mutable package-level globals removed, all replaced with instance-owned state:

| Before (Global) | After (Instance) |
|---|---|
| `mux.Default` registry | `App.registry *mux.Registry` |
| `term.OwnerUUID` etc. | `SupabaseVerifier.config AuthConfig` |
| `term.jwksCache` + mutex | `SupabaseVerifier.keys` + `.mu` |
| `models.eventsCache` | `EventStore` interface |
| `term.sessionLinks` | `LinkStore` interface |
| `term.pendingCmds` | `CommandBroker` interface |
| `term.telemetryCache` | `TelemetryService.sessions` |
| `term.OnApproval` callback | `Notifier` interface |
| `http.DefaultServeMux` | `http.NewServeMux()` |
| `config.BASE_URL` (mobile) | `ConnectionProvider` context |
