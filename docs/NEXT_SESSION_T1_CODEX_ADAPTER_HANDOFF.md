# Next Session Handoff — T1 Codex Version-Specific Adapter

Status: **COMPLETE / SUPERSEDED — T1 accepted at `162266f83`; use the T2 handoff**

Historical execution instructions are retained below as acceptance provenance.
New work must use `docs/NEXT_SESSION_T2_CLAUDE_ADAPTER_HANDOFF.md`; do not restart
T1 from this document.

This is the authoritative T1 execution handoff. Work from the remote branch and
accepted T0 commit below. Do not infer the baseline from local history or an old
Agent Adapter phase document.

## 1. Repository recovery and commit visibility

```text
repository: https://github.com/mhkim315/DevRemote.git
branch: feature/phase10-multi-adapter
accepted T0: 3ce2604bd333dcb63142b5b1710185a823162efa
accepted M3b ancestor: 9cdf2f290299f70d0b6d58e139771bfc336adb57
```

Run:

```sh
git remote set-url origin https://github.com/mhkim315/DevRemote.git
git fetch origin feature/phase10-multi-adapter
git switch feature/phase10-multi-adapter
git merge --ff-only origin/feature/phase10-multi-adapter
git status --short
git log --oneline -15
git merge-base --is-ancestor 3ce2604bd333dcb63142b5b1710185a823162efa HEAD
```

The final command must exit 0 and the worktree must be clean. If this document is
not visible locally, fetch and read it from the remote rather than reconstructing
it:

```sh
git show origin/feature/phase10-multi-adapter:docs/NEXT_SESSION_T1_CODEX_ADAPTER_HANDOFF.md
```

Never reset, force-push, or discard another agent's work to recover the handoff.

## 2. Read before editing

Read in this order:

1. `docs/T0_COMMON_AGENT_EVENT_FINAL_ACCEPTANCE.md`
2. `docs/T0_COMMON_AGENT_EVENT_IMPLEMENTATION_REPORT.md`
3. `companion-daemon/internal/agent/contract/contract.go`
4. `companion-daemon/internal/agent/contract/cursor.go`
5. `companion-daemon/internal/agent/contract/validate.go`
6. `companion-daemon/internal/agent/contract/harness.go`
7. `docs/R1_RUNTIME_SIGNAL_MATRIX.md` (`Codex CLI` section)
8. `docs/R1_RUNTIME_SIGNAL_EVIDENCE_MANIFEST.md`
9. existing Codex fixtures under `companion-daemon/internal/agent/testdata/codex/`
10. existing historical Codex parser/detector and
    `docs/AGENT_PHASE_A6_BACKEND_ACCEPTANCE.md`
11. `docs/ADAPTER_DOCTOR_REPAIR_PLAN.md` for future compatibility constraints

The historical A6 Codex backend slice is evidence and regression coverage; it is
not T1 completion. T1 must implement the accepted T0 six-operation interface and
pass its fixed harness without changing the contract or harness.

## 3. T1 objective

Implement one version-specific Codex adapter behind the accepted T0 contract.
Prove that actual redacted Codex native records can be detected, bounded-read,
normalized, status-resolved, and approval-checked without provider fields leaking
into common DTOs.

The adapter must implement:

```text
Descriptor
Detect
DiscoverSessions
ReadEvents
NormalizeEvent
DetectApproval
GetStatus
```

Prefer a clearly isolated subtree such as:

```text
companion-daemon/internal/agent/adapters/codex/<supported-version>/
```

The exact package layout may follow repository conventions, but Codex-version
logic and fixtures must be separable from the Pokit-owned contract and from the
future Claude adapter.

## 4. Evidence and support boundary

Known local evidence:

```text
~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl
companion-daemon/internal/agent/testdata/codex/*.jsonl
docs/r1-fixtures/codex-app-server-0.144.1.json
```

Rules:

- establish the exact supported Codex version/shape from redacted evidence and
  record it in `Descriptor.SupportedVersions` and fixture metadata;
- preserve all existing Codex fixtures and tests;
- add new fixtures only after structural redaction; never commit raw prompts,
  commands, source, credentials, complete home paths, or private logs;
- unknown/new versions or incompatible shapes return degraded/unsupported, not a
  best-guess confident mapping;
- `history.jsonl` alone is not authoritative tool/approval evidence;
- the R1 app-server schema proves a versioned transport exists, but R1 did not
  correlate its thread IDs to an independently launched interactive Codex TUI.
  Do not attach app-server events to arbitrary TUI sessions;
- a future dedicated managed app-server launch mode requires explicit
  Pokit-session ↔ Codex-thread correlation and is outside T1 unless already proven
  by accepted evidence.

If the current fixtures cannot establish a claimed version or correlation, narrow
the supported capability and report the limitation. Do not invent evidence.

## 5. Required behavior

### Detection and discovery

- empty or unrelated process/session evidence returns `unknown` below the
  confidence floor;
- Codex detection uses declared evidence, not cwd substring alone;
- discovery is bounded and does not claim `proven` or `managed_launch`
  correlation without explicit evidence;
- file/path resolution is read-only, bounded, and confined to accepted Codex
  roots/patterns; symlinks, non-regular files, traversal, and unrestricted home
  scans fail closed;
- diagnostics expose redacted display paths only.

### Event reads and normalization

- apply T0 cursor, raw-record, batch-count, batch-byte, event-count, metadata,
  ordering, session-binding, and degraded-truncation rules;
- stable event ID and Seq survive rereads; the returned cursor is a compact
  bounded high-water mark, not accumulated event history;
- recognized version-native records map only to the closed common vocabulary;
- unknown fields remain forward-compatible; unknown event discriminators become
  bounded unknown/degraded evidence;
- malformed/truncated/oversized records cannot panic or fabricate typed events;
- provider-native field names remain inside the version-specific implementation;
- user prompt and terminal input content are omitted or safely redacted according
  to the accepted contract.

### Approval and status

- `approval_requested` requires a stable non-empty ApprovalID from structured,
  accepted native evidence;
- DetectApproval preserves ApprovalID, exact Pokit session, agent kind, source,
  confidence, and authoritative provenance binding;
- approval-like text, assistant messages, missing IDs, low confidence, screen,
  PTY structural, heuristic, prompt-hint, and unknown provenance produce no
  approval;
- approval resolved events bind to the same provider approval identity;
- status follows T0 provenance precedence and advisory-terminal downgrade rules;
- agent status never becomes daemon process lifecycle authority.

## 6. Fixed harness and regression proof

The adapter must invoke the accepted external harness:

```go
contract.RunAgentContract(...)
```

Supply real, redacted Codex fixtures for:

- positive detection;
- known normalized events;
- distinct/sized valid records;
- malformed and unknown records;
- approval positive and adversarial near-miss records;
- failing adapter behavior.

Mandatory evidence:

- fixed T0 conformance suite passes with zero skips;
- retained older Codex fixture suite passes;
- current Codex parser/detector regression tests pass unchanged unless an explicit
  compatibility adapter is added;
- Claude and other adapters are unchanged and their regressions remain green;
- contract/harness files are not edited to make Codex pass;
- negative test proves an unsupported version/shape does not silently activate.

## 7. Scope boundaries

Do not implement during T1:

- Claude adapter (T2);
- Adapter Doctor/Repair, coding-agent patch generation, or activation (D1);
- public `/api/sessions` migration or mobile Transcript integration (T3);
- rich status UX (S1), approval action UX (A1), orchestration (O1);
- app-server-managed launch without explicit accepted correlation;
- Recorder, PTY, Terminal, lifecycle, auth, ticket, revoke, audit, mobile, Android,
  or iOS changes;
- Windows support or speculative Windows abstractions.

Do not weaken T0 bounds, provenance, approval, status, or session-correlation
rules to fit existing Codex code. If the adapter cannot pass without changing the
contract/harness, stop and report the mismatch for independent review.

## 8. Suggested work order

1. Audit existing Codex parser/detector/resolver and fixture provenance.
2. Write a precise supported-version and source/correlation decision before code.
3. Add the isolated T0 Codex adapter and redacted version fixtures.
4. Add fixed-harness invocation and targeted negative tests.
5. Preserve historical Codex/Claude regressions.
6. Run focused race tests repeatedly, then the full gate.
7. Write `docs/T1_CODEX_ADAPTER_IMPLEMENTATION_REPORT.md` with source/version
   matrix, changed files, six-op mapping, security bounds, test evidence,
   limitations, and verifier prompt.
8. Commit and push, then stop for independent verification. Do not start T2.

## 9. Required gates

```sh
gofmt on changed Go files
cd companion-daemon && go test -race ./internal/agent/... -count=1
cd companion-daemon && go build ./... && go vet ./... && go test -race ./...
cd mobile && npm run typecheck && npm test -- --runInBand
cd .. && sh scripts/build-gate.sh
git diff --check
git status --short
```

On a clean checkout, run the documented clean Expo Android prebuild before the
native compile gate when the tracked Android tree is partial. Do not claim a
physical-device gate that was not run.

## 10. Completion and review request

The final report/commit must contain:

```text
REVIEW REQUEST: T1 Codex Version-Specific Adapter
baseline: 3ce2604bd333dcb63142b5b1710185a823162efa
tip: <full SHA>
scope: T1 only
supported Codex version/shape: <exact declaration>
correlation claims: <proven / unavailable and why>
gate result: <exact commands/results>
deferred: T2, D1, T3, app-server managed launch, product integration
```

Push to `feature/phase10-multi-adapter`, verify remote tip equals local HEAD,
leave the worktree clean, and stop. A fresh verifier must inspect the actual
version-native mappings, fixed-harness evidence, approval negatives, path/read
bounds, unsupported-version behavior, and regression results.

Only after independent T1 ACCEPT should a new T2 Claude handoff be written.
