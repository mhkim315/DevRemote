# Next Executor Onboarding — SP0.5 Managed I/O and Lifecycle

Status: **AUTHORIZED AFTER SP0 ACCEPT — SP1/CLEANUP UNAUTHORIZED**

Date: 2026-07-15

## 1. Repository and accepted baseline

```text
checkout: /Users/mhk/Documents/codex/DevRemote
remote:   https://github.com/mhkim315/DevRemote.git
branch:   feature/phase10-multi-adapter
SP0 implementation: 2b35f524dc339c24d7c3d5ee4284c967300836c3
SP0 review marker:  6aaff30bde33ee7719227f1ae4bdc0a94f13ca67
accepted CP0 evidence ancestor: 42429c8acd4c2840a93e1ab11c53f8e21c34bc19
```

At startup, fetch and fast-forward only, verify local/remote equality and both
ancestries, and require a clean worktree. Use only the checkout above. Do not
repeat the accepted SP0 live turn, schema research, artifact research, or CP0
harness work.

You are the execution agent. Preserve the accepted SP0 path; do not redesign
its owned registry, native authority, pinned runtime, or process lease unless a
new SP0.5 test demonstrates a concrete defect.

## 2. Product boundary and one objective

SP0.5 makes the accepted native Codex session minimally usable from the user's
existing local terminal and paired mobile client, and gives that session basic
daemon-owned lifecycle controls.

```text
pokit run codex
  -> the SAME ManagedCodexService / app-server session accepted in SP0
  -> bounded structured prompt input
  -> bounded projected native output
  -> local terminal and authenticated mobile consumers
  -> stop / kill / delete
  -> reconnect by snapshot + monotonic event cursor
```

Codex app-server stdio is JSON-RPC, not a terminal PTY. Do not expose protocol
bytes as terminal bytes, attach Recorder to app-server stdio, launch a second
Codex TUI, parse a screen, or build a terminal emulator. The user's terminal is
the visible I/O surface through a thin line-oriented POKIT client; native Codex
events remain the sole agent-semantic authority.

For this native runtime, terminal resize is **not applicable** because no PTY is
owned. Report that honestly in capability/DTO state; do not create a fake resize
implementation. PTY resize remains valid only for legacy controlled-PTY paths
until Cleanup removes them.

## 3. Mandatory short contract note

Before production edits, write a concise SP0.5 contract note containing:

- exact owner of input, provider transport, event projection, lifecycle, and
  reconnect state;
- immutable binding: SessionID + provider/version + launch epoch;
- one monotonic per-session output/event sequence;
- input states: idle, accepting, in-turn, closed;
- one-active-turn rule and conflict behavior;
- stop/kill/delete linearization points;
- reconnect snapshot/cursor rules;
- capacity, overflow, close, and daemon-restart behavior;
- public DTO allowlist and privacy bounds;
- explicit non-goals.

Keep this to the implementation contract. Do not write another provider
research report.

## 4. Packet SP0.5-A — local structured I/O

Implement the smallest usable local path for non-detached `pokit run codex`:

1. use the same structured Codex create request and ManagedCodexService as SP0;
2. remove the legacy command-string fallback for the exact recognized Codex
   invocation; managed-disabled remains explicit unavailable;
3. after create, attach through a new managed local IPC operation bound to the
   canonical SessionID and current epoch;
4. accept bounded UTF-8 line-oriented prompt input; no raw key stream, escape
   sequence, command, shell, or provider-payload input;
5. allow one active turn per session; a concurrent prompt returns conflict;
6. deliver prompts only through the owned app-server transport;
7. project only verified native assistant/output fields through a structural
   allowlist with byte bounds; never forward raw JSON-RPC objects;
8. render projected output to the existing terminal stdout without taking over
   the terminal, changing keymaps, or emulating the Codex TUI;
9. Ctrl-D detaches the local viewer; it does not imply process completion.

Do not implement approvals, tool-input forwarding, arbitrary commands, full
screen interaction, cursor motion, ANSI rendering, history search, or shell
features.

Required tests:

- real CLI parser -> local IPC -> same managed service, no shell/controlled PTY;
- bounded prompt -> exact native turn/start request;
- second prompt while active -> conflict and zero provider write;
- malformed/oversized/non-UTF-8 input -> fail closed;
- wrong SessionID/epoch/closed session -> zero provider write;
- raw prompt and raw provider payload absent from logs and public output;
- local detach does not kill the runtime;
- one bounded live turn only after deterministic tests pass.

Checkpoint and stop on a blocker. Do not begin B while A is failing.

## 5. Packet SP0.5-B — authenticated mobile/WebSocket projection

Add a session-scoped managed event surface. Reuse paired-device authentication,
host binding, bearer expiry, and permission machinery; do not reuse raw
`/term/ws` PTY framing if it would expose provider protocol bytes.

Minimum contract:

- bounded snapshot followed by monotonic events;
- fields limited to contractVersion, SessionID, epoch, Seq, kind, bounded text,
  observedAt, and terminal/lifecycle marker where needed;
- closed event vocabulary sufficient for assistant output, working,
  completed/idle, exited, gap, and unavailable;
- authenticated mobile input uses the current host-bound transport and terminal
  input permission;
- server derives device/host/permission identity; clients cannot assert it;
- duplicate/replayed cursor is idempotent; cursor ahead, wrong session, wrong
  epoch, or unknown fields fail closed;
- bounded store overflow inserts a gap and never silently presents a complete
  history;
- reconnect reads snapshot/cursor and cannot restore stale prior-epoch state.

Mobile work should be minimal: consume the managed DTO, show bounded output and
one prompt input, and clearly label the native managed session. Do not recreate
the Codex TUI, terminal renderer, pane UX, approval buttons, or notifications.

Required tests:

- paired-device read and input permissions;
- anonymous, legacy bearer, cross-host, wrong-session, and stale-epoch rejection;
- output ordering, deduplication, gap, snapshot isolation, and byte bounds;
- mobile decoder unknown-field/version rejection;
- session switch/unmount prevents stale response commit;
- backend DTO fixture passes the real mobile decoder;
- TypeScript typecheck and focused Jest when mobile changes.

## 6. Packet SP0.5-C — basic lifecycle and reconnect

Add lifecycle only for native managed sessions:

- Stop: graceful provider/process stop with a short bound, then non-current;
- Kill: process-group kill + reap;
- Delete: allowed only after terminal state; remove bounded output/status data;
- daemon shutdown continues to drain in-flight creates and owned children using
  the accepted SP0 lease boundary;
- natural child exit marks the session exited;
- lifecycle operations bind exact SessionID + current epoch and are idempotent;
- a stopped/killed/deleted session accepts no new prompt or native status;
- reconnect means client transport reconnect to the same live daemon/runtime,
  not daemon restart recovery;
- daemon restart recovery and persistent session brokerage remain deferred.

Do not route these operations through mux.Adapter discovery. Reuse the existing
authenticated lifecycle permissions and response vocabulary where compatible,
but keep the native implementation directly bound to ManagedCodexService.

Required tests:

- prompt-vs-stop and prompt-vs-kill deterministic interleavings;
- duplicate Stop/Kill/Delete;
- wrong/stale epoch;
- child natural exit;
- delete clears managed status/output and later events are inert;
- reconnect snapshot/cursor with no stale prior-epoch state;
- shutdown during prompt/turn leaves no child, lease, runtime, or current state;
- full race tests for service, event store, and lifecycle handler.

## 7. Future extensibility—only what is justified now

Allowed:

- one versioned provider-neutral managed event DTO used by backend/mobile;
- one narrow process handle/launcher seam, with OS-specific implementation
  files rather than OS-specific fields in shared contracts;
- provider/version/epoch identity stored in managed records;
- Codex-specific event projection isolated from transport/store code.

Not allowed:

- generic provider/plugin SDK;
- capability admission engine;
- Task/Dispatch/worker protocol;
- persistent broker or daemon-restart recovery;
- Claude implementation or speculative Claude abstractions;
- platform attestation or Windows implementation;
- generic terminal emulation, PTY wrapper for app-server, or second Codex process.

Extract an interface only when both a current production consumer and a
deterministic test need it. Do not generalize from imagined providers.

## 8. Explicitly deferred

### SP1 — native approval

- actionable approval request observation;
- provider request/action identity;
- mobile approve/reject;
- provider response delivery and exact consumption proof;
- duplicate, stale, timeout, cancellation, and retry semantics.

Approval capacity remains zero throughout SP0.5. `waiting_approval` remains
display-only and must not produce a CTA or input.

### Cleanup — observer removal

- migrate every remaining REST/WS/lifecycle consumer from mux.Registry;
- remove link/unlink, discovery, screen/JSONL authority, external mobile modes;
- unregister/delete tmux/cmux and snapshot transcript sources.

### Later product work

- notifications, Claude, Windows, cloud relay, orchestration, workspaces,
  Executor-Verifier, terminal fidelity, daemon restart recovery.

## 9. Efficient execution and gates

- Run focused tests during each packet; do not run the full repository gate
  after every edit.
- Run affected package race tests and `git diff --check` at each checkpoint.
- Spend at most one real Codex turn for A and one mobile physical smoke only if
  required; deterministic tests cover failures and races.
- Freeze the final implementation HEAD, then run the full authoritative backend,
  mobile, native, invariant, and secret gates once.
- If a packet spends more time on protocol research or test harness tooling than
  implementation, stop and report the smallest blocker.
- Never weaken bounds, authentication, native authority, or acceptance criteria
  to keep moving.

## 10. SP0.5 acceptance gate

Submit SP0.5 only when:

- non-detached `pokit run codex` uses the accepted native runtime, not PTY/shell;
- local bounded prompt/output works for a real turn;
- authenticated mobile bounded prompt/output works through production transport;
- native semantic status remains sole authority;
- one-active-turn, bounds, privacy, and wrong/stale binding fail closed;
- Stop/Kill/Delete and natural exit are daemon-owned and discovery-independent;
- reconnect cannot restore stale state;
- daemon shutdown leaves no owned child or in-flight create;
- approval remains non-actionable and capacity zero;
- no terminal emulator, generic provider framework, or observer cleanup was built;
- focused and final gates pass on the exact pushed implementation tree;
- local/remote match, accepted SP0 ancestry passes, and worktree is clean.

Required marker:

```text
REVIEW REQUEST: SP0.5 Managed I/O and Lifecycle — <implementation SHA>
```

Then stop for independent verification. Do not start SP1 or Cleanup.

