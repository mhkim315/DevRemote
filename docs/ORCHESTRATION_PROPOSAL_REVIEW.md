# Multi-Agent Autonomous Orchestration Architecture Review

**Status:** COMPLETED ARCHITECTURE REVIEW
**Target Document:** `docs/ORCHESTRATION_PROPOSAL_REVIEW.md`
**Date:** 2026-07-25
**Final Verdict:** **ACCEPT WITH CHANGES**

---

## 1. Executive Summary

### 1.1 One-Sentence Definition
> An authoritative, POKIT-native multi-agent orchestration architecture that coordinates autonomous AI worker processes across isolated Git worktrees, combining PTY terminal control, provider-native structured event projection, deterministic validation gates, and human-in-the-loop mobile approvals.

### 1.2 Architectural Highlights & Risks
- **Strongest Part:** Integration with POKIT's existing `OwnedPTYRuntime`, `AuthoritativeApprovalStore`, and `TranscriptService` infrastructure. By building directly on POKIT's generation-bound device trust and real-time streaming foundations, the orchestration layer avoids re-inventing low-level IPC, terminal PTY multiplexing, or secret-redacted event persistence.
- **Weakest Assumption:** The assumption that terminal inactivity or raw CLI process exit code is an authoritative signal of task completion or success. Agents frequently pause for extended reasoning or tool execution, and CLI tools frequently exit `0` after failing semantically.
- **Primary Technical Risk:** Provider-independent state ambiguity—attempting to infer whether a CLI worker is reasoning, waiting for input, hung, or completed using pure PTY stream observation without provider-native hooks or event telemetry.
- **Primary Product Risk:** Feature scope creep into general-purpose agent swarm frameworks (e.g. AutoGen/LangChain clones), diluting POKIT's unique product moat: secure, remote, mobile-first agent supervision and approval arbitration.

---

## 2. Evaluation Across 10 Technical & Product Dimensions

### 2.1 Conceptual Validity
- **Terminal Inactivity ≠ Task Completion:** Terminal output silence (inactivity) occurs during extended model reasoning, long-running compiler/test executions, network stalls, or pending human approvals. Treating PTY silence as completion leads to premature task termination or false state transitions.
- **Process Exit Code 0 ≠ Semantic Success:** CLI AI providers frequently exit with code `0` after outputting error explanations, encountering rate limits, or failing to solve the target prompt. Process exit `0` guarantees only that the shell wrapper completed without an unhandled OS signal.
- **Authoritative Completion Requirement:** Task completion MUST be attested by an explicit validator gate (e.g., test suite execution, static analysis, or validator agent assertion), combined with provider-native turn/session completion events (such as Codex `turn/completed` or Claude `SessionEnd` hooks).

### 2.2 Technical Feasibility
- **Provider-Independent State Ambiguity:** It is technically impossible to reliably distinguish internal model reasoning from a hung process or unpromoted waiting state using pure stdout/PTY byte streams without provider-specific knowledge.
- **Provider Telemetry Seam:** To achieve technical feasibility, the architecture must establish a normalized `ProviderTelemetrySeam` mapping provider-specific signals into unified supervisor states:
  - `RUNNING_COMPUTING` (Process active, generating tokens or running tools)
  - `WAITING_FOR_APPROVAL` (Pending mobile approval in `AuthoritativeApprovalStore`)
  - `WAITING_FOR_USER_INPUT` (Explicit interactive prompt active)
  - `COMPLETED` (Provider turn/completed event received + validator pass)
  - `FAILED` (Process error, timeout, or validator gate failure)
  - `STALLED` (Heartbeat timeout exceeded with zero CPU/IO activity)

### 2.3 Orchestration Reliability
- **Supervisor State Machine:** Task lifecycle must be driven by an explicit, persistent state machine (`IDLE` → `DISPATCHED` → `EXECUTING` → `VALIDATING` → `COMMITTED` / `REJECTED` → `REMEDIATING`).
- **Duplicate Wake-Up Prevention:** Concurrent events (e.g. PTY stdout delta, JSONL append, and timer ticks) must be arbitrated using atomic Compare-And-Swap (CAS) state transitions and monotonic generation counters to prevent duplicate dispatch or race conditions.
- **Coordinator Restart Recovery:** On supervisor restart, worker process groups must be reconciled against active POKIT runtime IDs and process table entries. Stale or un-orphaned processes must be gracefully terminated (`SIGTERM` → `SIGKILL`) before new workers are spawned.
- **Worker Ownership & Leases:** Worker execution leases must be time-bounded (e.g., 300s max per turn) with mandatory heartbeat renewals to prevent hung workers from holding lock resources indefinitely.

### 2.4 Git Work Handoff & Branch Safety
- **Worktree Isolation:** Every executor worker MUST operate inside an isolated Git worktree (`git worktree add`). Workers must never mutate the primary working directory directly.
- **Commit Policies:** Workers must NOT auto-commit intermediate broken states to main. Commits are created inside the worker's isolated worktree branch only upon completing a discrete subtask.
- **Secret Prevention & Pre-Commit Gates:** Every worker commit must run POKIT's secret scanner (`containsSecretPattern`) and `git diff --check` to ensure no credentials or malformed diffs enter git history.
- **Parallel Attempt Resolution:** When multiple workers run competitive attempts on the same task, parallel branches are validated independently. Only the branch passing all validation gates is fast-forward merged into the primary branch; failing worktree branches are deleted.

### 2.5 Memory & Model Selection Dynamics
- **Small Sample Overfitting:** Evaluating agent prompt strategies on 1-2 trivial test cases risks introducing brittle rules. Prompts and evaluation criteria must be validated against standardized regression benchmarks.
- **Model-Version Drift:** Differences between provider model versions (e.g., Claude 3.5 vs 3.7, Codex CLI 0.144 vs 0.145) alter tool call formats and reasoning latency. The system must pin exact binary SHAs and attestation versions (e.g. `CertifiedClaudeVersion`, `CertifiedCodexAuthorityVersion`).
- **Validator Independence:** Executor and Validator agents MUST NOT share identical model instances without deterministic code gates. If the executor and validator use the same LLM, the validator inherits the executor's reasoning biases. Deterministic gates (`go test`, `tsc`, `build-gate.sh`) must serve as primary ground truth.
- **Trajectory Audit Logging:** All worker conversations, tool executions, and state transitions must be persisted in structured JSONL transcripts (`transcript_full.jsonl`) for failure analysis and post-mortem auditing.

### 2.6 Product Differentiation & Moat
- **VS. API Agent Frameworks (LangChain, AutoGen):** API frameworks operate in artificial code environments without real terminal PTYs, local toolchains, or mobile approval bridges. POKIT orchestrates native desktop CLI tools in real development environments.
- **VS. TMUX Orchestrators:** TMUX lacks structured event projection, secret scanning, JWT device authorization, or remote mobile integration.
- **VS. IDE Agents & Worktree Managers (Cursor, Windsurf):** IDE agents are desktop-bound and single-user. POKIT provides multi-agent coordination with mobile-first remote monitoring, approval arbitration, and cross-device session continuity.

### 2.7 Scope & Credible MVP Definition
- **Narrowest Credible Proof (1 Coordinator + 1 Executor + 1 Validator):**
  - **1 Coordinator:** Manages the task state machine, dispatches task prompts to the executor, handles timeout/retry loops.
  - **1 Executor:** Runs stock Codex or Claude TUI inside an isolated Git worktree under POKIT `OwnedPTYRuntime`.
  - **1 Validator:** Executes deterministic build/test scripts (`./scripts/build-gate.sh`) and reports pass/fail to Coordinator.
  - **Workflow:** Task Dispatch → Executor Worktree Execution → Validator Build Gate → Fast-Forward Merge (Pass) or Worktree Revert (Fail).

### 2.8 POKIT System Relationship
- **Architectural Placement:** The orchestration layer should be implemented as an internal service package (`companion-daemon/internal/orchestration`) within the POKIT daemon architecture.
- **Infrastructure Reuse:** Directly consumes POKIT's `OwnedPTYRuntime`, `TerminalTransport`, `AuthoritativeApprovalStore`, `TranscriptService`, and `deviceTrust` components rather than building a redundant daemon or external wrapper process.

### 2.9 Security & Safety Analysis
- **Sandboxed Workspace Boundaries:** All worker file operations are constrained to their designated Git worktree paths.
- **Credential Protection:** All output streams pass through `containsSecretPattern` before being stored in durable transcripts or broadcast over WebSockets.
- **Prompt Injection Defense:** External inputs (e.g., untrusted GitHub issue text) passed to workers are scoped as data payloads, not system instructions. Tool calls requiring elevated permissions are routed through POKIT's mobile approval bridge (`--permission-mode dontAsk` + `PermissionRequest` hook).
- **Branch Protection:** Auto-merging to main requires 100% clean validator gate pass + zero secret scan findings.

---

## 3. Component Categorization Matrix

| Component | Category | Description / Treatment |
|---|---|---|
| `OwnedPTYRuntime` | **Already-supported** | PTY process spawning, process group cleanup, signal handling |
| `TerminalTransport` | **Already-supported** | Generation-bound terminal input, resize, and fan-out stream |
| `AuthoritativeApprovalStore` | **Already-supported** | Single-responder CAS approval arbitration with mobile bridge |
| `TranscriptService` | **Already-supported** | Redacted, durable JSONL event projection and storage |
| `containsSecretPattern` | **Already-supported** | Real-time secret scanning gate for credentials and API keys |
| `claudeInteractiveBridge` | **Reusable-with-refactor** | HTTP hook bridge for Claude `PreToolUse` & `PermissionRequest` |
| `CodexTUIHost` | **Reusable-with-refactor** | Interactive Codex host with JSONL tailing & PTY streaming |
| `SessionTelemetry` | **Reusable-with-refactor** | Capability routing (`controlled_pty`, `codex`, `claude`) |
| Supervisor State Machine | **Must-build** | Task lifecycle state transitions, generation tracking, and retries |
| Git Worktree Manager | **Must-build** | Isolated worktree creation, branch cleanup, and fast-forward merging |
| Validator Gate Engine | **Must-build** | Execution of build/test scripts and parsing verification results |
| Multi-Worker Dispatcher | **Must-build** | Task queuing, worker allocation, and concurrency control |
| Provider-Independent State Inference | **Cannot-be-independent** | Reasoning vs waiting state detection (requires provider hooks) |
| Parallel Competitive Execution | **Optional-enhancement** | Multi-worker race on same task (deferred past MVP) |
| LLM-as-a-Validator | **Optional-enhancement** | Secondary LLM code review (deferred past MVP) |

---

## 4. Required Changes & Deferred Features

### 4.1 Mandatory Required Changes
1. **Eliminate Inactivity-Based Completion:** Replace PTY silence heuristics with explicit validator pass/fail results combined with provider-native turn events.
2. **Require Provider Telemetry Seam:** Require provider-specific adapters (Codex JSON-RPC, Claude hooks) for state classification rather than generic stdout regex matching.
3. **Enforce Worktree Sandboxing:** Mandate that all worker agents execute in isolated `git worktree` directories created under `TMPDIR` or `.git/worktrees/`.
4. **Mandate Pre-Merge Secret Scanning:** Run secret scan and `git diff --check` prior to committing or merging worker branches.

### 4.2 Deferred Features (Post-MVP)
1. **Competitive Parallel Execution:** Running N executor agents on the same prompt simultaneously.
2. **LLM Semantic Validator:** Using a secondary LLM for code style/design review (MVP relies exclusively on deterministic build gates).
3. **Dynamic Prompt Auto-Tuning:** Automatically altering executor system prompts based on past trajectory failures.

---

## 5. System Boundary & MVP Validation Plan

### 5.1 System Boundary
```text
┌────────────────────────────────────────────────────────────────────────┐
│ POKIT Companion Daemon (devremote)                                      │
│                                                                        │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │ internal/orchestration (Supervisor Engine)                        │  │
│  │                                                                  │  │
│  │  ┌──────────────┐      ┌──────────────┐      ┌──────────────┐   │  │
│  │  │ Coordinator  │ ───► │  1 Executor  │ ───► │ 1 Validator  │   │  │
│  │  │ (Task FSM)   │      │ (Worktree)   │      │ (Build Gate) │   │  │
│  │  └──────────────┘      └──────────────┘      └──────────────┘   │  │
│  └──────────────────────────────────────────────────────────────────┘  │
│                                │                                       │
│  ┌─────────────────────────────┴──────────────────────────────────┐  │
│  │ POKIT Core Foundations                                         │  │
│  │ - OwnedPTYRuntime  - TerminalTransport  - TranscriptService    │  │
│  │ - AuthoritativeApprovalStore - SecretScanner - DeviceTrust    │  │
│  └────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────┘
```

### 5.2 Validation Plan
1. **Unit & Race Testing:** 100% test coverage under `go test -race` for supervisor state machine transitions, timeout handling, and CAS resolution.
2. **Worktree Isolation Test:** Verify worker file modifications remain strictly contained within their designated worktree directory and never touch the primary working copy before validator approval.
3. **Secret Leak Test:** Inject dummy API keys into worker output and confirm `containsSecretPattern` blocks commit/merge.
4. **Deterministic Validation Gate:** Execute 10 consecutive task dispatches using `build-gate.sh` as the validator; confirm zero false positives or un-reaped worker process leaks.

---
**Report Completed:** 2026-07-25  
**Reviewer:** Antigravity (V2 Auditor & Architecture Verifier)
