# Next Session Handoff — T2 Claude Version-Specific Adapter

Status: **READY FOR A FRESH EXECUTION AGENT**

This is the authoritative T2 execution handoff. It begins only after independent
T1 ACCEPT. Work from the remote branch and commits below; do not reconstruct the
baseline from an older Agent Adapter document or from an unpushed worktree.

## 1. Repository recovery and commit visibility

```text
repository: https://github.com/mhkim315/DevRemote.git
branch: feature/phase10-multi-adapter
accepted T1: 162266f830caaf07bf701d9a1294557432855769
accepted T0: 3ce2604bd333dcb63142b5b1710185a823162efa
accepted M3b ancestor: 9cdf2f290299f70d0b6d58e139771bfc336adb57
```

Run before editing:

```sh
git remote set-url origin https://github.com/mhkim315/DevRemote.git
git fetch origin feature/phase10-multi-adapter
git switch feature/phase10-multi-adapter
git merge --ff-only origin/feature/phase10-multi-adapter
git status --short
git log --oneline -20
git merge-base --is-ancestor 162266f830caaf07bf701d9a1294557432855769 HEAD
git merge-base --is-ancestor 3ce2604bd333dcb63142b5b1710185a823162efa HEAD
```

Both ancestry commands must exit 0 and the worktree must be clean. If this file
is missing locally, fetch it directly instead of guessing:

```sh
git show origin/feature/phase10-multi-adapter:docs/NEXT_SESSION_T2_CLAUDE_ADAPTER_HANDOFF.md
```

Never reset, force-push, or discard another agent's work to recover the handoff.
If the remote tip advances while working, fetch and rebase normally, preserve
the accepted ancestry, rerun every required gate, and report the resulting tip.

## 2. Read before editing

Read in this order:

1. `docs/T1_CODEX_ADAPTER_FINAL_ACCEPTANCE.md`
2. `docs/T0_COMMON_AGENT_EVENT_FINAL_ACCEPTANCE.md`
3. `docs/T0_COMMON_AGENT_EVENT_IMPLEMENTATION_REPORT.md`
4. `companion-daemon/internal/agent/contract/contract.go`
5. `companion-daemon/internal/agent/contract/cursor.go`
6. `companion-daemon/internal/agent/contract/validate.go`
7. `companion-daemon/internal/agent/contract/harness.go`
8. accepted Codex implementation under
   `companion-daemon/internal/agent/adapters/codex/v0_144_1/`
9. `docs/R1_RUNTIME_SIGNAL_MATRIX.md` (`Claude Code` section)
10. `docs/R1_RUNTIME_SIGNAL_EVIDENCE_MANIFEST.md`
11. `docs/r1-fixtures/claude-hooks-2.1.206.jsonl`
12. existing Claude fixtures under
    `companion-daemon/internal/agent/testdata/claude/`
13. historical `companion-daemon/internal/agent/claude_adapter.go` and its tests
14. `docs/ADAPTER_DOCTOR_REPAIR_PLAN.md` and
    `docs/R2_MULTI_AGENT_EXPANSION_RESEARCH_PLAN.md` as future constraints only

The historical parser/detector/resolver is evidence and regression coverage. It
does not implement the frozen T0 `contract.AgentAdapter` and is not T2
completion. Do not replace or silently migrate public/product DTOs in T2.

## 3. T2 objective

Implement an isolated, version-specific Claude adapter behind the accepted T0
six-operation contract:

```text
Descriptor
Detect
DiscoverSessions
ReadEvents
NormalizeEvent
DetectApproval
GetStatus
```

Use a separable subtree such as:

```text
companion-daemon/internal/agent/adapters/claude/v2_1_202/
```

The exact directory may follow repository convention, but Claude-native schema,
cursor, fixtures, and mappings must remain outside the Pokit-owned contract and
outside the accepted Codex implementation.

T2 is adapter implementation and conformance only. It is not production
Transcript registration, hook installation, managed launch, mobile UI, or D1/R2
work.

## 4. Authoritative evidence and version boundary

Two distinct evidence surfaces exist and must not be conflated.

### Retained session JSONL evidence

The existing redacted session fixtures contain top-level version `2.1.202` and
Claude session JSONL shapes such as:

```text
user
assistant text/thinking/tool_use
user tool_result
permission-mode
file-history-snapshot / attachment / unknown
```

This is the current candidate for the version-specific T2 session adapter. The
implementation must enforce the exact version/shape actually supported by the
retained evidence. Missing, malformed, conflicting, or unsupported versions may
not activate confident typed mappings.

### R1 hook-stream evidence

R1 observed Claude Code `2.1.206` `UserPromptSubmit` hook lifecycle records in an
isolated print-mode invocation. That evidence proves a hook surface exists. It
does **not** prove:

- ordinary `pokit run claude` TUI correlation;
- Pokit-session to Claude `session_id` ownership;
- that hook and session JSONL shapes are interchangeable;
- approval semantics for the retained `permission-mode` fixture;
- permission to edit user/project/admin Claude settings.

Do not label `2.1.202` session fixtures as `2.1.206`, merge hook events into an
ordinary TUI stream, or claim hook/managed-launch correlation without new
accepted evidence. A separate 2.1.206 hook adapter is outside the minimal T2
path unless controlled evidence proves all required bindings without expanding
scope.

If fixture provenance cannot justify the intended version or semantic mapping,
narrow the capability and report it. Never invent supporting evidence.

## 5. Frozen contract and non-editable boundary

T2 imports the accepted T0 contract and invokes its fixed harness. Do not edit:

```text
companion-daemon/internal/agent/contract/contract.go
companion-daemon/internal/agent/contract/cursor.go
companion-daemon/internal/agent/contract/validate.go
companion-daemon/internal/agent/contract/harness.go
```

Do not change the accepted Codex adapter to make Claude pass. A T2 change under
the Codex subtree is a scope violation unless it is a separately demonstrated
regression caused by T2 infrastructure, in which case stop for review first.

## 6. Detection, discovery, and source boundaries

- empty/unrelated evidence returns `unknown` below the confidence floor;
- exact process evidence may identify Claude; CWD substring alone is not enough;
- manual/provider session identity supplied by request data cannot override the
  trusted `SessionContext` binding;
- R1 did not prove ordinary TUI correlation, so `DiscoverSessions` must not
  invent `proven` or `managed_launch` ownership;
- do not manufacture a provider session ID from the Pokit session ID;
- do not claim log discovery merely because the historical resolver constructs
  a `.claude/projects` glob;
- any file discovery added in T2 must be read-only, bounded, rooted in accepted
  Claude locations, reject traversal/symlinks/non-regular files, and expose only
  redacted display paths;
- if safe discovery is unnecessary for contract conformance, omit the capability
  and return the safe empty result.

## 7. Event normalization requirements

The adapter must preserve the T0 closed event vocabulary, provenance, confidence,
session binding, ordering, and bounds.

Candidate mappings require version-native structural evidence:

- top-level `user` with ordinary human content → `user_message`;
- `assistant` text → `assistant_message`;
- structured assistant `thinking` → `thinking` only if the retained fixture
  establishes the field and sensitive thinking text is omitted/redacted;
- structured `tool_use` → `tool_call_started` with safe identity metadata only;
- structured `tool_result` → `tool_call_finished`, without storing output,
  command, or prompt content;
- unknown/attachment/history-only records → bounded `unknown` unless a reviewed
  mapping exists.

Requirements:

- provider-native field names stay inside the version-specific package;
- user prompts, assistant private text, thinking text, commands, tool inputs,
  tool outputs, signatures, paths, tokens, and account/model secrets are not
  copied into common event text, metadata, diagnostics, or fixtures;
- unknown fields remain forward-compatible;
- unknown discriminators, partial records, malformed JSON, missing timestamps,
  truncated records, and oversized records cannot panic or fabricate a typed
  event;
- every emitted event passes `contract.ValidateEvent`;
- mechanical source and evidence provenance remain distinct;
- native-log or provider-hook provenance is assigned only when the input source
  actually establishes it.

## 8. Version, ordering, cursor, and bounds

Reuse accepted T1 lessons, not its provider-specific code.

- define and document one input policy; do not mix full-prefix and incremental
  window assumptions;
- version authority comes from exact current-source evidence, never an unsigned
  cursor boolean;
- conflicting later version/session headers fail closed persistently rather than
  becoming typed again after pagination;
- stable IDs and `Seq` are independent of timestamp ordering and page size;
- one-shot and `MaxEvents=1/2/default` reads return identical ordered
  `(ID, Seq, Type)` tuples;
- cursor is opaque, deterministic, bounded, and bound to the expected source
  position/anchor;
- malformed/tampered/lost/rotated cursor state returns zero events plus degraded;
- caller limits resume without omission or replay;
- count and byte bounds apply before parsing, hashing, or storing rejected input;
- oversized records are rejected before JSON parse, full-payload hash, text
  extraction, or anchor authority;
- processing cost and memory remain bounded independently of total history
  length;
- deduplication semantics must not change with page size.

Required cursor regressions include equal timestamps, out-of-order timestamps,
missing timestamps, caller-limit pagination, deep prefixes beyond
`MaxBatchRecords`, byte-bound continuation, duplicates across page boundaries,
stream rotation, position/anchor mismatch, and valid records surrounding an
oversized record.

## 9. Approval authority — do not inherit the historical false positive

The historical parser maps `permission-mode` to `approval_requested`. Do not
copy that behavior without stronger evidence.

`permissionMode: "ask"` or a `permission-mode` record describes configuration
or mode and is not, by itself, proof that a specific approval is currently
pending. Likewise, approval-like assistant text, tool-use text, prompts, screen
content, PTY structure, heuristic evidence, and missing IDs cannot establish an
approval.

A positive approval mapping requires all of:

- structured version-native request evidence;
- authoritative T0 provenance (`provider_hook`, provider protocol, runtime, or
  accepted native log);
- stable non-empty provider identity suitable for `ApprovalID` (for example a
  controlled fixture's actual permission/tool request ID, not a batch hash);
- exact Pokit session binding;
- a corresponding resolved event identity when the provider exposes resolution;
- positive fixture plus adversarial near-miss negatives.

R1's committed `UserPromptSubmit` fixture is not approval evidence. If a legally
usable, controlled, redacted `PermissionRequest`/permission-result fixture with
stable identity is not available, do not invent one or weaken
`SafeApprovalGate`. Stop and report the evidence/contract-harness mismatch for
independent review.

The fixed T0 harness requires positive approval evidence when the descriptor
advertises approval detection. Capability declaration, fixture evidence, and
actual behavior must agree.

## 10. Status authority

- delegate precedence and advisory downgrade to `contract.ResolveStatus`;
- advisory/heuristic/prompt/PTY evidence cannot assert terminal status;
- no Claude text pattern may become process lifecycle authority;
- empty evidence returns bounded unknown/degraded;
- status failure must not affect Live Terminal, Recorder, or daemon lifecycle.

## 11. Fixed harness and mandatory tests

The production Claude adapter test must invoke, unchanged:

```go
contract.RunAgentContract(...)
```

Supply real redacted Claude-format fixtures for:

- positive process detection;
- exact-version known events;
- distinct valid records;
- individually valid sized records whose aggregate exceeds `MaxBatchBytes`;
- malformed, partial, unknown-version, and unknown-discriminator records;
- failing-adapter isolation;
- approval positive and adversarial near-miss evidence only if justified as
  described above.

Mandatory adapter-specific tests:

- exact `2.1.202` version acceptance and newer/missing/non-string/conflicting
  version rejection;
- no-meta/no-version stream remains unknown/degraded;
- session JSONL and 2.1.206 hook shapes are not silently conflated;
- one-shot/paged tuple equality across caller limits;
- strict cursor tamper/loss/rotation failure;
- source-position ordering independent of timestamp;
- malformed and oversized mixed batches preserve neighboring valid events;
- no prompt/command/tool input/tool result/thinking/signature/path leakage;
- `permission-mode=ask`, approval-like text, tool-use alone, missing ID,
  advisory provenance, wrong session, and low confidence produce no approval;
- retained historical Claude parser/detector tests remain green;
- accepted Codex fixed-harness and regression tests remain unchanged and green.

No test may skip because a fixture, capability, or provider field is inconvenient.
Do not edit the fixed harness or accepted T0 tests.

## 12. Scope exclusions

Do not begin or modify during T2:

- R2 Gemini/OpenCode/Cline/Aider/OpenHands research;
- D1 Adapter Doctor/Repair or coding-agent patch generation;
- T3 Transcript storage/projection/mobile integration;
- S1 status product UI, A1 approval UX/actions, or O1 orchestration;
- Claude settings installation, global/project hook mutation, or automatic hook
  activation;
- production adapter registration or public `/api/sessions` DTO migration;
- Terminal, Recorder, PTY, authentication, lifecycle, tickets, audit, mobile,
  Android, or iOS behavior;
- Windows runtime or speculative Windows abstractions.

Windows remains deferred. Shared protocol and mobile authentication contracts
must remain OS-neutral, but T2 does not create HostKeyProvider, PTYRuntime,
LocalIPCTransport, ConPTY, CNG/TPM, DPAPI, Named Pipes, Windows Services, or NTFS
ACL work.

## 13. Suggested execution order

1. Recover the exact remote baseline and verify accepted T1/T0 ancestry.
2. Audit retained Claude fixture provenance and write the supported-version,
   source, correlation, and approval decision before code.
3. Add one isolated version-specific Claude `contract.AgentAdapter` package.
4. Implement strict version/source gating and bounded position/cursor behavior.
5. Add safe event mappings and explicit unknown/degraded fallbacks.
6. Add approval behavior only from accepted structured evidence; otherwise stop
   and report the evidence gap rather than fabricating a passing fixture.
7. Invoke the fixed T0 harness with provider-positive and adversarial fixtures.
8. Run retained Claude, accepted Codex, and full agent regressions repeatedly.
9. Write `docs/T2_CLAUDE_ADAPTER_IMPLEMENTATION_REPORT.md` with exact support
   matrix, six-op mapping, fixture provenance, capability decisions, bounds,
   known limitations, gate evidence, and REVIEW REQUEST.
10. Commit and push the focused T2 implementation, leave the worktree clean,
    and stop for independent verification.

Do not start R2 automatically after local T2 completion. R2 begins only after a
fresh verifier independently accepts T2.

## 14. Required gates

Run from a clean tree at the final commit:

```sh
gofmt on every changed Go file
cd companion-daemon && go test -race ./internal/agent/... -count=1
cd companion-daemon && go build ./... && go vet ./... && go test -race ./...
cd mobile && npm run typecheck && npm test -- --runInBand
cd .. && sh scripts/build-gate.sh
git diff --check
git status --short
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git merge-base --is-ancestor 162266f830caaf07bf701d9a1294557432855769 HEAD
```

The final two commit hashes must match and the ancestry command must exit 0.
Do not claim a physical-device or external-provider gate that was not run.

## 15. Required implementation report and review request

The final report must include:

```text
REVIEW REQUEST: T2 Claude Version-Specific Adapter
baseline: 162266f830caaf07bf701d9a1294557432855769
tip: <full SHA>
scope: T2 only
supported Claude version/shape: <exact declaration>
source: <session JSONL / hook, exact evidence>
correlation claims: <proven / unavailable and why>
approval evidence: <exact structured source and stable ID, or explicit blocker>
cursor/input policy: <exact declaration>
fixed T0 harness: <count, pass, zero skips>
retained Codex/Claude regressions: <commands/results>
full gate: <exact commands/results>
deferred: R2, D1, T3, S1, A1, O1, managed hooks, product integration, Windows
```

The report must distinguish observed behavior from inference and list every new
fixture with provider/version/source provenance and redaction method.

Push to `feature/phase10-multi-adapter`, verify remote tip equals local HEAD,
leave the worktree clean, and stop. Independent verification must inspect actual
Claude-native mappings, exact version gating, cursor/resource bounds, approval
authority, secret redaction, fixed-harness results, and unchanged Codex behavior.

Only after independent T2 ACCEPT may a new R2 execution handoff be created from
`docs/R2_MULTI_AGENT_EXPANSION_RESEARCH_PLAN.md`.
