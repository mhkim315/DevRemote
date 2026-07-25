# Comprehensive Landscape Review V2: Primary Source Verification & Corrected Technical Thesis

**Status:** COMPLETED — REVISED LANDSCAPE REVIEW (V2)  
**Target Document:** `docs/ORCHESTRATION_LANDSCAPE_REVIEW_V2.md`  
**Date:** 2026-07-26  
**Reviewer:** Antigravity (V2 Architecture Auditor)  
**New Executive Verdict:** **PROTOTYPE-NARROW-THESIS** (or **BUILD-AS-POKIT-FEATURE**)

---

## 1. Primary Source Verification & Correction Analysis

A rigorous code-level audit of primary sources revealed critical errors in initial surface-level reviews. Both **Overstory** and **AWS Labs CAO** possess significantly more sophisticated local daemon architectures than previously reported.

### 1.1 Overstory (`jayminwest/overstory`) — Source-Confirmed Trace
- **Daemon / Watchdog:** Contains a mechanical background daemon process (`ov daemon` / `watchdog`) that monitors active agent states, sweeps zombie/stalled processes, and reaps orphans from prior runs.
- **Coordinator:** A persistent project-root coordinator agent that decomposes user prompts and dispatches work items to child sessions.
- **Message Persistence:** Uses an SQLite-backed "mail" database (`overstory.db`) implementing an asynchronous, durable inter-agent messaging ledger.
- **Git Worktree Isolation:** Spawns headless sub-agents inside native Git worktrees (`git worktree add`).
- **Pluggable Runtime Adapters:** Defines an explicit `AgentRuntime` interface supporting Claude Code, Pi, Aider, and Gemini CLI.
- **Status:** Archived on GitHub (May 2026); successor is Warren (cloud sandboxed control plane).

### 1.2 AWS Labs CAO (`awslabs/cli-agent-orchestrator`) — Source-Confirmed Trace
- **`cao-server`:** Local FastAPI/Python daemon listening on loopback (`localhost`), managing session lifecycles, REST APIs, and SQLite indices.
- **Terminal & PTY WebSocket:** Spawns CLI providers (Claude Code, Cursor, Kiro) in `tmux` sessions while exposing a live PTY WebSocket endpoint (`/terminals/{id}/ws`).
- **Event Bus:** Internal in-process pub/sub (`services/event_bus.py`) that decouples terminal stream readers, status monitors, and inbox delivery modules.
- **Messaging Patterns:** Implements `Handoff` (sync), `Assign` (async/parallel), and `Send Message` inter-agent coordination.
- **Memory:** SQLite database with BM25 fallback indexing for cross-session long-term memory retrieval.
- **Status:** Active / Experimental under `awslabs`.

---

## 2. Corrected Comparison Matrix

> **Legend:** Source-Confirmed (`[S]`) vs README-Only Claim (`[R]`).

| Project | Execution Substrate | Local Daemon | Event Bus / Messaging | Worktree Isolation | State Persistence | Silence Heuristic Used? | Automated Build Gate | Secret Scanner | Remote / Mobile Auth | Maintainer & Status | License |
|---|---|---|---|---|---|---|---|---|---|---|---|
| **POKIT (Target)** | Go `OwnedPTYRuntime` [S] | Go `devremote` daemon [S] | `coordination.Broker` Go channels [S] | Native Git Worktree + Lease [S] | SQLite + Redacted JSONL [S] | ❌ No (CAS+Hooks) [S] | ✅ Deterministic `build-gate.sh` [S] | ✅ `containsSecret` pre-commit [S] | ✅ JWT / HMAC Nonce [S] | Active (POKIT) | MIT |
| **Overstory** | Headless subprocess [S] | Node `watchdog` daemon [S] | SQLite Mail Ledger [S] | Native Git Worktree [S] | SQLite (`overstory.db`) [S] | ⚠️ Partial (watchdog) [S] | ❌ Manual merge [S] | ❌ None [S] | ❌ None [S] | Jaymin West (Archived) [S] | MIT |
| **AWS Labs CAO** | `tmux` + PTY WebSocket [S] | Python `cao-server` daemon [S] | `services/event_bus.py` Pub/Sub [S] | Shared CWD (Worktree planned) [R] | SQLite + BM25 index [S] | ⚠️ Inactivity timer [S] | ❌ Manual review [S] | ❌ None [S] | ❌ Loopback only [S] | AWS Labs (Active) [S] | Apache 2.0 |
| **Gas Town** | Subprocess stdio [S] | Go CLI daemon [S] | File ledger (`.gastown/`) [S] | Native Git Worktree [S] | File-based [S] | ⚠️ Inactivity poll [S] | ❌ Task checklist [S] | ❌ None [S] | ❌ None [S] | Gas Town Hall (Active) [S] | Apache 2.0 |
| **AgentWrapper** | Node child_process [S] | Electron main daemon [S] | Agent OS event bus [S] | Temp dir clone [S] | SQLite [S] | ⚠️ Timer EOF [S] | ✅ CI/CD test runner [S] | ❌ None [S] | ❌ None [S] | AgentWrapper (Active) [S] | MIT |
| **agtx** | `tmux` windows [S] | TUI runner process [S] | In-memory Kanban [S] | Native Git Worktree [S] | In-memory [S] | ⚠️ Inactivity poll [S] | ❌ Visual TUI [S] | ❌ None [S] | ❌ None [S] | Fynn Flügge (Active) [S] | MIT |
| **OpenAI Symphony** | Docker containers [S] | Elixir/OTP OTP service [S] | Linear webhooks + OTP [S] | Ephemeral container [S] | Postgres [S] | ❌ Turn completed hook [S] | ✅ Linear PR Gate [S] | ❌ None [S] | ❌ Webhook API [S] | OpenAI (Preview) [S] | MIT |
| **Orca** | Node-pty (Electron) [S] | Electron main process [S] | WebSocket + IPC [S] | Native Git Worktree [S] | SQLite [S] | ⚠️ Webhook timer [S] | ❌ Visual compare [S] | ❌ None [S] | ✅ Mobile App (Proprietary) [R] | Stably AI (Active) [S] | Proprietary |

---

## 3. Revised Closest-Project Ranking

1. **AWS Labs CAO (`awslabs/cli-agent-orchestrator`)** — *Closest Architectural Overlap (80% similarity)*:
   - Already has a local `cao-server` daemon, SQLite storage, PTY WebSocket streaming, in-process Event Bus (`services/event_bus.py`), multi-provider CLI wrappers, and BM25 memory.
2. **Overstory (`jayminwest/overstory`)** — *Closest Conception Overlap (75% similarity)*:
   - Already pioneered the background watchdog daemon, SQLite mail messaging ledger, headless subprocess execution in Git worktrees, and pluggable `AgentRuntime` adapters.
3. **Gas Town (`gastownhall/gastown`)** — *Closest Language & Worktree Overlap (60% similarity)*:
   - Written in Go; manages multi-agent worktree execution across parallel workers.
4. **Orca (`onorca.dev`)** — *Closest Product/UX Overlap (50% similarity)*:
   - Provides desktop PTY orchestration with mobile companion supervision.

---

## 4. Reassessment of Technical Differentiation

After correcting primary source facts, **we must acknowledge that building a local daemon with an SQLite state machine, event bus, Git worktree manager, and CLI agent adapters is NOT novel.** Overstory and AWS Labs CAO have already demonstrated these concepts in production.

### Separation of Product Moat vs. Orchestration Technical Thesis

```text
┌────────────────────────────────────────────────────────────────────────┐
│ POKIT Product Moat (Non-Orchestration System Foundations)               │
│ - Remote mobile app pairing via WebSockets                             │
│ - 32-byte HMAC nonce JWT device trust (devicetrust.MutationAuthorizer)  │
│ - Authoritative mobile CAS approval sheets (AuthoritativeApprovalStore)│
└────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ Integrates with
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ Pure Local Orchestration Technical Thesis (To Be Proven)                │
│ 1. Monotonic Launch Generation & Stale Target Rejection (OwnedPTYRuntime)│
│ 2. Opaque-Worker AgentAdapter Contract with Provider Telemetry Seam    │
│ 3. Deterministic Build-Gate Verdict Ledger Bound to Tree Hashes        │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 5. Candidate Technical Theses: Evidence-Based Testing

We test 3 candidate technical theses against corrected primary-source evidence.

### Candidate Thesis 1: Authoritative Runtime/Session/Generation & Stale Rejection
- **Closest Predecessor:** AWS Labs CAO & Overstory.
  - *Predecessor Limitation:* CAO tracks session IDs, but lacks monotonic launch generation numbers (`Generation int64`). When a PTY process crashes and restarts on the same session ID, late messages or stale timer events can contaminate the new process instance.
- **Exact Difference:** POKIT enforces generation-bound process leases (`OwnedPTYRuntime.Create` + `Generation`). Every input, signal, or state transition must match the active `(SessionID, LaunchGeneration)`. Any event targeting a stale generation is rejected (`ErrStaleOwner`).
- **User Value:** Prevents zombie worker processes from executing unauthorized writes or corrupting workspace state after a restart.
- **Falsifiable Prototype:** Spawn a worker, force a crash/restart (incrementing generation), then replay a buffered input targeting Generation 1. Confirm `ErrStaleOwner` rejection and zero side effects.

### Candidate Thesis 2: Provider-Independent Opaque-Worker + Optional Native Extensions
- **Closest Predecessor:** Overstory (`AgentRuntime` interface) & Claude Code Teams.
  - *Predecessor Limitation:* Overstory's adapter interface is tightly coupled to stdio string parsing. Claude Code Teams is hardcoded to Claude JSONL logs. Neither provides an opaque worker boundary with optional capability-gated native extensions.
- **Exact Difference:** POKIT defines a universal 4-method lifecycle contract (`Start`, `Stop`, `Kill`, `Heartbeat`) paired with a provider-neutral 7-operation `AgentAdapter` (`Detect`, `DiscoverSessions`, `ReadEvents`, `NormalizeEvent`, `DetectApproval`, `GetStatus`, `Descriptor`). Provider-specific extensions (e.g. Claude hook bridge) are runtime-discovered via capability interfaces (`CapabilityChecker`).
- **User Value:** Allows stock, un-modified CLI tools (`codex`, `claude`, generic bash) to run as first-class workers without code forks or provider lock-in.
- **Falsifiable Prototype:** Run a generic `bash` script worker and a stock `codex` CLI worker under the exact same supervisor FSM without changing supervisor code.

### Candidate Thesis 3: Verified User/Repo-Specific Model-Routing & Build-Gate Memory
- **Closest Predecessor:** AgentWrapper & AWS Labs CAO.
  - *Predecessor Limitation:* CAO uses BM25 text retrieval over past prompts. AgentWrapper runs CI/CD tests but does not store immutable verdict bindings tied to tree hashes.
- **Exact Difference:** POKIT records deterministic `Verdict` records (`verdicts` table) binding `(RepositoryID, SnapshotID, TreeHash, GateResults)` to exact model/attempt pairs. If an agent attempt fails a build gate (`./scripts/build-gate.sh`), the supervisor marks that strategy/model combination stale for that specific tree hash via `workspace.FindingBinding.Stale()`.
- **User Value:** Eliminates repetitive LLM failure loops on identical codebase snapshots by forcing fallback or human escalation when a strategy repeatedly fails deterministic build gates.
- **Falsifiable Prototype:** Run an attempt that fails `go vet`, verify `verdict` recorded in SQLite, dispatch second attempt, and confirm supervisor automatically alters prompt/model strategy based on recorded verdict.

---

## 6. Revised Build / Fork / Extend Recommendation

| Strategy | Feasibility | Technical Debt | Alignment with POKIT | Verdict |
|---|---|---|---|---|
| **Fork AWS Labs CAO** | Medium | High (Python/FastAPI code into Go daemon, heavy TMUX dependency) | Poor (requires running Python sidecar alongside Go daemon) | ❌ **REJECT** |
| **Fork Overstory** | Low | High (Archived Node.js project, no PTY management) | Poor (language mismatch, dead repo) | ❌ **REJECT** |
| **Extend CAO / Overstory** | Low | High (Cross-process IPC over WebSockets) | Poor (violates single-daemon constraint) | ❌ **REJECT** |
| **BUILD-AS-POKIT-FEATURE (`internal/orchestration`)** | **High** | **Low** (Reuses ~4,755 LOC of existing Go code) | **Optimal** (Native Go, direct access to `OwnedPTYRuntime` and `ApprovalStore`) | ✅ **RECOMMENDED** |

---

## 7. Revised Executive Verdict

### New Executive Verdict: **BUILD-AS-POKIT-FEATURE (or PROTOTYPE-NARROW-THESIS)**

```text
FINAL DECISION: BUILD-AS-POKIT-FEATURE
Implement `companion-daemon/internal/orchestration` as an internal feature package 
inside the POKIT Go daemon. 

Do NOT attempt to market the local daemon orchestration engine as a standalone 
revolutionary runtime — CAO and Overstory have already built similar local daemons. 
Instead, position it as POKIT's native multi-agent execution engine, leveraging 
POKIT's unique generation-bound PTY runtime, secret scanner, build-gate validator, 
and mobile approval bridge.
```

### MVP Scope & Non-Goals

1. **Narrow MVP Scope:**
   - Single package: `companion-daemon/internal/orchestration`.
   - Feature flag: `Config.EnableOrchestration` (default `false`).
   - Components: `Supervisor` FSM + `WorktreeManager` + `ShellWorker` / `CodexWorker` + `build-gate.sh` Validator.
   - Proof Target: 1 Coordinator dispatches 1 Task → Executor runs in Git worktree → Validator runs `./scripts/build-gate.sh` → Merge on PASS.

2. **Explicit Non-Goals (Post-MVP):**
   - ❌ TMUX session management.
   - ❌ Desktop Kanban UI.
   - ❌ Vector DB / BM25 search engines.
   - ❌ Parallel multi-worker competitive execution.

3. **Stop Conditions:**
   - Halt implementation if `build-gate.sh` verification produces un-reapable zombie processes or if generation-bound lease locking fails under `go test -race`.

---

**Report Completed:** 2026-07-26  
**Reviewer:** Antigravity (V2 Architecture Auditor)
