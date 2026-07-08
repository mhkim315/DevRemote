# E8f2-pre — Stream / PTY Ownership Audit

Date: 2026-07-08

## 1. Current HandleWS ownership

### stream.Read() location

`internal/term/pty.go:218-239` — inside `HandleWS`, per-WebSocket goroutine:

```go
if stream != nil {
    go func() {
        buf := make([]byte, 1024)
        for {
            n, err := stream.Read(buf)  // OWNED by this goroutine
            ...
            outbound <- ...
        }
    }()
}
```

### Who owns PTY reading

**Each WebSocket connection owns its own PTY stream.** `OpenStream()` is called per-WebSocket in HandleWS line 124. Two simultaneous WebSocket connections → two independent `stream.Read()` goroutines → two independent read loops → each reads from the same PTY independently.

### Where ActivityBuffer.Append() occurs

`internal/term/pty.go:228-240` — inside the stream.Read() goroutine:

```go
if h.Activity != nil && n > 0 && !skipCapture {
    ...
    h.Activity.Append(ActivityEvent{...})
}
```

Append is per-WebSocket-read. Two WebSockets → two goroutines appending → potential duplicate events if both read the same PTY data.

### Current replay path

`internal/term/pty.go:204-216` — initial screen snapshot:

```go
if sr, ok := s.(mux.ScreenReader); ok {
    if initial, snapErr := sr.ReadScreen(r.Context()); snapErr == nil && len(initial) > 0 {
        payload := "\033[2J\033[H" + string(initial)
        outbound <- ...
    }
}
```

This sends the current screen state (`\033[2J\033[H` = clear+home) as the first message. This IS captured by ActivityBuffer (contains printable text after ANSI codes). The `skipCapture` flag was added to suppress this initial burst.

### Current history/snapshot path

`internal/term/telemetry.go:202-219` — `HandleSessionsV2` `?history=` fallback:

```go
if hr, ok := sess.(mux.HistoryReader); ok {
    out, err = hr.ReadHistory(r.Context(), 10000)
} else if sr, ok := sess.(mux.ScreenReader); ok {
    out, err = sr.ReadScreen(r.Context())
}
```

This is one-shot, not streaming. Not captured by ActivityBuffer.

## 2. Adapter capability matrix

| Adapter | StreamOpener | ScreenReader | HistoryReader | InputWriter | Single-reader? |
|---------|-------------|-------------|---------------|-------------|----------------|
| localpty | yes | yes | yes | yes | no — each OpenStream creates new PTY reader |
| tmux | yes | yes | yes | ? | no — each OpenStream connects to tmux pane |
| cmux | yes | yes | yes | ? | no — each OpenStream connects to cmux session |

**Critical finding**: No adapter guarantees single-reader semantics. `OpenStream()` can be called multiple times concurrently, creating independent readers that each consume the same PTY output. This means:

- Two WebSocket viewers → two readers → both appending to ActivityBuffer → **DUPLICATE events**
- The current ActivityBuffer is already vulnerable to multi-viewer duplication

## 3. Ownership proposal

### Target architecture

```
Session (one)
  ↓
SessionRecorder (one per session, daemon-lifetime)
  ↓
ActivityBuffer (append-only, monotonic seq)
  ↓
Broadcast channel → N WebSocket subscribers
```

### NOT the current architecture

```
N WebSocket handlers
  ↓
stream.Read() per handler
  ↓
ActivityBuffer (vulnerable to duplicate appends)
```

### Proposed insertion point

The recorder should be created when a session becomes "active" (first WebSocket connection or first API access). It should:

1. Call `OpenStream()` exactly ONCE per session
2. Read from the PTY in a single goroutine
3. Write to both the broadcast channel (for WebSocket subscribers) AND ActivityBuffer
4. Stop when the session is terminated

### WebSocket subscriber model

HandleWS should:
1. Receive from the recorder's broadcast channel instead of `OpenStream()`
2. NOT own a PTY read loop
3. NOT append to ActivityBuffer directly

## 4. Risk analysis

### Multiple viewers
**Risk**: Two WebSocket connections currently create two independent PTY readers → ActivityBuffer duplicate appends.
**Mitigation**: Session-scoped recorder with single reader.

### Race conditions
**Risk**: Multiple goroutines calling `ActivityBuffer.Append()` for the same session.
**Mitigation**: ActivityBuffer already uses `sync.Mutex`. Single reader eliminates the race at source.

### Replay semantics
**Risk**: Initial screen snapshot (`ReadScreen()` + `\033[2J\033[H`) captured as terminal_output.
**Mitigation**: `skipCapture` flag suppresses initial burst. Recorder should also skip the first snapshot.

### Subscriber backpressure
**Risk**: Slow WebSocket subscriber could block the broadcast channel.
**Mitigation**: Buffered channel (size 32 as currently used) + drop-on-full semantics.

### ActivityBuffer ownership
**Risk**: ActivityBuffer currently owned by Handlers struct (one per daemon). Multiple sessions share one buffer.
**Mitigation**: Acceptable for MVP. Session-scoped buffer is future optimization.

### Session cleanup
**Risk**: Recorder goroutine leak when session ends.
**Mitigation**: Context-based cancellation tied to session lifecycle.

### Adapter differences
**Risk**: localpty `OpenStream()` creates a fresh PTY reader each call. tmux/cmux `OpenStream()` connects to existing pane.
**Mitigation**: Recorder calls `OpenStream()` once. If the stream dies, recorder re-opens once (not per-subscriber).

## 5. Boundary definition

### IN SCOPE for E8f2-pre (this audit)
- ownership clarification ✅
- recorder insertion point identification ✅
- reader ownership documented ✅
- replay ownership documented ✅
- adapter matrix ✅

### IN SCOPE for E8f2 (recorder implementation)
- single session-owned PTY reader
- broadcast channel for WebSocket subscribers
- ActivityBuffer fed by recorder
- reconnect does not replay into ActivityBuffer
- session cleanup

### OUT OF SCOPE
- transcript UI
- LTE/remote polish
- persistence across daemon restart
- semantic grouping
- transcript readability
- session recreation / adapter restart edge cases (documented but not handled)
- late subscriber attachment resync

## 6. Open questions

1. **Stream replacement**: If `OpenStream()` returns a stream that dies (pipe closed), how does the recorder recover? One-shot re-open? Signal to subscribers?

2. **Late subscriber**: If a WebSocket connects mid-session, how does it get the current terminal state? Current replay path (`ReadScreen` + clear) works but is captured by ActivityBuffer as new output.

3. **Adapter restart**: If tmux/cmux adapter restarts (session list refresh), does the recorder need to re-attach?

4. **Concurrent sessions**: One ActivityBuffer per daemon handles all sessions. Is there a risk of session cross-contamination? (No — events are keyed by sessionID.)
