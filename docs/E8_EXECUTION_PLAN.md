# E8 Execution Plan — Transcript Read Mode Pivot

Date: 2026-07-08

## Current accepted evidence

Accepted / established:

- A5~A10 platform foundation.
- P1a Live Dashboard Core.
- P2 Core Feed Taxonomy.
- E6 no-login local test branch.
- E8 instrumentation package.
- E8a connection-state correctness direction.
- Desktop runtime E8DIAG evidence.
- Desktop reconnect / PTY replay evidence.
- Mobile WebView evidence showing scroll-up visual duplication while counters
  remain unchanged.

Current technical conclusion:

```text
Backend replay: ruled out for mobile idle-scroll duplication.
WebSocket reconnect: ruled out for mobile idle-scroll duplication.
Terminal data integrity: preserved.
Desktop xterm scroll: behaves correctly.
Mobile Android WebView + xterm.js: visual row duplication occurs during upward touch scrolling.
```

Failed or insufficient patch attempts:

- fitTerminal debounce;
- viewport.invalidate();
- write buffering;
- refresh / repaint strategies;
- CSS / user-scalable experiments.

## Product / architecture pivot

Stop patching xterm.js mobile scrollback.

Pokit is an agent cockpit, not a generic remote terminal app. On mobile, the
history-reading experience should not depend on xterm.js scrollback when the
evidence points to Android WebView/xterm viewport rendering instability.

New principle:

```text
Do not build a custom terminal emulator.
Split responsibilities.
```

Responsibilities:

```text
xterm.js
→ live interactive terminal
→ current prompt / input / live ANSI behavior
→ bottom/live portion

Pokit transcript renderer
→ mobile historical reading
→ stable scroll/read UX
→ search/copy/collapse/summarize-ready output model
```

This is not a full terminal renderer. It is a read mode for past output.

## Revised E8 sequence

### E8e — Transcript Read Mode Scope

Goal:

```text
Define the mobile-only transcript/read-mode architecture before implementation.
```

Acceptance:

- new architecture documented;
- live terminal vs transcript responsibilities defined;
- unsupported/degraded terminal semantics defined;
- no custom VT100 emulator scope;
- no backend replay/WebSocket contract change;
- next implementation phases defined.

Implementation in E8e:

- documentation only;
- no full transcript renderer;
- no backend stream rewrite;
- no new terminal backend;
- no further xterm scrollback patching.

### E8f — Terminal Output Capture Model

Goal:

```text
Define and implement the minimum output model needed for read mode.
```

Possible model:

```ts
TranscriptLine {
  id: string
  sessionId: string
  timestamp: string
  text: string
  kind: "stdout" | "stderr" | "input" | "system"
  spans?: BasicAnsiSpan[]
}
```

Scope guard:

- line-oriented, read-only output;
- plain text first;
- basic ANSI color/style only if cheap;
- do not emulate cursor movement, alternate screen, or scroll regions.

### E8g — Mobile Read Mode UI

Goal:

```text
Provide stable mobile history reading without xterm scrollback.
```

MVP architecture:

```text
Do not merge transcript and xterm into one continuous scroll surface.
Use explicit modes.
```

Mode responsibilities:

```text
Live Terminal Mode
→ xterm.js only
→ current interaction / current prompt
→ full terminal behavior
→ no transcript renderer participation

Transcript Mode
→ read-only historical output
→ mobile-optimized list rendering
→ explicit Return to Live Terminal action
→ no terminal input or control surface
```

Expected behavior:

- xterm remains available for live interaction;
- scrolling upward may offer or enter transcript/read mode, but does not stitch
  transcript rows into xterm scrollback;
- transcript uses native/mobile list rendering, not xterm scrollback;
- “Return to Live” affordance exists;
- live input remains in Live Terminal Mode;
- no duplicate visible rows during read-mode scrolling.

Explicit MVP non-goals:

- no seamless transcript/xterm boundary synchronization;
- no shared scroll container between transcript and xterm;
- no attempt to align transcript boundary rows with the live xterm viewport;
- no duplicate/missing boundary-line reconciliation logic;
- no ANSI state transfer from transcript mode back into xterm;
- no cursor/progress/spinner continuity across modes.

Reason:

Seamless scroll integration would introduce a new synchronization problem that
is separate from the original Android WebView/xterm duplication bug:

- duplicate boundary lines;
- missing boundary lines;
- off-by-one alignment;
- ANSI state mismatch;
- cursor/progress mismatch;
- live viewport alignment regressions.

For the MVP, these risks are higher than the product value of a seamless scroll
surface. Pokit should optimize for reliable mobile reading and safe return to
the live prompt, not terminal-emulator purity.

### E8h — Fallback / Compatibility

Goal:

```text
Handle terminal semantics transcript mode cannot represent.
```

Unsupported/degraded in transcript mode:

- vim / nano / less / htop / top;
- alternate screen;
- cursor movement;
- scroll region;
- full-screen TUI;
- mouse interaction;
- complex ANSI behavior;
- wide-character layout edge cases beyond the chosen renderer contract.

Fallback behavior:

- keep live xterm terminal available;
- show “Full Terminal Mode recommended” or equivalent for unsupported output;
- transcript degradation must not break live terminal;
- transcript inaccuracies must be isolated from backend/WebSocket/session state.

## Explicit non-goals

Do not:

- build a custom VT100/xterm emulator;
- replace xterm.js for live interactive terminal use;
- change backend replay/WebSocket contracts for E8e;
- add a new terminal backend;
- add a new tunnel manager;
- weaken `--insecure-local-only`;
- continue patching xterm mobile scrollback as the primary strategy.

## Acceptance rule going forward

E8 terminal-scroll acceptance should no longer require xterm.js mobile scrollback
to become reliable.

Instead, acceptance should require:

- live terminal remains usable for current interaction;
- historical reading uses transcript/read mode on mobile;
- transcript and live terminal are explicit modes in E8g MVP;
- no seamless scroll-surface or boundary-sync claim is made in E8g MVP;
- unsupported terminal semantics degrade clearly;
- backend data integrity remains unchanged;
- no vendor-specific mobile behavior branch is introduced.

## Long-term architecture note

Seamless transcript/xterm boundary synchronization should not become a default
product goal. It may be revisited only as a separate research phase after the
mode-switch MVP is proven, and only if there is strong product evidence that
users need a unified scroll surface more than they need reliability.

Any future seamless-scroll proposal must prove:

- no duplicate or missing boundary rows;
- no live prompt/input regression;
- no ANSI/cursor/progress mismatch that misleads users;
- no backend/WebSocket contract expansion solely to support presentation;
- graceful fallback to explicit mode separation.
