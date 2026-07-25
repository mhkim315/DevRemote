# Technical Thesis & Product Boundary Definition: Terminal Agent Orchestration

**Status:** APPROVED TECHNICAL THESIS & BOUNDARY DEFINITION  
**Target Document:** `docs/ORCHESTRATION_TECHNICAL_THESIS.md`  
**Date:** 2026-07-26  
**Reviewer:** Antigravity (V2 Architecture Auditor)  
**Final Verdict:** **PROTOTYPE-FIRST (RUNTIME-KERNEL)**

---

## 1. Feature Classification & Removal of Non-Differentiators

To build a defensible, high-leverage architecture, we strictly strip away commodity features and false differentiators. Implementing a feature differently from existing tools does NOT make it a product differentiator.

| Candidate Feature | Classification | Rationale & Treatment |
|---|---|---|
| **TMUX multiplexing** | **REMOVE** | Commodity CLI wrapper pattern (used by CAO, agtx, Claude Squad). POKIT's native Go `OwnedPTYRuntime` renders TMUX obsolete and eliminates screen-scraping fragility. |
| **Kanban Board TUI** | **REMOVE** | Commodity UI distraction (agtx, CCManager). POKIT focuses on backend daemon execution and mobile supervision, not terminal drag-and-drop boards. |
| **Raw Stdio Streaming** | **REUSE** | Already implemented by POKIT's `TerminalTransport` and `Recorder`. Not a new differentiator. |
| **Multi-LLM API Gateway** | **COMMODITY** | Solved by OpenRouter/LiteLLM. POKIT orchestrates native CLI binaries (`codex`, `claude`), not raw LLM API endpoints. |
| **Basic Git Worktree Creation** | **REUSE** | Commodity Git CLI capability (`git worktree add`). The differentiator is epoch-bound lease locking and drift detection, not running the git binary. |
| **Slack / Discord Webhooks** | **INTEGRATE** | Generic notification hooks. Trivial integration using existing daemon push channels. |
| **Epoch-Bound CAS Mobile Approval Bridge** | **TRUE DIFFERENTIATOR #1** | Single-responder CAS approval routing (`AuthoritativeApprovalStore` + `claudeInteractiveBridge`) between local PTY workers and remote mobile devices (`X-POKIT-Nonce`). No competitor possesses this. |
| **Deterministic Build-Gate Validator Worktrees** | **TRUE DIFFERENTIATOR #2** | Automatic isolation of AI execution in Git worktrees paired with 100% deterministic build/test/secret/invariant verification (`build-gate.sh`) prior to branch merge. |
| **Provider Telemetry Seam & Generation-Bound FSM** | **TRUE DIFFERENTIATOR #3** | Combining OS process group lifecycle (`OwnedPTYRuntime`) with provider-native event projection (`TranscriptService`) and a crash-resilient SQLite supervisor state machine. |

---

## 2. Narrow Technical Thesis (Max 3)

### Thesis 1: Deterministic Build-Gate Verification is Mandatory for Safe Unattended Branch Merging
- **What Predecessors Do:** Overstory, Gas Town, and Orca rely on visual user inspection or LLM-as-a-Validator self-review.
- **What They Fail to Do:** LLM self-review inherits executor reasoning biases and hallucinations, passing subtly broken code. Visual inspection requires human presence, destroying the value of unattended background orchestration.
- **Why the Difference Matters:** Deterministic verification gates (`go test -race`, `npm test`, static analysis, secret scanning) provide binary ground truth (`PASS`/`FAIL`). A branch is merged if and only if 100% of deterministic checks pass.
- **Provable in Small Prototype?** Yes. 1 Executor + 1 Shell Validator running `./scripts/build-gate.sh`.
- **Requires POKIT Code?** Benefits directly from POKIT's `containsSecretPattern` and `workspace.Lease` epoch tracking.

### Thesis 2: Provider-Native Telemetry Seam Prevents False Completion Kills
- **What Predecessors Do:** CAO, agtx, and Claude Squad use stdout silence (inactivity timers) or raw process exit codes to infer task completion.
- **What They Fail to Do:** Terminal silence occurs during extended model reasoning (e.g. Claude extended thinking), long compilation steps, or pending interactive prompts. Inactivity timers prematurely kill active workers or report false success.
- **Why the Difference Matters:** Task completion must be attested by provider-native turn events (Codex RPC `turn/completed`, Claude `SessionEnd` hooks) paired with OS process table verification (`kill(pid, 0)`).
- **Provable in Small Prototype?** Yes. Running complex multi-minute reasoning prompts and comparing inactivity timeouts against provider-native event completion.
- **Requires POKIT Code?** Uses POKIT's `AgentAdapter` 7-operation contract and `TranscriptService`.

### Thesis 3: CAS Mobile Approval Arbitration Preserves Subprocess Safety in Unattended Execution
- **What Predecessors Do:** Symphony auto-allows all tool executions (dangerous). Other tools block execution indefinitely in an un-monitored PTY or fail closed.
- **What They Fail to Do:** Neither allows out-of-band human authorization when an agent requests elevated operations (`rm`, `git push --force`, cloud mutations).
- **Why the Difference Matters:** POKIT routes CLI permission prompts to the user's mobile device via an HTTP bridge (`claudeInteractiveBridge`), authenticated by 32-byte HMAC nonces (`X-POKIT-Nonce`) with Compare-And-Swap (CAS) first-response-wins semantics.
- **Provable in Small Prototype?** Yes. A CLI worker triggering a permission request while the user approves via a mobile API call within 120 seconds.
- **Requires POKIT Code?** Requires POKIT's `AuthoritativeApprovalStore`, `devicetrust.MutationAuthorizer`, and push notification infrastructure.

---

## 3. Build-vs-Fork Analysis for 3 Closest Projects

| Dimension | Overstory (`jayminwest/overstory`) | Gas Town (`gastownhall/gastown`) | AWS Labs CAO (`awslabs/cli-agent-orchestrator`) |
|---|---|---|---|
| **License** | MIT | Apache-2.0 | Apache-2.0 |
| **Status / Maintenance** | 🔴 Archived (moved to Warren) | 🟢 Active | 🟡 Experimental |
| **Language** | Node.js / TypeScript | Go | Python / Bash |
| **Architecture** | CLI mail ledger in SQLite | Wasteland trust network | `cao-server` + TMUX panes |
| **Coupling** | Stdio pipes, no PTY | Subprocess pipes, file ledger | Hard TMUX dependency |
| **Test Quality** | Moderate unit tests | Basic tests | Experimental / Minimal |
| **Security & Auth** | None (local CLI) | None (local CLI) | None (local CLI) |
| **Provider Assumptions** | Hardcoded adapters | Hardcoded adapters | Screen scraping TMUX |
| **Migration Cost** | High (converting Node.js to Go daemon) | High (unraveling Wasteland DAG) | High (rewriting Python in Go) |
| **Verdict** | **DO NOT FORK** (Reuse mail schema concepts) | **DO NOT FORK** (Reuse worktree lifecycle concepts) | **REJECT FORK** (TMUX dependency is an anti-pattern) |

### Justified Recommendation: **NO FORKS. Build `internal/orchestration` inside POKIT daemon.**
- Forking external projects introduces technical debt, incompatible languages (Node.js/Python), missing PTY management, and zero mobile authorization support.
- Building an internal package (`companion-daemon/internal/orchestration`) composes POKIT's existing ~4,755 lines of tested Go infrastructure (`OwnedPTYRuntime`, `AuthoritativeApprovalStore`, `TranscriptService`, `devicetrust`, `coordination.Broker`).

---

## 4. Minimum Novel Prototype Design

### Objective
Demonstrate a 3-agent orchestration run (1 Coordinator + 1 Executor + 1 Validator) executing a multi-step code modification in an isolated Git worktree without human terminal presence, enforcing secret scanning and deterministic build-gate validation.

### Prototype Specification

```text
                               ┌────────────────────────────────┐
                               │ Supervisor State Machine (FSM)  │
                               └───────────────┬────────────────┘
                                               │ Spawns & Controls
                        ┌──────────────────────┴──────────────────────┐
                        ▼                                             ▼
       ┌────────────────────────────────┐            ┌────────────────────────────────┐
       │ Executor Agent (Codex/Claude)  │            │ Validator Agent (Shell)        │
       │ - Runs in isolated Git worktree│            │ - Runs ./scripts/build-gate.sh │
       │ - Managed by OwnedPTYRuntime   │            │ - Inspects worktree tree hash  │
       └────────────────────────────────┘            └────────────────────────────────┘
```

- **Hypothesis:** A supervisor state machine driving a PTY executor in an isolated Git worktree, paired with a deterministic build-gate validator, achieves 100% build safety and 0% false inactivity terminations across 10 sequential tasks.
- **Setup:**
  - Repo: `DevRemote` primary worktree.
  - Base SHA: Current `HEAD`.
  - Task: Create a new utility function in Go + unit tests.
  - Worktree: `$TMPDIR/pokit-orch/task-001`.
- **Evidence Gathered:**
  - `runs`, `tasks`, `attempts`, `events`, `leases`, `verdicts` tables in SQLite.
  - Redacted JSONL transcript in `transcript.Service`.
  - Git commit SHA on successful merge.
- **Success Conditions:**
  1. Worktree created clean; main repository untouched during execution.
  2. Executor completes task; provider turn/completed event detected (no inactivity timeout).
  3. Secret scanner passes with 0 findings.
  4. Validator executes `./scripts/build-gate.sh`; returns `PASS`.
  5. Supervisor fast-forward merges worktree branch into primary branch and cleans up worktree.
- **Failure Conditions:**
  - Build gate fails → worktree preserved for debugging, branch NOT merged, attempt marked `failed`.
  - Secret scan triggers → execution aborted, worktree deleted, secret alert emitted.

---

## 5. Universal Worker Contract & 6. Native Extension Contract

### 5.1 Universal Worker Taxonomy

The contract strictly separates 5 tiers of information authority:

```text
┌─────────────────────────────────────────────────────────────────────────┐
│ 1. OS Facts (Kernel Authority)                                          │
│    PID, process group ID, exit code, CPU/IO bytes, open FDs, PTY status  │
├─────────────────────────────────────────────────────────────────────────┤
│ 2. Inferred Signals (Heuristic Authority)                               │
│    Output silence duration, token generation velocity, prompt patterns   │
├─────────────────────────────────────────────────────────────────────────┤
│ 3. Worker Claims (Agent Self-Reporting)                                 │
│    "I have completed task X", "I am editing file Y", tool call requests │
├─────────────────────────────────────────────────────────────────────────┤
│ 4. Coordinator Decisions (Supervisor Authority)                         │
│    Dispatch task, grant lease, issue SIGINT, trigger retry, request human│
├─────────────────────────────────────────────────────────────────────────┤
│ 5. Validator Decisions (Ground Truth Authority)                         │
│    Build gate PASS/FAIL, secret scan PASS/FAIL, tree hash binding       │
└─────────────────────────────────────────────────────────────────────────┘
```

### 5.2 Universal Worker Interface

```go
type UniversalWorker interface {
    // OS & Lifecycle Control
    Start(ctx context.Context, cfg WorkerConfig) error
    Stop(ctx context.Context, deadline time.Duration) error
    Kill() error
    Heartbeat(ctx context.Context) error
    
    // Identity & Authority
    SessionID() string
    RuntimeID() string
    Generation() int64
    WorktreePath() string
    
    // Status Query
    Status() WorkerState
}
```

### 5.3 Native Extension Contract (Provider-Specific)

```go
type NativeExtension interface {
    // Structured Event Projection
    ReadStructuredEvents(ctx context.Context, sinceSeq int64) ([]AgentEvent, error)
    
    // Approval Routing
    DetectPendingApproval(ctx context.Context) (*ApprovalRequest, bool)
    DeliverApprovalDecision(ctx context.Context, approvalID string, decision Decision) error
    
    // Provider Completion
    IsTurnCompleted(ctx context.Context, rawOutput []byte) bool
    
    // Token & Cost Accounting
    UsageStats() (inputTokens, outputTokens int64, estimatedCostUSD float64)
}
```

---

## 7. Supervisor State Machine & 8. Git Ownership Matrix

### 7.1 Supervisor State Machine

```text
                     ┌───────────┐
                     │   IDLE    │
                     └─────┬─────┘
                           │ Task Dispatched
                           ▼
                    ┌─────────────┐
                    │ DISPATCHED  │ (Worktree Created, Lease Granted)
                    └──────┬──────┘
                           │ Worker Started
                           ▼
                    ┌─────────────┐
                    │  EXECUTING  │ (PTY Active, Heartbeat Monitored)
                    └──────┬──────┘
             ┌─────────────┼─────────────┐
             │ Turn        │ Timeout/    │ Approval Required
             │ Completed   │ Crash       ▼
             ▼             │      ┌──────────────┐
     ┌──────────────┐      │      │ WAITING_     │
     │  VALIDATING  │      │      │ APPROVAL     │
     └──────┬───────┘      │      └──────┬───────┘
     Pass   │   Fail       │             │ Approved / Denied
     ┌──────┴──────┐       │             └───────┬───────┘
     ▼             ▼       ▼                     │
┌───────────┐ ┌──────────────┐                   │
│ COMMITTED │ │ REMEDIATING/ │ ◄─────────────────┘
│  (Merged) │ │   RETRYING   │
└───────────┘ └──────────────┘
```

- **Linearization Points:**
  1. `AcquireLease`: Atomic CAS update in `leases` table.
  2. `CommitWorktree`: Pre-commit secret scan + git commit in worktree branch.
  3. `RecordVerdict`: Immutable insertion into `verdicts` table bound to tree hash.
  4. `FastForwardMerge`: Single-threaded git merge into target branch.
- **Idempotency Keys:** `run_id` + `task_id` + `attempt_number` for all state transitions.

### 7.2 Git Ownership Matrix

| Operation | Worker Agent | Supervisor FSM | Validator Gate | Coordinator | User |
|---|---|---|---|---|---|
| **Create Worktree** | ❌ No | ✅ **Owner** | ❌ No | ❌ No | ❌ No |
| **Modify Workspace** | ✅ **Owner** (in worktree) | ❌ No | ❌ No | ❌ No | Manual override |
| **Stage & Commit** | ❌ No | ✅ **Owner** (pre-commit check) | ❌ No | ❌ No | ❌ No |
| **Run Tests / Build** | ❌ No | ❌ No | ✅ **Owner** | ❌ No | ❌ No |
| **Fast-Forward Merge**| ❌ No | ✅ **Owner** (post-verdict) | ❌ No | ❌ No | ❌ No |
| **Delete Worktree** | ❌ No | ✅ **Owner** (cleanup) | ❌ No | ❌ No | Manual override |

---

## 9. Memory & Routing Architecture

### Storage Layer Taxonomy (No Over-Engineered ML)

```text
┌────────────────────────────────────────────────────────────────────────┐
│ SQLite Database (runs.db)                                              │
│ - Authoritative relational state: runs, tasks, attempts, agents,       │
│   events, leases, verdicts                                             │
├────────────────────────────────────────────────────────────────────────┤
│ Structured JSONL Files (transcripts/)                                  │
│ - High-frequency, append-only, secret-redacted event streams           │
├────────────────────────────────────────────────────────────────────────┤
│ Git Metadata & Tree Hashes                                             │
│ - Immutable codebase state, branch commits, diff digests               │
└────────────────────────────────────────────────────────────────────────┘
```

- **No Vector DBs or Unnecessary ML:** Model selection and routing use simple deterministic rule tables (e.g. task type → preferred provider) based on past verdict pass rates recorded in SQLite.

---

## 10. POKIT Relationship & Alpha Stability

### Relationship Definition: **Internal Package (`companion-daemon/internal/orchestration`)**

- **Why:** Full access to POKIT's Go infrastructure (`OwnedPTYRuntime`, `AuthoritativeApprovalStore`, `TranscriptService`, `devicetrust`, `coordination.Broker`) without IPC or serialization overhead.
- **What is Reused As-Is:**
  - `OwnedPTYRuntime` (PTY process management)
  - `TerminalTransport` (Output fan-out)
  - `AuthoritativeApprovalStore` (CAS approval arbitration)
  - `TranscriptService` (Redacted JSONL logging)
  - `devicetrust.MutationAuthorizer` (JWT & intent authorization)
  - `containsSecretPattern` (Secret scanner)
  - `coordination.Broker` (Typed inter-session messaging)
  - `workspace.Manager` (Lease & drift detection)
- **What Stays POKIT-Only:** React Native mobile UI components (`FeedScreen`, `TranscriptRenderer`).
- **Alpha Stability Guarantee:** The orchestration supervisor is **feature-flagged** via `Config.EnableOrchestration` (default `false`). Zero existing production paths are altered when disabled. Every migration step must pass `./scripts/build-gate.sh`.

---

## 11. Product Positioning

### Primary Position:
> **The Secure, Mobile-Supervised Orchestration Engine for Native Coding Agents.**

### Secondary Positions:
1. *Deterministic Git Worktree Isolation for Autonomous AI Development.*
2. *Enterprise-Grade Device Trust & Mobile Approval Arbitration.*

### What NOT to Put in the Headline:
- ❌ "Swarm Framework" (confuses with AutoGen/CrewAI API loops)
- ❌ "TMUX Manager" (implies screen scraping and TMUX dependencies)
- ❌ "LLM Code Reviewer" (implies non-deterministic evaluation)

---

## 12. Final Executive Decision

### Final Decision: **PROTOTYPE-FIRST (RUNTIME-KERNEL)**

```text
RECOMMENDATION: Build internal package `companion-daemon/internal/orchestration` 
as a feature-gated prototype-first runtime kernel inside POKIT.
```

- **Reason:** POKIT has already built ~4,755 lines of hard systems infrastructure (PTY, approval CAS, device trust, transcript projection). Building the orchestration supervisor as an internal package requires only ~30% new code (state machine + worktree manager) while reusing 100% of POKIT's security and process control foundations.
- **Closest Substitute:** Overstory (for worktree mail) + Orca (for multi-agent desktop UI).
- **Narrow MVP Scope:** 1 Supervisor + 1 Codex/Claude Executor in a Git worktree + 1 Shell Validator running `build-gate.sh`.
- **Explicit Non-Goals (Post-MVP):**
  - ❌ Building custom TUI Kanban boards.
  - ❌ Supporting TMUX wrappers.
  - ❌ Building vector database memory stores.
  - ❌ Multi-worker competitive races on the same task.
- **Proof Before Expanding:** Achieve 100% clean execution on 10 consecutive automated tasks via `build-gate.sh` with zero secret leaks and zero process leaks.
- **Repository Strategy:** Single repo (`companion-daemon/internal/orchestration`).
- **First Provider:** Codex CLI (0.145.0) & Claude Code CLI (2.1.219).
- **First Client:** Daemon CLI / React Native Mobile app.
- **Stop Conditions:** If deterministic build-gate validation fails to prevent dirty worktree corruption or if provider-native event integration proves unmaintainable across minor CLI releases, halt expansion and revert to manual human-in-the-loop operation.

---

**Report Completed:** 2026-07-26  
**Reviewer:** Antigravity (V2 Architecture Auditor)
