# E8f2 Executor Onboarding — Session-owned Activity Recorder

Date: 2026-07-09

## Read this first

This document is for a fresh execution agent after context reset.

You are continuing E8f2. Do not restart from the older E8 scroll debugging
work. The xterm mobile scrollback patching path is closed.

Current branch:

```text
feature/phase10-multi-adapter
```

Current remote HEAD at the time this onboarding was written:

```text
c04fafb E8f2: add behavior tests — no-WebSocket capture + multi-subscriber
```

Important: `c04fafb` is currently REJECTED by verifier. Continue from it only to
fix the listed blockers.

## Required reading

Read these files before editing code:

- `docs/E8F2_PRE_OWNERSHIP_AUDIT.md`
- `docs/E8F2_RECORDER_PLAN.md`
- `docs/E8_EXECUTION_PLAN.md`
- `docs/NEXT_SESSION_E8_HANDOFF.md`
- `docs/E8_RUNTIME_RELIABILITY_BUG_REPORT.md`

## Accepted baseline

Accepted before E8f2:

```text
E8f  ACCEPT — Activity capture pipeline MVP
E8g  ACCEPT — Mobile Transcript Read Mode
E8h  ACCEPT — Best-effort transcript fallback/disclaimer
E8f2-pre ACCEPT — Stream / PTY Ownership Audit
```

Accepted E8f2-pre conclusions:

- WebSocket-owned capture records viewer lifetime, not session lifetime.
- E8f2 must move terminal output capture to a session-owned recorder.
- WebSocket clients must become subscribers.
- ActivityBuffer append for terminal output must be recorder-owned.
- `ReadScreen`, `ReadHistory`, bootstrap, replay, and reconnect bytes must not
  append new ActivityEvents.
- `tmux` has no `InputWriter`; its stream-only `stream.Write(msg)` fallback is
  required and must be preserved or replaced equivalently.
- terminal input metadata may remain WebSocket-owned, but raw input text must
  never be stored.

## Current implementation state

Recent relevant commits:

```text
5d2886f E8f2-pre: fix remaining replay path factual error
35c94b2 E8f2: SessionRecorder — single PTY reader per session
154cf77 E8f2a: EnsureRecorder — lifecycle-first recorder
70fb211 E8f2a: lifecycle-first recorder — telemetry start + session cleanup
92cd3cf E8f2a: TelemetryService owns ActivityBuffer for lifecycle capture
c04fafb E8f2: add behavior tests — no-WebSocket capture + multi-subscriber
```

What is already implemented:

- `Recorder` type exists in `companion-daemon/internal/term/recorder.go`.
- `EnsureRecorder()` returns or creates one recorder per session.
- `TelemetryService` now has an `ActivityBuffer` field.
- Production app wires the same `ActivityBuffer` into `Handlers` and
  `TelemetryService`.
- `TelemetryService.processSession()` calls `EnsureRecorder(id, opener,
  s.activity)`.
- `HandleWS` subscribes via `EnsureRecorder()`.
- Session DELETE calls `DeleteRecorder(id)` and `h.Activity.Clear(id)`.
- Basic recorder tests exist in `recorder_test.go`.

## Current verifier status

Latest verifier verdict on `c04fafb`:

```text
REJECT
```

The implementation direction is close, but the tests do not yet prove the
production E8f2 contract.

## Current blockers to fix

### Blocker 1 — no-WebSocket test bypasses production path

Current test calls:

```go
EnsureRecorder("test:recorder-test", opener, activity)
```

This proves the helper can capture output when called directly. It does not
prove the production path:

```text
TelemetryService.processSession
→ EnsureRecorder(id, opener, s.activity)
→ recorder reads stream
→ ActivityBuffer append
→ no WebSocket involved
```

Required fix:

Add or replace with a production-path test:

```text
NewTelemetryService(..., activity)
→ processSession(...) or Run(ctx) discovers a StreamOpener session
→ fake stream emits output
→ no WebSocket is opened
→ activity.List(sessionID) contains terminal_output
```

The test must fail if `TelemetryService` is constructed with a nil
`ActivityBuffer`.

### Blocker 2 — multi-subscriber test bypasses EnsureRecorder

Current test manually creates `Recorder` and inserts it into
`recorderRegistry`.

That bypasses the code that must be verified:

```text
EnsureRecorder(session, opener, activity)
EnsureRecorder(session, opener, activity) again
```

Required fix:

Add a test proving:

- fake opener counts `OpenStream()` calls;
- first `EnsureRecorder` opens the stream once;
- second `EnsureRecorder` returns existing recorder;
- `OpenStream()` call count remains 1;
- both subscribers receive the same live output;
- `ActivityBuffer` has exactly one terminal_output append for one PTY output.

### Blocker 3 — delete cleanup test does not prove ActivityBuffer clear

Current test verifies only:

```text
DeleteRecorder(...)
→ GetRecorder(...) == nil
```

But E8f2 policy requires:

```text
session delete/end
→ recorder stops
→ subscribers closed
→ ActivityBuffer cleared
```

Required fix:

Add a test through the real cleanup path or a shared cleanup function that proves:

- recorder existed;
- `ActivityBuffer` had events;
- delete cleanup removes recorder;
- `ActivityBuffer.List(sessionID)` is empty;
- same session ID reuse resets seq.

The existing `HandleSessionCRUD DELETE` path is acceptable if easy to test.
Alternatively introduce a small helper used by the handler and test that helper.

### Blocker 4 — tmux stream-only input fallback is not tested

Audit-critical fact:

```text
tmux has no InputWriter.
HandleWS must preserve stream.Write(msg) fallback.
```

Current implementation uses:

```go
} else if rec != nil {
    rec.WriteInput(msg)
}
```

Required fix:

Add a test with a session that:

- implements `StreamOpener`;
- does not implement `InputWriter`;
- has a fake stream recording `Write()` calls;
- sends WebSocket input through `HandleWS`;
- verifies the stream received the bytes;
- verifies `terminal_input` ActivityEvent stores `Text == ""`.

If full WebSocket test is hard, factor the input write decision into a small
testable helper without changing semantics.

### Blocker 5 — build gate reproducibility

Verifier observed:

- direct `go test -race ./... -count=1` passed;
- `scripts/build-gate.sh` sometimes reported hidden `go test -race` failure.

Required fix:

Run and report:

```sh
sh scripts/build-gate.sh
```

If it fails, do not claim all gates passed. Capture the failing command output by
running the equivalent test visibly:

```sh
cd companion-daemon
go test -race ./... -count=1
```

## Do not do this

Do not:

- resume xterm mobile scrollback patching;
- change Transcript UI in E8f2;
- add LTE / tunnel / remote connectivity work;
- store raw terminal input text;
- append `ReadScreen`, `ReadHistory`, bootstrap, replay, or reconnect bytes to
  `ActivityBuffer` as new terminal_output;
- create one PTY reader per WebSocket viewer;
- remove `tmux` stream-only input fallback;
- hide failing tests as "fixture-only" if production ownership is not proven.

## E8f2 acceptance checklist

E8f2 can be accepted only when all of the following are true:

1. Session-owned recorder starts from session lifecycle/discovery, not only from
   `HandleWS`.
2. WebSocket clients subscribe to an existing recorder.
3. For a session with active recorder, additional WebSocket viewers do not call
   `OpenStream()` again.
4. `ActivityBuffer` terminal_output appends come from recorder only.
5. No-WebSocket output capture is proven through `TelemetryService` or equivalent
   production path.
6. `tmux` stream-only input fallback is preserved and tested.
7. terminal_input remains metadata-only; raw input text is never stored.
8. Session delete/end removes recorder and clears `ActivityBuffer`.
9. Same-ID recreation does not reuse stale recorder or stale activity events.
10. Recorder read error closes subscribers and WebSocket receives 1011.
11. `ReadScreen`, `ReadHistory`, bootstrap, replay, and reconnect do not append
    new ActivityEvents.
12. `sh scripts/build-gate.sh` passes.

## Suggested implementation order

1. Fix/add production-path tests first.
2. Run the targeted tests and ensure they fail on the current implementation if
   the behavior is missing.
3. Make the smallest code changes needed.
4. Run:

   ```sh
   cd companion-daemon
   go test -race ./... -count=1
   ```

5. Run:

   ```sh
   sh scripts/build-gate.sh
   ```

6. Commit with a message like:

   ```text
   E8f2: prove lifecycle recorder ownership
   ```

## Completion statement format

Use this when handing back to verifier:

```text
E8f2 lifecycle recorder fix complete.

Commit: <sha>

Fixed:
- production-path no-WebSocket capture test
- EnsureRecorder multi-subscriber/no-multi-open test
- delete cleanup clears ActivityBuffer
- tmux stream-only input fallback test

Validation:
- go test -race ./... -count=1: PASS
- sh scripts/build-gate.sh: PASS

Not changed:
- Transcript UI
- xterm mobile scrollback
- LTE/remote connectivity
```
