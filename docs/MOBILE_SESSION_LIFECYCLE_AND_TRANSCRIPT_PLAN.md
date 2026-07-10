# Mobile Session Lifecycle and Transcript Projection Plan

Status: approved implementation roadmap
Branch: `feature/phase10-multi-adapter`
Runtime baseline: E10b / `6b40b32`

## Decision

Execution order:

```text
M0  Lifecycle contract
M1  Safe session creation and daemon-owned profiles
M1.5 Session ownership, access, and local terminal host contract
M2  Stop / kill / delete lifecycle
M2.5 Minimum device-trust security foundation
M3  Mobile UX and real-device gate
R1  Runtime signal discovery (research only)
T0  Transcript contract reset
T1  Byte-stream projector foundation
T2  Codex / Claude TUI safe degradation
T3  Semantic enrichment
```

The projects are not implemented together. Lifecycle state is authoritative in
the daemon and is not derived from Transcript. Transcript may later project a
lifecycle notice, but it does not own session state.

## Current production-path findings

### Mobile create/profile mismatch

The current mobile `createOrUpdateSession()` sends `id`, `runner`, and color to
`POST /api/sessions`. The daemon interprets `id` as a canonical adapter session
ID and does not use `runner` as the command. This explains why the UI reports
`Failed to save agent profile`: the UI models profile persistence while the API
models immediate session creation.

The old profile-save flow will be replaced, not patched into a second runtime.

### Termination/history mismatch

The current `DELETE /api/sessions?id=...` terminates the adapter session, stops
the Recorder, and clears Activity. The product contract requires separate
operations: detach, stop, force kill, and delete history.

### Transcript responsibility mismatch

Recorder currently reads the PTY and also strips ANSI, rewrites carriage
returns, detects cmux sentinels, and appends Transcript data. ActivityBuffer
also merges adjacent output and strips ANSI again. Projection and storage must
be separated without changing the raw Live Terminal path.

## Cross-project invariants

1. Recorder is the single PTY reader.
2. No mobile/local/web viewer independently opens or reads the PTY.
3. Live Terminal receives the unmodified raw PTY byte stream.
4. Transcript failure cannot terminate, delay, or corrupt Live Terminal.
5. Stop does not delete Transcript/Activity history.
6. Raw terminal input text is not stored.
7. cmux remains snapshot/best-effort and does not share byte-stream heuristics.

# Project M — Mobile Session Lifecycle

## Ownership

The daemon owns two related but separate structures:

```text
Runtime Registry
  process / process group
  PTY
  Recorder
  subscribers

Session Catalog
  canonical session ID
  profile ID
  lifecycle state
  start/end timestamps
  exit code / failure
  retained-history state
```

An exited runtime may disappear from the active Registry while its catalog row
and history remain readable.

## Lifecycle state machine

```text
starting → running → stopping → exited
    │          │                   ▲
    └→ failed  └──── natural exit ─┘
```

`force_killing` may remain internal for MVP. Public clients must at least see
`starting`, `running`, `stopping`, `exited`, and `failed`.

## API contract

### Profiles

```http
GET /api/session-profiles
```

Initial daemon-owned presets:

- `shell`: user default shell, with safe daemon fallback;
- `codex`: executable `codex`;
- `claude`: executable `claude`.

Display-only color/order may remain app-local. Executable policy is daemon-owned.

### Create

```http
POST /api/sessions
Content-Type: application/json

{
  "adapter": "controlled_pty",
  "profileId": "claude",
  "name": "backend-fix",
  "cwd": "/allowed/workspace"
}
```

The daemon returns the canonical ID and lifecycle DTO. Clients do not construct
canonical IDs.

Custom commands use executable plus argv, never an untrusted shell string:

```json
{
  "profileId": "custom",
  "command": {
    "executable": "go",
    "args": ["test", "./..."]
  },
  "cwd": "/allowed/workspace"
}
```

Remote custom commands are disabled by default and require explicit daemon
policy. The existing CLI payload may remain temporarily for compatibility, but
must not define the final remote-security contract.

### Stop, kill, and delete

```http
POST   /api/sessions/{id}/stop
POST   /api/sessions/{id}/kill
DELETE /api/sessions/{id}
```

- `stop`: idempotent graceful process-group termination;
- `kill`: explicit destructive fallback with confirmation;
- `DELETE`: remove an ended session's catalog/history;
- viewer Back/Close: subscriber detach only, no daemon lifecycle request.

The query-style legacy DELETE may remain deprecated during migration. New
mobile code must not use it for Stop.

## Termination policy

```text
Stop request
→ atomically mark stopping
→ signal controlled PTY process group with SIGTERM
→ wait bounded timeout (initially 5 seconds)
→ SIGKILL if still alive
→ PTY EOF
→ Recorder stops exactly once
→ all subscribers receive session-ended/EOF
→ local terminal restores raw mode
→ catalog state becomes exited
→ history remains
```

Ctrl+C is an interrupt, not Stop. Sending `exit\n` is not the lifecycle API.
MVP Stop terminates the whole daemon-owned controlled PTY process group, not
only an agent nested inside a shell. Nested-agent interrupt is future work.

Concurrent Stop calls share the same termination result. Requests against
`stopping` or `exited` return the current state rather than starting another
cleanup path.

## M phases and gates

### M0 — Lifecycle Contract

- document detach/stop/kill/delete/natural-exit semantics;
- define lifecycle DTO and transitions;
- audit process-group ownership created by the PTY library;
- define CLI compatibility and controlled_pty-only boundary.

Acceptance: the API can represent every required lifecycle action without
overloading DELETE or terminal text input.

### M1 — Safe Creation and Profiles

- implement daemon-owned Shell/Codex/Claude profiles;
- accept safe argv/CWD creation contract;
- make daemon generate canonical IDs;
- initialize catalog and Recorder before exposing a running session;
- reject invalid profile, CWD, executable, and unauthorized custom command;
- preserve `pokit run` behavior.

Acceptance tests:

- each preset launches the expected executable/argv;
- CWD propagation is exact;
- duplicate/invalid names are handled deterministically;
- remote custom execution is denied by default;
- creation response exposes canonical ID and lifecycle state;
- Recorder single-reader and no-WebSocket capture regressions pass.

Acceptance: corrective commit `a99070015` closes the `e4e2704d0` blockers and
passes the targeted race suite plus the complete build gate. See
`docs/M0_M1_A990700_ACCEPTANCE.md`.

### M1.5 — Session Ownership and Access Contract

This is a narrow capability/contract gate, not a new terminal implementation.

- distinguish Pokit-managed sessions from externally owned sessions;
- define `managedLifecycle` independently from input/control capability;
- position tmux as external attachable and cmux as external observer;
- define Terminal.app/VS Code and future terminal applications as local hosts,
  not adapters;
- define geometry ownership, detach behavior, and multi-writer limitation;
- update M2/M3 acceptance so lifecycle actions are capability/ownership driven.

Acceptance: adding a new POSIX terminal host must require compatibility evidence,
not a new runtime adapter, and external sessions must never inherit managed Stop
or Kill merely because they support input.

### M2 — Stop / Kill / Delete

- implement idempotent Stop and explicit Kill;
- converge natural exit and requested Stop on one cleanup path;
- retain catalog/history after Stop;
- allow explicit deletion only after terminal state.

Acceptance tests:

- concurrent Stop terminates once;
- graceful exit and timeout/SIGKILL paths;
- child process group leaves no descendant;
- Recorder stops once and subscribers receive EOF;
- local attached terminal restores state;
- Stop retains Transcript/Activity;
- Delete removes only the intended ended session/history;
- cross-session isolation.

Acceptance: corrective commit `eae1e96c9` passes targeted race tests and the
complete build gate. See `docs/M2_EAE1E96_ACCEPTANCE.md`.

### M2.5 — Minimum Device Trust Security

M2.5 runs before M3 because remote lifecycle buttons must not ship on the
static `dev-token` boundary. It is intentionally smaller than public-release
security:

- record the Cloudflare confidentiality trust assumption;
- establish host identity and paired-device registry;
- implement LAN-only QR pairing with local approval;
- authenticate a device-key challenge and issue a short-lived in-memory session;
- route REST and WebSocket authentication through common boundaries;
- support revoke and a minimal content-free audit log.

Detailed phases, gates, and deferred security work are defined in
`docs/M2_5_DEVICE_TRUST_SECURITY_PLAN.md`.

### M3 — Mobile Lifecycle UX

- replace AgentProfileModal save semantics with New Session UX;
- choose daemon profile, name, and CWD;
- open Live Terminal after successful creation;
- separate Back/Detach, Stop, Force Kill, and Delete History;
- render starting/running/stopping/exited/failed;
- disable input while stopping/ended.

Execution acceptance uses source, API tests, typecheck, build, and emulator.
Physical-device/LTE usability remains a manual validation track.

# Project R1 — Runtime Signal Discovery

R1 runs after M3 and before T0. It is limited to one or two working days and
produces evidence and architecture decisions, not production integration code.

Its purpose is to determine whether official Claude Code hooks, Codex
app-server/JSON events, native logs, process lifecycle, or PTY structural
signals can provide authoritative runtime state. Transcript still requires a
generic readable projection for shell output and unknown agents.

Detailed scope and execution handoff:

- `docs/R1_RUNTIME_SIGNAL_DISCOVERY_PLAN.md`
- `docs/NEXT_SESSION_R1_RUNTIME_SIGNAL_HANDOFF.md`

## M rollback points

- gate the new mobile flow while retaining existing CLI creation;
- add controlled_pty lifecycle endpoints without changing tmux/cmux semantics;
- preserve legacy POST payload during migration;
- keep catalog ownership separate from Registry/Recorder so it can be disabled
  without reverting the stable PTY runtime.

# Project T — Transcript Projection Refactor

## Architecture

```text
PTY
 ↓
Recorder (single reader)
 ├─ raw bootstrap ring
 ├─ raw live subscribers
 └─ bounded projection queue
       ↓
  asynchronous TranscriptProjector
       ↓
  TranscriptStore
       ↓
  Transcript API / mobile read mode
```

Recorder owns projector lifecycle, but not projection rules. ActivityBuffer or
its replacement stores already-projected events and does not parse, strip, or
merge terminal chunks.

Projection must not block PTY capture. Queue overflow must produce an explicit
`projection_gap` marker/metric rather than silent loss.

## Projectors

1. `ByteStreamProjector`: controlled_pty, tmux, localpty.
2. `SnapshotProjector`: cmux best-effort only, separate implementation.
3. `SemanticEnricher`: optional agent-native/Common Event enrichment after T2.

## Minimal v2 event model

```text
TranscriptEvent
  version: 2
  id
  seq
  sessionId
  kind:
    text
    input_boundary
    system_notice
    terminal_ui_omitted
    projection_gap
  text?
  source: pty | snapshot | agent_log
  timestamp
  byteRange?
  metadata?
```

Do not add command/tool/agent-message semantics until a reliable shell
integration or existing agent-native event provides evidence. Runtime status
belongs to the lifecycle/status model, not Transcript authority.

## Byte-stream projection rules

- maintain ANSI parser state across PTY chunks;
- commit ordinary text on newline;
- treat carriage return as replacement of the current pending progress line;
- apply backspace/erase-line only to the pending line;
- do not append erase-screen as document content;
- collapse alternate-screen/complex cursor regions to a bounded omission marker;
- do not globally deduplicate identical normal log lines;
- preserve normal output before and after TUI regions;
- never store raw terminal input text.

When safe conversion is impossible:

```text
[Interactive terminal UI omitted — open Terminal for live view]
```

Raw Transcript attachments are out of MVP scope because they expand sensitive
data retention.

## T phases and gates

### T0 — Contract Reset

- audit current Recorder ANSI/CR/cmux logic and ActivityBuffer merge logic;
- classify old heuristics as remove/retain/snapshot-only;
- capture redacted bash, Codex, and Claude fixtures;
- define v1/v2 API compatibility and shadow comparison.

### T1 — Byte-stream Foundation

First scenario only:

```text
pokit run bash
pwd
ls
echo hello
```

Acceptance:

- readable, correctly ordered text and newlines;
- chunk boundaries do not change output;
- split ANSI sequences are handled;
- CR progress keeps a stable final value;
- Transcript storage is bounded;
- raw input remains unstored;
- slow/failed/overflowing projection cannot block Recorder;
- Live Terminal bytes are identical with projector enabled/disabled;
- cmux path is unchanged.

### T2 — Codex / Claude Safe Degradation

- detect alternate screen, complex cursor motion, and repaint bursts;
- emit bounded omission markers instead of giant flattened text;
- preserve useful ordinary output around TUI regions.

Scoped acceptance does not require perfect agent-message extraction.

### T3 — Semantic Enrichment

- map existing Common Event/Interaction contracts explicitly;
- attach tool/approval/agent-message semantics only when evidence exists;
- handle duplicate PTY/native-log representations;
- keep native logs optional.

## T migration and rollback

- preserve `GET /api/sessions?activity=` during migration;
- add v2 events additively or expose a versioned transcript endpoint;
- mobile uses a v1/v2 compatibility mapper;
- run v2 in shadow mode before switching the UI;
- feature-flag projector per adapter/session;
- leave raw live broadcast/bootstrap untouched;
- retain immediate rollback to legacy Activity read path.

# Verification policy

## BLOCKER

- second PTY reader;
- PTY output loss/corruption;
- Stop targets the wrong process/session;
- descendant process leak or local terminal not restored;
- Stop deletes history;
- unrestricted remote shell-string execution;
- lifecycle API cannot distinguish detach/stop/kill/delete;
- Transcript changes or blocks Live Terminal;
- silent projection loss.

## FOLLOW-UP

- visual polish;
- advanced command/agent semantic extraction;
- additional profiles;
- nested-agent-only interrupt;
- raw attachments;
- daemon-restart persistence;
- optional refactors/tests without product-boundary impact.

## SCOPED ACCEPT

Use when controlled_pty mobile lifecycle works with documented limitations,
bash Transcript is stable, Codex/Claude TUI safely degrades, and no runtime or
security invariant is violated.
