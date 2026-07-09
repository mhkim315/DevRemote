# E10b — Local Terminal Attach Design

Date: 2026-07-09
Status: Design (pre-implementation)

## Problem

`pokit run codex` creates a controlled_pty session but does not attach
the current Mac terminal. User must switch to mobile/web to interact.
This breaks the expected "run locally + observe remotely" flow.

## Goal

```
pokit run <command>
  → create controlled_pty session
  → attach current terminal as subscriber
  → user interacts from Mac terminal immediately
  → mobile/web can also observe/interact
```

## Non-Goal

- Local terminal must NOT be a second PTY reader.
- Recorder remains the single PTY read owner.
- No process injection into existing terminal apps.
- No screen scraping / accessibility APIs.

## Architecture

```
                            ┌──────────────────┐
                            │  Recorder (owner) │
                            │  single PTY reader │
                            └────────┬─────────┘
                                     │ broadcast
                    ┌────────────────┼────────────────┐
                    │                │                │
               subscriber      subscriber       subscriber
               (WebSocket)     (WebSocket)      (local stdin/stdout)
               mobile web      mobile app       pokit run terminal
```

The local terminal is a subscriber — it reads from the Recorder's broadcast
channel and writes to the PTY via WriteInput, exactly like a WebSocket client.

## Implementation Plan

### 1. Local subscriber: Unix socket bridge

Current `runClient` uses HTTP API to create session then exits.
New behavior:

```
pokit run <command>
  1. POST /api/sessions → create session → get sessionID
  2. Connect to daemon via Unix socket: /tmp/pokit.sock
  3. Send "sub:<sessionID>" to subscribe
  4. Daemon bridges:
     - Recorder broadcast → socket writes → local stdout
     - socket reads → WriteInput → PTY
  5. On Ctrl+C/D: unsubscribe, optionally terminate session
```

### 2. Daemon-side: subscriber bridge

Add a Unix socket listener (already exists partially for linker).
When a local terminal connects with `sub:<sessionID>`:
- If recorder exists, get its subscriber channel.
- Read from subscriber channel, write to socket (local stdout).
- Read from socket, write via InputWriter (or recorder.WriteInput).
- De-duplicate: do NOT create a second recorder.

```
Daemon socket handler:
  conn ← accept Unix socket
  read "sub:controlled_pty:run-<id>\n"
  rec := GetRecorder(sessionID)
  if rec == nil → error, session not found
  subCh := rec.Subscribe()
  defer rec.Unsubscribe(subCh)

  // Local stdout: recorder broadcast → socket
  go func() {
    for data := range subCh {
      conn.Write(data)
    }
  }()

  // Local stdin: socket → WriteInput
  go func() {
    buf := make([]byte, 1024)
    for {
      n, _ := conn.Read(buf)
      if n > 0 {
        session.WriteInput(ctx, buf[:n]) // or rec.WriteInput(buf[:n])
      }
    }
  }()
```

### 3. Terminal raw mode

Before bridging, put local terminal in raw mode (already in old runClient):
- `term.MakeRaw(os.Stdin.Fd())`
- Restore on exit.

### 4. Resize handling

- Local terminal size changes → SIGWINCH or periodic poll.
- Send resize to PTY: `stream.Resize(rows, cols)`.
- Daemon socket protocol: `resize:<cols>x<rows>\n`.

### 5. Ctrl+C / Ctrl+D

- Raw mode passes all keystrokes through to PTY.
- Ctrl+C → WriteInput(0x03) → PTY receives SIGINT.
- Ctrl+D → WriteInput(0x04) → PTY receives EOF.
- If session should persist after detach: handle as normal input.

### 6. Detach

```
pokit run --detach <command>
  → create session only
  → print URL
  → do not attach local terminal
```

Default is attach. `--detach` for background sessions.

### 7. Multi-viewer

- Local terminal + mobile + web all subscribers to same recorder.
- All receive same output via broadcast channels.
- All can send input via WriteInput.
- Existing multi-viewer protections apply (no duplicate recorder).

## Risk Assessment

| Risk | Mitigation |
|------|-----------|
| Unix socket not available (Windows) | Degrade: print URL, suggest mobile |
| PTY process exits while local terminal attached | Recorder EOF → subscriber channels close → local terminal gets EOF → exit raw mode |
| Input from multiple viewers interleaves | Accept for MVP (same as multiple WebSocket viewers) |
| Raw mode not restored on crash | defer term.Restore(), signal handler |

## Implementation Order

1. **E10b-1**: Daemon Unix socket subscriber bridge (accept `sub:<id>`, relay broadcast ↔ WriteInput).
2. **E10b-2**: `runClient` attach mode (connect socket, raw mode, relay stdin/stdout).
3. **E10b-3**: `--detach` flag (skip attach, print URL).
4. **E10b-4**: Resize propagation (local size → PTY).
5. **E10b-5**: Exit handling (Ctrl+C/D, session persist/terminate on detach).

## Tests

- Local attach → subscriber count increases by 1.
- Local stdin reaches PTY (WriteInput called).
- PTY output reaches local stdout.
- Detach does not create subscriber.
- Ctrl+C propagates to PTY.
- Session persists after local detach (unless terminated).
- Multiple subscribers (local + WebSocket) receive same output.

## Out of Scope

- Windows support.
- Multiple local terminals attached simultaneously (same as multiple WebSocket viewers — already works).
- Attaching to existing Terminal.app/iTerm2 sessions.
- tmux session attach (different architecture).
