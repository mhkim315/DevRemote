# E8f2 — Session-level Activity Recorder Plan

Date: 2026-07-08

## Current limitation

ActivityBuffer captures PTY output only through HandleWS → stream.Read().
This is tied to WebSocket lifetime:
- WebSocket connected → capture works
- WebSocket disconnected → no capture
- Multiple WebSocket viewers → each reads independently (potential duplication)

## Target architecture

```
Session PTY
  ↓
SessionRecorder (one per session, daemon-lifetime)
  ↓
ActivityBuffer (append-only, monotonic seq)
  ↓
?activity=<sessionId> (available to any client)
```

WebSocket clients become SUBSCRIBERS to the recorder's output channel,
not owners of the read loop.

## Required changes

1. Move PTY read loop out of HandleWS into a session-scoped recorder.
2. WebSocket clients receive from the recorder's broadcast channel.
3. ActivityBuffer is fed by the recorder, not by individual WebSocket handlers.
4. Recorder starts when session starts, stops when session ends.
5. Multiple WebSocket viewers share one PTY reader.
6. Reconnect does not replay old output as new seq.

## Non-goals for E8f2

- Poll-based screen capture (PTY stream is preferred)
- Diff-based content tracking
- Session lifecycle management changes
- Transcript UI changes

## Acceptance

1. Start session with no WebSocket attached.
2. Generate output.
3. Connect mobile later.
4. ?activity=<sessionId> returns output from step 2.
5. No duplicate reads from multiple WebSocket connections.
6. Existing live streaming still works.
