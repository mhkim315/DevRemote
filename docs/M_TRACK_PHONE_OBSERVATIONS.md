# M-track Phone Observations

Date: 2026-07-08
Context: product owner testing from phone on LTE, not same Wi-Fi as daemon.

These observations are manual product validation notes. They do not by
themselves reject E-track implementation, but they must be triaged before alpha.

## Observed issues

### M-OBS-001 — Terminal output duplicates repeated text

The terminal view showed the same text repeated multiple times.

Updated observation:
- Activity tab prints the content once.
- Terminal tab duplicates output heavily.
- The duplication appears after the user scrolls up or down in the terminal.
- Pressing refresh returns the terminal viewport to the bottom.
- Earlier, the duplication was severe enough that scrolling could not return the
  user to the latest message because duplicate terminal content kept expanding.
- The user suspects the terminal reloads or replays a whole data block during
  scroll, and the replay is appended as duplicate output.
- This was observed while using terminal input.

Impact:
- The user cannot tell whether the agent repeated itself, the terminal replayed
  old output, or the mobile UI duplicated frames.
- The duplication is severe enough that the conversation cannot be followed.
- In the severe case, the user cannot recover normal position by scrolling.

Needs investigation:
- WebSocket frame replay / reconnect behavior
- terminal buffer append logic
- xterm/WebView rendering behavior
- history + live stream merge behavior
- terminal scroll handler / viewport resize behavior
- terminal refresh behavior and scroll position reset;
- whether scroll triggers history reload or terminal re-render;
- whether WebView receives the same terminal payload multiple times;
- whether terminal input path and terminal output append share a replay buffer;
- why Activity tab remains single-render while Terminal tab duplicates.

### M-OBS-002 — Activity appears as one large block instead of state/category bubbles

The product expectation was that responses would be separated by state/category
such as message, thinking, tool, interaction, error, and completed.

Current observed behavior:
- content appears as one combined block;
- the user does not see the intended P2 feed taxonomy in the visible experience.

Needs investigation:
- whether the user was viewing Terminal tab instead of Activity tab;
- whether `/api/sessions.Events` contains categorized events for the session;
- whether mobile is rendering `EventBubble` for live sessions;
- whether agent parser events are missing for this runtime path.

### M-OBS-003 — Input was entered but did not reach the agent

The user typed input, but it was not delivered to the agent conversation.

Impact:
- This is a critical interaction reliability issue if reproduced.

Needs investigation:
- whether the input was sent through terminal WebSocket or interaction request;
- WebSocket connection state at the time of input;
- mobile send button / keyboard submit path;
- terminal focus and WebView input bridge;
- backend write path to the selected session;
- whether LTE/no-daemon environment caused a false observation.

### M-OBS-004 — Agent activity segmentation may collapse after initial input

The earlier working hypothesis was:

- Claude creates separate question/answer bubbles.
- Codex appears to collect everything into one large bubble.

Updated observation:
- The user is no longer confident this is Claude-vs-Codex specific.
- Current observation suggests Claude may also collect later content into one
  bubble.
- The first input may be the only one that appears clearly separated.
- This is based on user memory and needs objective verification.

Impact:
- The P2 feed taxonomy may be visually implemented but not product-visible if
  agent activity is not segmented into stable common events.
- The issue may be event segmentation, Activity renderer grouping, optimistic
  reconciliation, or server refresh behavior rather than one specific parser.

Needs investigation:
- compare `/api/sessions.Events` for Claude vs Codex;
- verify whether both agents produce distinct `user_message`, `assistant_message`,
  `thinking`, `tool_call_started`, and `tool_call_finished` events;
- verify whether only the first user input is separated;
- verify whether later events are grouped into one bubble by the mobile renderer;
- verify whether this issue is parser/event segmentation, mobile grouping, or
  optimistic message reconciliation.

### M-OBS-005 — Message visible on mobile but no assistant response observed

Before the user sent "살아있어?", a prior message was visible in the mobile UI,
but no assistant response appeared.

Later, the assistant received the combined message as one user message.

Impact:
- The mobile UI may show a sent message before the backend/agent has confirmed
  receipt or processing.
- The user cannot distinguish "sent locally", "delivered to backend", and
  "agent responded".

Needs investigation:
- whether Activity tab input has a local optimistic render path;
- whether backend delivery has an acknowledgement;
- whether message boundaries are preserved when multiple inputs are sent during
  unstable connectivity;
- whether the UI can display pending/sent/failed states.

### M-OBS-006 — Network interruption can silently drop input

The user observed a second failure mode:

- a message was not visible on mobile;
- sending the next message did not recover it;
- the whole input appeared to be lost;
- this happened from both Terminal and Activity usage, so it does not currently
  look tab-specific.

Impact:
- Network interruption may cause silent message loss.
- This is interaction reliability, not visual polish.

Needs investigation:
- send attempt logging from mobile;
- backend acknowledgement for each send;
- WebSocket/API disconnected state at send time;
- reconnect behavior;
- unsent queue or retry affordance;
- visible failed-send state.

### M-OBS-007 — Mobile layout forces excessive line wrapping

The terminal and Activity views both wrap text too aggressively on mobile.

Observation:
- Terminal content is forced into the narrow mobile UI width.
- Activity content also wraps, but it is easier to read than Terminal.
- Chat-style chrome such as left profile/avatar area and right timestamp area
  reduces the usable text width.
- The reduced width causes excessive line breaks.

Impact:
- Terminal output becomes difficult to read.
- Code, command output, and long agent responses lose structure.
- The product feels constrained by chat UI layout rather than optimized for
  terminal/work review.

Desired direction:
- Terminal should preserve a desktop-like terminal canvas instead of forcing
  mobile-width reflow.
- The user should be able to pan/scroll and pinch-zoom the terminal canvas.
- Activity can remain a readable mobile-first feed, but should avoid wasting
  horizontal space with non-essential chrome.

Important dependency:
- Terminal vertical scrolling currently has severe duplication issues, so
  terminal pan/scroll/zoom work should happen after or alongside the E8 terminal
  scroll duplication fix.

### M-OBS-008 — Activity user bubble is absorbed into assistant bubble

When the user types in Activity:

- the message first appears as the user's right-aligned bubble;
- when the assistant response arrives, the user's question moves into or appears
  inside the assistant bubble.

Impact:
- Message authorship becomes incorrect.
- The user cannot trust whether a bubble is their input or the assistant output.
- This suggests message identity, event boundary, optimistic rendering, or
  reconciliation is wrong.

Needs investigation:
- whether optimistic local user messages have stable IDs;
- whether server events later replace the optimistic message instead of merging
  with it incorrectly;
- whether the Activity renderer groups events by timestamp/session too broadly;
- whether Codex event segmentation emits combined user+assistant content;
- whether assistant response arrival triggers a full feed refresh that reorders
  or regroups bubbles incorrectly.

## Triage recommendation

Before more manual UX polish, add an execution-verifiable diagnostic slice:

```text
E8 — Runtime Interaction Reliability Diagnostics
```

Priority:
1. Terminal scroll duplication: Activity is single-render, Terminal duplicates
   severely on scroll.
2. Input delivery acknowledgement and failed-send state.
3. Agent event segmentation and Activity bubble grouping.
4. Terminal/mobile layout reflow and excessive wrapping.
5. Activity optimistic user bubble reconciliation.

Suggested E8 scope:
- instrument terminal output frame IDs or append counts;
- instrument Terminal WebView scroll/history/live-stream merge behavior;
- verify scroll does not append already-rendered terminal data;
- log mobile send attempts with session ID and transport path;
- add a send acknowledgement model or explicit failed-send state;
- distinguish local optimistic render, backend delivery, and agent processing;
- preserve message boundaries across reconnects;
- expose network/WebSocket/API connection state in the session UI;
- expose last input/send error in debug UI or diagnostic endpoint;
- add regression test for history + live stream duplication if reproducible;
- compare Claude vs Codex event segmentation through `/api/sessions.Events`;
- verify whether first input vs later inputs follow different grouping paths;
- verify Activity message IDs, authorship, and optimistic-to-server
  reconciliation;
- add a fixture/demo session that renders P2 EventBubble categories without a
  physical daemon.
- evaluate terminal canvas sizing, pinch-zoom, and pan behavior after terminal
  duplication is fixed.

Remaining manual validation:
- reproduce on same Wi-Fi;
- reproduce on physical device with real daemon;
- capture screen recording if possible;
- confirm whether issue occurs on Terminal tab, Activity tab, or both.
