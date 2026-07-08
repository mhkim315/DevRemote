# E8 Runtime Interaction Reliability Bug Report

Date: 2026-07-08
Source: product-owner phone testing and manual observation.

## Purpose

This document is the execution handoff for the next slice:

```text
E8 — Runtime Interaction Reliability Diagnostics and Fixes
```

The goal is not visual polish. The goal is to make terminal/activity runtime
behavior reliable enough that the user can trust what they see and what they
send.

Manual observations are not by themselves implementation proof. The executor
must convert these observations into source-code inspection, reproducible tests,
runtime instrumentation, and documented evidence.

## Current accepted baseline

- Platform A5-A10: complete.
- Product P1a: accepted.
- Product P2: accepted.
- E6 no-login local test branch: accepted.
- E7 local installer / packaging: accepted.

Execution and manual validation are intentionally separated:

- E-track accepts objectively provable source/test/build/runtime evidence.
- M-track covers physical-device UX, onboarding quality, first-time pairing,
  real push, visual polish, and subjective usability.

Do not block E8 because physical-device validation remains. Do block E8 if the
runtime paths below cannot be objectively proven or fixed.

## P0 — Terminal scroll duplication

### Observation

Terminal output duplicates severely.

Specifics:
- Activity tab prints the same content once.
- Terminal tab duplicates output heavily.
- Duplication appears after scrolling up or down in the terminal.
- In a severe case, duplicate content expanded so much that the user could not
  scroll back to the latest message.
- Pressing refresh returns the terminal viewport to the bottom.
- This was observed while using terminal input.

### Why this matters

This makes the terminal unusable. The user cannot tell whether:

- the agent repeated itself;
- the terminal replayed old output;
- the WebView duplicated frames;
- history and live stream were appended twice.

### Likely areas

Inspect:

- mobile terminal WebView bridge;
- xterm buffer write path;
- terminal scroll handler;
- terminal refresh handler;
- history loading path;
- live WebSocket append path;
- first render / reconnect / resize behavior;
- backend `ReadScreen` / `ReadHistory` / `/term/ws` behavior.

### Required execution evidence

Executor must provide at least one of:

- regression test proving history + live stream are not appended twice;
- instrumentation showing unique frame IDs / append counts;
- before/after runtime log proving scroll does not replay already-rendered
  terminal content;
- code-level explanation proving scroll cannot trigger duplicate append.

### Acceptance

- Scrolling Terminal does not append duplicate historical content.
- Refresh may reload, but does not duplicate already-rendered content.
- User can scroll back to the latest terminal position after viewing history.
- Activity tab remains unaffected.

## P0 — Input delivery reliability and failed-send state

### Observation

Network instability can cause messages to disappear or become ambiguous.

Observed modes:

1. Message appears in mobile UI but no assistant response appears.
2. Later, the assistant may receive combined content as one message.
3. Message does not appear in mobile UI and the next send does not recover it.
4. The issue appears from both Terminal and Activity usage, so it is not yet
   tab-specific.

### Why this matters

The user cannot distinguish:

- locally rendered input;
- backend-delivered input;
- agent-processed input;
- failed/unsent input.

Silent input loss is a product blocker.

### Likely areas

Inspect:

- Activity input path;
- Terminal input path;
- mobile send button / keyboard submit path;
- WebSocket state at send time;
- API request failure handling;
- retry/reconnect behavior;
- local optimistic render logic;
- backend acknowledgement or absence of acknowledgement.

### Required execution evidence

Executor should add or prove:

- send attempt logging with session ID and transport path;
- explicit success/failure acknowledgement;
- visible failed-send or pending-send state;
- no silent drop when network/WebSocket/API is disconnected;
- test or runtime evidence for disconnected send behavior.

### Acceptance

- If send fails, user sees failure.
- If send is pending, user sees pending.
- If send succeeds, UI state is consistent with backend delivery.
- Activity and Terminal input paths have clear transport/error boundaries.

## P1 — Activity message boundary and authorship

### Observation

Activity input can appear as the user's right-aligned bubble, then move into or
appear inside the assistant bubble when the assistant response arrives.

Additional observation:
- Earlier assumption was that Claude separated bubbles correctly and Codex did
  not.
- Updated observation is less certain: both Claude and Codex may collapse later
  messages into one bubble.
- The first input may be the only clearly separated one.

### Why this matters

Message authorship becomes untrustworthy. The user cannot tell what they wrote
versus what the agent answered.

### Likely areas

Inspect:

- optimistic local user message IDs;
- server event IDs;
- Activity event grouping logic;
- timestamp/session grouping rules;
- first input vs later input path;
- full feed refresh reconciliation;
- `/api/sessions.Events` payloads for Claude and Codex.

### Required execution evidence

Executor must compare:

```text
tmux + Claude /api/sessions.Events
tmux + Codex /api/sessions.Events
cmux + Claude or Codex /api/sessions.Events
```

Check:
- event count;
- event type sequence;
- user vs assistant authorship;
- whether user and assistant content is already merged before mobile rendering;
- whether mobile grouping merges distinct events.

### Acceptance

- User input remains a user bubble.
- Assistant output remains an assistant/agent bubble.
- Optimistic user messages reconcile with server events without being absorbed
  into assistant content.
- First input and later inputs follow the same boundary rules.

## P1 — cmux-specific Activity and Terminal degradation

### Observation

Important adapter difference:

```text
tmux:
- Activity bubbles appear to separate normally.
- Terminal is colorful.

cmux:
- Activity prints all messages as one combined block.
- Terminal appears monochrome / no color.
```

### Why this matters

This narrows the problem. Mobile UI and P2 taxonomy have at least one working
path through tmux. cmux likely loses structure, ANSI color, or event boundaries
before mobile rendering.

### Likely areas

Inspect:

- cmux adapter stream;
- cmux history/screen capture;
- ANSI escape preservation/stripping;
- cmux parser/event source;
- adapter capabilities;
- differences between tmux and cmux `ReadScreen`, `ReadHistory`, and stream
  behavior.

### Required execution evidence

Compare tmux vs cmux:

- raw terminal stream sample;
- screen/history output sample;
- ANSI escape presence;
- `/api/sessions.Events` event type sequence;
- mobile payload received by Activity.

### Acceptance

- If cmux cannot provide structured activity, it must degrade explicitly rather
  than pretending all output is agent answer.
- If cmux strips color by design, capability/status should make that clear.
- If color or event structure should be preserved, regression tests should prove
  preservation.

## P1 — Terminal command output labeled as agent answer

### Observation

When the user enters a shell command such as `ls`, Activity shows the result as
an agent answer.

### Why this matters

Shell output and AI-agent output are semantically different. Labeling terminal
stdout as agent answer makes Activity misleading.

### Required execution evidence

Inspect whether events distinguish:

- user terminal input;
- shell command output;
- agent user message;
- agent assistant message;
- tool result;
- raw terminal output.

### Acceptance

- Activity does not label plain terminal command output as an agent answer.
- If terminal stdout is shown in Activity, it uses a distinct category such as
  Terminal Output / Command Output.
- No vendor-specific mobile branch is introduced.

## P2 — tmux initial terminal hydration gap

### Observation

tmux has a first-entry / first-Claude-start issue:

- Activity bubble segmentation appears better than cmux.
- Terminal color appears normal.
- But on first entry or first Claude start, the top/initial terminal output may
  be missing.
- A normal terminal would show prompt/current directory or initial state.
- tmux screen can look empty, so the user cannot tell whether execution started.
- After the user types a terminal command, it works normally.

### Likely areas

Inspect:

- first attach screen snapshot;
- `ReadScreen` result;
- `ReadHistory` result;
- first WebSocket frame;
- mobile terminal initial render;
- terminal fit/clear behavior on first render;
- attach timing relative to Claude startup.

### Acceptance

- First terminal entry shows enough initial state to know the session is alive.
- Initial prompt/current screen is not cleared by mobile render.
- If no initial content exists, UI should show an explicit connected/empty state.

## P2 — Mobile terminal layout and wrapping

### Observation

Terminal and Activity wrap too aggressively on mobile.

Specifics:
- Terminal is forced into narrow mobile width.
- Activity is easier to read but still loses width to avatar/profile and time
  chrome.
- Terminal should feel like a desktop terminal canvas that can be panned or
  pinch-zoomed, not reflowed into a chat-width column.

### Dependency

Do not implement major terminal pan/zoom until P0 terminal duplication is fixed
or isolated. More scroll interaction may worsen duplication until the append bug
is corrected.

### Acceptance for later slice

- Terminal canvas can preserve desktop-like width.
- User can pan/scroll/zoom without duplicating output.
- Activity reduces nonessential horizontal chrome.

## P3 — Terminal discovery / agent attach flow

### Product observation

When adding an agent, the user should first choose which terminal/backend to use.

Desired flow:

1. Scan user's PC terminal/session backends.
2. Show available tmux, cmux, localpty, and future backends.
3. Show backend/session capability:
   - available;
   - not installed;
   - permission needed;
   - degraded;
   - observe-only;
   - control-capable.
4. User chooses terminal/session context.
5. Then user chooses or attaches agent.

### Not E8 blocker

This is a product direction, not part of the immediate E8 reliability fix.
Likely future slice:

```text
E9 — Terminal Discovery / Agent Attach Flow
```

## Recommended E8 execution order

1. Reproduce and fix Terminal scroll duplication.
2. Add send attempt / ack / failed-send diagnostics.
3. Compare tmux vs cmux raw streams, history, screen, and event payloads.
4. Fix Activity authorship/boundary reconciliation.
5. Fix or explicitly degrade cmux event/color capabilities.
6. Add tmux first-entry hydration proof.

## E8 non-goals

Do not spend E8 on:

- visual polish;
- first-time onboarding quality;
- real push delivery;
- physical-device-only validation;
- installer packaging;
- new agents;
- new terminal backends;
- E9 terminal discovery flow.

## Suggested validation commands

Run available deterministic checks:

```sh
sh scripts/build-gate.sh
```

Add targeted tests or diagnostics for:

```text
terminal history + live append dedupe
terminal scroll refresh behavior
send failure state
Activity event grouping
tmux vs cmux event payload comparison
```

## Expected completion statement

Executor should report:

```text
E8 implementation complete.

Changed:
- ...

Fixed:
- terminal scroll duplication: ...
- input send ack/failure: ...
- Activity authorship boundary: ...
- cmux/tmux comparison: ...

Validation:
- build gate: ...
- targeted regression tests: ...
- runtime evidence: ...

Remaining M-track:
- physical-device UX
- onboarding quality
- real push/tap behavior
- visual polish
```
