# E8f2-pre / E8f2 — Session-level Activity Recorder Plan

Date: 2026-07-08

## Status

Accepted:

```text
E8f2-pre — Stream / PTY Ownership Audit
E8f2 — Session-owned Activity Recorder
```

E8f2 accepted at commit `ee1daf0`.

Verified:

- `go test -race ./internal/term -run TestRecorder_MultipleSubscribers -count=50`
- `go test -race ./internal/term -count=5`
- `go test -race ./... -count=1`
- `sh scripts/build-gate.sh`

Current next phase:

```text
R1a — Connectivity Baseline for LTE validation
```

## Historical limitation fixed by E8f2

Before E8f2, ActivityBuffer captured PTY output only through
HandleWS → stream.Read(). That was tied to WebSocket lifetime:

- WebSocket connected → capture works
- WebSocket disconnected → no capture
- Multiple WebSocket viewers → each reads independently (potential duplication)

E8f2 replaced this with a session-owned recorder.

## Accepted architecture

```
Session PTY
  ↓
SessionRecorder (one per session, daemon-lifetime)
  ↓
ActivityBuffer (append-only, monotonic seq)
  ↓
?activity=<sessionId> (available to any client)
```

WebSocket clients become SUBSCRIBERS to the recorder's output channel,
not owners of the read loop.

## Roadmap

```text
E8f2-pre
→ Stream / PTY Ownership Audit ✅ ACCEPTED

E8f2
→ Session-owned Activity Recorder ✅ ACCEPTED

R1a
→ Connectivity Baseline for LTE validation

E8g2
→ Transcript Readability Polish

E8i
→ Real-device Transcript Validation

R1b
→ Connectivity Product Polish
```

R1a intentionally comes before E8g2 because real-device validation will happen
outside the local network. Transcript readability polish should happen after
recorder correctness and basic LTE reachability are both available.

## R1a connectivity baseline

R1a is not final remote connectivity product polish. It is the minimum
connectivity work needed to keep real-device validation productive while the
phone is on LTE or otherwise outside the daemon's local network.

In scope:

- validate stored `BASE_URL` before marking the app connected;
- distinguish daemon unreachable from an empty session list;
- distinguish sessions API failure from zero sessions;
- show terminal WebSocket connection failure clearly;
- show transcript/activity read failure clearly;
- support an LTE-capable validation route such as a tunnel URL or equivalent
  manual remote URL;
- keep diagnostics sufficient to classify failure as auth, daemon reachability,
  sessions API, WebSocket, or transcript read path.

Out of scope:

- final pairing/onboarding UX;
- push notification delivery;
- installer/package changes;
- production-grade tunnel automation;
- login flow redesign;
- transcript readability polish;
- recorder ownership rewrites unless R1a evidence proves an E8f2 regression.

## E8f2-pre scope

Audit only.

In scope:

- current ownership clarification;
- recorder insertion point analysis;
- reader ownership analysis;
- replay / snapshot / history ownership analysis;
- adapter capability matrix;
- E8f2 implementation recommendation.

Out of scope:

- runtime behavior changes;
- recorder implementation;
- WebSocket behavior changes;
- transcript UI changes;
- LTE / remote connectivity changes;
- persistence across daemon restart;
- durable disk transcript storage;
- semantic grouping;
- transcript readability polish;
- adapter lifecycle rewrite.

## E8f2-pre required audit

### 1. Current HandleWS ownership

Document the current path:

```text
WebSocket connect
→ initial screen / snapshot / history bootstrap, if any
→ stream.Read(...)
→ ActivityBuffer.Append(...)
→ outbound WebSocket write
```

The audit must identify:

- where `stream.Read()` occurs;
- who owns PTY reading today;
- where `ActivityBuffer.Append()` occurs;
- current replay path;
- current history/snapshot path;
- which path appends to ActivityBuffer;
- which path only displays/bootstrap data;
- which path can replay old bytes.

### 2. Adapter capability matrix

For every supported adapter, including at least:

- `localpty`;
- `tmux`;
- `cmux`;
- fixture/test adapters if relevant.

Document:

- whether stream reading is single-reader or multi-reader;
- whether stream can be opened without a WebSocket;
- what happens on a second stream open;
- screen support;
- history support;
- replay capability;
- stream replacement / adapter restart behavior;
- safest MVP recorder attachment point.

### 3. Ownership proposal

The target ownership model remains:

```text
One Session
  ↓
One Recorder
  ↓
ActivityBuffer
  ↓
N WebSocket subscribers
```

Not:

```text
N WebSocket handlers
  ↓
stream.Read()
  ↓
ActivityBuffer
```

The audit must explicitly state whether this model is feasible for each adapter.

### 4. Lifecycle policy

Define the recommended MVP policy for:

- recorder start condition;
- recorder stop condition;
- session delete/end cleanup;
- same-ID session recreation;
- seq reset vs seq continuation;
- adapter restart / stream replacement;
- daemon restart, explicitly out of scope.

Recommended default unless audit proves otherwise:

```text
session end/delete
→ recorder stop
→ subscribers close
→ ActivityBuffer clear

same ID recreated later
→ new recorder
→ seq may reset
```

### 5. Subscriber policy

Define:

- late subscriber behavior;
- whether WebSocket receives only live bytes from attachment point onward;
- whether historical data is read through `?activity=<sessionId>`;
- slow subscriber / backpressure policy;
- subscriber disconnect cleanup.

Recommended default:

```text
late subscriber
→ receives live output from attachment point onward
→ reads historical transcript through ?activity=<sessionId>
→ WebSocket live channel does not replay ActivityBuffer automatically
```

### 6. Replay ownership

The audit must explicitly preserve this rule:

```text
PTY live bytes
→ recorder
→ ActivityBuffer append

snapshot / history / replay bytes
→ display/bootstrap only
→ must not append as new ActivityEvent seq
```

### 7. Input ownership

Output ownership changes in E8f2, but input metadata can remain WebSocket-owned.

The audit must define:

```text
terminal_output
→ recorder-owned

terminal_input metadata
→ WebSocket handler-owned acceptable

terminal_input raw text
→ never stored
```

### 8. Risk analysis

Cover:

- PTY ownership;
- multiple viewers;
- duplicate reads;
- output loss;
- race conditions;
- replay semantics;
- late subscriber attachment;
- subscriber backpressure;
- ActivityBuffer ownership;
- memory/capacity growth;
- session cleanup;
- session recreation;
- adapter differences.

### 9. Implementation recommendation

The audit must end with a concrete E8f2 implementation recommendation:

- files likely affected;
- minimal change path;
- known risks;
- tests required;
- adapters supported in the first implementation slice;
- fallback for adapters that cannot support recorder-owned reads yet.

## E8f2 implementation requirements

The following are implementation requirements for the later E8f2 phase, not
E8f2-pre.

1. Move PTY read loop out of HandleWS into a session-scoped recorder.
2. WebSocket clients receive from the recorder's broadcast channel.
3. ActivityBuffer is fed by the recorder, not by individual WebSocket handlers.
4. Recorder starts when session starts, stops when session ends.
5. Multiple WebSocket viewers share one PTY reader.
6. Reconnect does not replay old output as new seq.

## Non-goals for E8f2-pre / E8f2

- Poll-based screen capture (PTY stream is preferred)
- Diff-based content tracking
- broad session lifecycle management changes
- Transcript UI changes
- LTE / remote connectivity changes
- persistence across daemon restart
- durable disk transcript storage
- semantic grouping
- transcript readability polish

## E8f2-pre acceptance

E8f2-pre can be accepted when the audit document covers:

1. Current `HandleWS` stream ownership diagram.
2. Current live output / snapshot / history / replay path separation.
3. Current `ActivityBuffer.Append()` ownership.
4. Adapter capability matrix for localpty, tmux, cmux, and relevant test adapters.
5. Stream open feasibility without WebSocket.
6. Single-reader vs multi-reader behavior per adapter.
7. Recommended recorder insertion point.
8. Session recreation policy.
9. Late subscriber behavior.
10. Replay/snapshot/history append rule.
11. Terminal input ownership policy.
12. Slow subscriber / backpressure policy.
13. Recorder lifecycle policy.
14. Session cleanup policy.
15. Concrete E8f2 implementation recommendation.
16. Required E8f2 tests.

No runtime behavior changes should be included in E8f2-pre.

## E8f2 implementation acceptance

1. Start session with no WebSocket attached.
2. Generate output.
3. Connect mobile later.
4. ?activity=<sessionId> returns output from step 2.
5. No duplicate reads from multiple WebSocket connections.
6. Existing live streaming still works.
