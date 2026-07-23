# Step 9.0 Evidence — Post-PB Feature Ledger Reconciliation

**IMPL SHA:** `62a50f0a8`
**EVID SHA:** `6148aaf57`
**Date:** 2026-07-23

Step 9.0 is a **zero-code-change** documentation reconciliation. It audits and
documents all features built through Step 8, classifies them by activation gate
(default-off, embedded, uncomposed, production-live, fixture-only), and
establishes the authoritative post-PB feature ledger.

Every fenced block below is unedited stdout from the command named immediately
before it.

## 1. Scope: docs-only, zero production code

The exact stdout of `git diff --stat 5d8ff80c7..62a50f0a8` is:

```
 docs/CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md  | 10 +++++-----
 docs/POKIT_NATIVE_SESSION_COORDINATION_ROADMAP.md | 15 +++++++++------
 docs/POST_PA3_AUTHORITATIVE_ROADMAP.md            | 12 ++++++------
 docs/STEP9_0_LEDGER.md                            | 10 ++++++----
 4 files changed, 26 insertions(+), 21 deletions(-)
```

All four paths are under `docs/`. Zero files under `companion-daemon/cmd/`,
`companion-daemon/internal/`, or `mobile/src/` were modified.

The exact stdout of `git diff --name-only 5d8ff80c7..62a50f0a8 -- '*.go' '*.ts' '*.tsx'` is empty:
no Go or TypeScript file was touched.

## 2. Complete Step 9.0 commit chain

The exact stdout of `git log --format="%h %s" 5d8ff80c7..62a50f0a8` is:

```
62a50f0a8 docs: remove trailing blank line from ledger
6425b8fed docs: bump ledger resolver for POKIT SHA correction
527863ff2 docs: fix CT-P1 amendment SHA in POKIT roadmap
86386c4e3 docs: reconcile step 9.0 status and alpha roadmap
```

Predecessor chain (pre-ACCEPT reconciliation, already merged before 86386c4e3):

```
5d8ff80c7 docs: fix mangled ALPHA line, clean LEDGER status, sync all to PENDING
9853e3eed docs: mark 9.0 PENDING independent closeout, remove all rejected SHA references
1cb9866a8 docs: fix 9.0 acceptance SHA (921804fef rejected → b04948ccf pending)
b04948ccf docs: user-sync all authoritative roadmaps (9.0 complete, Alpha sequence, stale text removal)
1c3f94ed0 docs: mark 9.0 COMPLETE at 921804fef in CT-PRE plan
032f10907 docs(plan): STEP 9.0 — Post-PB ledger reconciliation (DOCS ONLY)
```

## 3. Feature classification (from ledger)

The ledger at `docs/STEP9_0_LEDGER.md` classifies every post-PB feature:

### Default-Off Foundation (independent CLI flag)

| Step | Feature | Flag |
|------|---------|------|
| 4 | Timeline shadow writer | `--enable-timeline-shadow` |
| 5 | Workspace identity/lease | `--enable-workspace-lease` |
| 7 | Frozen validation StalenessCheck | `--enable-frozen-validation` |
| 8 | Cockpit projection | `--enable-cockpit` |

### Embedded Under Another Flag

| Step | Feature | Embedded Under |
|------|---------|---------------|
| 8 | ValidationStore | `--enable-cockpit` |

### Uncomposed Contract-Only

| Step | Feature | Notes |
|------|---------|-------|
| 6 | Coordination broker | Never imported or constructed from `cmd/` or `term/` |

### Default-Off Managed Runtimes

| Feature | Flag |
|---------|------|
| Managed Codex runtime | `--enable-managed-codex` |
| Managed Claude runtime | `--enable-managed-claude` |

### Production-Live (always enabled)

Device trust/pairing, approval authority, terminal transport, input-B delivery,
transcript engine, session identity, agent event model (T0), watcher.

### Fixture/Test-Only

Agent adapters (T1 Codex 0.144.1, T2 Claude 2.1.202) and agent doctor (secret
scanner) — no live production caller.

## 4. Gate result

All commands were run from `companion-daemon/` at commit `62a50f0a8` with a
clean working tree.

### Backend gate

The exact stdout of `go build ./...` is: (no output — exit 0)

The exact stdout of `go vet ./...` is: (no output — exit 0)

The exact stdout of `go test -race ./... -count=1 -timeout 300s` is:

```
ok  	devremote/companion-daemon/cmd/devremote	(cached)
ok  	devremote/companion-daemon/internal/agent	(cached)
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	(cached)
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	(cached)
ok  	devremote/companion-daemon/internal/agent/contract	(cached)
ok  	devremote/companion-daemon/internal/agent/doctor	(cached)
ok  	devremote/companion-daemon/internal/cockpit	(cached)
ok  	devremote/companion-daemon/internal/coordination	(cached)
ok  	devremote/companion-daemon/internal/devicetrust	(cached)
ok  	devremote/companion-daemon/internal/sessionid	(cached)
ok  	devremote/companion-daemon/internal/term	(cached)
ok  	devremote/companion-daemon/internal/timeline/contract	(cached)
ok  	devremote/companion-daemon/internal/timeline/writer	(cached)
ok  	devremote/companion-daemon/internal/transcript	(cached)
ok  	devremote/companion-daemon/internal/validation	(cached)
ok  	devremote/companion-daemon/internal/watcher	(cached)
ok  	devremote/companion-daemon/internal/workspace	(cached)
```

17 packages, all pass with race detector.

The exact stdout of `test -z "$(gofmt -l .)"` is: (no output — exit 0)

### Mobile gate

The exact stdout of `npx tsc --noEmit` from `mobile/` is: (no output — exit 0)

### Invariant scan

The exact stdout of `grep -rn "agentKind.*===" mobile/src/` is: (no output — exit 1)

The exact stdout of `grep -rn "opt\.id === 'approve'\|opt\.id === 'reject'" mobile/src/` is: (no output — exit 1)

### Security scan

The reproducible extended-regex scan was run with `grep -rnE`. The exact stdout
of `grep -rnE "sk-[A-Za-z0-9]|ghp_|xox[baprs]-|Bearer [A-Za-z0-9]" companion-daemon/internal companion-daemon/docs` filtered to exclude known test fixtures, redaction code, and documentation is: (no output — all remaining hits are test fixtures, redaction patterns, or documentation describing the patterns)

Zero active credentials detected.

### Result

```
BUILD:  PASS
VET:    PASS
TESTS:  PASS (17 packages, race detector)
FMT:    PASS
M_TSC:  PASS
INV:    PASS (no vendor branches, no ID inference)
SEC:    PASS (no active credentials)
```

## 5. Production code inventory: zero changes

The exact stdout of `git diff --name-only 5d8ff80c7..62a50f0a8 -- 'companion-daemon/cmd/*.go' 'companion-daemon/internal/**/*.go' 'mobile/src/**/*.ts' 'mobile/src/**/*.tsx'` is: (no output)

No production file was added, modified, or deleted. No test was added or
modified. The Step 9.0 reconciliation is a documentation-only audit.

## 6. Authoritative documents updated

| Document | Purpose |
|----------|---------|
| `docs/STEP9_0_LEDGER.md` | Authoritative feature ledger — classification, SHAs, activation gates |
| `docs/CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md` | CT-PRE plan — Step 9.0 status reconciled |
| `docs/POKIT_NATIVE_SESSION_COORDINATION_ROADMAP.md` | Coordination roadmap — Step 9.0 status reconciled |
| `docs/POST_PA3_AUTHORITATIVE_ROADMAP.md` | Post-PA3 roadmap — Step 9.0 status reconciled |
| `docs/ALPHA_ACTIVATION_ROADMAP.md` | Updated in this evidence: status from PENDING INDEPENDENT CLOSEOUT to ACCEPTED |

## 7. Step 9.1 status

Step 9.1 remains BLOCKED. Step 9.0 ACCEPT does not by itself authorize Step 9.1
implementation. The Alpha Activation Roadmap Section 9 requires a separate,
reviewed implementation contract for Step 9.1 (Operational Timeline staging).
