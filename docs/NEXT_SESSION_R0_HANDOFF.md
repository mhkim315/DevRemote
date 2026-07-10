# Next Session Handoff — R0 E10b Product Polish

Status: execution handoff  
Branch: `feature/phase10-multi-adapter`  
Baseline commit: `6b40b32` — E10b Claude mobile width / PTY geometry mirror

## Mission

Finish E10b product polish without changing the core runtime architecture.

Pokit currently works as:

```text
pokit run <agent-or-command>
        ↓
controlled_pty
        ↓
Recorder = single PTY reader
        ↓
subscribers:
  - local terminal
  - mobile terminal
  - web terminal
```

Do not alter this ownership model.

## Current state to assume

Accepted / mostly accepted:

- session-owned Recorder
- adapter capability split
- controlled_pty runtime
- `pokit run`
- local terminal attach
- raw PTY bootstrap ring buffer
- atomic `SubscribeWithBootstrap`
- local attach exit/reset improvements
- mobile Codex Send split
- reconnect backoff
- mobile foreground auto-reconnect
- IPC socket robustness
- PTY geometry mirror via `GET /term/size`

The current close-range work is product validation and small fixes.

## Highest priority R0 tasks

### R0.1 — Claude mobile terminal width / scroll validation

Latest behavior:

- Codex was working with horizontal swipe/no forced wrap.
- Claude still wrapped on mobile under the earlier fixed-100-col viewer.
- Commit `6b40b32` changed mobile terminal sizing:
  - backend exposes `GET /term/size?session=<id>`.
  - mobile xterm mirrors the real PTY geometry.
  - mobile viewer does not resize the shared PTY.
  - both-axis scroll is allowed.

Observed root cause from executor validation:

- Codex renders in the main buffer with a narrow UI around 59 columns, so it was
  already fine in the fixed-100-col mobile viewer.
- Claude enters the alternate screen (`?1049h`) and draws a full-width TUI at
  the PTY width.
- For locally attached sessions, PTY width follows the host terminal width
  (often above 100 columns). A fixed 100-col mobile xterm therefore re-wrapped
  Claude even though Terminal.app / VS Code were correct.

The current solution is mirror-only:

- local attach may resize the shared PTY to the host terminal size.
- mobile fetches `GET /term/size` and resizes its xterm to the same rows/cols.
- mobile must not resize the shared PTY.
- local attach size is effectively the mobile view geometry.

Validate narrowly:

```text
pokit run claude
open same session on mobile
verify Claude TUI does not re-wrap to phone width
verify horizontal/both-axis scroll works
verify local Terminal.app / VS Code still display correctly
verify no duplicate subscribers or reconnect loops
```

Suggested real-device commands:

```sh
adb -s R3CX106PTFD install -r mobile/android/app/build/outputs/apk/release/app-release.apk
adb -s R3CX106PTFD reverse tcp:9171 tcp:9171
```

Additional checks:

- start `pokit run claude` from a wide Terminal.app window.
- verify phone mirrors the wide layout without wrapping.
- resize Terminal.app and confirm the phone follows within roughly 3 seconds.
- run `pokit run codex` and confirm Codex still behaves normally.
- if the alternate-screen height is clipped, record whether vertical scrolling works.

Do not:

- redesign xterm.
- change Recorder.
- change ActivityBuffer.
- change Transcript logic.

Acceptance:

- Claude is usable enough on mobile Terminal.
- If still imperfect, document exact visual failure with whether backend `/term/size` reports the expected PTY cols/rows.
- No runtime ownership invariant is touched.

### R0.2 — Mobile `+ new session` / agent profile save failure

Observed issue:

- existing sessions are visible.
- mobile `+` new session flow can show `failed to save agent profile`.

Treat this as separate from controlled_pty runtime.

Investigate:

- is it local storage?
- is it a mobile API/client path?
- is it blocked by no-login test mode?
- is it trying to save a profile that no longer matches current runtime model?

Acceptance:

- `+ new session` either creates a controlled_pty-backed session successfully,
  or the failure is diagnosed and the UI clearly tells the user what is missing.

Do not:

- change Recorder ownership.
- add a new adapter.
- mix this with Transcript.

### R0.3 — Real-device validation checklist

Run and record:

- `pokit run bash`
  - local attach opens.
  - `exit` returns host shell to a usable prompt.
  - mobile session shows ended state.
  - mobile input controls are hidden after ended state.
- `pokit run codex`
  - local attach works.
  - mobile Send sends exactly one command.
  - mobile Enter behavior works.
  - reconnect after app foreground works.
- `pokit run claude`
  - local attach works.
  - mobile terminal width/scroll is acceptable.
  - exit/reset does not leak terminal control sequences into host shell.
- Remote/LTE:
  - sessions list loads.
  - terminal connects or shows clear failure.
  - activity/transcript endpoints remain reachable.

Evidence should be concrete:

- commit hash
- daemon restarted with latest binary
- APK build source verified through source map/hash/UTF-16LE-aware check if needed
- exact session type (`bash`, `codex`, `claude`)
- exact observed failure if any

## Transcript is not R0

Current transcript problem:

- Claude/Codex TUI/status/thinking output can create huge horizontal duplicated/merged transcript lines.

Do not keep tuning the current transcript heuristics in R0.

Reason:

- Prior cmux tuning left transcript semantics confused.
- Transcript is a projection/document product, not Live Terminal.
- It may be better to restart from a simpler T1 contract rather than patch the existing projection.

R0 may only document transcript evidence. Implementation belongs to T1.

## T1 preview — do not implement unless R0 is closed

T1 should start from the contract in:

- `docs/ROADMAP_AFTER_E10B.md`

Guidance:

```text
Live Terminal = raw PTY / xterm-compatible
Transcript = readable projection
```

T1 should preserve simple output and filter/summarize/placeholder complex TUI output.

## Validation policy for this handoff

### BLOCKER

Use for:

- Recorder single-reader violation
- local/mobile attach broken
- `pokit run` launch/runtime broken
- mobile Send corrupts input
- ended sessions still accept input
- security issue
- API/product contract broken

### FOLLOW-UP

Use for:

- transcript quality
- minor visual polish
- optional tests
- helper extraction
- implementation style

### SCOPED ACCEPT

Use when:

- R0 product behavior is usable
- limitations are documented
- no runtime invariant is violated

## Executor instruction

Work narrowly.

For R0, prefer:

- real-device validation,
- small targeted fixes,
- documentation of exact remaining failures.

Avoid:

- new runtime architecture,
- cmux reliability work,
- transcript redesign,
- new adapter work.
