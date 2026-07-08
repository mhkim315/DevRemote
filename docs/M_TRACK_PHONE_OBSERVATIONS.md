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
- The user suspects the terminal reloads or replays a whole data block during
  scroll, and the replay is appended as duplicate output.
- This was observed while using terminal input.

Impact:
- The user cannot tell whether the agent repeated itself, the terminal replayed
  old output, or the mobile UI duplicated frames.
- The duplication is severe enough that the conversation cannot be followed.

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

### M-OBS-004 — Codex activity is not segmented like Claude activity

The user reports this existed before the recent refactor:

- Claude creates separate question/answer bubbles.
- Codex appears to collect everything into one large bubble.

Impact:
- The P2 feed taxonomy may be visually implemented but not product-visible for
  Codex if the Codex event source is not segmented into useful common events.

Needs investigation:
- compare `/api/sessions.Events` for Claude vs Codex;
- verify whether Codex produces distinct `user_message`, `assistant_message`,
  `thinking`, `tool_call_started`, and `tool_call_finished` events;
- verify whether the issue is parser/event segmentation rather than mobile
  bubble rendering.

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

## Triage recommendation

Before more manual UX polish, add an execution-verifiable diagnostic slice:

```text
E8 — Runtime Interaction Reliability Diagnostics
```

Priority:
1. Terminal scroll duplication: Activity is single-render, Terminal duplicates
   severely on scroll.
2. Input delivery acknowledgement and failed-send state.
3. Codex event segmentation vs Claude event segmentation.

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
- add a fixture/demo session that renders P2 EventBubble categories without a
  physical daemon.

Remaining manual validation:
- reproduce on same Wi-Fi;
- reproduce on physical device with real daemon;
- capture screen recording if possible;
- confirm whether issue occurs on Terminal tab, Activity tab, or both.
