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
E8e — Transcript Read Mode Scope
```

## Mission

Document the architecture pivot:

```text
xterm.js remains live interactive terminal.
Pokit transcript renderer handles mobile historical reading.
```

Do not implement the full transcript renderer yet.

## Why

Validated evidence now shows:

- backend replay is not the mobile idle-scroll cause;
- WebSocket reconnect is not the mobile idle-scroll cause;
- terminal data integrity is preserved;
- desktop xterm behaves correctly;
- Android WebView + xterm.js duplicates visible rows during upward touch scroll;
- repeated xterm scrollback patches have diminishing returns.

The product should avoid relying on xterm.js as the mobile history reader.

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

Pokit transcript renderer:
  mobile historical reading
  stable scroll UX
  read-only transcript
  future search/copy/summarize/collapse affordances
```

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

