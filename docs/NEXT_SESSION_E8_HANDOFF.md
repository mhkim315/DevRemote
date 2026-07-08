# Next Session E8 Handoff

Read first:

- `docs/E8_EXECUTION_PLAN.md`
- `docs/E8_INSTRUMENTATION_REPORT.md`
- `docs/E8_RUNTIME_RELIABILITY_BUG_REPORT.md`
- `docs/M_TRACK_PHONE_OBSERVATIONS.md`

## Current priority

Do not continue patching xterm mobile scrollback.

Immediate target:

```text
E8f2-pre — Stream / PTY Ownership Audit
```

## Mission

Audit current stream ownership before implementing the session-owned recorder.

```text
No recorder implementation yet.
No runtime behavior changes.
No WebSocket behavior changes.
```

Read:

- `docs/E8F2_RECORDER_PLAN.md`
- `docs/E8_EXECUTION_PLAN.md`
- `docs/E8_RUNTIME_RELIABILITY_BUG_REPORT.md`
- `docs/M_TRACK_PHONE_OBSERVATIONS.md`

## Why

Validated real-device evidence now shows:

- backend replay is not the mobile idle-scroll cause;
- WebSocket reconnect is not the mobile idle-scroll cause;
- terminal data integrity is preserved;
- desktop xterm behaves correctly;
- Android WebView + xterm.js duplicates visible rows during upward touch scroll;
- repeated xterm scrollback patches have diminishing returns.
- transcript capture currently depends on WebSocket lifetime;
- output generated while no WebSocket viewer is connected is not captured.

The product goal requires daemon/session-lifetime recording, not
viewer-lifetime recording. Before changing PTY read ownership, audit current
ownership and adapter capabilities.

## E8f2-pre scope

Audit only:

- current `HandleWS` stream ownership;
- current `stream.Read()` location;
- current `ActivityBuffer.Append()` ownership;
- live output vs snapshot/history/replay paths;
- adapter capability matrix;
- recorder insertion point recommendation;
- session lifecycle and cleanup policy;
- late subscriber behavior;
- replay append rule;
- terminal input ownership policy;
- slow subscriber/backpressure policy.

Avoid:

- recorder implementation;
- runtime behavior changes;
- WebSocket behavior changes;
- transcript UI changes;
- LTE / remote connectivity changes;
- daemon restart persistence;
- semantic grouping;
- readability polish.

## E8f2-pre completion statement

Use this format:

```text
E8f2-pre ownership audit complete.

Commit: <sha>

Changed:
- docs/E8F2_RECORDER_PLAN.md or dedicated audit doc

Audit covers:
- HandleWS stream ownership: ...
- adapter matrix: ...
- recorder insertion point: ...
- replay append rule: ...
- subscriber/backpressure policy: ...
- lifecycle/session cleanup policy: ...

Not included:
- recorder implementation
- runtime behavior changes
- WebSocket behavior changes
- transcript UI changes
```

---

Historical E8e/E8f/E8g guidance below remains useful context, but E8f2-pre is
now the immediate target.

## E8e scope

Implement documentation only:

- define transcript read mode;
- define live terminal vs transcript responsibilities;
- define unsupported/degraded terminal semantics;
- define why this is not a custom terminal emulator;
- define E8f/E8g/E8h follow-up phases.

Avoid:

- full transcript renderer implementation;
- backend stream rewrite;
- WebSocket contract changes;
- new terminal backend;
- new tunnel manager;
- new daemon bind flag;
- new xterm scrollback patch;
- claiming final terminal duplication fix.

## Required architecture statement

Use this responsibility split:

```text
xterm.js:
  live interactive bottom terminal
  current prompt/input
  live ANSI/TUI behavior
  source of truth for interactive terminal state

Pokit transcript renderer:
  mobile historical reading
  stable scroll UX
  read-only transcript
  future search/copy/summarize/collapse affordances
  read projection over captured activity events
```

Transcript Mode is not a terminal emulator. It is a read projection. Transcript
capture/render failure must never break live terminal interaction.

## E8f capture model direction

Do not design E8f as raw text lines only. Terminal text is the first payload,
but the durable model should be activity-oriented.

Important:

```text
ActivityEvent is the durable direction.
It is not the full E8f MVP implementation scope.
```

Preferred shape:

```ts
ActivityEvent {
  id: string
  sessionId: string
  seq: number
  timestamp: string
  source: "terminal" | "agent" | "system"
  type:
    | "terminal_output"
    | "terminal_input"
    | "agent_message"
    | "tool_call"
    | "approval_request"
    | "artifact"
    | "error"
    | "status"
  body: {
    text?: string
    stream?: "stdout" | "stderr"
    command?: string
    toolName?: string
    approvalId?: string
    artifactRef?: string
  }
  presentation?: {
    severity?: "info" | "warning" | "error"
    collapsed?: boolean
    degraded?: boolean
  }
  rawRef?: string
}
```

Rules:

- ordered by explicit sequence, not UI scroll position;
- terminal output may start as plain text;
- E8f MVP supports only terminal activity subset:
  - `terminal_output`;
  - `terminal_input` if safely available;
  - `system`;
  - `status` / degraded state if needed;
- tool / approval / artifact events are future-only unless they can link to
  existing A7/A9 contract data without new parsing;
- E8f must not duplicate Common Event or Interaction Contract semantics;
- E8f may reference existing events by id/ref, but must not redefine their
  meaning;
- do not parse ANSI beyond plain text and optional basic styling;
- do not emulate cursor movement, alternate screen, or scroll regions.

E8f MVP acceptance should require:

- minimal ActivityEvent-compatible schema;
- sequence ordering;
- terminal output capture;
- terminal input capture only if safely available;
- degraded/system/status event when capture fails;
- read-only, non-authoritative storage/projection contract;
- no mobile Transcript Mode UI implementation;
- no tool / approval / artifact parser implementation;
- no VT100 emulation;
- no backend/WebSocket behavior regression.

## E8g MVP constraint

When E8g starts, do not merge transcript and xterm into one continuous scroll
surface.

Use two explicit modes:

```text
Live Terminal Mode:
  xterm.js only
  current prompt/input
  full terminal behavior

Transcript Mode:
  read-only history
  mobile-native list rendering
  explicit Return to Live Terminal action
```

E8g MVP must avoid:

- shared scroll container between transcript and xterm;
- boundary-line stitching;
- off-by-one synchronization logic;
- ANSI/cursor/progress state transfer between modes;
- claiming that transcript mode is a terminal emulator.
- treating transcript mode as the source of truth for live terminal state.

Acceptance for E8g should require reliable mode switching, stable transcript
scrolling, and safe return to the live prompt. It should not require seamless
scroll continuity.

## Explicitly unsupported in transcript mode

- vim / nano / less / htop / top;
- alternate screen;
- cursor movement;
- scroll region;
- full-screen TUI;
- mouse interaction;
- complex ANSI behavior.

## Completion statement

Use this format:

```text
E8e scope update complete.

Commit: <sha>

Changed:
- ...

Validation:
- build gate: ...
- docs define live terminal vs transcript responsibilities: ...
- docs define unsupported/fallback behavior: ...

Not included:
- transcript renderer implementation
- backend/WebSocket contract changes
- xterm scrollback patch
- final terminal duplication fix
```
