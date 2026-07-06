# Terminal Adapter Behavior Matrix — Phase 0 Baseline

- 기준 커밋: `dc026d5`
- **Code**: interface 구현 존재 여부
- **Effect**: 실제 효과
- **Verified**: 실기기 확인 여부
- **Status**: Contract (보장) / Accidental (변경 가능)

## 1. Session Capability Matrix

각 adapter의 session struct가 직접 구현한 capability. `OpenStream`이 반환하는
`TerminalStream`의 write/resize는 별도 섹션에 기록한다.

### tmux session

| Capability | Code | Effect | Verified | Notes |
|---|---|---|---|---|
| `OpenStream` | YES | PTY attach | PASS | |
| `ScreenReader` | YES | `capture-pane -p` | PASS | |
| `HistoryReader` | YES | `capture-pane -pS -N` | PASS | |
| `ProcessProvider` | YES | `display-message -p -t <target> #{pane_start_time},#{pane_pid},#{pane_current_path}` | PASS | |
| `ProcessSnapshotProvider` | NO | — | N/A | |
| `SessionCreator` | YES | `tmux new-session -d -s <name>` | PASS | |
| `SessionTerminator` | YES | `tmux kill-session -t <name>` | PASS | |
| `InputWriter` | NO | — | — | input via `TerminalStream.Write` |
| `Resizer` | NO | — | — | resize via `TerminalStream.Resize` |
| `KeyWriter` | NO | — | — | |

### cmux session

| Capability | Code | Effect | Verified | Notes |
|---|---|---|---|---|
| `OpenStream` | YES | surface pipe (500ms poll, 1.5s timeout) | PASS | |
| `ScreenReader` | YES | `read-screen --surface <id>` (plain text, no ANSI) | PASS | |
| `HistoryReader` | YES | `read-screen --surface <id> --scrollback --lines N` | PASS | |
| `ProcessProvider` | YES | `top --all --processes --format tsv` → PID resolve | PASS | |
| `ProcessSnapshotProvider` | YES | `top --all --processes --format tsv` (all surfaces) | NOT TESTED | |
| `SessionCreator` | YES | `new-surface --type terminal --workspace <ws>` | NOT TESTED | |
| `SessionTerminator` | YES | `close-surface --surface <id>` | NOT TESTED | |
| `InputWriter` | YES | `send --surface <id> <text>` | PASS | |
| `KeyWriter` | YES | `send-key --surface <id> <key>` | PASS | Esc, arrows |

### cmux Ctrl+C

| Path | Code | Effect | Verified |
|---|---|---|---|
| `WriteInput("\x03")` → `WriteKey("ctrl-c")` | YES | NOT SUPPORTED | FAIL (실기기 미작동) |

## 2. TerminalStream Capability (returned by OpenStream)

| Capability | tmux (PTY) | cmux (pipe) |
|---|---|---|
| `Read` | YES | YES |
| `Write` | YES | NO (cmux uses session `WriteInput`) |
| `Close` | YES | YES |
| `Resize` | YES (PTY resize) | YES (**NO-OP**, success return only) |

## 3. Discovery

| Item | tmux | cmux |
|---|---|---|
| Command | `tmux ls` | `cmux tree --all` |
| Timeout | N/A | 3s (`exec.CommandContext`) |
| Ordering | canonical ID lexical ascending (`adapter:local`) | same |
| Stale on failure | preserved | preserved |

## 4. Canonical ID Golden Examples

```text
tmux:ai               → adapter=tmux, localID=ai
tmux:aider            → adapter=tmux, localID=aider
tmux:tmux:aider       → adapter=tmux, localID=tmux:aider (local has ':')
tmux:한글             → adapter=tmux, localID=한글 (Unicode)
cmux:surface:1        → adapter=cmux, localID=surface:1
cmux:42               → Parse→(cmux,42), MigrateLegacyID→cmux:surface:42
```

## 5. API Response Golden Fixtures (dc026d5)

- GET `/api/sessions`: 200, JSON array of `SessionTelemetry` (keys: `id`,`state`,`load`,`runner`,`runnerColor`,`adapter`,`events`; omitempty: `stale`,`lastSuccessAt`,`lastError`)
- POST `/api/sessions`: `{"id":"tmux:test",...}` → `{"status":"ok","id":"test"}` (local ID — accidental; Phase 1 canonicalizes)
- DELETE `/api/sessions?id=tmux:test`: `{"status":"ok"}`
- GET `/api/sessions?history=...`: events JSON or 404; screen fallback returns `[{"id","session","type","summary","detail","timestamp","agent","toolCallId"}]`

## 6. Contract vs Accidental

| Behavior | Status |
|---|---|
| Canonical ID `<adapter>:<local-id>` | Contract |
| Legacy migration `cmux:N`→`cmux:surface:N` | Contract |
| Stale snapshot on refresh failure | Contract |
| Session not found → 404 | Contract |
| Stream error → WS 1011 | Contract |
| Ordering: canonical ID lexical ascending | Contract (Registry sort) |
| POST returns local ID | **Accidental** — Phase 1 |
| cmux resize no-op | **Accidental** — Phase 2 |
| cmux command names (`tree`,`read-screen`,`new-surface`) | Accidental |
| cmux timeouts (3s discovery, 1.5s stream) | Accidental |
| mobile `replace(/^(tmux\|cmux):/, '')` | Accidental — Phase 2 |
