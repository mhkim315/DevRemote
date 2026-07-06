# Terminal Adapter Behavior Matrix — Phase 0 Baseline

- 기준 커밋: `dc026d5`
- **Code**: interface 구현 존재 여부
- **Effect**: 실제 효과 (실제 CLI 실행 / no-op)
- **Verified**: 실기기 확인 여부
- **Status**: Contract (보장) / Accidental (변경 가능)

## 1. Adapter Capabilities (receiver: adapter struct, not session)

| Capability | tmux | cmux | Notes |
|---|---|---|---|
| `ListSessions` | YES | YES | 필수 계약. tmux: `list-sessions -F "#{session_id}::POKIT::#{session_name}"` 고정 delimiter. cmux: `tree --all` (3s timeout) |
| `SessionCreator` | YES | YES | tmux: `new-session -d -s <name>`. cmux: `new-surface --type terminal --workspace <ws>` |
| `SessionTerminator` | YES | YES | tmux: `kill-session -t <name>`. cmux: `close-surface --surface <id>` |
| `ProcessSnapshotProvider` | NO | YES | cmux: `top --all --processes --format tsv` (all surfaces batch) |

## 2. Session Capabilities (receiver: session struct)

| Capability | tmux | cmux | Notes |
|---|---|---|---|
| `OpenStream` | YES | YES | tmux: PTY attach. cmux: surface pipe (500ms poll, 1.5s timeout) |
| `ScreenReader` | YES | YES | tmux: `capture-pane -p -t $session_id`. cmux: `read-screen --surface <id>` (plain text, no ANSI) |
| `HistoryReader` | YES | YES | tmux: `capture-pane -pS -N -t $session_id`. cmux: `read-screen --surface <id> --scrollback --lines N` |
| `ProcessProvider` | YES | YES | tmux: `display-message -p -t <target> #{pane_start_time},#{pane_pid},#{pane_current_path}`. cmux: `top --all --processes --format tsv` → PID resolve |
| `InputWriter` | NO | YES | tmux input via `TerminalStream.Write`. cmux: `send --surface <id> <text>` |
| `KeyWriter` | NO | YES | cmux: `send-key --surface <id> <key>` |
| Resize | NO (on stream) | NO (on session) | tmux: stream.Resize=YES. cmux: stream.Resize=NO-OP |

## 3. TerminalStream Capabilities (returned by OpenStream)

| Capability | tmux (PTY) | cmux (pipe) |
|---|---|---|
| `Read` | YES | YES |
| `Write` | YES | NO |
| `Close` | YES | YES |
| `Resize` | YES (PTY resize) | YES (NO-OP, success only) |

## 4. Functional Behavior Matrix — Common Dimensions

tmux와 cmux에 동일 항목 적용. 미지원은 "NOT SUPPORTED"로 기록.

| Dimension | tmux | cmux | Status |
|---|---|---|---|
| **Discovery** | | | |
| Command | `list-sessions -F "#{session_id}::POKIT::#{session_name}"` | `tree --all` | Accidental |
| Internal target vs title | `$session_id` target / `#{session_name}` title | surface ID / `[state] "title"` | Accidental |
| New session appears | Next refresh (≤10s TTL) | Next refresh (≤10s TTL) | Contract |
| Deleted session removed | Next refresh | Next refresh | Contract |
| Healthy ordering | canonical ID lexical ascending | same | Contract |
| Stale on adapter failure | preserved | preserved | Contract |
| **Initial Screen** | | | |
| WS connect first frame | `capture-pane -p` snapshot + PTY | pipe first read | Accidental |
| Adapter-name branch in handler | YES (`pty.go` tmux check) | N/A | Accidental |
| **Live Output** | | | |
| Stream type | Binary PTY | 500ms poll (1.5s timeout) | Accidental |
| ANSI color/style | YES (raw PTY) | NO (plain text) | Accidental |
| Latency | Real-time | ≤500ms | Accidental |
| **Text Input** | | | |
| Printable ASCII | PASS (stream.Write) | PASS (`send --surface`) | Verified |
| Enter | CR/LF/CRLF → stream.Write | LF → `send-key enter` | Verified |
| Ctrl+C | SIGINT (stream.Write 0x03) | `WriteKey("ctrl-c")` — NOT SUPPORTED | Verified |
| Arrow/Up/Down/Left/Right | escape sequences (stream.Write) | `send-key` keyCode | Verified |
| Escape | escape sequence (stream.Write) | `send-key escape` | Verified |
| Unicode/CJK | PASS (stream.Write) | Limited (`send` text) | Verified (tmux only) |
| **Session Lifecycle** | | | |
| POST create result | local ID returned (accidental) | code exists, not tested | Accidental |
| DELETE terminate | `kill-session` | `close-surface` | Accidental |
| **Disconnect / Reconnect** | | | |
| Client disconnect | stream.Close, WS 1011 close | stream.Close, WS 1011 close | Contract |
| Daemon restart | sessions re-discovered | sessions re-discovered | Contract |
| New WS = new stream | YES | YES | Contract |
| **Error Handling** | | | |
| Session not found | HTTP 404 | HTTP 404 | Contract |
| Stream read error | WS 1011 close | WS 1011 close | Contract |
| Transient adapter failure | stale snapshot preserved | stale snapshot preserved | Contract |

## 5. Canonical ID Golden

```text
tmux:ai               → (tmux, ai)
tmux:tmux:aider        → (tmux, tmux:aider) local has ':'
tmux:한글             → (tmux, 한글) Unicode
cmux:surface:1        → (cmux, surface:1)
cmux:42               → Parse→(cmux,42), Migrate→cmux:surface:42
```

## 6. API Golden (dc026d5)

- GET `/api/sessions`: 200, `[{id,state,load,runner,runnerColor,adapter,events,stale?,lastSuccessAt?,lastError?}]`
- POST: `{"id":"tmux:test",...}` → `{"status":"ok","id":"test"}` (local ID — accidental; Phase 1 canonicalizes)
- DELETE: `{"status":"ok"}`
- GET history: events JSON or 404; screen fallback returns `AgentEvent[]`

## 7. Contract vs Accidental

| Behavior | Status |
|---|---|
| Canonical ID `<adapter>:<local-id>` | Contract |
| Legacy `cmux:N`→`cmux:surface:N` | Contract |
| ID lexical ascending ordering | Contract |
| Stale snapshot on refresh failure | Contract |
| Session not found → 404 | Contract |
| Stream error → WS 1011 | Contract |
| POST returns local ID | **Accidental** — Phase 1 |
| tmux "tmux" fallback in create | **Accidental** — Phase 1 |
| cmux resize no-op | **Accidental** — Phase 2 |
| All backend command names/flags | Accidental |
| cmux timeouts (3s/1.5s) | Accidental |
| mobile `replace(/^(tmux\|cmux):/, '')` | Accidental — Phase 2 |
