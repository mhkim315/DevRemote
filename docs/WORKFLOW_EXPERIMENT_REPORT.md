# Workflow Experiment Report

**Status:** LIVING DOCUMENT — updated as experiments are conducted.

**Purpose:** Capture empirical data from orchestration workflow experiments
across POKIT implementation steps. This document informs future orchestration
strategy decisions with actual measurements, not abstract reasoning.

**Last updated:** Step 9.3 completion.

---

## 1. Experiment History

### Step 9.1 — Baseline (legacy workflow)

**Setup:** T1/T2 + V1/V2 + EVID (introduced mid-step). No pre-gate. No manifest.
No retry control. No worker terminal states.

| Phase | Rounds | Details |
|-------|--------|---------|
| Contract | 7 | T1 4 + T2 3 |
| Implementation | 24 | T1 11 + T2 13 |
| Evidence | 7 | EVID Agent manual |
| V2 Audit | 5 | |
| **Total** | **43** | |

**Defects:**
| Category | Count |
|----------|-------|
| Blind retries | 3 (T1 contract self-auth x2, T2 blind wiring) |
| Silent stops | 3 |
| Evidence/doc REJECT | 3 |
| Push missing | multiple (not tracked) |

**Key findings:**
- Evidence/doc REJECT was 7% of total (down from CT-P1's 71%)
- Implementation was 24 rounds — main cost center
- EVID Agent introduced mid-step reduced evidence REJECT dramatically

---

### Step 9.2 — Pre-gate + Manifest + Retry Control

**New controls:**
- Implementation pre-gate: worker claim vs repository verification
- Deterministic evidence manifest
- Blind-retry classification
- Worker terminal states (DONE/BLOCKED/SPLIT_REQUIRED)
- R3-A/R3-B split (comparator core then contract matrix)

| Phase | Rounds | Details |
|-------|--------|---------|
| Contract | 6 | T1 2 + T2 4 |
| Implementation | 21 | R3-A 7 + R3-B 3 + T1 3 + T2 18 |
| Evidence | 3 | Manifest-derived |
| V2 Audit | 3 | |
| **Total** | **33** | |

**Pre-gate catches:** 6 (4 push missing + 1 tests missing + 1 gofmt)
**Pre-gate prevented V1 dispatches:** 2
**Blind retries:** 0

**Key findings:**
- Pre-gate prevented 2 wasted V1 rounds
- Deterministic manifest eliminated manual SHA corrections
- R3-A/R3-B split enabled independent comparator freezing
- Push missing was the most common pre-gate defect (4x)

---

### Step 9.3 — Enhanced Pre-gate + Micro-tasks + Self-contained Design

**New controls:**
- Push missing = PRE_GATE_BLOCKED (not returned to implementation rounds)
- Micro-task split for mobile/backend separation
- Self-contained N1 (no cockpit dependency)
- Defect classification: contract vs implementation vs pre-gate vs evidence

| Phase | Rounds | Details |
|-------|--------|---------|
| Contract | 3+1 | T1 2 + T2 1 + amend 1 |
| Implementation | 12 | T1 10 + T2 2 |
| Pre-gate returns | 2 | test failure, gofmt |
| Evidence | 4 | Manifest-derived |
| V2 Audit | 3 | contract scope + counts |
| **Total** | **22** | |

**Pre-gate catches:** 2 (test failure, gofmt)
**Pre-gate prevented V1 dispatches:** 2
**Blind retries:** 1 (T2 R1→R2, escalated to T1)
**Push-missing PRE_GATE_BLOCKED:** 0 (rule effective)

**Defect classification:**
| Category | Count | % |
|----------|-------|---|
| Contract REJECT | 2 | 11% |
| Implementation defect | 10 | 56% |
| Pre-gate workflow defect | 2 | 11% |
| Evidence/manifest defect | 4 | 22% |

**Key findings:**
- Total rounds decreased: 43→33→22 (49% reduction from baseline)
- Implementation rounds halved: 24→21→12
- Push-missing PRE_GATE_BLOCKED rule eliminated the most common defect
- T2 combined contract+impl approach saved 3 contract rounds vs Step 9.2
- Self-contained design (no cockpit dependency) prevented integration defects
- Evidence defects remained at 3-4 rounds — manifest helps but doesn't eliminate

---

## 2. Control Effectiveness Matrix

| Control | Step 9.1 | Step 9.2 | Step 9.3 | Proven? |
|---------|----------|----------|----------|---------|
| Pre-gate verification | ❌ | ✅ 6 catches | ✅ 2 catches | **YES** |
| Deterministic manifest | ❌ | ✅ 0 SHA fixes | ✅ 0 SHA fixes | **YES** |
| Blind-retry prevention | ❌ 3 blind | ✅ 0 blind | ✅ 1 caught | **YES** |
| Push-missing=PRE_GATE_BLOCKED | ❌ | ❌ | ✅ 0 occurrences | **YES** |
| DONE/BLOCKED/SPLIT_REQUIRED | ❌ 3 silent | ✅ 0 silent | ✅ 0 silent | **YES** |
| Micro-task split | ❌ | ✅ 2 splits | ✅ 3 splits | **YES** |
| Combined contract+impl (T2) | ❌ | ❌ | ✅ -3 rounds | **PROMISING** |
| Self-contained design | ❌ | ❌ | ✅ 0 cockpit deps | **PROMISING** |
| Defect classification | ❌ | ❌ | ✅ 4 categories | **YES** |

---

## 3. Executor Patterns

### T1 (DeepSeek)
**Strengths:** Persistent, good at iterative refinement, solid on backend
implementation once API is understood. Epoch-based atomicity (R12-R13) was a
smart architectural choice.

**Weaknesses:** Does not read existing code before writing contracts (Step 9.2
R1, Step 9.3 R1). Mobile codebase understanding is poor (6 rounds of mobile
fixes). Frequently forgets gofmt. Worker reporting reliability varies — silent
stops occur if not explicitly reminded.

**Best used for:** Backend implementation with clear API boundaries. Contract
writing only after reading actual code. Not for mobile UI work.

### T2 (Codex)
**Strengths:** Architectural correctness, structural redesign, concurrency
modeling. Combined contract+impl approach is effective (Step 9.3 R1).

**Weaknesses:** Context limits cause incomplete implementations. Push-missing
was the most common defect (4 consecutive in Step 9.2). Mobile UX still
required 2 rounds. Silent stops when blocked — requires explicit BLOCKED
reporting protocol.

**Best used for:** Structural architecture, combined contract+impl for
well-scoped steps, concurrency model fixes. Not for multi-file mobile changes
in single context.

---

## 4. What Worked

1. **Micro-task splitting** — Backend/mobile separation (Step 9.3 R5-A/R5-B)
   cut through T1's mobile weakness. Single-file changes (Step 9.2 R3-A)
   bypassed Codex context limits.
2. **Epoch-based atomicity** (T1 R12-R13) — Elegant solution to revoke/commit
   race. Simpler than lock-based approach.
3. **Self-contained design** — N1 serving its own event data eliminated
   cockpit dependency. Prevented integration defect category entirely.
4. **T2 combined contract+impl** — Saved 3 contract rounds vs separate phases.
5. **Pre-gate push verification** — Push-missing went from 4 occurrences
   (Step 9.2) to 0 (Step 9.3) after PRE_GATE_BLOCKED rule.

---

## 5. What Didn't Work

1. **Observer/callback pattern** (Step 8) — 11 rounds of concurrency bugs.
   Replaced by ring buffer polling. Lesson: prefer pull over push for
   read-only data.
2. **Self-referential SHA** (Step 9.0) — 28 rounds of evidence doc fixes.
   Replaced by git log resolver. Lesson: never embed a commit's own SHA.
3. **Evidence docs written from memory** (Steps 9.0-9.1) — Replaced by
   deterministic manifest. Still requires 3-4 rounds for contract sync.
4. **T1 mobile work** — 6 rounds, minimal progress per round. Mobile tasks
   should go to T2 with explicit file-level instructions, or be separately
   decomposed.

---

## 6. Recommended Default Workflow (Post-Experiment)

```
1. CONTRACT: T2 combined contract+impl for well-scoped steps.
   T1 contract only after reading actual code files.
2. PRE-GATE: Verify SHA, upstream, worktree, gofmt, required tests,
   changed-file scope. Push missing = PRE_GATE_BLOCKED.
3. V1: Code/behavior/security only. No evidence/doc REJECTs.
4. MANIFEST: Generated from Git + test output. Machine-derived values.
5. EVID: Explain + organize manifest. No reconstruction from memory.
6. V2: Milestone only — evidence accuracy, SHA lineage, contract sync.
7. RETRY: Blind retry classification. 2 same-signature → escalate.
   No third blind retry.
8. SPLIT: Multi-package changes → micro-tasks. Mobile/backend separate.
```

---

## 7. Open Questions (for future experiments)

- Can evidence rounds be reduced below 3-4 with better manifest templates?
- Would T2 combined contract+impl work for all steps, or only well-scoped ones?
- What is the optimal split point for backend vs mobile tasks?
- Can pre-gate be fully automated (script) rather than Coordinator-manual?
- Does the epoch-based atomicity pattern generalize to other concurrency problems?

---

## 8. Update Log

| Date | Step | Key Changes |
|------|------|-------------|
| 2026-07-23 | 9.3 | Initial report: Step 9.1 baseline + Step 9.2 pre-gate + Step 9.3 micro-tasks |
