# Next Session Handoff — D1 Adapter Doctor/Repair

Status: **READY FOR A FRESH EXECUTION AGENT**

This is the authoritative D1 execution handoff. It starts from the independently
accepted R2 research tip and authorizes D1 only. Do not reconstruct the task from
an older roadmap, an unpushed worktree, or the earlier planning document alone.

## 1. Repository recovery and commit visibility

```text
repository: https://github.com/mhkim315/DevRemote.git
branch: feature/phase10-multi-adapter
accepted R2 / D1 baseline: 6f940b03bbb451dd0ddcfc6bf7ce5f86bbec3b50
accepted T2: ef4a162c7f9a5644fd52d89501e97f4e62301dfa
accepted T1: 162266f830caaf07bf701d9a1294557432855769
accepted T0: 3ce2604bd333dcb63142b5b1710185a823162efa
```

Run before editing:

```sh
git remote set-url origin https://github.com/mhkim315/DevRemote.git
git fetch origin feature/phase10-multi-adapter
git switch feature/phase10-multi-adapter
git merge --ff-only origin/feature/phase10-multi-adapter
git status --short
git log --oneline -25
git merge-base --is-ancestor 6f940b03bbb451dd0ddcfc6bf7ce5f86bbec3b50 HEAD
git merge-base --is-ancestor ef4a162c7f9a5644fd52d89501e97f4e62301dfa HEAD
git merge-base --is-ancestor 162266f830caaf07bf701d9a1294557432855769 HEAD
git merge-base --is-ancestor 3ce2604bd333dcb63142b5b1710185a823162efa HEAD
```

Every ancestry command must exit 0 and the worktree must be clean. If this file
is not present locally, do not guess its contents. Fetch and inspect it directly:

```sh
git show origin/feature/phase10-multi-adapter:docs/NEXT_SESSION_D1_ADAPTER_DOCTOR_REPAIR_HANDOFF.md
```

If the referenced baseline or handoff is not visible, fetch before asking the
user to paste it. Never reset, force-push, or discard another agent's work. If
the remote advances during implementation, fetch and rebase normally, preserve
all accepted ancestry, rerun the complete gate, and report the final full SHA.

## 2. Read before editing

Read in this order:

1. this handoff;
2. `docs/ADAPTER_DOCTOR_REPAIR_PLAN.md`;
3. `docs/R2_MULTI_AGENT_EXPANSION_RESEARCH_REPORT.md`;
4. `docs/r2-fixtures/MANIFEST.json` and the R2 D1 scenarios;
5. `docs/T0_COMMON_AGENT_EVENT_FINAL_ACCEPTANCE.md`;
6. `docs/T1_CODEX_ADAPTER_FINAL_ACCEPTANCE.md`;
7. `docs/T1_CODEX_ADAPTER_IMPLEMENTATION_REPORT.md`;
8. `docs/T2_CLAUDE_ADAPTER_IMPLEMENTATION_REPORT.md`;
9. `companion-daemon/internal/agent/contract/contract.go`;
10. `companion-daemon/internal/agent/contract/cursor.go`;
11. `companion-daemon/internal/agent/contract/validate.go`;
12. `companion-daemon/internal/agent/contract/harness.go`;
13. accepted Codex adapter and tests under
    `companion-daemon/internal/agent/adapters/codex/v0_144_1/`;
14. accepted Claude adapter and tests under
    `companion-daemon/internal/agent/adapters/claude/v2_1_202/`;
15. `docs/ROADMAP_AFTER_E10B.md` (`T0-R2-D1-T3` section).

T0, T1, T2, and R2 are accepted inputs. D1 may consume their contracts,
descriptors, fixtures, and reports. It may not silently revise them.

## 3. D1 objective

Implement a constrained, local Adapter Doctor/Repair workflow for ordinary
Codex or Claude version drift:

```text
detect incompatible adapter/version/shape
→ produce bounded and redacted drift evidence
→ create an isolated repair workspace for one selected adapter/version
→ allow a local coding-agent runner to propose a narrow patch
→ reject every path outside the selected adapter allowlist
→ run Pokit-owned fixed compatibility and regression tests
→ produce an inert review bundle containing diff, provenance, and results
→ require explicit user approval before any activation
→ preserve a reversible rollback target
```

D1 is successful only if the safety boundary is enforced by production code and
adversarial tests. A helper, prompt template, report-only implementation, or
green happy-path test is not sufficient.

## 4. Frozen Pokit-owned contract

The following operations and their T0 semantics are immutable D1 inputs:

```text
detect
discoverSessions
readEvents
normalizeEvent
detectApproval
getStatus
```

Do not modify the T0 interface, common `AgentEvent`, provenance/confidence tiers,
correlation vocabulary, approval authority, status precedence, cursor/resource
bounds, safe defaults, or fixed conformance harness to make a repair pass.

The repair agent may never edit:

```text
companion-daemon/internal/agent/contract/
companion-daemon/internal/term/
companion-daemon/internal/devicetrust/
mobile/
scripts/build-gate.sh
```

It also may not change authentication, tickets, audit, lifecycle, Recorder,
Terminal, PTY, public DTOs, or Windows runtime behavior.

## 5. Authorized repair targets

One repair request selects exactly one provider and one new version target.

Codex repair writes may target only a newly created version-specific subtree and
its new redacted fixtures, for example:

```text
companion-daemon/internal/agent/adapters/codex/<new_version>/
```

Claude repair writes may target only:

```text
companion-daemon/internal/agent/adapters/claude/<new_version>/
```

The accepted subtrees `codex/v0_144_1` and `claude/v2_1_202`, their fixtures,
and their expected results are immutable regression evidence. Prefer adding a
new version package over editing an accepted package. Any exceptional change to
an accepted version is outside this handoff and must stop for separate review.

The repair workflow must reject:

- a path outside the selected provider's new version subtree;
- a change to the other provider;
- deletion, rename, symlink, submodule, binary patch, generated executable, or
  path traversal;
- a change to the contract, fixed harness, gates, activation code, or tests that
  judge the patch;
- a fixture without provider/version/source provenance and redaction manifest.

R2's Goose recommendation is planning evidence only. D1 does not implement a
Goose, Gemini, OpenCode, Cline, OpenHands, or Aider production adapter.

## 6. Required production boundaries

Use repository naming conventions, but keep these responsibilities separate and
testable. A suitable new package is `companion-daemon/internal/agent/doctor/`.

### 6.1 Compatibility assessment

Accept explicit adapter descriptors and already prepared evidence. Return a
typed result such as compatible, drift_detected, unsupported, or inaccessible.
Do not recursively scan `$HOME`, guess private paths, or read raw provider logs.

The drift report may include only bounded values such as:

- provider and reported version;
- accepted adapter version/capabilities;
- missing or changed path category, without private full path;
- unknown discriminator and bounded field-name/type shape;
- cursor/read/fixture failure category;
- provenance and remaining uncertainty.

Prompts, assistant content, thinking, commands, tool input/output, source code,
tokens, keys, signatures, raw JSONL, terminal input, and private absolute paths
must not enter diagnostics, logs, review bundles, or tests.

### 6.2 Repair request and runner boundary

Define an injectable repair-runner interface. The runner receives only:

- a temporary workspace containing the selected new-version adapter skeleton;
- frozen contract documentation copied read-only or supplied as bounded context;
- redacted drift evidence;
- new-version fixture material explicitly admitted by the user/workflow;
- the exact allowed-file manifest.

The runner must not receive activation credentials, secrets, host logs, the
user's home directory, unrestricted repository access, network access, or an
arbitrary shell. Production code must use an explicit command/process allowlist;
tests must use a fake runner. Do not make tests invoke Codex or Claude binaries.

If a secure real coding-agent invocation cannot be implemented without granting
arbitrary filesystem/process access, keep the runner pluggable and fail closed;
do not weaken the sandbox or claim end-to-end repair execution.

### 6.3 Patch inspection

Treat generated output as hostile. Parse and validate the patch before applying
it anywhere. Canonicalize every path and reject ambiguous encodings, absolute
paths, `..`, symlinks, hard-link escapes, mode changes that create executables,
and writes through an ancestor outside the workspace.

Compute a deterministic patch digest and bind it to:

- request ID;
- provider and target version;
- accepted baseline SHA;
- allowed-file manifest;
- evidence digest;
- fixed-suite result;
- rollback target.

Any change after validation invalidates tests and approval.

### 6.4 Fixed-suite execution

Tests and their expected results live outside the runner-writable workspace. The
runner cannot edit, skip, replace, filter, or select them. The D1 orchestrator
owns the exact fixed command list.

At minimum run:

- frozen T0 conformance against the proposed new adapter;
- accepted Codex and Claude contract/regression suites unchanged;
- current and retained older-version fixtures;
- malformed, unknown, truncated, oversized, and hostile inputs;
- cursor/order/dedup/session-correlation/resource-bound tests;
- approval positive evidence only where capability and fixture justify it;
- approval near-miss negatives for text, prompts, policy modes, missing IDs,
  wrong sessions, advisory provenance, and low confidence;
- status precedence and advisory downgrade;
- diagnostics/fixture secret and path leakage checks;
- filesystem allowlist and patch-parser adversarial tests;
- failure isolation and unchanged common DTO snapshots.

No generated patch may update an expected result merely to turn red into green.

### 6.5 Review bundle and approval

Before approval, produce an inert, serializable review bundle containing:

```text
request ID
provider / observed version / target adapter version
accepted baseline and rollback SHA
bounded drift evidence and provenance
allowed and actually changed files
unified diff and patch digest
new fixture manifest and redaction result
fixed test commands and complete results
accepted older-version regression result
approval false-positive result
remaining unknown or unsupported evidence
activation status = pending
```

No raw credential or user content may be present. Approval must bind to the
exact patch digest and test result. Unknown, expired, mismatched, already-used,
or altered approval material fails closed.

There is no default, timeout, green-test, background, or coding-agent approval.
Only an explicit local user action may authorize activation. Do not expose the
review/approval surface through the remote tunnel.

### 6.6 Activation and rollback boundary

Activation must be a separately invoked operation after approval validation.
Patch generation and test completion must never call it automatically.

Do not invent runtime hot-loading for compiled Go adapters. If this repository
does not yet have a safe, well-defined activation mechanism, implement and test
the inert review/approval boundary, report activation as genuinely blocked, and
stop for design review rather than rewriting build/install/daemon lifecycle.
Do not equate writing a source patch, committing it, rebuilding a binary, or
restarting the daemon with silent user-approved runtime activation.

Any implemented activation must preserve the prior accepted adapter as an
immediate rollback target and must not rewrite stored common events.

## 7. Required safety behavior

The implementation must prove:

1. unknown or malformed evidence never fabricates a semantic event;
2. absence of required data becomes unsupported/degraded, not guessed repair;
3. inaccessible, encrypted, or removed source data is not recoverable by D1;
4. adapter failure never blocks Live Terminal or changes process lifecycle;
5. repairing Codex cannot change Claude, and vice versa;
6. approval-like UI/text/PTY evidence never becomes approval authority;
7. the coding agent cannot change the stable contract, tests, gates, approval,
   activation, sandbox, or rollback logic;
8. a test result is valid only for the exact approved patch digest;
9. concurrent requests cannot cross-wire provider, evidence, patch, or approval;
10. cancellation/unmount/process failure leaves no approved or active partial
    state and cleans only D1-owned temporary resources;
11. logs and errors remain bounded and secret-free;
12. no network retrieval occurs without a separate user-authorized research
    step outside the repair runner.

## 8. Mandatory adversarial tests

Include deterministic tests for:

- valid Codex repair request and valid Claude repair request using fake runners;
- cross-provider patch attempt;
- contract/harness/gate/activation path modification;
- traversal, absolute path, symlink, hard link, rename, delete, binary patch,
  executable-bit and oversized patch rejection;
- prompt/token/private-path/raw-record leakage sentinels;
- malformed and oversized evidence;
- patch mutation after tests and after approval;
- approval for the wrong request/provider/version/baseline/digest;
- replayed, expired, missing, and implicit approval;
- runner crash, timeout, cancellation, partial output, and hostile diagnostics;
- simultaneous repair requests with no state crossover;
- retained T1/T2 fixtures and fixed harness still passing;
- failure leaves the existing adapter usable and Terminal unaffected.

Run race tests repeatedly for shared request/review state. Tests must inspect
actual production controller/orchestrator functions rather than copied helper
logic.

## 9. Scope exclusions

Do not begin or implement during D1:

- T3 Transcript projection, storage, or mobile integration;
- S1 product status model/UI;
- A1 approval product UX or remote approval actions;
- O1 orchestrator;
- a third production adapter from R2;
- provider installation, package updates, or network research;
- automatic mutation of Codex/Claude settings or hooks;
- Terminal, Recorder, PTY, authentication, lifecycle, tickets, audit, mobile,
  Android, or iOS changes;
- Windows runtime or speculative Windows abstractions.

Windows remains deferred. Do not add ConPTY, CNG/TPM, DPAPI, Named Pipes,
Windows Services, NTFS ACL logic, or generalized Windows-ready abstractions.

## 10. Suggested execution order

1. Recover the exact remote baseline and verify all accepted ancestry.
2. Write a short design note naming the new package, immutable boundaries,
   request states, allowed paths, fixed commands, and activation limitation.
3. Implement typed compatibility/drift evidence with strict bounds/redaction.
4. Implement the injected repair-runner boundary and fake runner tests.
5. Implement hostile patch parsing, canonical allowlist enforcement, digest and
   request binding.
6. Implement fixed-suite ownership outside the runner workspace.
7. Implement inert review bundle and explicit digest-bound approval state.
8. Implement activation only if the repository already supports a safe,
   reversible mechanism; otherwise fail closed and document the blocker.
9. Add the full adversarial/race suite and rerun accepted T0/T1/T2 regressions.
10. Write `docs/D1_ADAPTER_DOCTOR_REPAIR_IMPLEMENTATION_REPORT.md`.
11. Commit and push one or more narrow, reviewable D1 commits, leave the
    worktree clean, and stop for independent verification.

Do not begin T3 automatically after local D1 completion.

## 11. Required gates

Run from the final tree:

```sh
gofmt on every changed Go file
cd companion-daemon && GOCACHE=/tmp/devremote-d1-go-cache go test -race ./internal/agent/... -count=1
cd companion-daemon && GOCACHE=/tmp/devremote-d1-go-cache go test -race ./internal/agent/... -count=10
cd companion-daemon && GOCACHE=/tmp/devremote-d1-go-cache go build ./... && go vet ./... && go test -race ./...
cd mobile && npm run typecheck && npm test -- --runInBand
cd .. && python3 docs/r2-fixtures/validate_acp.py
sh scripts/build-gate.sh
git diff --check
git status --short
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git merge-base --is-ancestor 6f940b03bbb451dd0ddcfc6bf7ce5f86bbec3b50 HEAD
```

If `jsonschema` is absent, install only the documented pinned validation
dependency in the execution environment; do not weaken or skip the R2 fixture
gate. Record the exact version used. Final local and remote hashes must match,
ancestry must exit 0, and the worktree must be clean.

Do not claim a coding-agent sandbox, activation, rollback, or physical product
gate that was not actually executed.

## 12. Required implementation report and REVIEW REQUEST

The report must provide a requirement-to-evidence matrix and include:

```text
REVIEW REQUEST: D1 Adapter Doctor/Repair
baseline R2: 6f940b03bbb451dd0ddcfc6bf7ce5f86bbec3b50
tip: <full SHA>
scope: D1 only
production entry/controller: <exact files and functions>
supported repair targets: <exact provider/version/path allowlists>
immutable paths: <exact declaration>
evidence bounds/redaction: <limits and tests>
runner sandbox: <real boundary or explicit fail-closed limitation>
patch parser/allowlist: <accepted and rejected operations>
fixed suite ownership: <commands, location, proof runner cannot edit>
review bundle/digest binding: <exact contract>
approval: <explicit local action and replay/mismatch tests>
activation/rollback: <implemented proof or honest blocker>
T0/T1/T2 regressions: <commands/results, zero skips>
race/adversarial tests: <commands/results>
full gate: <exact commands/results>
deferred: T3, S1, A1, O1, third adapters, network research, Windows
```

The final commit message or implementation report must contain the literal
heading `REVIEW REQUEST: D1 Adapter Doctor/Repair` so the verifier can distinguish
an intermediate checkpoint from the final review request.

Push to `feature/phase10-multi-adapter`, verify remote tip equals local HEAD,
leave the worktree clean, and stop. A fresh verifier must inspect actual
production boundaries and adversarial behavior; the implementation report alone
is not proof.

