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
→ source of truth for interactive terminal state

Pokit transcript renderer
→ mobile historical reading
→ stable scroll/read UX
→ search/copy/collapse/summarize-ready output model
→ read projection over captured activity events
```

This is not a full terminal renderer. It is a read projection over captured
activity. The transcript must not become the source of truth for live terminal
state.

## Revised E8 sequence

### E8e — Read Mode Architecture

Goal:

```text
Define the mobile-only transcript/read-mode architecture before implementation.
```

Acceptance:

- new architecture documented;
- live terminal vs transcript responsibilities defined;
- transcript defined as a read projection, not a terminal emulator;
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

### E8f — Activity Capture Pipeline

Goal:

```text
Define and implement the minimum activity model needed for read mode.
```

The capture model should evolve toward structured activity events rather than
raw text lines only. Raw text is allowed as payload, but the contract should not
force the product into a terminal-output-only model.

Important scope constraint:

```text
ActivityEvent is the durable direction.
It is not the full E8f MVP implementation scope.
```

Possible model:

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

Scope guard:

- activity-oriented, read-only projection;
- plain text terminal output is the first supported event payload;
- stdout / stderr / input / system events are minimum terminal sources;
- E8f MVP supports only terminal activity subset:
  - `terminal_output`;
  - `terminal_input` if safely available;
  - `system`;
  - `status` / degraded state if needed;
- tool / approval / artifact events may be linked when already available from
  agent contracts;
- tool / approval / artifact support must stay future-only unless it can reuse
  existing A7/A9 contract data without new parsing;
- E8f must not duplicate Common Event or Interaction Contract semantics;
- E8f may reference existing events by id/ref, but must not redefine their
  meaning;
- ordered by explicit sequence, not UI scroll position;
- plain text first; optional basic ANSI color/style only if cheap;
- do not parse ANSI beyond plain text and optional basic styling;
- do not emulate cursor movement, alternate screen, or scroll regions.

E8f MVP acceptance:

- minimal ActivityEvent-compatible schema exists;
- terminal output can be captured as ordered read events;
- terminal input can be captured only if already safely available;
- capture failures can surface as degraded/system/status events;
- transcript storage/projection remains read-only and non-authoritative;
- no mobile Transcript Mode UI implementation;
- no tool / approval / artifact parser implementation;
- no VT100 emulation;
- no backend/WebSocket behavior regression.

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
→ not a source of truth for live terminal state
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
- transcript capture/render failure must not block live input or live terminal
  rendering.

### E8f2 — Session-owned Activity Recorder ✅

Status: ACCEPTED at `ee1daf0`.

E8f2 corrected the transcript capture lifetime issue discovered during
real-device validation. Activity capture is no longer owned by an individual
WebSocket viewer.

Accepted architecture:

```text
Session
→ one recorder
→ one PTY reader
→ ActivityBuffer append once
→ N WebSocket subscribers
```

Verified:

- `go test -race ./internal/term -run TestRecorder_MultipleSubscribers -count=50`
- `go test -race ./internal/term -count=5`
- `go test -race ./... -count=1`
- `sh scripts/build-gate.sh`

### R1a — Connectivity Baseline for LTE validation

R1a is the next phase after E8f2.

Goal:

```text
Make real-device validation possible when the phone is outside the local network.
```

In scope:

- validate stored `BASE_URL` before connected state;
- distinguish daemon unreachable from empty sessions;
- distinguish sessions API failure from zero sessions;
- expose terminal WebSocket connection failure;
- expose transcript/activity read failure;
- support a known LTE-capable validation route such as a tunnel or manual remote
  URL.

Out of scope:

- final onboarding/pairing UX;
- push delivery;
- production-grade tunnel automation;
- installer/package changes;
- transcript readability polish.

Revised order:

```text
E8f2 ✅
→ R1a Connectivity Baseline
→ E8g2 Transcript Readability Polish
→ E8i Real-device Transcript Validation
→ R1b Connectivity Product Polish
```

## Explicit non-goals

Do not:

- build a custom VT100/xterm emulator;
- replace xterm.js for live interactive terminal use;
- make Transcript Mode a source of truth for live terminal state;
- require Transcript Mode to represent every byte-level terminal mutation;
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
- transcript/read mode is a projection over captured activity events;
- live xterm remains the source of truth for interactive terminal state;
- transcript failure does not break live terminal interaction;
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
