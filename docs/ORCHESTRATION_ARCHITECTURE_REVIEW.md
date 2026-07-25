# POKIT Orchestration System Feasibility Architecture Review

**Status:** READ-ONLY ARCHITECTURE REVIEW — NO IMPLEMENTATION
**Date:** 2026-07-25
**Repository:** DevRemote (POKIT) — companion-daemon + mobile
**Review Scope:** 12 analysis tasks covering managed runtime, component reuse, generic workers, liveness, supervision, task state, git worktrees, coordination, memory, API refactoring, security, and migration staging.

---

## Verdict

POKIT's daemon contains a **production-hardened managed runtime core** that can
serve as the foundation for a general orchestration system. Approximately **60%
of the codebase is directly reusable or refactorable**. The key assets are the
generation-bound process lifecycle (`OwnedPTYRuntime`), the sole-reader PTY
transport (`Recorder` + `TerminalTransport`), the provider-agnostic Transcript
projection (`transcript.Service`), and the exact-generation authorization
framework (`MutationAuthorizer`, `AuthorizeAndCommit`).

**Three critical blockers** must be resolved before implementation:
1. The daemon assumes exactly two provider types (Codex, Claude) — generic
   worker launch requires a provider-neutral `WorkerSpec` interface.
2. There is no task/event persistence — all state is in-memory. Orchestration
   requires an append-only event store with at-least-once delivery.
3. Git worktree management does not exist in the codebase — it must be built.

**Recommendation:** Extract the reusable core into a `pokitd` orchestration
daemon in 10 staged waves. Start with a minimal PoC (single worker, no
persistence) to validate the extraction boundaries, then layer on persistence,
multi-worker, git worktrees, coordination, and client API.

---

## 1. Reusable Assets

### 1.1 Directly Reusable (no changes)

| Component | File(s) | Reuse Rationale | Risk |
|-----------|---------|-----------------|------|
| **OwnedPTYRuntime** | `internal/term/owned_pty_runtime.go` | Generation-bound process lifecycle: Create spawns PTY, registers generation, starts exit watcher. Stop/Kill/Delete are authorization-gated and idempotent. Transport fan-out is built-in. | Assumes `controlled_pty:*` adapter prefix. Minor: adapter prefix parameterization. |
| **TerminalTransport** | `internal/term/terminal_transport.go` | Exact-generation PTY fan-out, write, resize, input ownership. `SubscriberFanOut` creates per-client channels. `WriteInput` enforces one-writer policy via `ClaimInput`. `Retire` invalidates all handles atomically. | Assumes a single PTY writer. Multi-writer scenarios need arbitration layer on top. |
| **Recorder** | `internal/term/recorder.go` | Sole PTY reader contract. Reads from PTY, broadcasts to subscribers via TerminalTransport, feeds Transcript. Never exposes raw PTY bytes outside the daemon. | Tightly coupled to `transcript.Service` — but that's the correct coupling. |
| **transcript.Service** | `internal/transcript/service.go` | Provider-agnostic bounded Transcript store. `FeedAgentSegments` accepts arbitrary provider events. `ListTranscript` is client-agnostic. Correlation and arbitration are built-in. | `ContractVersion` must be bumped for new event types. |
| **LifecycleService** | `internal/term/lifecycle_service.go` | Provider-dispatch lifecycle: `ownerFor` routes by adapter prefix. Stop/Kill/Delete are generation-gated, idempotent, and authorization-gated. `ProviderLifecycleOwner` interface is the extension point. | Adapter prefix dispatch could be generalized to a `LifecycleOwner` registry. |
| **MutationAuthorizer** | `internal/devicetrust/` | `AuthorizeAndCommit(deviceID, deviceEpoch, intent, fn)` — atomic authorization + mutation. The pattern (authorize under lock, commit, release) is universally applicable. | Device identity concept may need to generalize to "principal." |
| **ManagedSessionRegistry** | `internal/term/managed_registry.go` | Generation-gated session registry. `Get`, `List`, `Remove` with epoch comparison. Prevents stale-generation registration. | Adapter-specific; would need a generic `SessionRecord` type. |

### 1.2 Reusable After Extraction (minor refactoring)

| Component | Refactoring Needed |
|-----------|-------------------|
| **ManagedCodexService** | Extract JSON-RPC client from `codexManagedRuntime`. The JSON-RPC framing, handshake, and turn protocol are Codex-specific, but the pattern (spawn binary, communicate over stdio with structured protocol, project events to Transcript) is generic. Extract a `StdioWorker` interface. |
| **ClaudeInteractiveHost** | Extract PTY spawn + hook bridge + JSONL tailer pattern. The hook configuration, nonce authentication, and JSONL normalizer are Claude-specific, but the pattern (spawn binary in PTY, tail structured output file, receive hook callbacks) is generic. Extract a `PTYWorker` interface. |
| **ManagedRuntimeCatalog** | Currently federates exactly two registries (Codex + Claude). Generalize to an N-provider registry with dynamic `ManagedSurfaceCapabilities`. |
| **TelemetryService** | The `SessionTelemetry` DTO carries mobile-specific fields (`terminalSurface`, `adapterCapabilities`). Extract a generic `WorkerStatus` DTO and keep `SessionTelemetry` as the mobile projection. |
| **IPC Server** | The `ipc.go` Unix socket server is generic (accept JSON operations, dispatch by operation name). The create/managed-attach/pair ops are POKIT-specific but the server skeleton is reusable. |

### 1.3 Reusable After Redesign (significant changes)

| Component | Redesign Needed |
|-----------|----------------|
| **AgentStatusStore** | Currently coupled to Agent Detector evidence model. For orchestration, needs a generic `ActivitySink` that accepts structured events from any worker. |
| **Input-B Protocol** | The two-frame ACK protocol (text + Enter as separate WebSocket messages with operationId tracking) is designed for mobile terminal input. A general orchestration system needs a simpler write contract: one message, one ACK. |
| **Approval Store** | `AuthoritativeApprovalStore` is designed for provider tool-approval requests. For orchestration, needs to generalize to "decision point" — any place where the coordinator must approve/deny an action. |

### 1.4 POKIT-Specific (keep separate)

| Component | Why Separate |
|-----------|-------------|
| **QR Pairing** (`qr_pair.go`, `pair.go`) | Mobile-device pairing protocol. Not relevant to orchestration. |
| **Device Trust** (`devicetrust/` — pairing, key exchange) | Device identity and pairing are POKIT-specific. The authorization pattern (`AuthorizeAndCommit`) is reusable; the pairing protocol is not. |
| **Notification/Push** | Mobile push notification delivery. Orchestration uses different notification channels (webhook, message queue). |
| **Mobile DTOs** (`SafeApprovalDTO`, `AgentCard`, `SessionTelemetry` surface fields) | Mobile-specific JSON shapes. Keep as projection layer over generic worker state. |

### 1.5 Obsolete or Unsafe (do not reuse)

| Component | Reason |
|-----------|--------|
| **Legacy adapter registry** (`mux/adapter.go`) | tmux/cmux adapters are not managed lifecycles. Deprecated for orchestration. |
| **Ambient process discovery** | Any path that scans for running processes or transcripts. Explicitly prohibited since DS-CL1. |

---

## 2. Managed Runtime Path Trace (Task 1)

### 2.1 Claude Create Path

```
createFromProfile (create.go:38)
  ├─ req.ProfileID == "claude" && h.ManagedClaude != nil → legacy headless path
  └─ ClaudeInteractiveHost.Create (claude_interactive_host.go:82)
       ├─ newClaudeSessionUUID() → POKIT-generated UUID
       ├─ os.MkdirTemp → hookDir
       ├─ startClaudeInteractiveBridge → HTTP server on random port
       ├─ hmacNonce → launch-bound 32-byte auth key
       ├─ writeInteractiveHookSettings(hookDir, token, port, nonce)
       ├─ ownpty.Create(ctx, cfg, "claude", "Claude", deviceID, deviceEpoch)
       │    └─ OwnedPTYRuntime.Create (owned_pty_runtime.go:78)
       │         ├─ reserve generation (atomic increment)
       │         ├─ NativePTYLauncher.Launch → creack/pty + exec.Cmd
       │         ├─ Recorder.Start(sessionID, pty)
       │         ├─ TerminalTransport (generation-bound, single writer)
       │         ├─ Register in OwnedPTYRuntime store
       │         └─ go exitWatcher() → reaps process, updates state
       └─ startClaudeJSONLNormalizer(canonicalID, claudeUUID, transcriptSvc)
            └─ tails ~/.claude/projects/<dir>/<uuid>.jsonl
            └─ projectLine → FeedAgentSegments
```

**Key types:** `ClaudeInteractiveHost`, `claudeInteractiveRuntime`, `claudeInteractiveBridge`, `claudeJSONLNormalizer`
**Lock ordering:** `h.mu` (ClaudeInteractiveHost) → `ownedPTY` internal lock
**Generation assignment:** `OwnedPTYRuntime.Create` atomically reserves and publishes generation

### 2.2 Codex Create Path

```
CodexTUIHost.Create (codex_tui_host.go:66)
  ├─ ownpty.Create(ctx, cfg, "codex", "Codex", deviceID, deviceEpoch)
  │    └─ OwnedPTYRuntime.Create (same as Claude path above)
  └─ startCodexJSONLTailer(canonicalID, transcriptSvc)
       └─ waits up to 60s for Codex session directory
       └─ tails Codex session JSONL file
       └─ projectLine → FeedAgentSegments
```

**Key types:** `CodexTUIHost`, `codexTUIRuntime`, `codexJSONLTailer`
**Key difference from Claude:** Codex has no hook bridge (no approval delegation). The JSONL tailer watches a provider-managed session file at `~/Library/Application Support/orca/codex-runtime-home/home/sessions/`.

### 2.3 PTY Transport

```
TerminalTransport (terminal_transport.go)
  └─ SubscriberFanOut(sessionID) → (bootstrap []byte, chan []byte, *Recorder, bool)
  │    └─ Creates per-subscriber channel, registers with Recorder
  │    └─ Bootstrap: replay buffer since channel creation
  ├─ WriteInput(data, deviceID, deviceEpoch) → (int, error)
  │    ├─ Check retired → fail-closed
  │    ├─ ClaimInput(deviceID, "") → one-writer enforcement
  │    ├─ AuthorizeAndCommit(deviceID, deviceEpoch, IntentWSInput, fn)
  │    └─ w.Write(data) → PTY write
  ├─ WriteInputAsOwner(data, deviceID, deviceEpoch) → (int, error)
  │    └─ Same as WriteInput but skips ClaimInput (pre-verified owner)
  ├─ Resize(rows, cols, deviceID, deviceEpoch) → error
  ├─ Retire() → sets retired flag, closes subscriber channels
  └─ ClaimInput / ReleaseInput / RefreshInput → owner lease management
```

**Lock ordering:** `t.mu.RLock()` → `authorizer.AuthorizeAndCommit` → `w.Write(data)` (no lock held across external I/O)

### 2.4 Identity Flow

```
Generation assignment:
  OwnedPTYRuntime.Create:
    h.mu.Lock()
    generation = h.nextGen
    h.nextGen++
    store[canonicalID] = SessionLifecycle{State: LifecycleRunning, Epoch: generation}
    h.mu.Unlock()

  TerminalTransport.generation = generation (immutable after creation)

  Every WriteInput/Resize/Retire checks:
    - transport.retired? → fail-closed
    - authorizer.AuthorizeAndCommit(deviceID, deviceEpoch, intent) → generation-gated

  LifecycleService.Stop/Kill/Delete:
    deriveEpoch(id) → catalog.Get(id).Epoch
    owner.Stop(id, rec.Epoch, deviceID, deviceEpoch)
    owner compares request epoch vs. runtime epoch under its own lock
```

---

## 3. Generic Worker Feasibility (Task 3)

### 3.1 What the Current Runtime Already Supports

The `OwnedPTYRuntime.Create` method accepts a `SpawnConfig`:

```go
type SpawnConfig struct {
    Name       string   // local ID component
    CWD        string   // working directory
    Executable string   // binary path
    Args       []string // argv
    Command    string   // legacy shell string (privileged only)
}
```

This is **already generic** — any executable can be launched. The provider-specific
logic is in the callers (`ClaudeInteractiveHost.Create`, `CodexTUIHost.Create`),
not in `OwnedPTYRuntime`.

### 3.2 What Assumes a Known Provider

| Assumption | Where | How to Generalize |
|------------|-------|-------------------|
| Adapter prefix (`claude_headless:`, `codex_app_server:`) | `managed_catalog.go`, `lifecycle_service.go`, `create.go` | Replace with a `WorkerType` registry. Prefix becomes `worker:<type>:<id>`. |
| Provider-specific surface capabilities | `ManagedSurfaceCapabilities` | Workers declare capabilities at registration. |
| Provider-specific event decoding | `managed_codex.go` pump, `claude_interactive_host.go` normalizer | Workers implement `EventSource` interface: `Start(ctx) <-chan StructuredEvent`. |
| Provider-specific prompt contract | `ManagedCodexService.SubmitPrompt` | Workers implement `PromptSink` interface: `Submit(ctx, text) error`. |
| Provider-specific approval | `claudeInteractiveBridge` | Workers implement `ApprovalObserver` interface. |

### 3.3 Smallest Generic Worker Interface

```go
type WorkerSpec struct {
    Type       string   // "claude", "codex", "shell", "custom"
    Executable string   // binary path
    Args       []string
    CWD        string
    Env        []string
}

type WorkerRuntime interface {
    // Identity
    SessionID() string
    RuntimeID() string
    Generation() int64

    // Lifecycle (implemented by OwnedPTYRuntime)
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Kill(ctx context.Context) error

    // I/O (implemented by TerminalTransport)
    WriteInput(data []byte, principal Principal) error
    SubscribeOutput(ctx context.Context) (<-chan []byte, error)
    Resize(rows, cols int) error

    // Structured events (provider-specific)
    EventSource
    PromptSink // optional
}
```

---

## 4. Liveness Signals (Task 4)

| Signal | Source | Exists? | Reliable? | Provider-Independent? | Race Risks |
|--------|--------|---------|-----------|----------------------|------------|
| **Process alive** | `exec.Cmd.ProcessState` via `exitWatcher` | ✅ | ✅ `Wait()` is OS-guaranteed | ✅ | None |
| **Exit code** | `exec.Cmd.ProcessState.ExitCode()` | ✅ | ✅ | ✅ | Must read before reaping |
| **PTY timestamp** | `Recorder` last read time | ✅ | ✅ | ✅ | Clock skew between daemon and observer |
| **Input timestamp** | `TerminalTransport` last write time | ✅ | ✅ | ✅ | Write may be buffered in PTY |
| **Child process** | Process group (`Setpgid: true`) | ✅ | ✅ via `syscall.Kill(-pid, 0)` | ✅ | PID reuse (mitigated by generation guard) |
| **CPU activity** | Not implemented | ❌ | — | — | Would need `/proc` or `task_info` |
| **FS activity** | Not implemented | ❌ | — | — | Would need `FSEvents` or `inotify` |
| **Git activity** | Not implemented | ❌ | — | — | Would need worktree awareness |
| **Network activity** | Not implemented | ❌ | — | — | Provider-specific |
| **Provider events** | Codex: JSON-RPC pump; Claude: JSONL tailer | ✅ | Partial (tailer has latency) | ❌ Provider-specific | File rotation, partial writes |
| **Shell prompt** | Not implemented | ❌ | — | — | ANSI parsing is forbidden |
| **Terminal mode** | Not implemented | ❌ | — | — | Terminal mode detection requires ANSI parsing |
| **Pending approval** | `AuthoritativeApprovalStore` / `claudeInteractiveBridge` waiters | ✅ | ✅ | ❌ Claude-specific (Codex uses different path) | Timeout vs. response race (mitigated by CAS) |
| **Pending question** | Not implemented | ❌ | — | — | Would need `AskUserQuestion` hook |

**Key gap:** No generic "worker is idle" signal. The daemon cannot distinguish
"thinking" from "waiting for input" from "crashed" without provider-specific
event parsing. For orchestration, this is the most important liveness signal.

---

## 5. Supervisor Boundary (Task 5)

### Recommendation: New daemon (`pokitd`), reusing existing locking/generation

The non-LLM supervisor should be a **new binary** that imports the reusable core
as a Go library. It should NOT live inside the existing POKIT daemon.

**Rationale:**
1. The existing daemon is tightly coupled to mobile device pairing, tunnel, and
   QR flow. Adding orchestration would bloat it.
2. The supervisor lifecycle is different: long-lived worker pools vs.
   interactive sessions.
3. Separation enforces the API boundary — the supervisor communicates with
   POKIT daemon through the same client API as mobile.

**Minimum supervisor responsibilities:**
1. Accept `WorkerSpec` → launch via `OwnedPTYRuntime`
2. Maintain worker pool (create, monitor, stop, reap)
3. Route input to correct worker (writer lease per worker)
4. Collect structured events from workers
5. Expose worker state via REST API
6. Persist task/event state for restart survival

**Reuse from POKIT:**
- `OwnedPTYRuntime` — process lifecycle
- `TerminalTransport` — PTY I/O fan-out
- `Recorder` — sole PTY reader
- `LifecycleService` — Stop/Kill/Delete dispatch
- `MutationAuthorizer` — authorization pattern
- `ManagedSessionRegistry` — generation-gated session tracking
- `transcript.Service` — event store

---

## 6. Task/Event State Schema (Task 6)

### Minimum Persistent Schema

```sql
-- Immutable task definition
CREATE TABLE tasks (
    task_id      TEXT PRIMARY KEY,  -- UUID
    worker_type  TEXT NOT NULL,     -- "claude", "codex", "shell", "custom"
    worker_spec  JSON NOT NULL,     -- {executable, args, cwd, env}
    created_at   TIMESTAMP NOT NULL,
    created_by   TEXT NOT NULL      -- principal ID
);

-- One task may have multiple attempts (retries)
CREATE TABLE attempts (
    attempt_id   TEXT PRIMARY KEY,  -- UUID
    task_id      TEXT NOT NULL REFERENCES tasks(task_id),
    attempt_num  INTEGER NOT NULL, -- 1, 2, 3...
    runtime_id   TEXT NOT NULL,     -- POKIT runtime ID
    generation   INTEGER NOT NULL,  -- POKIT generation
    session_id   TEXT NOT NULL,     -- canonical session ID
    status       TEXT NOT NULL,     -- pending|running|completed|failed|killed
    started_at   TIMESTAMP,
    ended_at     TIMESTAMP,
    exit_code    INTEGER,
    UNIQUE(task_id, attempt_num)
);

-- Append-only event log (at-least-once delivery)
CREATE TABLE events (
    event_id     TEXT PRIMARY KEY,  -- UUID
    attempt_id   TEXT NOT NULL REFERENCES attempts(attempt_id),
    seq          INTEGER NOT NULL,  -- monotonic per attempt
    event_type   TEXT NOT NULL,     -- "worker_output"|"worker_input"|"lifecycle"|...
    payload      JSON NOT NULL,
    observed_at  TIMESTAMP NOT NULL,
    UNIQUE(attempt_id, seq)
);

-- Coordinator acknowledgements (idempotent replay)
CREATE TABLE acks (
    ack_id       TEXT PRIMARY KEY,
    event_id     TEXT NOT NULL REFERENCES events(event_id),
    ack_by       TEXT NOT NULL,     -- principal ID
    ack_at       TIMESTAMP NOT NULL,
    status       TEXT NOT NULL      -- consumed|replayed|superseded
);

-- Writer leases (one active writer per attempt)
CREATE TABLE leases (
    lease_id     TEXT PRIMARY KEY,
    attempt_id   TEXT NOT NULL REFERENCES attempts(attempt_id),
    owner_id     TEXT NOT NULL,     -- principal ID
    acquired_at  TIMESTAMP NOT NULL,
    expires_at   TIMESTAMP NOT NULL,
    released_at  TIMESTAMP
);

-- Commit records (immutable validation verdicts)
CREATE TABLE commits (
    commit_id    TEXT PRIMARY KEY,
    attempt_id   TEXT NOT NULL REFERENCES attempts(attempt_id),
    commit_sha   TEXT,
    worktree_path TEXT,
    message      TEXT,
    created_at   TIMESTAMP NOT NULL
);

-- Recovery state (checkpoint for restart)
CREATE TABLE recovery (
    attempt_id   TEXT PRIMARY KEY REFERENCES attempts(attempt_id),
    last_event_seq INTEGER NOT NULL,
    checkpoint   JSON NOT NULL,     -- serialized worker state
    updated_at   TIMESTAMP NOT NULL
);
```

### At-Least-Once, Idempotent, Restart-Safe

- `events.seq` is monotonic per attempt. Replay from last acked seq.
- `acks` enables idempotent replay: coordinator replays unacked events on restart.
- `leases` expire by wall clock. On restart, expired leases are released.
- `recovery` checkpoints worker state. On restart, worker resumes from checkpoint.

---

## 7. Git Worktrees (Task 7)

**Current state: NO git worktree support exists in the codebase.**

All git operations would need to be built. The supervisor daemon should execute
git commands (never the worker process directly — the worker's `Bash` tool runs
in a PTY and should not be given direct git access to the orchestration repo).

**Who executes Git commands:** The supervisor daemon, via `os/exec` of the
system `git` binary. The worker process has NO direct git access.

**Required operations:**
1. `git worktree add --detach <path> <base-ref>` — create isolated worktree
2. `git worktree remove <path>` — cleanup
3. `git -C <worktree> status --porcelain` — detect uncommitted changes
4. `git -C <worktree> stash --include-untracked` — auto-checkpoint
5. `git -C <worktree> add -A && git commit -m "..."` — auto-commit
6. `git -C <worktree> push` — push to remote
7. Secret scan before commit: `grep -rE 'sk-ant|ghp_|BEGIN PRIVATE KEY' <worktree>`
8. Commit trailer: `Co-Authored-By: <worker-type> <worker-id>`

**Prevent simultaneous ownership:** A `worktree_owners` table in the database.
`INSERT INTO worktree_owners (path, attempt_id, acquired_at) VALUES (...)` —
unique constraint on `path` ensures only one owner.

---

## 8. Coordinator Wake-Up (Task 8)

**Current state: NO coordinator exists.** All coordination is implicit in the
mobile app's polling loop.

For orchestration, the coordinator needs explicit wake-up:

| Wake-Up Method | Feasibility | Notes |
|---------------|-------------|-------|
| **PTY injection** | Not recommended | Injecting into a running PTY is fragile. Use stdin pipe (already available via `TerminalTransport.WriteInput`). |
| **REST API** | ✅ Recommended | Supervisor exposes `POST /workers/{id}/prompt`. Worker stdin receives text + Enter via `WriteInput`. |
| **Message queue** | Future | NATS/Redis for multi-daemon deployments. |
| **Resume** | ✅ | `POST /workers/{id}/resume` restarts a stopped worker from checkpoint. |
| **Restart** | ✅ | `POST /workers/{id}/restart` kills and recreates worker with same spec. |

**Edge case handling:**
- **Generating output:** Coordinator polls `GET /workers/{id}/events?cursor=N`. New events → process.
- **Waiting for input:** Worker status is `running` but no recent output. Coordinator decides: send prompt, wait, or interrupt.
- **Processing old event:** Already-acked events are skipped by cursor.
- **Duplicate wake:** Idempotent — `POST /workers/{id}/prompt` with same text is detected by operation cache.
- **Crash:** Worker status → `failed`. Coordinator can `POST /workers/{id}/retry` to create a new attempt.
- **Replacement:** New attempt invalidates old generation. Old events are rejected.
- **Stale queue:** Events older than `expires_at` are dropped.
- **Multiple simultaneous completions:** Each worker is independent. Race is at the coordinator level.

---

## 9. Memory System (Task 9)

**Current state:** No persistent memory system exists. Transcripts are in-memory
(`transcript.Service`), telemetry snapshots are polled, agent activity is
advisory and ephemeral.

**Required for orchestration:**

| Memory Type | Source of Truth | Retrieval | Versioning | Confidence | Invalidation |
|-------------|----------------|-----------|------------|------------|--------------|
| Transcript | `transcript.Service` (append-only) | REST API `GET /transcript?after=N` | Immutable segments | Provider-assigned | Never invalidated |
| Summary | Coordinator-generated after each turn | Separate `summaries` table | Versioned by turn | Explicit confidence field | Invalidated on model version change |
| Metadata | Worker spec + attempt record | SQL query | Immutable | Not applicable | Not applicable |
| Performance | Event timing data | Time-series DB or SQL | Aggregated | Not applicable | Not applicable |
| Validator outcomes | `commits` table | SQL query | Immutable | Pass/Fail/Error | Never invalidated |
| Playbooks | Coordinator configuration | Config file or DB | Versioned | Not applicable | Manual update |

**Critical rule:** Untrusted worker text must NEVER become authority. Worker
output is evidence, not truth. The coordinator validates, summarizes, and
decides. Memory is coordinator-authored, not worker-authored.

---

## 10. Client-Independent API (Task 10)

### 10.1 Current Coupling to Mobile

| Endpoint | Coupled To | Severity |
|----------|-----------|----------|
| `GET /api/sessions` → `SessionTelemetry` | `terminalSurface`, `adapterCapabilities`, `SafeApprovalDTO` | Medium — mobile-specific surface fields |
| `GET /term/ws` | WebSocket binary frames, ticket auth, mobile WebView | High — assumes browser/xterm.js client |
| `POST /api/sessions/{id}/approvals/{approvalId}` | Device bearer auth, mobile notification flow | High — assumes paired mobile device |
| `POST /api/sessions` | `profileId`, adapter routing, QR/device pairing context | Medium — assumes POKIT session model |
| `GET /api/managed-sessions/{id}/events` | `ManagedNativeStatusDTO`, Codex-only event store | Medium — provider-specific |

### 10.2 Proposed Generic Coordinator API

```
POST   /v1/workers                    → create worker from WorkerSpec
GET    /v1/workers                    → list workers with status
GET    /v1/workers/{id}               → worker status + capabilities
DELETE /v1/workers/{id}               → stop + delete worker

POST   /v1/workers/{id}/prompt        → send input to worker
POST   /v1/workers/{id}/interrupt     → interrupt worker
POST   /v1/workers/{id}/resize        → resize PTY

GET    /v1/workers/{id}/events        → structured events (NDJSON)
GET    /v1/workers/{id}/output        → raw PTY output (binary WebSocket)
GET    /v1/workers/{id}/transcript    → bounded Transcript projection

POST   /v1/workers/{id}/leases/claim  → claim writer lease
POST   /v1/workers/{id}/leases/release → release writer lease

POST   /v1/tasks                      → create task
GET    /v1/tasks/{id}                 → task status
POST   /v1/tasks/{id}/attempts        → create new attempt
```

---

## 11. Security Analysis (Task 11)

| Threat | Current Mitigation | Gap for Orchestration |
|--------|-------------------|----------------------|
| **Arbitrary command execution** | `createFromProfile` validates against known profiles; custom executables require IPC (0600 socket). | Worker spec must validate executable path against an allowlist. |
| **Credential access** | Provider API keys are in the user's environment, not stored by daemon. | Worker processes inherit daemon environment. Consider credential isolation per worker. |
| **Cross-worktree access** | No worktree support exists. | Worktree ownership table + filesystem permissions. |
| **Git hooks** | No git hook support. | Git hooks in worktrees must be disabled (`core.hooksPath=/dev/null`). |
| **Malicious repo** | Not addressed. | Clone into daemon-owned directory with restrictive permissions. Never run worker in the orchestration repo. |
| **Prompt injection** | No sanitization of user input to provider. | Worker input is trusted (coordinator-authored). Worker output is untrusted — never parsed as instructions. |
| **Auto-commit** | No auto-commit exists. | Worker never has git credentials. Only supervisor commits. |
| **Local API (IPC socket)** | 0600 permissions. | Same for orchestration socket. |
| **Remote client auth** | JWT + device bearer + tunnel. | For orchestration: API key or mTLS. |
| **Third-party auth** | Supabase JWT verification. | Orchestration uses its own auth (API keys, not Supabase). |

---

## 12. Migration Stages (Task 12)

### Wave 1: Extraction PoC (1-2 days)
- Clone `OwnedPTYRuntime`, `TerminalTransport`, `Recorder` into new `internal/worker/` package
- Remove POKIT-specific adapter prefix assumptions
- Define `WorkerSpec`, `WorkerRuntime` interfaces
- Launch a single `echo hello` worker from a test
- **Deliverable:** Worker package compiles, single test passes

### Wave 2: Generic Lifecycle (1-2 days)
- Generalize `LifecycleService` to N-provider registry
- Implement `WorkerRegistry` (generation-gated, like `ManagedSessionRegistry`)
- Test: create, stop, kill, delete worker
- **Deliverable:** Full lifecycle for generic workers

### Wave 3: Event Store (2-3 days)
- Implement append-only event store (SQLite)
- Migrate `transcript.Service` to persist events
- Implement cursor-based event replay
- **Deliverable:** Events survive daemon restart

### Wave 4: Task/Attempt Model (1-2 days)
- Implement `tasks` + `attempts` tables
- Implement retry logic (new attempt with same spec)
- Test: task creation, attempt lifecycles, retry
- **Deliverable:** Multi-attempt task model

### Wave 5: Git Worktrees (2-3 days)
- Implement worktree create/assign/cleanup
- Implement auto-commit with secret scan
- Prevent simultaneous ownership via DB constraint
- **Deliverable:** Workers can operate in isolated worktrees

### Wave 6: Coordinator Core (2-3 days)
- Implement coordinator loop: poll events, decide, prompt
- Implement writer lease management
- Implement interrupt + restart
- **Deliverable:** Coordinator can drive a single worker through a task

### Wave 7: Multi-Worker (1-2 days)
- Worker pool with concurrency limit
- Independent worker lifecycles
- Fan-out event collection
- **Deliverable:** Multiple workers running concurrently

### Wave 8: Memory + Summarization (2-3 days)
- Implement summary generation after each turn
- Implement memory retrieval (relevant summaries for context)
- Model-version invalidation
- **Deliverable:** Coordinator maintains persistent memory across turns

### Wave 9: Client API (1-2 days)
- Implement generic REST API (no mobile DTOs)
- WebSocket for raw PTY output
- API key authentication
- **Deliverable:** External clients can drive workers

### Wave 10: Production Hardening (2-3 days)
- Secret scanning, credential isolation
- Worktree security (no hooks, restrictive permissions)
- Crash recovery, restart survival
- Performance: connection pooling, event batching
- **Deliverable:** Production-ready orchestration daemon

---

## 13. System Boundary Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                    pokitd (Orchestration Daemon)              │
│                                                               │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌─────────────┐ │
│  │ Worker    │  │ Event     │  │ Git        │  │ Client API   │ │
│  │ Runtime   │  │ Store     │  │ Worktrees  │  │ (REST + WS)  │ │
│  │           │  │           │  │            │  │              │ │
│  │ OwnedPTY  │  │ SQLite    │  │ git add     │  │ POST /worker  │ │
│  │ Terminal  │  │ append-   │  │ git commit  │  │ GET  /events  │ │
│  │ Transport │  │ only      │  │ git push    │  │ WS   /output  │ │
│  │ Recorder  │  │           │  │            │  │              │ │
│  └─────┬─────┘  └─────┬─────┘  └──────┬─────┘  └──────┬──────┘ │
│        │              │               │               │         │
│  ┌─────┴─────┐  ┌─────┴─────┐  ┌──────┴─────┐  ┌──────┴──────┐ │
│  │ Lifecycle  │  │ Task/      │  │ Worktree    │  │ Auth         │ │
│  │ Service    │  │ Attempt    │  │ Owner       │  │ (API Key)    │ │
│  │            │  │ Model      │  │ Table       │  │              │ │
│  │ Stop/Kill  │  │            │  │             │  │              │ │
│  │ Delete     │  │ Retry      │  │ Prevent     │  │              │ │
│  │            │  │ Logic      │  │ Dual-Own    │  │              │ │
│  └───────────┘  └───────────┘  └────────────┘  └─────────────┘ │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ Coordinator Loop                                           │   │
│  │  poll events → decide → prompt → wait → repeat              │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
        │                  │                  │
        ▼                  ▼                  ▼
   ┌─────────┐     ┌─────────────┐     ┌──────────┐
   │ Worker   │     │ Git Remote   │     │ Client    │
   │ Process  │     │ (GitHub)     │     │ (CLI/UI)  │
   │ (PTY)    │     └─────────────┘     └──────────┘
   └─────────┘
```

---

## 14. Reuse Estimate

| Category | Count | Examples |
|----------|-------|----------|
| **Directly reusable files** | ~15 | `owned_pty_runtime.go`, `terminal_transport.go`, `recorder.go`, `lifecycle_service.go`, `managed_registry.go`, `transcript/service.go`, `devicetrust/` authorizer |
| **Refactorable files** | ~10 | `managed_codex.go` (extract JSON-RPC client), `claude_interactive_host.go` (extract PTY+tailer+hook pattern), `managed_catalog.go`, `telemetry_service.go` |
| **Tests preserving contracts** | ~50 | All `*_test.go` files for reusable packages |
| **Contract interfaces** | ~8 | `ProviderLifecycleOwner`, `ManagedRuntimeCatalog`, `ManagedProcess`, `MutationAuthorizer`, `OperationalEventSink` |
| **Separate (POKIT-only)** | ~20 | `qr_pair.go`, `pair.go`, mobile DTOs, Supabase auth, push notifications, device pairing, tunnel |

### Reuse Percentage: ~60%
- Direct: ~30% (core runtime, transport, lifecycle)
- Refactorable: ~20% (provider hosts → generic worker pattern)
- Contracts/tests: ~10% (interfaces, test harnesses)
- Separate: ~40% (mobile, device, pairing, tunnel)

---

## 15. Missing Components

| Component | Status | Priority |
|-----------|--------|----------|
| Event persistence (SQLite) | NOT EXIST | P0 — required for restart survival |
| Task/attempt model | NOT EXIST | P0 — required for retry |
| Git worktree management | NOT EXIST | P1 — required for code-changing agents |
| Coordinator loop | NOT EXIST | P1 — required for autonomous operation |
| Generic WorkerSpec | EXISTS as `SpawnConfig` | P1 — needs generalization |
| Worker registry (N-provider) | PARTIAL (`ManagedRuntimeCatalog` federates exactly 2) | P1 |
| Memory/summarization | NOT EXIST | P2 |
| Client API (generic) | PARTIAL (endpoints are mobile-coupled) | P2 |
| Multi-worker pool | NOT EXIST | P2 |
| Credential isolation | PARTIAL (daemon inherits user env) | P2 |
| Liveness: CPU, FS, network | NOT EXIST | P3 |
| Liveness: shell prompt, terminal mode | NOT EXIST | P3 (forbidden — requires ANSI parsing) |

---

## 16. Critical Blockers

1. **No event persistence.** The orchestration daemon cannot survive a restart.
   All worker state is in-memory. **Must implement in Wave 3.**

2. **No generic worker abstraction.** `OwnedPTYRuntime` is close but the
   adapter prefix system assumes exactly two provider types. **Must implement
   in Wave 1-2.**

3. **No git worktree support.** The orchestration daemon cannot create,
   isolate, or clean up git worktrees. **Must implement in Wave 5.**

4. **No coordinator logic.** The mobile app drives sessions through polling.
   An orchestration system needs an event-driven coordinator. **Must implement
   in Wave 6.**

5. **No task persistence.** Tasks, attempts, and their relationships are not
   modeled. Restart loses all task context. **Must implement in Wave 4.**

---

## 17. Minimal PoC

A single-file Go program (~200 lines) that:
1. Accepts a `WorkerSpec` (executable + args + cwd)
2. Launches the worker via `OwnedPTYRuntime`
3. Reads PTY output via `Recorder` → subscriber channel
4. Writes input via `TerminalTransport.WriteInput`
5. Prints output to stdout
6. Exits when worker exits

This proves the extraction boundary is correct and all reusable components compose.

---

## 18. Final Recommendation

**Proceed with extraction.** The POKIT daemon contains a production-hardened,
generation-gated, authorization-bound process runtime that generalizes well to
orchestration. The core abstractions (`OwnedPTYRuntime`, `TerminalTransport`,
`Recorder`, `LifecycleService`, `transcript.Service`, `MutationAuthorizer`) are
the right primitives.

**Start with the Minimal PoC** to validate the extraction boundaries. Then
follow the 10-wave migration plan, implementing persistence before any
multi-worker or autonomous features.

**Do not modify the existing POKIT daemon.** The orchestration daemon is a new
binary that imports the reusable core as a library. POKIT's mobile/device
surface remains unchanged.

**Estimated total effort:** 15-25 days for a production-capable single-daemon
orchestration system with persistence, git worktrees, and a generic client API.

---

**Review complete:** 2026-07-25
**Next:** Independent architecture review → PoC implementation
