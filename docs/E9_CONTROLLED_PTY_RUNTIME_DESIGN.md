# E9-pre — Controlled PTY Runtime Design

Date: 2026-07-09
Status: Design (pre-implementation)
Target: E9 Controlled PTY Runtime MVP

## Summary

Pokit will own and manage a PTY-backed shell/agent session as a first-class
Control Adapter. This gives Pokit the same reliable byte-stream capture that
tmux/localpty provide, without depending on external terminal multiplexers.

The Controlled PTY Runtime is NOT an "attach to existing terminal" feature.
It launches its own processes through a PTY it owns.

## macOS Boundary

### Why existing terminal attach is rejected

- macOS Terminal.app, iTerm2, Ghostty, Warp each own the PTY master fd.
- There is no stable public API for another process to capture that byte stream.
- `/dev/ttys*` devices cannot be safely opened by a second reader.
- Accessibility/AppleScript/UI scraping is not a reliable byte-stream source.

These approaches are explicitly out of MVP scope.

### Public APIs used

- `posix_openpt()` / `grantpt()` / `unlockpt()` — standard POSIX PTY creation.
- `fork()` + `exec()` — launch child process in the PTY.
- `creack/pty` Go library (already in use by localpty adapter).
- No private macOS frameworks. No IOKit. No accessibility APIs.

## Runtime Lifecycle

### Create PTY

```
NewSession(cwd, command, env) → NativeSession{PTY, Cmd}
```

- Create PTY pair via `pty.Start(cmd)`.
- Set TERM=xterm-256color.
- Inherit daemon environment (PATH, HOME, API keys).
- Session ID: `controlled_pty:<uuid>` or user-provided name.

### Launch shell/agent

```
pokit run claude
pokit run codex
pokit run "bash -c 'my-script.sh'"
```

- Command is user-provided or selected from known agent profiles.
- Default: `$SHELL -l` (login shell).
- Process runs as child of the daemon.

### Recorder ownership

```
Controlled PTY session
  → EnsureRecorder(sessionID, opener, activity)
  → Recorder owns PTY read loop
  → ActivityBuffer receives deltas
  → N WebSocket subscribers for live terminal
```

- Same recorder model as tmux/localpty.
- Session discovery via TelemetryService.processSession.
- Output captured without a WebSocket viewer.

### Resize

- xterm.js `term.resize(cols, rows)` → POST `/term/size` → `ptm.Setsize()`.
- Already implemented for localpty.

### Input

- WebSocket keyboard input → `mux.InputWriter.WriteInput()` → PTY write.
- Raw input text NOT stored in ActivityBuffer (existing invariant).
- Input metadata (bytes, timestamp) stored as terminal_input event.

### Process exit

- PTY read returns EOF when child process exits.
- Recorder sets readErr, closes subscriber channels.
- HandleWS detects closed channel → sends 1011 close frame.
- Session state transitions to "ended."
- DeleteRecorder + ActivityBuffer.Clear on cleanup.

### Cleanup

- DELETE /api/sessions → TerminateSession → kill process → DeleteRecorder → ActivityBuffer.Clear.
- Daemon restart: no session persistence (in-memory only for MVP).

## Security

### No raw input stored

- WebSocket input → PTY write.
- ActivityBuffer stores terminal_input with bytes only, Text="" (existing policy).

### Local-first boundary

- PTY runs on localhost.
- No cloud relay for PTY data.
- Existing JWT auth protects API/WebSocket endpoints.

### Command/environment handling

- Daemon inherits user environment.
- Commands run as the daemon user.
- No privilege escalation.
- No `sudo` passthrough.

### No private API

- Only standard POSIX PTY APIs.
- No accessibility/AppleScript/UI scraping.
- No IOKit or private macOS frameworks.

## Failure Modes

| Failure | Behavior |
|---------|----------|
| Process launch fails (binary not found) | Session creation returns error, no session created |
| PTY read error (process killed) | Recorder closes subscribers, WebSocket 1011 close |
| WebSocket disconnect | Subscriber removed, recorder continues capturing |
| Multiple viewers | Single recorder, multiple subscribers (existing model) |
| Daemon restart | PTY process orphaned/killed, sessions lost (in-memory) |
| Resize fails | Graceful degradation, xterm.js handles locally |

## Adapter Contract

```
Adapter: controlled_pty
TranscriptCaptureMode: CaptureModeByteStream
Capabilities: observe, control, input, liveTerminal, reliableTranscript
```

- `StreamOpener`: opens PTY stream.
- `InputWriter`: writes to PTY.
- `ScreenReader`: reads current terminal state (if supported).
- `SessionCreator`: creates new PTY sessions.
- `SessionTerminator`: kills PTY processes.

## Test Plan for E9 MVP

1. **PTY creation**: create session, verify process running, PTY fd open.
2. **Output capture**: write to PTY, verify ActivityBuffer receives events.
3. **No WebSocket capture**: start session, no viewer connected, verify activity captured via telemetry.
4. **Multiple viewers**: two WebSocket connections, verify single recorder, same output.
5. **Input**: send keystrokes, verify PTY receives them, raw text not stored.
6. **Resize**: change terminal dimensions, verify Setsize called.
7. **Process exit**: kill child process, verify recorder cleanup, session ended state.
8. **Delete cleanup**: delete session, verify recorder removed, ActivityBuffer cleared.
9. **Reconnect**: disconnect and reconnect WebSocket, verify history accessible.
10. **Race detector**: `go test -race ./... -count=1`.

## Out of MVP Scope

- Attaching to existing Terminal.app/iTerm2/Ghostty/Warp sessions.
- Session persistence across daemon restart.
- Multi-daemon / remote PTY.
- Terminal-specific plugins.
- Agent profile UI.
- `pokit run` CLI.
