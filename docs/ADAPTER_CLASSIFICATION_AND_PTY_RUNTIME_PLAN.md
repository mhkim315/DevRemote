# Adapter Classification and Controlled PTY Runtime Plan

Date: 2026-07-09

Status: Planning / architecture direction

This document updates the post-E8 roadmap after real-device validation showed
that not all terminal adapters provide the same source semantics.

## Summary

Pokit should no longer treat every terminal adapter as if it can provide the
same level of control, transcript reliability, and live terminal behavior.

The roadmap should formally distinguish:

```text
Control Adapter
→ reliable control path
→ PTY byte stream or equivalent append-oriented source
→ Recorder-owned capture
→ reliable Transcript
→ input/control capable

Observe Adapter
→ observation path
→ screen snapshot, file/log read, or other non-authoritative source
→ best-effort activity projection
→ limited or no input/control
→ Transcript may be degraded or unavailable
```

This is a capability boundary, not a vendor boundary.

## Evidence that forced the roadmap change

Accepted / established:

- E8f2 Session-owned Recorder is complete.
- ActivityBuffer ownership is recorder-owned for stream output.
- Multi-viewer duplication has been resolved.
- R1a LTE connectivity baseline is accepted.
- tmux/localpty fit the Recorder model naturally.

Real-device cmux evidence:

- Terminal live view produced unbounded duplicate scrollback.
- cmux Terminal live view is now disabled for `screen_snapshot_delta` adapters.
- cmux Transcript showed duplicate content in raw API output.
- PC-side scrolling changed Transcript source content even when no new agent
  output occurred.
- Character-by-character mutable input appeared in Transcript and later merged.
- cmux source snapshots are viewport-state-dependent, not append-only history.

Conclusion:

```text
tmux/localpty:
  PTY byte stream
  → Recorder
  → ActivityBuffer
  → reliable Transcript

cmux:
  viewport-dependent screen snapshot
  → best-effort observation only
  → not authoritative history
```

## Adapter categories

### Control Adapter

Examples:

- `tmux`
- `localpty`
- future `controlled_pty`

Expected characteristics:

- Recorder owns the PTY byte stream or equivalent append-oriented source.
- Output generated without a WebSocket viewer is still captured.
- Multiple viewers subscribe to one recorder; they do not create duplicate
  readers.
- Transcript is reliable enough to be user-facing as history.
- Input/control can be exposed when the capability contract allows it.

Recommended capability fields:

```ts
capabilities: {
  observe: true,
  control: true,
  input: true,
  liveTerminal: true,
  reliableTranscript: true,
  bestEffortTranscript: false
}
```

### Observe Adapter

Examples:

- `cmux` in its current form
- future app/log observers such as IDE-centric sessions when direct input is not
  possible

Expected characteristics:

- Source is not a stable append-only history.
- Output may be derived from screen snapshots, logs, or external app state.
- Transcript is best-effort or unavailable.
- Live terminal may be disabled or degraded.
- Input/control must not be exposed unless the adapter explicitly supports it.

Recommended capability fields:

```ts
capabilities: {
  observe: true,
  control: false,
  input: false,
  liveTerminal: false,
  reliableTranscript: false,
  bestEffortTranscript: true
}
```

## cmux position

cmux should be repositioned as an Observe Adapter unless a stable cmux output
history API is found.

cmux Transcript must be documented as:

```text
Best-effort observation derived from viewport-dependent screen snapshots.
It is not an authoritative append-only history.
PC-side scrolling or viewport changes may affect captured output.
Use tmux/localpty/Controlled PTY Runtime for reliable transcript.
```

Do not continue expanding cmux snapshot heuristics to chase tmux-equivalent
reliability. Heuristics may be used only for bounded degradation and display
quality, not as proof of reliable history.

## Controlled PTY Runtime

Pokit should introduce a formal roadmap item for a Pokit-owned PTY runtime.

This should not mean attaching to arbitrary already-running terminal apps.

Non-goal:

```text
Do not attach to existing Terminal.app, iTerm2, Ghostty, Warp, or other
already-running terminal sessions as a reliable product feature.
```

Reason:

- The terminal emulator owns the PTY master file descriptor.
- macOS does not provide a stable public API for another process to safely
  capture that existing byte stream.
- Writing to discovered TTY devices risks unsafe input injection.
- Accessibility/AppleScript/UI scraping is not a reliable byte-stream adapter.

Goal:

```text
Pokit launches shell/agent processes through a PTY it owns.

Controlled PTY
→ Session-owned Recorder
→ ActivityBuffer
→ Terminal / Transcript / Interaction UX
```

Candidate launch surfaces:

- `pokit shell`
- `pokit run claude`
- `pokit run codex`
- `pokit run opencode`
- mobile/desktop "New Agent" flow backed by a Pokit-owned PTY

## Revised roadmap

### E8g5 — Adapter Capability Reclassification

Goal:

```text
Make adapter class and transcript/control capability explicit across backend
and mobile boundaries.
```

Scope:

- Add or document adapter class:
  - `control`
  - `observe`
- Add or document capability semantics:
  - `observe`
  - `control`
  - `input`
  - `liveTerminal`
  - `reliableTranscript`
  - `bestEffortTranscript`
- Mark tmux/localpty as control-capable.
- Mark cmux as observe/snapshot-based.
- Ensure mobile renders capabilities rather than vendor-specific branches.
- Preserve A7 unknown/future fallback behavior.

Acceptance:

- Mobile does not hardcode backend names to decide behavior.
- cmux Terminal is unavailable/degraded through capability, not vendor branch.
- cmux Transcript is labeled best-effort/degraded.
- tmux/localpty live terminal and reliable Transcript remain unchanged.
- Existing A7/A9 capability principles are preserved.

Out of scope:

- Controlled PTY implementation.
- New terminal app integrations.
- More cmux transcript heuristics.

### E8g6 — cmux Observe Mode Stabilization

Goal:

```text
Make cmux honest and safe as an observe adapter.
```

Scope:

- Document cmux source limitations in product-facing terms.
- Ensure raw API and mobile UI do not claim reliable history for cmux.
- Keep cmux Terminal live disabled unless a safe screen-replace renderer is
  explicitly implemented.
- Keep cmux Transcript best-effort.
- Define when cmux Transcript should show degraded/experimental messaging.

Acceptance:

- cmux is not presented as tmux-equivalent.
- Known limitations mention viewport-dependent snapshots.
- UI shows best-effort/degraded state without blocking tmux/localpty.
- No further heuristic patches are accepted without evidence and a bounded
  acceptance test.

Out of scope:

- Building a custom terminal emulator.
- Making cmux Transcript authoritative.
- Re-enabling cmux xterm live terminal.

### E9-pre — Controlled PTY Runtime Design

Goal:

```text
Design a Pokit-owned PTY runtime before implementation.
```

Required design sections:

- macOS boundary:
  - why arbitrary existing terminal attach is not a reliable goal;
  - which APIs are public/safe;
  - which approaches are out of scope.
- Runtime lifecycle:
  - create PTY;
  - launch shell/agent;
  - recorder ownership;
  - resize;
  - input;
  - process exit;
  - cleanup.
- Security:
  - no raw input stored in ActivityBuffer;
  - local-first boundary;
  - command/environment handling;
  - no private API or UI scraping.
- Failure modes:
  - process launch failure;
  - PTY read failure;
  - reconnect;
  - multiple viewers;
  - daemon restart limitations.

Acceptance:

- Design document exists.
- Existing terminal attach is explicitly rejected as MVP scope.
- Pokit-owned PTY is defined as the implementation path.
- Test plan for E9 MVP is defined.

### E9 — Controlled PTY Runtime MVP

Goal:

```text
Pokit can launch and own a PTY-backed shell/agent session.
```

Acceptance:

- Session output is captured without a WebSocket viewer.
- Reconnect shows recorder-backed history.
- Multiple viewers do not duplicate recording.
- Mobile Terminal works.
- Transcript uses ActivityBuffer events.
- Input works and raw input text is not stored.
- Resize works or degrades explicitly.
- Process exit is visible.
- Session delete cleans recorder/activity.
- `go test -race ./...` and build gate pass.

Out of scope:

- Attaching to existing terminal app tabs.
- Terminal-specific plugins.
- Full semantic transcript grouping.

### E10 — Agent Launch UX

Goal:

```text
Make controlled PTY sessions usable by people.
```

Scope:

- `pokit run <command>` or equivalent.
- Saved launch commands.
- cwd selection.
- basic environment handling.
- clear launch failure messages.
- mobile/desktop "New Agent" flow if feasible.

Acceptance:

- User can start a controlled PTY session without manual daemon internals.
- Capabilities shown correctly.
- Failed launches are recoverable and diagnosable.

### E11 — Optional Terminal App Integrations

Goal:

```text
Optional integrations with user-preferred terminal apps.
```

Important scope:

This is not arbitrary existing-session attach.

Possible scope:

- Open a Pokit-controlled session in a preferred terminal UI.
- Terminal-specific helper scripts or plugins.
- iTerm2/Ghostty/Terminal.app/Warp integrations only if they use public APIs and
  degrade safely.

Acceptance:

- Optional.
- No private API reliance.
- Core Controlled PTY Runtime works without this phase.

## Risks and hidden assumptions

### PTY byte stream is not a perfect semantic transcript

PTY byte streams can still contain:

- ANSI cursor movement;
- carriage-return progress updates;
- alternate screen;
- vim/nano/htop;
- prompt echo.

However, they are still a better source than viewport-dependent snapshots
because PC-side scrolling does not mutate the capture source.

### Control/Observe must remain capability-based

The product must not branch by adapter name in mobile UI. Adapter class is a
coarse label. Capabilities are the actual contract.

### cmux downgrade may feel like feature loss

This is acceptable. Showing unreliable data as if it were authoritative is worse
than showing a clear observe/degraded state.

### Existing terminal attach may be tempting

Do not add it to MVP scope without a separate feasibility proof. It is likely to
pull the project toward private APIs, UI automation, or unsafe TTY injection.

## Immediate next action

Proceed with:

```text
E8g5 — Adapter Capability Reclassification
```

Do not implement Controlled PTY Runtime yet.

Do not continue cmux Transcript heuristic work unless it is limited to
observe/degraded safety and has explicit evidence.

