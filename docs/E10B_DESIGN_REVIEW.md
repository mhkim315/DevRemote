# E10b Design Review

Date: 2026-07-09
Review of: ea76b6ebb (`docs/E10B_LOCAL_TERMINAL_ATTACH_DESIGN.md`)

## 1. Recorder Ownership

**Verdict: PRESERVED** ✅

The design correctly models the local terminal as a subscriber.
The Recorder remains the single PTY reader. No second `OpenStream` call.

One risk: the design mentions `rec.WriteInput(buf[:n])` as the stdin path.
`WriteInput` writes to `r.stream.Write(data)`. For byte_stream adapters,
the session's `InputWriter` is the correct target (it writes to PTY stdin).
Using `rec.WriteInput` works for tmux/cmux (stream-only fallback) but for
controlled_pty, `session.WriteInput` (which writes to native.PTY) is more
direct. This distinction should be explicit in implementation.

**Recommendation**: In the socket bridge, check if session implements
`InputWriter` first (preferred), fall back to `rec.WriteInput`.

## 2. Unix Socket Bridge

**Verdict: ACCEPTABLE for MVP** ⚠️

Pros:
- Zero network overhead (local IPC).
- Already partially in codebase (`/tmp/pokit.sock` for linker).
- Simple text protocol.

Cons:
- Unix sockets are macOS/Linux only (Windows would need named pipe).
- `/tmp/pokit.sock` is world-accessible by default on macOS.
- No auth — any local process can connect and inject input.

**Recommendation**: 
- Set socket file permissions to 0600 (owner-only) in daemon.
- Accept for MVP. For production, pair with auth token validation.
- Document Windows limitation explicitly.

## 3. Raw Terminal Mode

**Verdict: ACCEPTABLE** ⚠️

The old `runClient` already has `term.MakeRaw` + `term.Restore` with
signal handler. This can be reused.

Risk: if the Go process crashes before `term.Restore`, the terminal
is left in raw mode. The user sees no echo and must type `reset` blindly.

**Recommendation**: 
- Register `defer term.Restore` early, before any `log.Fatal`.
- Add `SIGTERM`/`SIGINT` handler (already in old code).
- Consider a watchdog timer: if no bytes received for N seconds, auto-restore.

## 4. Resize Propagation

**Verdict: FOLLOW-UP** ⚠️

Design mentions `resize:<cols>x<rows>` socket protocol. Correct direction.

Risk: local terminal resize and mobile WebSocket resize both call `Resize()`
on the stream. Not harmful (idempotent), but adds complexity.

**Recommendation**: 
- Implement after basic attach works. Not blocking MVP.
- SIGWINCH detection via `term.GetSize` polling (500ms-1s interval).

## 5. Ctrl+C / Ctrl+D / Ctrl+Z

**Verdict: ACCEPTABLE** ✅

In raw mode, all keystrokes pass through. Ctrl+C (0x03) is sent to PTY.
The child process receives SIGINT. No special handling needed.

Edge case: user hits Ctrl+C twice rapidly. First sends SIGINT to child.
If child dies, PTY returns EOF. Recorder stops. Local subscriber gets EOF.
Exit raw mode. Second Ctrl+C would go to the `pokit run` process itself,
which should already be cleaning up.

**Recommendation**: 
- After EOF from recorder, exit raw mode before any further terminal I/O.
- Document that Ctrl+C terminates the child process (expected behavior).

## 6. Local Disconnect, Session Continues

**Verdict: ACCEPTABLE** ✅

Subscriber model handles this. Local terminal calls `rec.Unsubscribe(ch)`.
Session continues capturing via Recorder. Mobile/web unaffected.
This is the same as WebSocket disconnect — already proven.

## 7. Process Exit

**Verdict: FIXED** ✅

With `f681db363` (terminated flag), process exit → EOF → recorder stops →
terminated flag set → no restart loop. All subscriber channels close.
Local terminal gets EOF, exits raw mode.

## 8. --detach Flag

**Verdict: ACCEPTABLE** ✅

Clear semantics: create session + print URL, skip socket connect.
Implementation is a single `if` branch in `runClient`.

## 9. Required Tests

**ACCEPTANCE REQUIREMENT** — must exist before implementation is accepted:

| Test | Verifies |
|------|----------|
| `TestLocalAttach_SubscriberCount` | `rec.Subscribe()` called once for local attach |
| `TestLocalAttach_StdinToPTY` | bytes written to socket reach `WriteInput` |
| `TestLocalAttach_PTYToStdout` | recorder broadcast reaches socket writer |
| `TestLocalAttach_Detach` | `--detach` does not call `Subscribe()` |
| `TestLocalAttach_CtrlCPropagates` | 0x03 byte written to PTY via WriteInput |
| `TestLocalAttach_SessionPersistsAfterDetach` | unsubscription does not stop recorder |
| `TestLocalAttach_MultiViewer` | local + WebSocket receive same broadcast |
| `TestLocalAttach_RawModeRestore` | terminal restored after exit/crash |

## 10. Security: Unix Socket Access

**BLOCKER** — must be addressed before implementation ships:

Current `/tmp/pokit.sock` has default permissions. Any local user/process
can connect and:
- Read PTY output (session data leak).
- Inject keystrokes (input injection).

Mitigations:
1. `os.Chmod(socketPath, 0600)` after `net.Listen`.
2. Socket placed in user-owned directory (`~/.pokit/`).
3. For production: require `POKIT_TOKEN` or `Authorization` header in
   socket protocol handshake.

**Recommendation**: 
- Set 0600 permissions always.
- Document that `--insecure-local-only` mode trusts local processes.
- Add socket auth for production mode (BLOCKER for release, not MVP).

## Summary

| Finding | Classification |
|---------|---------------|
| Recorder ownership preserved | ✅ ACCEPTABLE |
| Unix socket bridge | ⚠️ ACCEPTABLE (set 0600 perms) |
| Raw terminal mode | ⚠️ ACCEPTABLE (defer restore) |
| Resize propagation | FOLLOW-UP |
| Ctrl+C/D/Z handling | ✅ ACCEPTABLE |
| Local disconnect | ✅ ACCEPTABLE |
| Process exit | ✅ FIXED (f681db363) |
| --detach flag | ✅ ACCEPTABLE |
| Required tests | ACCEPTANCE REQUIREMENT (8 tests) |
| Unix socket security | BLOCKER (0600 perms minimum) |

## Recommendation

**PROCEED with E10b-1** after:
1. Unix socket file permissions set to 0600 (one-line fix in daemon).
2. Accept that Windows is not supported for attach mode (doc limitation).

Implementation can start on E10b-1 (socket bridge) + E10b-2 (runClient attach).
Tests can be added incrementally per implementation step.
