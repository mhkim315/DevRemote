# Orchestration Architecture — High-Level Feasibility Analysis

**Status:** COMPLETED — ARCHITECTURE & PRODUCT ANALYSIS  
**Date:** 2026-07-25  
**Reviewer:** Antigravity (V2 Architecture Auditor)  
**Companion Code Trace:** T1 (DeepSeek) — separate deep trace  

---

## Q1. Where Should the Orchestration Supervisor Live?

### Recommendation: **Inside the daemon — as `internal/orchestration`**

**Rationale:**

| Option | Pros | Cons | Verdict |
|--------|------|------|---------|
| **Inside daemon** (`internal/orchestration`) | Zero-copy access to `OwnedPTYRuntime`, `TranscriptService`, `AuthoritativeApprovalStore`, `DeviceTrust`, `coordination.Broker`; single process group for clean shutdown; shared JWT/JWKS auth stack; no IPC serialization overhead | Daemon restart kills supervisor state; monolith growth risk | **RECOMMENDED** |
| Separate process (sidecar) | Independent restart; fault isolation | Must duplicate auth, device trust, PTY management, or build RPC layer; two process groups to manage; doubles deployment surface | Rejected |
| Separate binary (microservice) | Maximum isolation | Entirely new IPC protocol; loses generation-bound device trust chain; requires service discovery; overkill for single-machine orchestration | Rejected |

**Key architectural reasons:**

1. **`OwnedPTYRuntime` is not remotable.** PTY file descriptors, process group ownership (`syscall.SysProcAttr.Setpgid` in `pty_launcher_v1.go`), and signal delivery (`syscall.Kill(-pgid, sig)`) require same-process authority. The runtime's `Create()` → `watchExit()` → `finalize()` chain (lines 97–416 of `owned_pty_runtime.go`) holds direct references to the OS process handle, the `Recorder`, and the transcript service — none of which are serializable across process boundaries without building an entire PTY proxy layer.

2. **`AuthoritativeApprovalStore` CAS is in-memory.** The approval waiter channels (`chan approvalDecision`) and compare-and-swap resolution live in daemon memory. A separate process would require either shared memory or an RPC approval bridge — adding latency and failure modes to the time-critical Claude hook response path (120s hook timeout in `claude_interactive_host.go`).

3. **Device trust chain is daemon-scoped.** The `compositionMutationAuthorizer` (wired at `app.go:124`) composes `DeviceRegistry`, `IPCMutationAuthorizer`, and `InsecureLocalOnlyMutationAuthorizer`. The 17 distinct `MutationIntent` values (`session:create`, `session:stop`, `ws:input`, `approval:claim`, etc.) are validated against device epoch and JWT claims in-process. Extracting this to a sidecar would require forwarding raw JWTs and re-validating — a security anti-pattern.

4. **Crash recovery is solvable without process separation.** The supervisor state machine can persist its task ledger to SQLite (§Q2). On daemon restart, the supervisor reconciles active workers against the OS process table (`kill(pid, 0)`) and POKIT runtime IDs (via `OwnedPTYRuntime.List()`), then resumes or reaps.

5. **Wiring precedent is already established.** The daemon's `NewAppWithDeps` function (`app.go:267–640`) initializes 10+ services in dependency order. Adding an `orchestration.Supervisor` as step 11 (after `LifecycleService` and interactive hosts) follows the existing pattern exactly.

---

## Q2. Minimal Task/Event Schema (SQLite)

### 7 Core Tables

```sql
-- 1. Runs: top-level orchestration goals
CREATE TABLE runs (
    id              TEXT PRIMARY KEY,            -- UUID
    objective       TEXT NOT NULL,               -- human-readable goal
    repository_id   TEXT NOT NULL,               -- workspace.Identity.RepositoryID
    base_sha        TEXT NOT NULL,               -- starting commit
    status          TEXT NOT NULL DEFAULT 'pending',
        -- pending | running | completed | failed | cancelled
    created_at      TEXT NOT NULL,               -- ISO8601
    updated_at      TEXT NOT NULL,
    completed_at    TEXT,
    result_sha      TEXT,                        -- final merged commit (if completed)
    error_summary   TEXT
);

-- 2. Tasks: decomposed units of work within a run
CREATE TABLE tasks (
    id              TEXT PRIMARY KEY,
    run_id          TEXT NOT NULL REFERENCES runs(id),
    parent_task_id  TEXT REFERENCES tasks(id),   -- DAG support
    ordinal         INTEGER NOT NULL,            -- execution order within run
    description     TEXT NOT NULL,               -- task prompt / objective
    status          TEXT NOT NULL DEFAULT 'pending',
        -- pending | dispatched | executing | validating
        -- | committed | rejected | skipped
    max_attempts    INTEGER NOT NULL DEFAULT 3,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

-- 3. Attempts: individual execution tries for a task
CREATE TABLE attempts (
    id              TEXT PRIMARY KEY,
    task_id         TEXT NOT NULL REFERENCES tasks(id),
    attempt_number  INTEGER NOT NULL,            -- 1-indexed
    agent_id        TEXT NOT NULL REFERENCES agents(id),
    worktree_path   TEXT,                        -- isolated git worktree absolute path
    worktree_branch TEXT,                        -- branch name in worktree
    status          TEXT NOT NULL DEFAULT 'pending',
        -- pending | running | completed | failed | timed_out | cancelled
    started_at      TEXT,
    completed_at    TEXT,
    exit_code       INTEGER,
    commit_sha      TEXT,                        -- worker's commit (if any)
    error_summary   TEXT
);

-- 4. Agents: registered worker identities
CREATE TABLE agents (
    id              TEXT PRIMARY KEY,
    role            TEXT NOT NULL,               -- executor | validator | coordinator
    provider        TEXT NOT NULL,               -- codex | claude | shell
    runtime_id      TEXT,                        -- OwnedPTYRuntime ID
    session_id      TEXT,                        -- POKIT canonical session ID
    generation      INTEGER NOT NULL DEFAULT 0,  -- POKIT launch generation
    status          TEXT NOT NULL DEFAULT 'idle', -- idle | busy | stopped | failed
    capabilities    TEXT,                        -- JSON array
    created_at      TEXT NOT NULL,
    last_heartbeat  TEXT
);

-- 5. Events: append-only audit log
CREATE TABLE events (
    id              TEXT PRIMARY KEY,
    run_id          TEXT REFERENCES runs(id),
    task_id         TEXT REFERENCES tasks(id),
    attempt_id      TEXT REFERENCES attempts(id),
    agent_id        TEXT REFERENCES agents(id),
    event_type      TEXT NOT NULL,
        -- dispatch | start | heartbeat | output | completion
        -- | validation_pass | validation_fail | commit
        -- | merge | timeout | error | cancelled | reap
    payload         TEXT,                        -- JSON (redacted via containsSecretPattern)
    created_at      TEXT NOT NULL
);

-- 6. Leases: time-bounded worker execution claims
CREATE TABLE leases (
    id              TEXT PRIMARY KEY,
    attempt_id      TEXT NOT NULL REFERENCES attempts(id),
    agent_id        TEXT NOT NULL REFERENCES agents(id),
    repository_id   TEXT NOT NULL,
    epoch           INTEGER NOT NULL,            -- monotonic per repository
    granted_at      TEXT NOT NULL,
    expires_at      TEXT NOT NULL,
    heartbeat_at    TEXT NOT NULL,
    released_at     TEXT,
    status          TEXT NOT NULL DEFAULT 'active'
        -- active | expired | released | revoked
);

-- 7. Verdicts: validator judgments on attempts
CREATE TABLE verdicts (
    id              TEXT PRIMARY KEY,
    attempt_id      TEXT NOT NULL REFERENCES attempts(id),
    validator_id    TEXT NOT NULL REFERENCES agents(id),
    verdict         TEXT NOT NULL,               -- pass | fail | inconclusive
    gate_results    TEXT NOT NULL,               -- JSON: build/test/vet/secret/invariant
    evidence_sha    TEXT,                        -- commit SHA of evidence
    snapshot_id     TEXT NOT NULL,               -- workspace.SnapshotManifest binding
    tree_hash       TEXT NOT NULL,               -- exact tree hash validated
    created_at      TEXT NOT NULL
);

CREATE INDEX idx_tasks_run       ON tasks(run_id);
CREATE INDEX idx_attempts_task   ON attempts(task_id);
CREATE INDEX idx_events_run      ON events(run_id);
CREATE INDEX idx_events_task     ON events(task_id);
CREATE INDEX idx_leases_active   ON leases(status) WHERE status = 'active';
CREATE INDEX idx_verdicts_attempt ON verdicts(attempt_id);
```

**Design rationale:**
- **Runs → Tasks → Attempts** is a 3-level hierarchy supporting DAG task dependencies (`parent_task_id`) and retry semantics (`max_attempts`).
- **Events** is append-only for crash forensics and trajectory auditing. Payload is redacted via `containsSecretPattern` (from `managed_codex.go`) before insertion.
- **Leases** mirrors the existing `workspace.Lease` pattern (`Epoch`, `HeartbeatAt`, `ExpiresAt`) but persists to SQLite for crash recovery. The epoch monotonic counter matches `workspace.Manager.epochs`.
- **Verdicts** bind to exact `SnapshotManifest` tree hashes, compatible with the existing `workspace.FindingBinding.Stale()` staleness check.

---

## Q3. Generic Worker Interface: Provider-Specific vs Universal Boundary

### The Existing Adapter Pattern

POKIT already has a provider-neutral adapter interface — the T0 `AgentAdapter` contract in `internal/agent/contract/contract.go` (lines 272–307):

```go
type AgentAdapter interface {
    Descriptor() AgentAdapterDescriptor
    Detect(ctx, session SessionContext) (AgentIdentity, error)
    DiscoverSessions(ctx, in DiscoveryInput) ([]DiscoveredSession, error)
    ReadEvents(ctx, in ReadInput) (ReadResult, error)
    NormalizeEvent(ctx, rec RawRecord) (AgentEvent, DegradedInfo)
    DetectApproval(ctx, events []AgentEvent) ([]AgentApproval, error)
    GetStatus(ctx, in StatusInput) (StatusResult, error)
}
```

This interface handles **observation** (detect, read events, normalize, detect approval, get status). What it does NOT cover is **lifecycle control** (start, stop, kill, dispatch prompt). The orchestration `Worker` interface extends this observation layer with lifecycle commands.

### Proposed Universal Worker Interface

```go
// Worker is the universal interface every orchestrated agent worker implements.
// It composes lifecycle control (start/stop/kill) with the existing
// AgentAdapter observation layer.
type Worker interface {
    Start(ctx context.Context, cfg WorkerConfig) error
    Status() WorkerState
    Heartbeat(ctx context.Context) error
    Stop(ctx context.Context, deadline time.Duration) error
    Kill() error
    SessionID() string
    RuntimeID() string
    Adapter() contract.AgentAdapter   // existing T0 observation layer
}
```

### Boundary Matrix

| Concern | Universal | Provider-Specific | Rationale |
|---------|-----------|-------------------|-----------|
| PTY process spawn | ✅ `OwnedPTYRuntime.Create(SpawnConfig{})` | — | All providers run in PTYs via identical `SpawnConfig` |
| Process group kill | ✅ `syscall.Kill(-pgid, sig)` | — | OS-level, provider-agnostic |
| Heartbeat (alive?) | ✅ `kill(pid, 0)` | — | OS-level |
| Terminal I/O fan-out | ✅ `TerminalTransport.SubscriberFanOut()` | — | Same Recorder→subscriber model |
| Cleanup hooks | ✅ `RegisterCleanupHook(id, fn)` | — | Generic hook registry in `OwnedPTYRuntime` |
| Event normalization | — | ✅ `AgentAdapter.NormalizeEvent()` | Codex JSONL vs Claude JSONL vs shell stdout |
| Turn/completion detection | — | ✅ `AgentAdapter.GetStatus()` / provider signals | Codex `turn/completed` vs Claude hook |
| Approval routing | — | ✅ `AgentAdapter.DetectApproval()` / hook bridge | Claude `PermissionRequest` vs Codex `--auto-edit` |
| CLI argv construction | — | ✅ Provider-specific `SpawnConfig.Args` builder | `codex --json` vs `claude --session-id --permission-mode dontAsk` |
| Binary attestation | — | ✅ Provider-specific SHA-256 verification | `CertifiedClaudeVersion` vs `CertifiedCodexAuthorityVersion` |
| Session ID format | — | ✅ Provider-specific prefix pattern | `codex_app_server:*` vs `claude_headless:*` |

**Conclusion:** Universal ~40% (lifecycle, PTY, heartbeat, transport, cleanup). Provider-specific ~60% (events, completion, approval, CLI, attestation). This maps exactly to the existing split: `OwnedPTYRuntime` + `TerminalTransport` (universal) vs `CodexTUIHost` / `ClaudeInteractiveHost` + `AgentAdapter` (provider-specific).

---

## Q4. Coordinator Wake-Up Mechanism

### Recommendation: **Event-driven channel fan-in with bounded heartbeat fallback**

The coordinator IS a managed terminal session (`OwnedPTYRuntime` instance). It cannot receive HTTP callbacks or stdin injection from outside the daemon process. The safest mechanism uses Go channels within the same process.

```
┌────────────────────────────────────────────────────────────────┐
│ Supervisor Event Loop (single goroutine in internal/orchestration) │
│                                                                │
│   select {                                                     │
│   case ev := <-workerEvents:      // OwnedPTYRuntime finalize  │
│   case ev := <-approvalEvents:    // ApprovalStore resolution  │
│   case ev := <-coordinationMsg:   // coordination.Broker       │
│   case ev := <-transcriptEvents:  // transcript.Service append │
│   case <-heartbeatTicker.C:       // 30s periodic sweep        │
│   case <-ctx.Done():              // daemon shutdown           │
│   }                                                            │
└────────────────────────────────────────────────────────────────┘
```

**Why this is the safest mechanism:**

1. **No busy polling.** The goroutine blocks on `select` — zero CPU when idle.
2. **No external HTTP dependency.** Worker state changes are delivered via Go channels from the same process, not HTTP callbacks that could fail or be lost.
3. **Heartbeat ticker as dead-man's switch.** Every 30s, the supervisor scans active leases for expiry (`workspace.Manager.Check()`), detects hung workers (no heartbeat renewal), and reaps zombies via `OwnedPTYRuntime.Kill()`. This catches edge cases where a channel send is missed.
4. **Coordination Broker integration.** The existing `coordination.Broker` already defines `Envelope` with 10 message types (`Instruction`, `Status`, `Result`, `ValidationRequest`, `Cancellation`, `HandoffMessage`, `FailureRecovery`, `Question`, `Finding`, `RevisionRequest`). These map directly to supervisor wake-up events.
5. **Duplicate wake-up safety.** Each event is matched against the current task/attempt state in SQLite (`attempts.status`). A duplicate `completion` event for an already-committed attempt is a no-op. CAS epoch check prevents stale events from advancing the state machine.

**Integration points with existing code:**
- `OwnedPTYRuntime.finalize()` (line 372): Emits `LifecycleExited` / `LifecycleKilled` → fan-in to `workerEvents`.
- `ApprovalStore.Resolve()` → fan-in to `approvalEvents`.
- `coordination.Broker.Transition()` → fan-in to `coordinationMsg`.
- `transcript.Service.AppendSegment()` → fan-in to `transcriptEvents`.

**Anti-patterns avoided:**
- File-system polling (watching a sentinel file) — race-prone, non-atomic.
- PTY stdout regex matching — fragile, unbounded latency.
- External HTTP webhook to a supervisor port — adds network failure modes.

---

## Q5. Git Worktree Ownership Model

### Recommendation: **Supervisor-owned `WorktreeManager` (internal service)**

| Model | Description | Verdict |
|-------|-------------|---------|
| Coordinator-owned | AI coordinator session runs `git worktree add` in its PTY | **Rejected** — coordinator should dispatch, not execute shell commands; output parsing is fragile |
| Worker-owned | Each worker creates its own worktree before starting | **Rejected** — creates cleanup orphan risk if worker crashes before registering worktree |
| **Supervisor-owned** | `orchestration.Supervisor` owns a `WorktreeManager` service | **RECOMMENDED** — supervisor controls full lifecycle: create before dispatch, delete after verdict |
| Dedicated OS service | Separate `internal/worktree` service package | Acceptable variant for post-MVP refactor; extra indirection for MVP |

**WorktreeManager design:**

```go
type WorktreeManager struct {
    repoRoot    string  // primary repository path
    worktreeDir string  // e.g., /tmp/pokit-worktrees/ or $TMPDIR/pokit-orch/
    mu          sync.Mutex
    active      map[string]WorktreeInfo  // taskID → info
}

func (m *WorktreeManager) Create(taskID, baseSHA string) (path, branch string, err error)
func (m *WorktreeManager) Delete(taskID string) error
func (m *WorktreeManager) List() []WorktreeInfo
func (m *WorktreeManager) SecretScan(taskID string) error      // pre-commit gate
func (m *WorktreeManager) CommitInWorktree(taskID, msg string) (sha string, err error)
func (m *WorktreeManager) FastForwardMerge(taskID, target string) error
func (m *WorktreeManager) ReconcileOrphans() error             // cleanup on restart
```

**Branch naming convention:** `orch/<run-id-short>/<task-ordinal>/<attempt>` (e.g., `orch/a1b2c3/01/1`).

**Lifecycle integration with existing `workspace.Lease`:**
- Before creating a worktree, the supervisor calls `workspace.Manager.Acquire()` with `ModeSharedSequential`.
- On attempt completion, the supervisor calls `workspace.Manager.Release()`.
- `workspace.Manager.DetectDrift()` is called before verdict to confirm the worktree tree hash hasn't diverged.

**Cleanup guarantees:**
- Attempt failure: worktree preserved 24h for forensics, branch NOT merged.
- Attempt success + verdict pass: fast-forward merge, then worktree deleted.
- Daemon restart: `ReconcileOrphans()` cross-references active leases in SQLite against `git worktree list --porcelain`; removes worktrees whose leases have expired.
- Secret scan runs BEFORE `git add`/`git commit` inside the worktree, using the existing `containsSecretPattern` function.

---

## Q6. Client-Independent API Refactoring

### Already client-independent (reusable without changes):

| API / Component | Current Client | Orchestrator Usage | Notes |
|-----------------|---------------|-------------------|-------|
| `POST /api/sessions` (create) | Mobile | Supervisor creates executor/validator sessions | Routes through `createFromProfile` |
| `POST /api/sessions/{id}/input` | Mobile `FeedScreen` | Supervisor injects task prompt into worker PTY | Uses `TerminalTransport.WriteInput()` |
| `POST /api/sessions/{id}/interrupt` | Mobile | Supervisor sends SIGINT to hung worker | Uses `OwnedPTYRuntime.Interrupt()` |
| `POST /api/sessions/{id}/stop` | Mobile | Graceful worker shutdown | Uses `OwnedPTYRuntime.Stop()` |
| `POST /api/sessions/{id}/kill` | Mobile | Force-kill unresponsive worker | Uses `OwnedPTYRuntime.Kill()` |
| `DELETE /api/sessions/{id}` | Mobile | Worker cleanup after attempt | Uses `OwnedPTYRuntime.Delete()` |
| `GET /api/sessions/{id}/transcript` | Mobile `TranscriptRenderer` | Validator reads executor output | Uses `transcript.Service` |
| `POST /api/sessions/{id}/approvals/{id}` | Mobile approval sheet | Auto-approve or route to mobile | Uses `ApprovalStore` |
| `GET /term/ws` (WebSocket) | Mobile terminal view | Supervisor observes worker output | Uses `TerminalTransport.SubscriberFanOut()` |
| `coordination.Broker.Enqueue()` | Internal coordination | Task dispatch / result messages | Already provider-neutral |
| `workspace.Manager.Acquire()` | Internal workspace | Worktree lease management | Already has heartbeat + drift detection |

### Needing small additive changes:

| API | Current Shape | Required Change | Effort |
|-----|--------------|----------------|--------|
| Session creation routing | `profileId` maps to `codex`/`claude`/standard | Add `orchestration` profile in `create.go` routing (lines 38–237) that creates workers under supervisor ownership | ~20 lines |
| Session telemetry DTO | Returns fixed capability fields | Add `orchestratorRunId`, `taskId`, `attemptNumber` to response | ~10 lines |
| `build-gate.sh` | Shell script (11 gate stages) | Wrap as Go function callable from validator worker: `func RunBuildGate(worktreePath string) (GateResult, error)` | ~60 lines |
| Transcript query | Returns all segments | Add `sinceSequence` parameter (already has `Seq int64` in `TranscriptSegment`) for incremental polling | ~15 lines |
| Process cleanup | `cleanupHooks` map on `OwnedPTYRuntime` | Emit cleanup event to supervisor channel on hook execution | ~5 lines |

**Quantitative estimate:** ~85% of the existing HTTP API surface (11 of 13 critical endpoints) is directly reusable without modification. The remaining ~15% requires only additive changes (new fields, new profile routing), not breaking refactors.

---

## Q7. 10-Step Migration Plan (Non-Destabilizing)

> **Principle:** Each step produces a green build gate (`./scripts/build-gate.sh`). No step modifies existing production code paths. Feature-flag isolation via `--enable-orchestration` (default OFF).

| Step | Description | Changes to Existing Code | Risk | Deliverable |
|------|-------------|------------------------|------|-------------|
| **M1** | Add `internal/orchestration` package skeleton: `Supervisor` struct, `Config`, SQLite migration | **None** — new package only | Minimal | Empty supervisor, schema files, unit tests |
| **M2** | Implement `WorktreeManager`: `Create`/`Delete`/`List`/`ReconcileOrphans`/`SecretScan` | **None** — new files in `internal/orchestration` | Low | Integration tests with real `git worktree` ops |
| **M3** | Define `Worker` interface + implement `ShellWorker` (runs arbitrary commands in worktree) | **None** — new files | Low | ShellWorker executes `build-gate.sh` in isolated worktree |
| **M4** | Implement supervisor event loop + task state machine (SQLite-backed, 7-state FSM) | **None** — new files | Medium | Deterministic state transition tests |
| **M5** | Wire `Supervisor` into `app.go`: `--enable-orchestration` flag, initialization after `LifecycleService` | **~15 lines added** to `app.go` (feature-gated, no behavioral change when OFF) | Low | Feature-flagged supervisor, zero alpha impact |
| **M6** | Implement `CodexWorker`: wraps `CodexTUIHost` + `codex_adapter.go` in worktree context | **None** — new file composing existing hosts | Medium | Codex worker launches in isolated worktree |
| **M7** | Implement `ClaudeWorker`: wraps `ClaudeInteractiveHost` + `claude_adapter.go` in worktree context | **None** — new file composing existing hosts | Medium | Claude worker with approval bridge forwarding |
| **M8** | Implement `ValidatorWorker`: runs `build-gate.sh` + records `Verdict` in SQLite | **None** — new file | Low | Validator runs gate, reports pass/fail |
| **M9** | End-to-end integration test: Supervisor dispatches 1 task → Executor → Validator → merge | M5–M8 | High | Full MVP proof with real Codex/Claude worker |
| **M10** | Mobile UI: orchestration run status view + task progress indicators | New RN screens consuming existing APIs | Medium | React Native screens for run monitoring |

**Alpha stability guarantees:**
- Steps M1–M4 add only new files in `internal/orchestration/`. Zero existing files touched.
- Step M5 adds `~15 lines` to `app.go`, all behind `Config.EnableOrchestration` flag (default `false`).
- Steps M6–M8 create new adapter wrappers that **compose** existing hosts, not modify them.
- The existing `build-gate.sh` enforces: `go build`, `go vet`, `go test -race`, `git diff --check`, `npm typecheck`, `npm test`, Kotlin compile, vendor scan, ID inference scan, secret scan. All must pass at every step.

---

## Q8. Reuse Estimate

### Quantitative Breakdown

| Category | % | Components |
|----------|---|------------|
| **Directly reusable** | **~45%** | `OwnedPTYRuntime` (~350 LOC), `TerminalTransport` (~380 LOC), `Recorder`, `transcript.Service` (~200 LOC), `AuthoritativeApprovalStore` (~100 LOC), `devicetrust.*` (~400 LOC: `MutationAuthorizer`, `DeviceRegistry`, `IPCMutationAuthorizer`, `AuthHandler`, `ChallengeStore`, `WSTicketStore`), `containsSecretPattern`, `coordination.Broker` (~210 LOC), `coordination.Envelope` + `Handoff`, `workspace.Identity` + `workspace.Lease` + `workspace.Manager` (~260 LOC), `AgentAdapter` contract + 2 provider adapters (~300 LOC), HTTP handlers (session CRUD, input, interrupt, approvals, transcript, terminal WS — ~1,200 LOC) |
| **Reusable with refactor** | **~20%** | `CodexTUIHost` (wrap `Create()` to accept worktree `CWD`), `ClaudeInteractiveHost` (wrap `Create()` + `hookDir` to accept worktree context), `create.go` (add `orchestration` profile route), `SessionTelemetry` DTO (add orchestration fields), `build-gate.sh` (wrap as Go-callable function) |
| **Must build new** | **~30%** | `Supervisor` state machine + event loop, `WorktreeManager`, `Worker` interface + `CodexWorker` / `ClaudeWorker` / `ShellWorker` / `ValidatorWorker`, SQLite schema + migration, task DAG resolver, crash recovery / orphan reconciliation, supervisor HTTP endpoints (optional) |
| **POKIT-only** | **~5%** | Mobile React Native UI (`FeedScreen.tsx`, `TranscriptRenderer.tsx`, approval sheets) — these consume the API but are not part of the orchestration engine itself |

### Existing Infrastructure Inventory

| Infrastructure | Package | Approx LOC | Orchestration Role |
|---------------|---------|-----------|-------------------|
| PTY process lifecycle | `internal/term/owned_pty_runtime.go` | ~500 | Worker process spawning, cleanup hooks, signal delivery, exit watching |
| PTY launcher | `internal/term/pty_launcher_v1.go` | ~150 | `SpawnConfig` → OS PTY creation |
| Terminal fan-out | `internal/term/terminal_transport.go` | ~380 | Worker output streaming, input ownership, subscriber bootstrap |
| Session routing | `internal/term/create.go` | ~240 | Profile → provider host dispatch |
| Coordination messaging | `internal/coordination/coordination.go` | ~210 | Inter-session envelope delivery + capability check |
| Workspace identity & lease | `internal/workspace/` | ~260 | Worktree lease, heartbeat, drift detection, finding staleness |
| Transcript projection | `internal/transcript/contract.go` | ~180 | Structured event segments with availability states |
| Device trust & auth | `internal/devicetrust/` | ~700 | 17 mutation intents, JWT challenge, device registry |
| Agent adapter contract | `internal/agent/contract/` | ~310 | 7-operation provider-neutral observation interface |
| Codex adapter | `internal/agent/codex_adapter.go` | ~200 | Codex JSONL event normalization |
| Claude adapter | `internal/agent/claude_adapter.go` | ~200 | Claude JSONL event normalization |
| Secret scanning | `internal/term/managed_codex.go` | ~15 | `containsSecretPattern` pre-commit gate |
| Approval CAS | `internal/term/claude_interactive_host.go` | ~100 | `approvalWaiter` first-response-wins arbitration |
| HTTP handlers | `internal/term/pty.go` + lifecycle/approval handlers | ~1,200 | Session CRUD, input, interrupt, approvals, transcript, WS |
| Build gate | `scripts/build-gate.sh` | ~110 | 11-stage deterministic validation gate |
| **Total directly reusable** | | **~4,755** | |

**Bottom line:** POKIT has already built the hard infrastructure across 14 internal packages. The orchestration layer is primarily a **state machine and workflow engine** (~30% new code) that composes these existing primitives through well-defined Go interfaces — not a ground-up systems build. The `AgentAdapter` contract (7 operations), `OwnedPTYRuntime` (full PTY lifecycle), `coordination.Broker` (typed envelope delivery), and `workspace.Lease` (cooperative locking with drift detection) collectively provide ~70% of what the orchestration supervisor needs.

---

## Summary

| Question | Answer |
|----------|--------|
| **Q1. Supervisor location** | Inside daemon as `internal/orchestration` — not remotable due to PTY fd ownership, approval CAS, and device trust chain |
| **Q2. SQLite schema** | 7 tables: `runs`, `tasks`, `attempts`, `agents`, `events`, `leases`, `verdicts` — mirrors existing `workspace.Lease` epoch pattern |
| **Q3. Worker boundary** | Universal ~40% (lifecycle/PTY/heartbeat). Provider-specific ~60% (events/completion/approval/CLI). Extends existing `AgentAdapter` with lifecycle control |
| **Q4. Wake-up mechanism** | Channel fan-in (`workerEvents` + `approvalEvents` + `coordinationMsg` + `transcriptEvents` + 30s heartbeat ticker) — event-driven, no polling |
| **Q5. Worktree ownership** | Supervisor-owned `WorktreeManager` — creates before dispatch, deletes after verdict, reconciles orphans on restart |
| **Q6. API reuse** | ~85% of HTTP surface reusable as-is (11/13 endpoints). ~15% needs small additive changes (profile routing, DTO fields) |
| **Q7. Migration** | 10 steps, feature-flagged (`--enable-orchestration`), each step green on build gate, zero existing file modification until M5 |
| **Q8. Reuse** | ~45% direct reuse (~4,755 LOC), ~20% refactor, ~30% new build, ~5% POKIT-only. The orchestration supervisor is a workflow engine composing existing primitives |

---

**Report Completed:** 2026-07-25  
**Reviewer:** Antigravity (V2 Architecture Auditor)
