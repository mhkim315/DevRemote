# Terminal Adapter Behavior Matrix — Phase 0 Baseline

- 기준 커밋: `dc026d5`
- 실기기 검증: macOS daemon + Android mobile
- 목적: tmux/cmux의 현재 동작을 객관적으로 기록하고, 이후 Phase에서 회귀 판단 기준으로 사용

## 1. Adapter Capability Matrix

| Capability | tmux | cmux | Notes |
|---|---|---|---|
| Discovery (`ListSessions`) | PASS | PASS | cmux uses `cmux top`, tmux uses `tmux ls` |
| Session identity (canonical ID) | `tmux:<name>` | `cmux:surface:<id>` | Legacy `cmux:<N>` migrated to `cmux:surface:<N>` |
| Local ID contains `:` | PASS | N/A | `tmux:aider`, `tmux:cmux-38` 등 콜론 포함 이름 정상 |
| Display title | session name | `surface:N "title"` | |
| Live stream (OpenStream) | PTY attach | surface pipe read | |
| Live input (WriteInput) | PTY write | surface keystroke inject | |
| Enter (CR/LF/CRLF) | PASS | PASS | LF → `send-key enter` 변환 |
| Ctrl+C | PASS | NOT SUPPORTED | tmux: SIGINT via PTY |
| Resize | PASS | PASS | |
| Screen snapshot (ReadScreen) | `capture-pane -p` | `surface get --name ...` | Plain text only; cmux no ANSI |
| History (ReadHistory) | `capture-pane -pS -<lines>` | `surface get --lines <n>` | |
| Process info (ProcessInfo) | `tmux list-panes -F #{pane_pid}` | `surface info --name` → PID lookup | |
| Batch process (ProcessSnapshotProvider) | PASS | FAIL (list-based, not batch) | tmux: one `tmux` call; cmux: per-session |
| Create session (SessionCreator) | `tmux new-session -d -s <name>` | NOT SUPPORTED | |
| Terminate session (SessionTerminator) | `tmux kill-session -t <name>` | NOT SUPPORTED | |
| WriteKey (special keys) | NOT SUPPORTED | PASS | cmux: Esc, arrows via keyCode injection |
| Reconnect (stream recovery) | PASS | PASS | New PTY attach / new pipe |
| Stale snapshot on adapter failure | PASS | PASS | Registry preserves last successful snapshot |

## 2. Behavior Matrix — Functional Dimensions

| Dimension | tmux | cmux |
|---|---|---|
| **Discovery** | | |
| Session list stability | Stable (alphabetical) | Stable (numeric order) |
| New session appears | Next refresh (≤10s TTL) | Next refresh (≤10s TTL) |
| Deleted session removed | Next refresh | Next refresh |
| Transient failure | Stale cache preserved | Stale cache preserved |
| **Initial Screen** | | |
| First frame after connect | PTY attach → immediate output | Pipe first read |
| tmux-specific snapshot | `capture-pane -p` sent as initial frame | N/A |
| **Live Output** | | |
| Streaming mode | Binary PTY pipe | Polling (500ms) |
| ANSI color/style | PASS (raw PTY) | NOT SUPPORTED (plain text snapshot) |
| Output latency | Real-time | Up to 500ms |
| **Text Input** | | |
| Printable ASCII | PASS | PASS |
| Unicode / CJK | PASS | Limited (surface keyCode) |
| Enter (CR/LF/CRLF) | Direct PTY write | `send-key enter` |
| Ctrl+C | SIGINT via PTY | NOT SUPPORTED |
| Arrow keys | PASS (escape sequences) | PASS (keyCode injection) |
| **Session Lifecycle** | | |
| Create from API | PASS | NOT SUPPORTED |
| Delete from API | PASS | NOT SUPPORTED |
| External create detection | Next refresh | Next refresh |
| External termination | Session removed on next list | Session removed on next list |
| **History** | | |
| Scrollback access | `capture-pane -pS -N` | `surface get --lines N` |
| History line limit | 10000 | configurable |
| **Process** | | |
| PID detection | `list-panes -F #{pane_pid}` | `surface info` → PID |
| Agent log resolution | PASS (Claude/Codex/Gemini) | PASS |
| **Reconnect** | | |
| Daemon restart | Sessions re-discovered | Sessions re-discovered |
| Client disconnect | Stream closed, new WS = new stream | Stream closed, new WS = new stream |
| **Error Handling** | | |
| Adapter unavailable | Stale cache preserved | Stale cache preserved |
| Session not found | HTTP 404 | HTTP 404 |
| Stream read error | WS 1011 close | WS 1011 close |
| Command timeout | N/A | 5s per command |

## 3. Canonical ID Examples (Golden)

```text
# tmux
tmux:ai
tmux:aider
tmux:cmux-1
tmux:cmux-38
tmux:____

# cmux (canonical after migration)
cmux:surface:1
cmux:surface:2
cmux:surface:3

# Legacy cmux (migrated to canonical on first access)
cmux:42  →  cmux:surface:42
```

## 4. API Response Golden Fixture

### GET /api/sessions (excerpt)

```json
[
  {
    "id": "cmux:surface:1",
    "state": "idle",
    "load": 0,
    "runner": "agent",
    "runnerColor": "#58a6ff",
    "adapter": "cmux",
    "events": [],
    "lastSuccessAt": "2026-07-06T12:13:05.175722Z"
  },
  {
    "id": "tmux:ai",
    "state": "working",
    "load": 100,
    "runner": "claude",
    "runnerColor": "#58a6ff",
    "adapter": "tmux",
    "events": [
      {
        "id": "20260706120415.000001",
        "session": "tmux:ai",
        "type": "tool_use",
        "summary": "Write",
        "detail": "/path/to/file"
      }
    ],
    "lastSuccessAt": "2026-07-06T12:13:05.185496Z"
  }
]
```

### GET /api/sessions?history=cmux:surface:1 (excerpt)

```json
[
  {
    "id": "fallback-0",
    "session": "cmux:surface:1",
    "type": "message",
    "summary": "Terminal History",
    "detail": "actual terminal content...",
    "timestamp": "2026-07-06T12:00:00Z"
  }
]
```

### POST /api/sessions (create tmux)

```json
// Request
{"id": "tmux:test", "runner": "claude", "runnerColor": "#58a6ff"}

// Response
{"status": "ok", "id": "tmux:test"}
```

### DELETE /api/sessions?id=tmux:test

```json
{"status": "ok"}
```

## 5. "Accidentally Working" vs "Guaranteed Contract"

| Behavior | Status | Rationale |
|---|---|---|
| cmux session polling interval (500ms) | Accidental | tmux adapter has no equivalent; not in Adapter contract |
| tmux initial snapshot on WS connect | Accidental | Hardcoded in `pty.go` by adapter name check |
| create API defaults to "tmux" | Accidental | `HandleSessionCRUD` has `adapterName = "tmux"` fallback |
| mobile `replace(/^(tmux\|cmux):/, '')` | Accidental | Mobile knows backend names |
| canonical ID format `<adapter>:<local-id>` | Contract | Central to API stability |
| stale snapshot on refresh failure | Contract | Explicit in Registry |
| adapter error → last snapshot preserved | Contract | Explicit in Registry.Refresh() |
| session not found → HTTP 404 | Contract | Explicit in handler |
| stream error → WS 1011 | Contract | Explicit in HandleWS |
