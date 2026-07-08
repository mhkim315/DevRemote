# E8f2-pre — Stream / PTY Ownership Audit

Date: 2026-07-09

## 1. Current HandleWS ownership

### stream.Read() location
`internal/term/pty.go` — inside `HandleWS`, per-WebSocket goroutine.
Each WebSocket connection opens its own stream and runs its own read loop.

### Where ActivityBuffer.Append() occurs
Inside the stream.Read() goroutine in HandleWS. Per-WebSocket-read append.

### Current replay path
`ReadScreen()` → `\033[2J\033[H` + screen content → sent as first WebSocket message.
This IS captured by ActivityBuffer (contains printable text after ANSI codes).
`skipCapture` flag suppresses initial burst capture (added in E8h).

### Current history/snapshot path
`HandleSessionsV2 ?history=` → `ReadHistory()` or `ReadScreen()` — one-shot, not streaming.
Not captured by ActivityBuffer.

## 2. Adapter capability matrix

| Adapter | OpenStream | InputWriter | stream-only fallback | 2nd OpenStream behavior | safest recorder point |
|---------|-----------|-------------|---------------------|------------------------|----------------------|
| localpty | SpawnPTY (new child per call) | WriteInput ✅ | N/A (has InputWriter) | Creates new PTY child — duplicate reads possible | OpenStream ONCE, then guard |
| tmux | SpawnPTY (new child per call) | ❌ none | **stream.Write(msg) REQUIRED** | Creates new tmux attach child — duplicate reads + terminal confusion | OpenStream ONCE, MUST preserve stream.Write fallback |
| cmux | CmuxStream (new stream per call) | WriteInput ✅ | N/A (has InputWriter) | Creates new CmuxStream — duplicate reads possible | OpenStream ONCE, then guard |

**Evidence from source:**
- `localpty_adapter.go:128`: `func (s *localptySession) WriteInput(...)` — InputWriter implemented
- `cmux_adapter.go:401`: `func (s *CmuxSession) WriteInput(...)` — InputWriter implemented
- `cmux_adapter.go:348`: `CmuxStream.Write not implemented, use InputWriter capability directly` — stream-only fallback NOT available for cmux
- `tmux_adapter.go:43`: `func (s *tmuxSession) OpenStream(...)` — no WriteInput, relies on HandleWS `stream.Write(msg)` fallback

**Critical: tmux has NO InputWriter. The `stream.Write(msg)` fallback in HandleWS is essential for tmux sessions. Any recorder must preserve this path or provide equivalent.**

## 3. Replay / snapshot / history append rule (STRICT)

```
Live PTY bytes from stream.Read()
→ ActivityBuffer.Append()  ← ONLY THIS PATH

ReadScreen / ReadHistory / bootstrap / replay / reconnect
→ display only
→ NEVER append to ActivityBuffer
```

Current code enforces this via `skipCapture` flag (suppresses initial screen snapshot).
Recorder must inherit this rule: skip the first ReadScreen burst.

## 4. Ownership proposal

```
Session (one)
  ↓
SessionRecorder (one per session, daemon-lifetime)
  ↓
ActivityBuffer (append-only, monotonic seq)
  ↓
Broadcast channel → N WebSocket subscribers
```

## 5. Recorder start condition policy

**Problem**: If recorder starts from HandleWS, no-WebSocket capture fails.
**Solution**: Recorder starts at session creation/link time, not WebSocket time.

| Option | Feasibility | E8f2 target? |
|--------|-----------|--------------|
| A: session create/link time | Requires session lifecycle hook. Best fit for product goal. | ✅ Recommended |
| B: daemon session discovery time | Adapters without per-session hooks can't support this. | Partial |
| C: first explicit attach action | User-initiated; not automatic. | Fallback |
| D: first WebSocket connection | Does NOT satisfy "capture without viewer" goal. | ❌ Rejected |

**Recommended**: Option A for localpty (has session lifecycle). Option B fallback for tmux/cmux (poll-based). Document which adapters support which.

## 6. Late subscriber policy

```
Late subscriber (WebSocket connects mid-session):
→ receives live PTY bytes from attachment point onward
→ does NOT receive ActivityBuffer replay over WebSocket
→ reads historical transcript through ?activity=<sessionId>
→ initial screen snapshot (ReadScreen) sent as first frame for visual bootstrap, NOT appended to ActivityBuffer
```

## 7. Session recreation policy

```
session deleted/ended:
→ recorder stops
→ subscribers closed
→ ActivityBuffer cleared for that sessionID

same sessionID recreated later:
→ new recorder starts
→ seq resets to 1
→ old data already cleared
```

## 8. Adapter restart / stream replacement policy

```
stream dies (EOF/error):
→ recorder logs error
→ recorder exits (does NOT auto-restart)
→ subscribers receive close signal
→ next WebSocket connect triggers new recorder via start condition

Recorder does NOT auto-reopen dead streams.
Stream replacement is a subscriber re-attach concern, not a recorder concern.
```

## 9. Error propagation policy

```
recorder read error:
→ recorder exits
→ subscriber channels close
→ WebSocket handler detects closed channel
→ WebSocket sends 1011 close frame
→ handler exits cleanly
```

## 10. Input ownership policy

```
terminal_output:
→ recorder-owned (single writer to ActivityBuffer)

terminal_input metadata:
→ WebSocket handler-owned (bytes only, no raw text)

stream-only input fallback (tmux):
→ recorder MUST preserve stream.Write(msg) path
→ OR recorder must own the stream.Write interface for input
```

## 11. E8f2 test plan

| # | Test | Proves |
|---|------|--------|
| 1 | recorder captures output without WebSocket subscriber | no-viewer capture |
| 2 | two subscribers receive same live output | multi-viewer no-duplicate |
| 3 | ActivityBuffer has exactly N events for M bytes of PTY output (M ≥ N) | no double append |
| 4 | late subscriber receives only live bytes from attach point | late-subscriber policy |
| 5 | ReadScreen/bootstrap does not create ActivityEvent seq | replay-isolation rule |
| 6 | stream read error closes subscriber channels + WebSocket 1011 | error propagation |
| 7 | session delete stops recorder + clears subscribers | lifecycle cleanup |
| 8 | same-ID recreation starts new recorder with reset seq | recreation policy |
| 9 | terminal_input remains metadata-only | input safety |
| 10 | tmux stream-only input fallback preserved | adapter compatibility |

## 12. Implementation recommendation

**First slice**: localpty + tmux (covers both InputWriter and stream-only paths)
**Files**: recorder.go (new), pty.go (HandleWS subscriber model), activity.go (no changes)
**Fallback for cmux**: identical to localpty path (has InputWriter)

**Risks**:
- tmux OpenStream creates new PTY child per call — must guard against multi-open
- stream.Read() blocking behavior varies by adapter — need context-based cancellation
- ActivityBuffer memory growth for long-running sessions — capacity already enforced (2000 events)
