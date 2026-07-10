# M3 — Mobile Session Lifecycle UX Plan

Status: ready after M2.5-4 authentication acceptance
Depends on: M0/M1/M1.5/M2 accepted, M2.5-4 authenticated REST/WS boundary
Does not depend on: Transcript refactor, provider semantic signals, cmux tuning

## Product decision

Replace the legacy “agent profile save” flow with a session lifecycle product.
A mobile user starts and manages daemon-owned `controlled_pty` runtimes; they
do not create or edit arbitrary adapter sessions.

```text
New Session
  → select daemon profile
  → optional display name and approved CWD
  → create controlled_pty
  → server returns recorder-ready canonical session
  → open Live Terminal

Back / close view
  → detach this mobile viewer only

Stop
  → graceful managed process-group stop

Force Kill
  → explicit destructive fallback

Delete History
  → only after terminal state; no process exists
```

These are distinct controls. No screen may label a viewer detach, process Stop,
and history deletion with one ambiguous “Close” or “Delete” action.

## Current-to-target migration

| Current surface | Problem | M3 disposition |
| --- | --- | --- |
| `AgentProfileModal` | models saving `{id, runner, color}` rather than launching a runtime | replace its New flow; do not extend it into a second daemon profile model |
| `createOrUpdateSession()` | sends legacy fields that do not match daemon-owned create | replace with typed create using profile/name/CWD contract |
| query `DELETE /api/sessions?id=...` | historically mixed termination/deletion semantics | mobile lifecycle UI must never use it |
| Dashboard `+ NEW AGENT` | optimistic profile row can become a phantom session | launch only after server response; show failure in-place |
| Terminal input | currently is a text submission control | hide/disable when session lifecycle is terminal or stopping |

Existing profile colors/order are presentation-only. Executable selection remains
daemon policy and is never accepted from mobile as a shell string.

## Preconditions and API contract

M3 consumes only authenticated, typed APIs. M2.5-4 supplies the device session
client/middleware; M3 must not construct bearer headers, device signatures, or
WebSocket tickets itself.

| Intent | API | Required result |
| --- | --- | --- |
| list launch profiles | `GET /api/session-profiles` | id, label, availability; no executable paths |
| create | `POST /api/sessions` | canonical id, adapter, profileId, name, lifecycle state |
| stop | `POST /api/sessions/{id}/stop` | id, action, authoritative state |
| kill | `POST /api/sessions/{id}/kill` | id, action, authoritative state |
| delete history | `DELETE /api/sessions/{id}` | id, action=delete, terminal result |
| list/refresh | `GET /api/sessions` | lifecycle state plus adapter capabilities |

M3 start is allowed only after the server reports `running`, meaning the
Recorder is ready. The client does not manufacture canonical IDs or a running
state. A request failure creates no local session row.

If an API response cannot express `managedLifecycle` capability and lifecycle
state independently, that is a backend blocker before M3 implementation.

## Capability and state policy

The mobile client branches on declared capabilities and state, never adapter
names.

| Session condition | Mobile actions |
| --- | --- |
| `managedLifecycle` + `starting` | show Starting; no input/Stop until server transition |
| `managedLifecycle` + `running` | Open/Detach, Stop; input only if `input` capability |
| `managedLifecycle` + `stopping` | show Stopping; disable input and repeated Stop; allow Force Kill only behind confirmation |
| `managedLifecycle` + `exited/killed/failed` | show terminal reason/state; view Activity/Transcript; Delete History only |
| external tmux/localpty | no Stop/Kill/Delete History lifecycle actions; retain declared input/live capabilities |
| cmux observe-only | no terminal input or lifecycle actions; best-effort Transcript explanation |
| unknown/missing capabilities | safe View Only; no destructive controls |

`Stop` means “terminate the entire Pokit-managed process group.” It is not a
request to stop only a nested Claude/Codex task. This scope is visible in the
confirmation copy.

## UX flows

### New Session

1. User taps `New Session` from Dashboard.
2. App loads daemon-owned profiles. Available profiles are selectable; unavailable
   profiles explain that the executable is absent on the Mac.
3. MVP profiles: Shell, Codex, Claude. Custom command is deferred.
4. User may provide a display name and absolute CWD. CWD validation remains
   authoritative on the daemon; the UI can only perform friendly validation.
5. On Create, disable duplicate submit and show `Starting…`.
6. On successful `running` response, refresh list and navigate to the returned
   canonical session ID.
7. On failure, remain in the form, preserve non-sensitive inputs, show the
   server-safe error, and do not add an optimistic Dashboard card.

### Detach / navigation

Back from a session view simply unmounts/detaches the mobile subscriber. It
does not call Stop, Kill, or Delete. Copy should say `Back to Sessions` or
`Detach`, never `Close session`.

### Stop

1. Only visible for `managedLifecycle` + running.
2. Confirmation names the session and says the whole managed terminal process
   will be stopped while its history remains.
3. On success, render server state `stopping`; disable terminal input and
   duplicate actions.
4. Refresh/poll until terminal state. A repeated Stop is treated as an
   idempotent state refresh, not an error.
5. If termination fails, render a recoverable error and keep the server’s
   authoritative state; do not claim the session ended.

### Force Kill

Force Kill is secondary. It is shown only while stopping or after a failed
graceful stop, with an explicit destructive confirmation. It calls Kill; it
does not send Ctrl-C, `exit`, or arbitrary terminal text.

### Natural exit and terminal state

When terminal EOF or a refreshed lifecycle state is terminal:

- terminal input/macros disappear;
- a persistent `Session ended`/`Failed`/`Killed` state is visible;
- Activity and Transcript remain viewable if retained;
- user may navigate back without a lifecycle request;
- Delete History is available only for managed terminal sessions.

### Delete History

Delete History is a separate destructive action on a terminal managed session.
The confirmation must state that captured Activity/Transcript and the session
record will be removed permanently. It must never appear for running/stopping
or external sessions. A 409 is rendered as “Stop the session first,” followed
by a refresh rather than a retry loop.

## State reconciliation

Source order:

```text
successful lifecycle API result
→ next session-list refresh
→ terminal EOF/WS close as a display hint only
```

The WebSocket is not lifecycle authority. Network loss must not turn a session
into `exited` locally. Show connection state separately and refresh on resume.
Mobile applies server state monotonically within one request cycle:

```text
starting → running → stopping → terminal
```

If a later list response contradicts a stale optimistic display, the server
state wins. No client-side timer invents terminal completion.

## Security boundary

- M3 uses a single authenticated Pokit client supplied by M2.5-4.
- New/Stop/Kill/Delete all require the same device `Principal` and permission
  checks; the UI is never the authorization boundary.
- UI capability hiding is defense in depth. The daemon must reject unsupported
  lifecycle actions even if a compromised client submits them.
- Error messages and client logs must not contain device tokens, pairing data,
  terminal input, or raw command arguments.
- No insecure/local development flag is enabled by mobile release UX.

## Execution versus manual validation

### Execution track (E)

- typed client serializes only profile/name/CWD create fields;
- client invokes the exact Stop/Kill/Delete endpoints, never legacy query
  DELETE for lifecycle;
- capability/state table has component or reducer tests;
- create failure does not create a local phantom session;
- terminal/stopping states disable input and hide inappropriate controls;
- API response/error mapping, TypeScript, Android build, and backend regression
  gates pass.

### Manual track (M)

- first-use clarity of New Session and profile availability;
- confirmation wording is understandable on a physical phone;
- a stop from LTE visibly converges and local terminal restores;
- Back demonstrably detaches without killing the process;
- destructive controls are discoverable but not easy to invoke accidentally;
- offline/auth-expired behavior is understandable and recoverable.

Missing manual evidence is not an E-track rejection. It remains an M-track
release gate.

## M3 implementation phases

### M3a — Typed client and New Session replacement

- add typed profile/create/lifecycle DTOs to the authenticated client;
- replace the `AgentProfileModal` New flow with a New Session surface;
- preserve Dashboard display-only styling separately from daemon profile data;
- prove no optimistic phantom row on failure.

### M3b — Session action surface

- add state/capability-aware Stop, Kill, Delete History controls;
- implement confirmations and state reconciliation;
- preserve viewer detach/back behavior;
- enforce terminal input disablement from authoritative state.

### M3c — Execution and real-device gates

- source/API/build/typecheck proof;
- emulator lifecycle smoke using controlled_pty;
- manual LTE/physical-device checklist;
- list known limitations rather than adding scope.

## Acceptance criteria

M3 is a scoped accept when:

- mobile can create Shell, Codex, and Claude through daemon profiles and opens
  the returned controlled session;
- Back detaches only;
- managed running sessions Stop, stopping sessions can explicitly Kill, and
  only terminal managed sessions Delete History;
- external/observe-only/unknown sessions never gain managed lifecycle controls;
- natural exit and requested Stop produce the same safe terminal presentation;
- Activity/Transcript history is retained after Stop and removed only by Delete;
- all actions use the authenticated M2.5-4 client boundary;
- E-track gates pass, with remaining physical-device evidence explicitly listed.

## Explicit non-goals

- custom remote commands in MVP;
- nested-agent-only stop/interrupt;
- process restart/resume;
- per-session ACL and multi-device role UI;
- Transcript semantic improvement;
- cmux lifecycle control;
- notification/push work;
- persistence across daemon restart.

## Rollback

Gate M3 behind the authenticated device client. Retain `pokit run` and local
IPC creation unchanged. If the mobile lifecycle surface is disabled, existing
external session observation and Live Terminal subscriptions must remain
unaffected.
