# Coordinator Handoff Document

## 1. Current State

### Repository
- **Repo:** `/Users/mhk/orca/DevRemote`
- **Branch:** `feature/phase10-multi-adapter`
- **HEAD:** `b4479c64c`
- **Remote:** `origin/feature/phase10-multi-adapter`
- **Workdir:** `companion-daemon/`

### Frozen Identities

| Identity | SHA | Meaning |
|----------|-----|---------|
| PA4 PRODUCTION | `4aaf3b76a` | Frozen managed-isolation implementation |
| PA4 ACCEPT | `74560edd8` | PA4 independent acceptance |
| PB PREREQUISITE | `74560edd8` | Required before PB |
| PB START BASELINE | `abe4df1d6` | Operational rollback anchor |
| PB DEVICE CANDIDATE | `ab1884662` | Source for daemon + APK build |
| PB ACCEPT | **UNSET** | Awaiting SM-S926N matrix |

### Accepted Waves & SHAs

| Wave | ACCEPT SHA | Content |
|------|-----------|---------|
| PA4 | `74560edd8` | Managed isolation |
| PB.0 | `321cd1a84` | Consumer inventory |
| PB.1 | `c0f5664d0` | localpty deletion |
| PB.2a | `359e3853e` | link/attach removal |
| PB.2b | `3c65b5990` | discovery/resolver removal |
| PB.3 | `2987b6fe1` | tmux deletion |
| PB.4 | `ed9bb5468` | cmux + snapshot removal |
| PB.5a | `4ed0d3dc3` | V1 production cutover |
| PB.5b-T2 | `0f0d57f30` | Consumer migration |
| PB.5b-T3 | `c2c0f542a` | Physical deletion |
| PB.6 | `82e550e9c` | Mobile/daemon cleanup |
| PB.7 | `b18123e77` | Automated closeout evidence |
| QR | `b55780c7f` | QR renderer/security |
| Input-A | `e28964875` | Permission/read-only UX |
| Input-B | `9b75c1e4a` | Acknowledged input delivery |
| TERM-G1 | `2d13020ae` | PTY geometry authority |
| TERM-C1 | `ba78b617a` | Single control bridge |
| ARTIFACT-ID1 | `68fc09140` | Matched daemon/APK provenance |

### Artifacts (ready for SM-S926N)

| Artifact | Path | SHA-256 |
|----------|------|---------|
| Daemon | `/tmp/pokit-pb-device-artifacts/pokit-daemon` | `5c1470a0d580...` |
| APK | `/tmp/pokit-pb-device-artifacts/pokit-app-release.apk` | `934febb17b...` |

### Sole Remaining Blocker
Samsung SM-S926N not connected via ADB. Device must show `device` state in `adb devices -l` before matrix can begin.

---

## 2. Team & Terminal Handles

| Role | Handle | Model | Worktree |
|------|--------|-------|----------|
| **Coordinator** | `term_9477482c-0b30-491d-a156-4b3911dc79c9` | — | Main DevRemote |
| **Executor (Tier 1)** | `term_9f142890-1eae-4509-9dee-3412b1d5f729` | DeepSeek (Claude+DeepSeek) | Main DevRemote |
| **Executor (Tier 2)** | `term_49048007-d5df-40ce-b084-70d3bd270166` | Codex 5.6 Sol | Main DevRemote |
| **Verifier** | `term_8ca20c76-b656-4e0a-85d6-1750515b05d2` | Codex | Main DevRemote |

### Executor Tier System
- **Tier 1 (DeepSeek):** Evidence documents, grep scans, gofmt, simple deletions, test adjustments, build/artifact tasks. Escalates to Tier 2 when stuck (same blocker 2x, compilation 5+ errors, architectural changes needed).
- **Tier 2 (Codex):** Architecture, large refactoring, V1 contract changes, complex compilation fixes, production cutover. Receives escalated tasks from Tier 1 or direct dispatch from Coordinator.
- **Ping-pong rule:** After 2 consecutive rejections on same executor, switch to the other tier. After another 2, switch back. Never let one executor fail 5+ times on same blocker.

### Verifier
- **Read-only.** Never modifies files.
- Returns ACCEPT or REJECT with exact file:line evidence.
- ACCEPT means independent acceptance — final authority.

---

## 3. Communication Protocol

### Coordinator → Executor (dispatch task)
```sh
orca terminal send --terminal <executor-handle> --text "<task description>" --enter --json
```

### Executor → Coordinator (worker_done)
```sh
orca terminal send --terminal term_9477482c-0b30-491d-a156-4b3911dc79c9 --text "worker_done — <summary>" --enter --json
```
**Never use `orca orchestration send`.** It is unreliable. Use `orca terminal send` directly.

### Coordinator → Verifier (dispatch verification)
```sh
orca terminal send --terminal term_8ca20c76-b656-4e0a-85d6-1750515b05d2 --text '...' --enter --json
```

### Reading messages from terminals
Since orchestration is unreliable, check Executor progress via git:
```sh
git pull --ff-only && git log --oneline -3
```

### Executor worker_done format
```
worker_done — <TASK NAME>. SHA <commit>. <what changed>. <gate results>. STOP.
```

---

## 4. Coordinator Role

### What Coordinator DOES
1. **Dispatch tasks** to Tier 1 or Tier 2 Executor based on complexity
2. **Run pre-gate checks** before sending to Verifier:
   - `go build ./...` (must pass)
   - `go vet ./...` (must pass)
   - `gofmt -l . | wc -l` (must be 0)
   - `go test -race ./... -count=1` (all must pass; doctor flaky test can be excluded)
   - git status (must be clean, HEAD==upstream)
   - Contract-specific checks (zero forbidden symbols, no deleted tests, etc.)
3. **Gate verdict:** "READY FOR VERIFIER" or "REJECT back to Executor"
4. **Escalate** between Tier 1 and Tier 2 when needed
5. **Read Verifier REJECT** and extract exact blockers for Executor

### What Coordinator DOES NOT DO
- **Never modify production code, tests, or architecture**
- **Never issue ACCEPT** — only Verifier does that
- **Never skip pre-gate** — even for "trivial" changes
- **Never delete terminals** — reuse existing ones

### Pre-gate Checklist (run from companion-daemon/)
```sh
cd /Users/mhk/orca/DevRemote/companion-daemon

# 1. Sync
git pull --ff-only

# 2. Build
go build ./... || { echo "REJECT: build"; exit 1; }

# 3. Vet
go vet ./... || { echo "REJECT: vet"; exit 1; }

# 4. Format
[ "$(gofmt -l . | wc -l)" -eq 0 ] || { echo "REJECT: gofmt"; exit 1; }

# 5. Tests (exclude flaky doctor test)
go test -race ./... -count=1 -timeout 300s

# 6. State
git status --short  # must be empty
[ "$(git rev-parse HEAD)" = "$(git rev-parse @{upstream})" ] || { echo "REJECT: not in sync"; exit 1; }

# 7. Contract-specific checks (vary by task)
# E.g.: grep "mux.Registry" for Task 2, grep "go:build legacy" for test migration, etc.

echo "READY FOR VERIFIER"
```

### Verifier Dispatch Format
```
<TASK NAME> VERIFICATION — HEAD <sha>

Pre-gate: <summary of what passed>

Verify:
1. <specific contract requirement>
2. <specific contract requirement>
...

ACCEPT or REJECT. Use orca terminal send --terminal term_9477482c-0b30-491d-a156-4b3911dc79c9 to reply.
```

---

## 5. Rejection Handling

### On First Rejection
1. Read Verifier's exact blockers (file:line:reason)
2. Forward to Executor with exact blocker list
3. Do not weaken the contract
4. Do not ask the wrong-tier Executor to fix code

### On Second Rejection (same blocker)
1. If Tier 1 (DeepSeek) → escalate to Tier 2 (Codex)
2. If Tier 2 (Codex) → escalate to Tier 1 (DeepSeek)
3. Split into smaller task boundary if needed

### Immediate REJECT (don't send to Verifier)
- Build, test, race, or gofmt fails
- Old and V1 paths coexist (bridge/wrapper)
- Evidence claims zero but grep proves otherwise
- Tests deleted without migration map
- Production code changed by EVID-only worker
- HEAD != upstream or worktree dirty

---

## 6. Common Patterns & Gotchas

### Evidence files
- Evidence SHA must be the ACTUAL commit SHA, not "(this commit)" or "TBD"
- Evidence HEAD must be recorded separately from production candidate
- All grep commands must use `grep -E` or `grep -rnE`
- Control: `printf "test" | grep -E "pattern"` exits 0 to prove pattern works
- Scan commands must use correct relative paths from companion-daemon/
- Jest count must be reproduced at exact HEAD

### Test requirements
- Never delete tests without migration map to V1 equivalents
- Tests must exercise production code paths (not clones/stubs)
- `//go:build legacy` tag is test hiding — equivalent to deletion
- Mobile tests: render actual FeedScreen, not just expect(true)

### Common DeepSeek failure patterns
- Evidence SHA = "(this commit)" instead of real SHA
- grep without -E flag
- Wrong paths in scan commands (mobile/src/ vs ../mobile/src/)
- Tests that don't exercise production code
- File permission/ownership inaccuracies

### Common Codex failure patterns
- Bridge/wrapper instead of actual deletion
- Production security boundary changes for test convenience
- Missing edge case tests

### Architecture invariants (must never be violated)
- Zero `mux.Registry`, `mux.Adapter`, `mux.Session`, `GetRecorder` in production
- `ManagedPTYLauncherV1.Spawn()` is the only controlled-PTY creation path
- No Registry fallback in managed paths
- PA3 captured-instance invariant preserved
- Loopback-only for insecure local mode

---

## 7. Quick Reference Commands

```sh
# List terminals
orca terminal list --worktree id:224412ce-4bad-4ef0-a68d-ce12acb30cbd::/Users/mhk/orca/DevRemote --json

# Read terminal output
orca terminal read --terminal <handle> --limit 20 --json

# Send to executor
orca terminal send --terminal term_9f142890-1eae-4509-9dee-3412b1d5f729 --text "..." --enter --json
orca terminal send --terminal term_49048007-d5df-40ce-b084-70d3bd270166 --text "..." --enter --json

# Send to verifier
orca terminal send --terminal term_8ca20c76-b656-4e0a-85d6-1750515b05d2 --text "..." --enter --json

# Check git state
git pull --ff-only && git log --oneline -5 && git status --short

# Full pre-gate
go build ./... && go vet ./... && [ "$(gofmt -l . | wc -l)" -eq 0 ] && go test -race ./... -count=1 -timeout 300s
```

---

## 8. Next Steps (for new Coordinator)

1. **Verify current state:** `git pull --ff-only && git log --oneline -3 && git status --short`
2. **Confirm terminals are connected:** list terminals, verify all 4 are `connected=True`
3. **Wait for user:** SM-S926N ADB connection is the user's responsibility
4. **When device connected:** Device matrix testing begins — Executor runs diagnostic commands, Verifier independently confirms results
5. **PB ACCEPT SHA remains UNSET** until physical matrix passes independent verification
