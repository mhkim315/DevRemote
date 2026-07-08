# M-track Phone Observations

Date: 2026-07-08
Context: product owner testing from phone on LTE, not same Wi-Fi as daemon.

These observations are manual product validation notes. They do not by
themselves reject E-track implementation, but they must be triaged before alpha.

## Observed issues

### M-OBS-001 — Terminal output duplicates repeated text

The terminal view showed the same text repeated multiple times.

Impact:
- The user cannot tell whether the agent repeated itself, the terminal replayed
  old output, or the mobile UI duplicated frames.

Needs investigation:
- WebSocket frame replay / reconnect behavior
- terminal buffer append logic
- xterm/WebView rendering behavior
- history + live stream merge behavior

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

## Triage recommendation

Before more manual UX polish, add an execution-verifiable diagnostic slice:

```text
E8 — Runtime Interaction Reliability Diagnostics
```

Suggested E8 scope:
- instrument terminal output frame IDs or append counts;
- log mobile send attempts with session ID and transport path;
- expose last input/send error in debug UI or diagnostic endpoint;
- add regression test for history + live stream duplication if reproducible;
- add a fixture/demo session that renders P2 EventBubble categories without a
  physical daemon.

Remaining manual validation:
- reproduce on same Wi-Fi;
- reproduce on physical device with real daemon;
- capture screen recording if possible;
- confirm whether issue occurs on Terminal tab, Activity tab, or both.
