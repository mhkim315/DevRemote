# Coordinator Handoff Document

**Status:** AUTHORITATIVE — complete operational knowledge for seamless
Coordinator replacement.

**Last updated:** 2026-07-24, Step 9.3 closeout. Step 9.4 contract phase pending.

---

## 1. Current State

### Repository

```
Repo:    /Users/mhk/orca/DevRemote
Branch:  feature/canonical-timeline-foundation
HEAD:    f7033b86c  (Step 9.3 IMPL ACCEPT)
EVID:    7b322f783   (Step 9.3 EVID ACCEPT)
Clean:   yes
```

### Step Completion Ledger

| Step | IMPL SHA | CONTRACT SHA | EVID SHA | Rounds | Date |
|------|----------|-------------|----------|--------|------|
| 9.0 | 62a50f0a8 | — | — | 28 | 2026-07-23 |
| 9.1 | dc376f9b7 | e857fd13c | a25476f03 | 43 | 2026-07-23 |
| 9.2 | c82fef47f | 93c337a42 | 26eb2e157 | 33 | 2026-07-23 |
| 9.3 | f7033b86c | a5e532fd9 | 7b322f783 | 49 | 2026-07-24 |
| **9.4** | **NOT STARTED** | **NOT STARTED** | — | — | — |

### Frozen Identities

| Identity | SHA | Meaning |
|----------|-----|---------|
| PB ACCEPT | `5354077af` | Independent final acceptance after SM-S926N device gate |
| PB Device Candidate | `059bef181c` | Matched daemon/APK source |
| CT-P1 ACCEPT | `317bb0cb7` | Timeline envelope contract |
| CT-P1 Amendment | `12135bd80` | Operational evidence-source boundary |
| Step 4 Shadow | `58eb55b92` | Fail-open Timeline writer |
| Step 5 Workspace | `ad83bae10` | Workspace identity/lease |
| Step 6 Coordination | `6c13aafe7` | Coordination envelope/broker |
| Step 7 Validation | `814868b5b` | Frozen clean-snapshot staleness |
| Step 8 Cockpit | `093e03f04` | Mobile cockpit projection |
| Step 9.1 Activation | `dc376f9b7` | Operational Timeline staging |
| Step 9.2 Projection | `c82fef47f` | Transcript/Activity convergence |
| Step 9.3 Notification | `f7033b86c` | N1 exact-event notification |

### Active Team

| Role | Handle | Model | Status |
|------|--------|-------|--------|
| **Coordinator** | `term_9477482c` | Claude | 🟢 |
| **T1 Executor** | `term_9f142890` | DeepSeek | 🟢 |
| **T2 Executor** | `term_814699f4` | Codex | 🟢 |
| **V1 Verifier** | `term_8ca20c76` | Codex | 🟢 |
| **V2 Auditor** | `term_7f08372e` | Codex | 🟢 (also Context Guardian) |
| **EVID Agent** | `term_44b79f67` | DeepSeek | 🟢 |

---

## 2. Validated Workflow Rules

### Must-Follow (proven effective across 3 steps)

**Pre-gate (before any V1 dispatch):**
```bash
# Run after every worker DONE report:
git pull --ff-only
[ "$(git rev-parse HEAD)" = "$(git rev-parse @{upstream})" ] || PRE_GATE_BLOCKED
[ -z "$(git status --short)" ] || PRE_GATE_BLOCKED
[ "$(gofmt -l . | wc -l)" -eq 0 ] || PRE_GATE_BLOCKED
go build ./... && go vet ./... || PRE_GATE_BLOCKED
# Check claimed SHA matches actual HEAD
# Check claimed changed files match actual diff
# Check claimed tests were actually run
```

Push missing = `PRE_GATE_BLOCKED`. Do not return to implementation rounds.
Record every catch. Do not dispatch V1 until pre-gate passes.

**Blind-retry prevention:**
- Record blocker signature for every rejection.
- Same signature + same approach = blind retry.
- After 2 blind retries: split task, switch model, or return BLOCKED.
- Never tell the same executor to "try again" without a changed hypothesis.

**Micro-task splitting:**
- Multi-package changes → split by package boundary.
- Mobile + backend changes → always separate tasks.
- Single-file changes preferred for Codex (context limit).
- Multi-round concurrency fixes → consider epoch model or structural redesign.

**Worker terminal states:**
Every dispatched worker MUST end with exactly one:
- `DONE. SHA <sha>. <summary>. STOP.`
- `BLOCKED. <reason, attempts, uncertainty>.`
- `SPLIT_REQUIRED. <proposed subtasks>.`

Silent workers → query once, then mark `STALLED`. Do not blindly restart.

**Evidence rules:**
- Generate deterministic manifest from Git + test output (not memory).
- EVID Agent explains and organizes, never reconstructs machine values.
- Every code block claiming command output must be byte-exact.
- Never label curated text as "literal stdout" or "unedited".
- EVID runs only AFTER V1 ACCEPT and implementation freeze.

**Defect classification (Step 9.4+):**
| Class | Meaning | Action |
|-------|---------|--------|
| `CONTRACT_REJECT` | Contract scope/API mismatch | Return to contract |
| `IMPL_REJECT` | Production code/behavior defect | Return to impl |
| `PRE_GATE_BLOCKED` | Push/gofmt/tests/scope failure | Return to worker, no V1 |
| `TECH_EVID_BLOCKED` | Missing proof/test coverage | Return to impl |
| `EVID_SYNC_BLOCKED` | SHA/counts/stale headers | Return to EVID |
| `CONTEXT_DRIFT_BLOCKED` | Cross-step authority conflict | Context Guardian |

**Milestone states (Step 9.4+):**
```
IMPLEMENTATION_PENDING → IMPLEMENTATION_ACCEPTED → CLOSEOUT_PENDING → FINAL_ACCEPT
```

---

## 3. Executor Capability Matrix

Based on observed performance across Steps 9.1-9.3:

| Domain | T1 (DeepSeek) | T2 (Codex) | Notes |
|--------|---------------|------------|-------|
| Backend state machines | ✅ | ✅ | |
| Concurrency primitives | ✅ (epoch) | ✅ (semaphore) | T1 epoch model was elegant |
| Storage/ring buffer | ✅ | ✅ | |
| React Native navigation | ❌ (6 rounds) | ✅ | Never assign T1 mobile |
| Mobile UX/components | ❌ | ✅ (2 rounds) | |
| Cross-boundary contract | ❌ (blind API refs) | ✅ | T1 must read code first |
| Architectural redesign | ⚠️ | ✅ | |
| Evidence generation | ✅ (R5+) | — | With manifest, not memory |
| gofmt discipline | ❌ (forgets) | ✅ | Pre-gate catches T1 |

**Assignment rules:**
- Mobile → T2 always.
- Contract → T2, or T1 only after reading actual code files.
- Backend concurrency → T1 first, escalate to T2 if 2 structural failures.
- Evidence → EVID Agent with manifest. Never assign evidence to executors.

---

## 4. Communication Protocol

### Coordinator → Worker (dispatch task)
```sh
orca terminal send --terminal <worker-handle> --text "<task description>" --enter --json
```

Always include at the end: `DONE/BLOCKED/SPLIT_REQUIRED 형식으로 보고. Push 필수.`

### Worker → Coordinator (completion)
```sh
orca terminal send --terminal term_9477482c-0b30-491d-a156-4b3911dc79c9 --text "DONE. SHA <sha>. <summary>. STOP." --enter --json
```

### Reading worker output
```sh
orca terminal read --terminal <handle> --limit 20 --json
```

### Checking all terminals
```sh
orca terminal list --worktree id:224412ce-4bad-4ef0-a68d-ce12acb30cbd::/Users/mhk/orca/DevRemote --json
```

---

## 5. Step 9.4 Execution Plan

### Scope

Accountless Onboarding: Homebrew install → daemon → QR pairing →
non-exportable device key → authenticated connection → restart/reconnect →
revoke/reinstall/replacement → physical-device proof.

### Packet Structure

Packets are isolated for defect containment. All packets integrate at ONE
frozen SHA before V1/V2/Context Guardian.

```
9.4-A: macOS installation + bootstrap
  - Homebrew/distribution path
  - pokit CLI, daemon install, LaunchAgent
  - upgrade, uninstall, failure rollback

9.4-B: QR pairing + device trust
  - Single-use challenge, host/device/boot binding
  - Expiry, replay rejection, QR payload minimization
  - Pairing cancellation, timeout, concurrent attempts

9.4-C: Android non-exportable key lifecycle
  - Android Keystore-backed, non-exportable private key
  - Key gen, proof of possession, restart persistence
  - Reinstall, corruption, no silent downgrade

9.4-D: Revoke, recovery, replacement
  - Device revoke, token invalidation
  - Epoch model (prefer over adding locks)
  - Replacement device, reinstall, lost-phone recovery
  - CONCURRENCY CONTRACT MUST BE FROZEN BEFORE IMPLEMENTATION

9.4-E: Physical-device clean-install journey
  - Complete path on SM-S926N + macOS
  - Record exact artifacts
  - USER_ACTION_REQUIRED for physical device steps
```

### Execution Order

```
1. CONTRACT: Write docs/STEP9_4_CONTRACT.md covering all packets.
   Freeze concurrency invariants for 9.4-D in contract.
2. 9.4-A (daemon bootstrap interface only) → freeze interface SHA
3. 9.4-B + 9.4-C (parallel: pairing + key lifecycle, after A interface frozen)
4. 9.4-A (Homebrew packaging, after daemon interface confirmed)
5. 9.4-D (revoke/recovery, after B+C)
6. INTEGRATE all packets at one SHA
7. Fresh V1 (full implementation review)
8. Deterministic manifest → EVID
9. Fresh V2 (evidence audit)
10. Context Guardian (cross-step consistency, persistent V2 Auditor)
11. 9.4-E (physical-device journey) — USER_ACTION_REQUIRED
12. FINAL_ACCEPT
```

### 9.4-A + 9.4-B Parallelization Conditions

Only parallelize if:
- Common bootstrap/pairing interface is frozen in contract
- Modified files and writer ownership do not overlap
- Both do not create independent pairing authorities
- Integration point and dependency SHA are explicit

If conditions not met: freeze A's daemon/bootstrap interface first, then B.

---

## 6. Context Guardian

The persistent V2 Auditor (`term_7f08372e`) serves as Context Guardian.

**Activation:** Only at milestone closeout (Step 9.4 FINAL_ACCEPT), not per-packet.

**Verification scope:**
- Consistency with prior POKIT authority decisions
- No resurrection of removed attach/observer or Registry-style authority
- No cross-step regression against Steps 9.1-9.3
- Roadmap and product-scope alignment
- Correct dependency and final authority state

---

## 7. Round Count Ledger

Round counts come from an orchestration ledger, NOT from `git log` commit count.

**Required fields per round:**
- Phase (contract/impl/pre-gate/evidence/V2/guardian)
- Worker (T1/T2/EVID/V1/V2/CG)
- Attempt number
- Dispatch state (DONE/BLOCKED/SPLIT_REQUIRED/PRE_GATE_BLOCKED/STALLED)
- Reported SHA
- Verified SHA (from pre-gate)
- Pre-gate result
- Verifier verdict
- Blocker signature
- Superseded attempt
- Model switch or task split

**Commits without a round:** Amend, rebase, pre-gate return with no new commit.

Keep the ledger in `docs/STEP9_4_LEDGER.md`.

---

## 8. Evidence Manifest Schema

Generated by deterministic tooling, NOT by EVID Agent from memory.

```json
{
  "step": "9.4",
  "contract_sha": "...",
  "implementation_sha": "...",
  "evidence_sha": "...",
  "branch": "feature/canonical-timeline-foundation",
  "head_equals_upstream": true,
  "worktree_clean": true,
  "gates": {
    "build": "PASS",
    "vet": "PASS",
    "fmt": "PASS",
    "test_race": {"packages": 17, "passed": 17},
    "mobile_tsc": "PASS",
    "mobile_jest": {"suites": 36, "tests": 552}
  },
  "commands": [
    {
      "command": "go test -race ./... -count=1",
      "candidate_sha": "...",
      "exit_status": 0,
      "raw_stdout_path": "/tmp/step9.4-test-output.txt",
      "raw_stdout_hash": "sha256:...",
      "byte_count": 12345,
      "parsed_result": "17 ok, 3 no-test"
    }
  ]
}
```

EVID Agent role: explain and organize manifest facts. Never invent, infer, or
manually update machine-derivable values.

---

## 9. Key Documents

| Document | Purpose |
|----------|---------|
| `docs/POKIT_NATIVE_SESSION_COORDINATION_ROADMAP.md` | Authoritative product direction + execution order |
| `docs/POST_PA3_AUTHORITATIVE_ROADMAP.md` | PA/PB identity ledger |
| `docs/CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md` | CT foundation contract |
| `docs/ALPHA_ACTIVATION_ROADMAP.md` | Base Alpha activation sequence |
| `docs/STEP9_0_LEDGER.md` | Feature/flag/capability matrix |
| `docs/WORKFLOW_EXPERIMENT_REPORT.md` | Experiment data + validated controls |
| `docs/COORDINATOR_HANDOFF.md` | This document |

---

## 10. Experiment Learnings Summary

From Steps 9.1-9.3 (125 total rounds across 3 steps):

**Proven controls:**
- Pre-gate verification (caught 10+ defects, prevented ~5 wasted V1 rounds)
- Deterministic manifest (eliminated manual SHA corrections)
- Blind-retry prevention (0 blind retries in Step 9.2, 1 caught in Step 9.3)
- Push-missing = PRE_GATE_BLOCKED (4→0 occurrences)
- DONE/BLOCKED/SPLIT_REQUIRED (silent stops 3→0)
- Micro-task splitting (mobile/backend separation saved 4+ rounds)
- Epoch-based atomicity (resolved 7-round concurrency saga in 2 rounds)

**Anti-patterns to avoid:**
- Observer/callback pattern (Step 8: 11 rounds of concurrency bugs)
- Self-referential SHA (Step 9.0: 28 rounds of evidence fixes)
- T1 on mobile (Step 9.3: 6 rounds, minimal progress)
- Evidence from memory (Steps 9.0-9.1: replaced by manifest)
- T1 contract without reading code (blind API references)
- Over-counting rounds (34→19→22→27→37→39 in Step 9.3)

**Executor patterns:**
- T1: backend state machines, concurrency, evidence. Forgets gofmt, weak on mobile.
- T2: architecture, combined contract+impl, mobile UX. Push-missing habit (fixed by rule).
- EVID: needs byte-exact discipline. Manifest + tooling over memory.

---

## 11. Quick Reference

```sh
# Pre-gate check
cd /Users/mhk/orca/DevRemote/companion-daemon
git pull --ff-only && [ "$(git rev-parse HEAD)" = "$(git rev-parse @{upstream})" ] && \
  [ "$(gofmt -l . | wc -l)" -eq 0 ] && go build ./... && go vet ./...

# Full Go gate
go test -race ./... -count=1 -timeout 300s

# Mobile gate
cd /Users/mhk/orca/DevRemote/mobile && npx tsc --noEmit && npm test -- --runInBand

# List terminals
orca terminal list --worktree id:224412ce-4bad-4ef0-a68d-ce12acb30cbd::/Users/mhk/orca/DevRemote --json

# Dispatch to T1
orca terminal send --terminal term_9f142890-1eae-4509-9dee-3412b1d5f729 --text "..." --enter --json

# Dispatch to T2
orca terminal send --terminal term_814699f4-f6e2-458c-a587-0a467c4a36b1 --text "..." --enter --json

# Dispatch to V1
orca terminal send --terminal term_8ca20c76-b656-4e0a-85d6-1750515b05d2 --text "..." --enter --json

# Dispatch to V2/CG
orca terminal send --terminal term_7f08372e-889f-4886-bbae-9335f04379c4 --text "..." --enter --json

# Dispatch to EVID
orca terminal send --terminal term_44b79f67-8fa8-4b9b-bab0-a642fd513e62 --text "..." --enter --json

# Read coordinator messages
orca terminal read --terminal term_9477482c-0b30-491d-a156-4b3911dc79c9 --limit 20 --json
```
