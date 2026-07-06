# Terminal Adapter Behavior Matrix — Phase 0 Baseline

- 기준 커밋: `dc026d5`
- 실기기 검증: macOS daemon + Android mobile (2026-07-06)
- **Code**: interface 구현 존재 여부
- **Effect**: 실제 효과 (no-op / 동작함 / 미구현)
- **Verified**: 실기기/테스트 확인
- **Status**: Contract (보장) / Accidental (현재 동작일 뿐, 변경 가능)

## 1. Adapter Capability Matrix

### tmux

| Capability | Code | Effect | Verified | Status |
|---|---|---|---|---|
| Discovery (`ListSessions`) | YES | `tmux ls` | PASS | Accidental |
| LiveStreamer (`OpenStream`) | YES | PTY attach | PASS | Accidental |
| InputWriter (`WriteInput`) | YES | PTY write | PASS | Accidental |
| ScreenReader (`ReadScreen`) | YES | `capture-pane -p` | PASS | Accidental |
| HistoryReader (`ReadHistory`) | YES | `capture-pane -pS -N` | PASS | Accidental |
| Resizer (`Resize`) | YES | PTY resize | PASS | Accidental |
| ProcessProvider (`ProcessInfo`) | YES | `list-panes -F #{pane_pid}` | PASS | Accidental |
| ProcessSnapshotProvider | NO | — | N/A | — |
| SessionCreator | YES | `tmux new-session -d -s <name>` | PASS | Accidental |
| SessionTerminator | YES | `tmux kill-session -t <name>` | PASS | Accidental |
| KeyWriter (`WriteKey`) | NO | — | N/A | — |
| Ctrl+C | N/A | SIGINT via PTY (raw input) | PASS | Accidental |

### cmux

| Capability | Code | Effect | Verified | Status |
|---|---|---|---|---|
| Discovery (`ListSessions`) | YES | `cmux tree --all` (3s timeout) | PASS | Accidental |
| LiveStreamer (`OpenStream`) | YES | surface pipe read (500ms poll, 1.5s stream timeout) | PASS | Accidental |
| InputWriter (`WriteInput`) | YES | surface keystroke inject | PASS | Accidental |
| ScreenReader (`ReadScreen`) | YES | `cmux read-screen --name ...` (plain text, no ANSI) | PASS | Accidental |
| HistoryReader (`ReadHistory`) | YES | `cmux read-screen --name ... --lines N` | PASS | Accidental |
| Resizer (`Resize`) | YES | **NO-OP** (function exists, no actual resize) | NOT TESTED | Accidental |
| ProcessProvider (`ProcessInfo`) | YES | `cmux top --all --processes --format tsv` → PID | PASS | Accidental |
| ProcessSnapshotProvider | YES | `cmux top --all --processes --format tsv` (all surfaces) | NOT TESTED | Accidental |
| SessionCreator | YES | `cmux surface create --name` | NOT TESTED | Accidental |
| SessionTerminator | YES | `cmux surface delete --name` | NOT TESTED | Accidental |
| KeyWriter (`WriteKey`) | YES | keyCode injection (Esc, arrows) | PASS | Accidental |
| Ctrl+C | NO | — | NOT SUPPORTED | — |

## 2. Behavior Matrix — Functional Dimensions

| Dimension | tmux | cmux | Status |
|---|---|---|---|
| **Discovery** | | | |
| Command | `tmux ls` | `cmux tree --all` | Accidental (adapter internal) |
| Timeout | N/A | 3s (`exec.CommandContext`) | Accidental |
| Ordering | Alphabetical | Numeric | Accidental (Registry ordering) |
| **Live Output** | | | |
| Streaming | Binary PTY pipe | Polling (500ms) | Accidental |
| Stream timeout | N/A | 1.5s per `read-screen` | Accidental |
| ANSI | PASS (raw PTY) | NOT SUPPORTED (plain text) | Accidental |
| **Text Input** | | | |
| ASCII | PASS | PASS | Verified |
| Enter | Direct PTY write | LF → `send-key enter` | Verified |
| Ctrl+C | SIGINT via PTY | NOT SUPPORTED | Verified |
| Arrow/Esc | PASS (escape seq) | PASS (keyCode) | Verified |
| Unicode/CJK | PASS | Limited | Verified (tmux) |
| **Lifecycle** | | | |
| Create API | local ID returned | code exists | Accidental: POST returns local `test`, not canonical `tmux:test` |
| Delete API | PASS | code exists | |
| **Screen/History** | | | |
| Screen command | `capture-pane -p` | `read-screen --name` | Accidental |
| History command | `capture-pane -pS -N` | `read-screen --name --lines N` | Accidental |
| **Process** | | | |
| PID command | `list-panes -F #{pane_pid}` | `top --all --processes --format tsv` | Accidental |
| **Reconnect** | | | |
| Daemon restart | re-discovered | re-discovered | Contract (Registry) |
| Client disconnect | stream closed, new WS | stream closed, new WS | Contract (HandleWS) |
| **Errors** | | | |
| Session not found | HTTP 404 | HTTP 404 | Contract |
| Stream read error | WS 1011 | WS 1011 | Contract |
| Stale snapshot | preserved | preserved | Contract (Registry) |

## 3. Canonical ID Golden Examples

```text
# tmux
canonical: tmux:ai              → adapter=tmux, localID=ai
canonical: tmux:aider           → adapter=tmux, localID=aider
canonical: tmux:tmux:aider      → adapter=tmux, localID=tmux:aider (local has ':')
canonical: tmux:한글            → adapter=tmux, localID=한글 (Unicode)

# cmux (canonical after migration)
canonical: cmux:surface:1       → adapter=cmux, localID=surface:1

# Legacy cmux → ParseSessionID splits at first ':', MigrateLegacyID canonicalizes
legacy:   cmux:42               → ParseSessionID → adapter=cmux, localID=42
                                 → MigrateLegacyID → cmux:surface:42
```

## 4. API Response Golden Fixtures (dc026d5 behavior)

### GET /api/sessions
- Status: 200, Content-Type: application/json
- Response is `[]SessionTelemetry` with fields: `id`, `state`, `load`, `runner`, `runnerColor`, `adapter`, `events`, `stale` (omitempty), `lastSuccessAt` (omitempty), `lastError` (omitempty)

### POST /api/sessions (create)
- Request: `{"id":"tmux:test","runner":"claude","runnerColor":"#58a6ff"}`
- Response: `{"status":"ok","id":"test"}` — **returns local ID** (accidental). Phase 1 will canonicalize.

### DELETE /api/sessions?id=tmux:test
- Response: `{"status":"ok"}`

### GET /api/sessions?history=tmux:golden (no events)
- Status: 404

## 5. Contract vs Accidental

| Behavior | Status |
|---|---|
| Canonical ID `<adapter>:<local-id>` format | Contract |
| Legacy `cmux:N` → `cmux:surface:N` migration | Contract |
| Stale snapshot on refresh failure | Contract |
| Session not found → HTTP 404 | Contract |
| Stream error → WS 1011 | Contract |
| POST returns local ID (not canonical) | **Accidental** — Phase 1 changes |
| cmux resize no-op | **Accidental** — Phase 2 may implement |
| tmux "tmux" fallback in create handler | **Accidental** — Phase 1 removes |
| cmux command names (`tree`, `read-screen`) | Accidental |
| cmux timeouts (3s discovery, 1.5s stream) | Accidental |
| mobile `replace(/^(tmux\|cmux):/, '')` | Accidental — Phase 2 removes |
