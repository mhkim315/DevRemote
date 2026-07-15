# Next Executor Onboarding — SP0 Native Managed Runtime

Status: **AUTHORIZED SCOPE DOCUMENT — IMPLEMENTATION NOT YET ACCEPTED**

Date: 2026-07-15

This handoff replaces the broad CP0 production-entry packet for the next
execution agent. SP0 is a deliberately small architecture proof. It is not A1.1,
SP0.5, SP1, observer cleanup, or a generic managed-runtime framework.

## 1. Role and single objective

You are the execution agent. Your only objective is to prove this production
path:

```text
pokit run --detach codex
  -> structured local IPC request (no shell command string)
  -> directly spawned, POKIT-owned Codex app-server
  -> canonical session in a minimal owned-session registry
  -> production native event pump
  -> native working -> completed/idle status
  -> authenticated REST list/get/status
```

The detached form is the SP0 certification target. Do not claim that the
interactive `pokit run codex` terminal experience is complete; local/mobile
terminal attachment belongs to SP0.5.

Codex native protocol events are the sole authority for **agent semantic
activity** in this path. The daemon-owned child process remains the authority
for process/session lifecycle. Screen text, PTY bytes, JSONL, prompts, process
names, CWD, and observer telemetry are never semantic-status authority.

## 2. Canonical repository identity and startup procedure

```text
canonical checkout: /Users/mhk/Documents/codex/DevRemote
remote:             https://github.com/mhkim315/DevRemote.git
branch:             feature/phase10-multi-adapter
start remote HEAD:  92cdb079582f09d6ffb77037a70a3eef14012a4e
accepted evidence:  42429c8acd4c2840a93e1ab11c53f8e21c34bc19
```

At startup:

1. use only the canonical checkout above;
2. `git fetch` and fast-forward to the current remote branch if it advanced;
3. record current local and remote SHA;
4. prove `42429c8...` is an ancestor;
5. require a clean worktree before editing;
6. if the previous executor's uncommitted files are present, stop and report
   them—do not incorporate, delete, stash, or commit them without reviewer
   direction;
7. never reset or rewrite history back to `42429c8`.

Git work begins at the current remote HEAD. The accepted trust boundary is
different: CP0 harness/evidence through `42429c8` may be reused without
repeating research, while `7a3667e`, `e1f73e6`, and the report-only `92cdb07`
contain an **unaccepted runtime scaffold** that must be independently checked.
Preserve useful code only where it satisfies this handoff; replace or isolate an
unsafe boundary rather than adding claims around it.

## 3. Read order

Read only what is necessary before the first edit:

1. this document;
2. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
3. `docs/A1_1_CP0_EVIDENCE_REPORT_3.md` for accepted provider evidence;
4. `docs/A1_1_CP0_EVIDENCE_REPORT_4.md` as an unaccepted claim inventory;
5. `companion-daemon/cmd/devremote/client.go` (`runClient`, `createViaSocket`);
6. `companion-daemon/internal/term/ipc.go` (`handleIPCConnection` create path);
7. `companion-daemon/internal/term/create.go` (`localCreateSpec`, controlled launch);
8. `companion-daemon/internal/term/codex_appserver_runtime.go` and its tests;
9. `companion-daemon/internal/term/telemetry.go`, `telemetry_service.go`, and REST session
   handlers only far enough to prove authority isolation;
10. `companion-daemon/cmd/devremote/app.go` composition and shutdown.

Do not restart schema, attestation, lifecycle-response, or provider-approval
research. The accepted CP0 evidence already establishes exact Codex 0.144.1
schema/lifecycle observations needed by SP0.

## 4. Frozen facts and reusable evidence

Reuse these facts; do not spend another live-model cycle reproving them:

- exact pinned Codex CLI 0.144.1 path, version check, shim/native digests, and
  redacted schema subset are recorded under `docs/a1_1_cp0_evidence/`;
- app-server initialize and a real native turn lifecycle have been observed;
- accepted redaction uses structural projection and stable pseudonyms;
- accept/decline/duplicate/timeout/cancel traces are future SP1 inputs, not SP0
  work;
- global Codex 0.144.4 is not modified or used for certification;
- production approval capacity remains zero and `provenActionMapping` remains
  empty;
- provider-neutral A1 safety contracts remain frozen and untouched;
- Recorder remains the single PTY reader, but SP0 does not add a PTY/Recorder
  path to app-server stdio.

Do not describe an explicit path plus artifact check as complete process-image
attestation. Code signing, cdhash, EndpointSecurity, TOCTOU hardening, and
supply-chain certification are outside SP0.

## 5. Minimal SP0 contract

Before implementation, write a short contract note—no research essay—covering:

- owner of the child process and app-server stdio;
- canonical SessionID creation;
- immutable binding: SessionID, provider, supported version, runtime epoch,
  launch generation, and opaque process identity;
- native states and the exact event that enters/leaves `working`;
- registry insertion, status update, close, and rollback linearization points;
- failed-spawn, failed-initialize, child-exit, and daemon-shutdown cleanup;
- capacity behavior;
- explicit non-goals.

Keep future extension deliberately small:

- the registry record may be provider-neutral, but implement Codex only;
- keep OS-specific process details behind a narrow launcher/process handle and
  expose only an opaque identity plus OS/architecture metadata if needed;
- do not put macOS inode, fd, Mach-O, code-signing, or IPC semantics into the
  stored provider-neutral contract;
- do not extract a generic provider SDK, plugin system, capability engine, or
  reconnect framework;
- add an interface only when the production path or deterministic testing needs
  the seam now.

## 6. Packet SP0-P1 — structured launch and owned child

Required production behavior:

1. `pokit run --detach codex` sends a structured recognized-profile request,
   not a joined `command` string;
2. conflicting profile/command/executable inputs fail closed;
3. the daemon directly starts the certified Codex executable with the exact
   app-server argv; no `bash`, `sh`, `-c`, or second interactive Codex process;
4. the runtime owns stdin, stdout, child wait/reap, and protocol framing;
5. initialize failure terminates/reaps the child and leaves no visible session;
6. the runtime is constructed by the real daemon composition root, not only a
   helper or test fixture.

Do not redesign arbitrary `pokit run <command>`. Touch only the recognized Codex
branch required for SP0.

Focused tests:

- real CLI parser/request -> real IPC create handler -> injected launcher,
  asserting exact executable/argv and absence of command string;
- malformed/conflicting launch requests rejected before spawn;
- failed version/digest/initialize -> no registered session and no child;
- production composition constructs the runtime when the supported Codex path
  is enabled.

Checkpoint only after focused tests pass. If this packet exceeds roughly four
hours because of new identity/attestation research, stop and report the narrow
blocker instead of expanding scope.

## 7. Packet SP0-P2 — minimal owned-session registry and native status

Use the smallest bounded, concurrency-safe owned-session registry that can
prove SP0. A full `ManagedRuntimeStore` framework is not required.

Minimum operations:

```text
Register(session)
Get(sessionID)
List()
UpdateNativeStatus(sessionID, epoch, status)
MarkExited(sessionID, epoch)
Remove(sessionID)
```

Minimum properties:

- daemon-generated canonical IDs;
- immutable session/provider/runtime identity;
- repository-owned capacity with fail-closed exhaustion;
- defensive snapshots;
- updates accepted only for the current epoch;
- no `mux.Registry.Refresh`, discovery, adapter snapshot, screen reader, JSONL,
  or telemetry dependency;
- native app-server events alone produce semantic `working` and
  `completed/idle` transitions;
- unknown native events cannot fabricate a known status;
- closed/old-epoch events cannot restore current status.

Do not implement reconnect, persistence, restart recovery, general replacement,
or multi-provider dispatch. A single runtime epoch plus stale-event rejection is
enough for SP0.

Focused tests:

- register/get/list and defensive-copy bounds;
- duplicate ID and capacity exhaustion fail without replacing state;
- ordered native events update status;
- old/closed epoch update rejected;
- child exit marks the record non-current/exited;
- race test for register/update/close/snapshot.

## 8. Packet SP0-P3 — production REST proof and observer isolation

Wire only the authenticated production REST list/get/status read required by
SP0. Do not migrate every existing REST, WebSocket, lifecycle, transcript, or
mobile consumer.

Legacy Registry rows may coexist temporarily. Managed rows must come directly
from the owned registry, use a disjoint canonical identity, and never be looked
up through adapter discovery.

Required proof:

1. an authenticated REST client lists and retrieves the newly launched managed
   Codex session;
2. a bounded real prompt through the same production app-server connection
   produces observable `working` and then `completed/idle`;
3. the production event pump—not a direct store call—causes those transitions;
4. contradictory screen text, PTY bytes, JSONL evidence, and legacy telemetry
   cannot overwrite the managed status;
5. failing tmux/cmux refresh does not affect managed create/get/list/status;
6. session and native status DTOs remain bounded and exclude prompts, command
   text, payloads, paths, tokens, raw protocol messages, and process details.

One real pinned Codex turn is sufficient for the final production proof. Use
deterministic fakes for failure and concurrency coverage. Do not repeatedly
spend model turns once the ordered production path is captured.

## 9. Minimal cleanup required in SP0

Full Stop/Kill/Delete and restart recovery belong to SP0.5. SP0 nevertheless
owns any process it starts, so it must provide only this minimum:

- creation rollback kills and reaps the child;
- child exit closes the event pump and makes status non-current;
- daemon shutdown stops/reaps the SP0-owned child with a bounded fallback;
- shutdown or close prevents later events from updating state.

Do not turn this into a general lifecycle service rewrite, process broker,
persistent session layer, reconnect loop, or tmux-equivalent daemon survival.

## 10. Explicitly deferred

### SP0.5 — managed terminal and lifecycle

- non-detached interactive `pokit run codex` certification;
- local/mobile/WebSocket terminal input and output;
- resize;
- complete Stop/Kill/Delete;
- reconnect and runtime-generation recovery;
- richer daemon lifecycle handling.

### SP1 — native approval

- actionable native approval observation;
- provider request identity and canonical actions;
- mobile approve/reject;
- provider response delivery and consumption evidence;
- stale, duplicate, cancellation, timeout, and retry behavior.

### Cleanup — observer removal

- migrate all remaining Registry consumers;
- remove link/unlink and arbitrary attach/discovery;
- remove screen/JSONL semantic authority globally;
- remove external/best-effort mobile paths;
- unregister/delete tmux and cmux;
- remove snapshot transcript sources and discovery-oriented abstractions.

tmux/cmux may remain compiled during SP0. The new managed path must be provably
unreachable from them. Do not delete them in this packet.

Also deferred: Claude, generic provider plugins, Windows implementation, cloud
relay changes, notifications, Task/Dispatch, orchestration, workspace
isolation, and Executor-Verifier flows.

## 11. Efficient gate strategy

Do not run the full repository gate after every small edit.

1. Before each packet, write/update the brief contract note and one adversarial
   example per critical invariant.
2. During implementation, run only the focused Go package/test selectors for
   that packet.
3. At each checkpoint, run the affected package race tests and `git diff
   --check`.
4. After P1-P3 are complete, freeze HEAD and run the authoritative full build,
   vet, race, invariant, secret, and applicable mobile gates once.
5. If the final tree changes after that gate, rerun only the affected focused
   tests plus the required final gate according to repository policy.

Green tests do not substitute for the production-path proof. Conversely, do not
expand a focused failure into unrelated research. If a requirement cannot be
proven within its packet, leave unsupported behavior disabled, report BLOCKED,
and stop.

## 12. Acceptance checklist

SP0 may be submitted for independent review only when all are true:

- real `pokit run --detach codex` reaches the production native runtime;
- exact direct executable/argv proven; no shell wrapper;
- canonical managed session registered without discovery refresh;
- production app-server transport owns its child and stdio;
- one real prompt produces native `working -> completed/idle`;
- authenticated REST exposes the same bounded status;
- screen, PTY, JSONL, and observer telemetry cannot overwrite it;
- tmux/cmux failure cannot affect the managed path;
- failed launch, child exit, and daemon shutdown leave no orphan and no current
  stale status;
- production-wired, deterministic-test, manual/live, skipped, unavailable, and
  deferred evidence are labeled honestly;
- final HEAD is frozen for the final gate, local equals remote, accepted-evidence
  ancestry passes, and the worktree is clean.

Required marker:

```text
REVIEW REQUEST: SP0 Native Managed Runtime — <implementation SHA>
```

Then stop. Do not start SP0.5, SP1, Cleanup, A1.1 provider-positive delivery,
N1, O1, or O2.

## 13. Mandatory stop conditions

Stop and report rather than broadening the task when:

- the worktree is not clean at startup;
- the pinned accepted toolchain cannot be verified;
- a real production turn cannot be reached through the structured local entry;
- native status would require screen, PTY, JSONL, or heuristic authority;
- the only proposed fix requires a generic provider SDK, persistence broker,
  terminal renderer, approval system, or platform-attestation project;
- any packet spends more time on new research/evidence tooling than on its
  bounded implementation;
- completing a packet requires modifying independently frozen A1 contracts.

An honest BLOCKED result is acceptable. A fixture-only, callback-only,
test-only, heuristic, or unwired implementation must never be reported as a
production SP0 completion.
