# Next Session Handoff — T0 Common AgentEvent Contract

Status: **READY FOR A FRESH EXECUTION AGENT**

This document is authoritative for the next implementation session. The next
agent must work from the remote commit named below, not from an assumed local
checkout or a pasted conversation summary.

## 1. Repository and baseline recovery

Canonical repository:

```text
https://github.com/mhkim315/DevRemote.git
branch: feature/phase10-multi-adapter
accepted M3b ancestor: 9cdf2f290299f70d0b6d58e139771bfc336adb57
```

Start with these commands:

```sh
git remote set-url origin https://github.com/mhkim315/DevRemote.git
git fetch origin feature/phase10-multi-adapter
git switch feature/phase10-multi-adapter
git merge --ff-only origin/feature/phase10-multi-adapter
git status --short
git log --oneline -12
git merge-base --is-ancestor 9cdf2f290299f70d0b6d58e139771bfc336adb57 HEAD
```

The final command must exit 0 and the worktree must be clean before editing. If
this handoff commit is not visible locally, do not reconstruct it from memory and
do not reset/force-push. Read it directly from the remote after fetching:

```sh
git show origin/feature/phase10-multi-adapter:docs/NEXT_SESSION_T0_COMMON_AGENT_EVENT_HANDOFF.md
```

Preserve user changes if the worktree is not clean. Stop and report an actual
overlap instead of discarding them.

## 2. Read before implementation

Read these in order:

1. `docs/M3B_FINAL_ACCEPTANCE.md`
2. `docs/ROADMAP_AFTER_E10B.md` (`T0-D1-T3` section)
3. `docs/MOBILE_SESSION_LIFECYCLE_AND_TRANSCRIPT_PLAN.md` (`T/D phases`)
4. `docs/AGENT_ADAPTER_LAYER_PLAN.md` (stable contract, common model, failure isolation)
5. `docs/R1_RUNTIME_SIGNAL_MATRIX.md`
6. `docs/R1_RUNTIME_SIGNAL_EVIDENCE_MANIFEST.md`
7. `docs/ADAPTER_DOCTOR_REPAIR_PLAN.md` (future constraints only; do not implement D1)
8. `docs/T0_TRANSCRIPT_CONTRACT_RESET_PLAN.md` only as historical T3 safety input

Important correction: `T0_TRANSCRIPT_CONTRACT_RESET_PLAN.md` is not the active
T0 execution plan. T0 now freezes the provider-neutral AgentEvent model and the
six-operation adapter boundary. Transcript projection begins at T3.

## 3. T0 objective

Freeze a small, operating-system-neutral contract that T1 Codex and T2 Claude
can implement independently without changing mobile, lifecycle, authentication,
Terminal, approval, or Transcript contracts.

Pokit owns exactly these stable operations:

```text
detect
discoverSessions
readEvents
normalizeEvent
detectApproval
getStatus
```

T0 must provide the common types, interfaces, validation/default behavior, and a
fixed external conformance harness. It must not implement Codex or Claude parsing.

## 4. Required design decisions

Before editing production code, audit the existing agent-related packages and
tests. Locate current detector/parser/event/status/approval types and every
composition-root consumer. Record which pieces can be retained, wrapped, or
deprecated. Do not create a parallel model without a migration map.

The T0 contract must define at least:

- stable agent identity and provider/version declaration;
- provider-neutral AgentEvent with immutable ID, session correlation, ordering,
  timestamp, event kind, provenance/source, confidence, and bounded metadata;
- a closed common event vocabulary plus a safe unknown/degraded representation;
- bounded incremental reads with an explicit opaque cursor;
- discovery results that never invent terminal ownership or cross-link sessions;
- normalization rules that cannot leak provider-native fields into public DTOs;
- approval evidence separated from ordinary events, with a default of no
  approval on ambiguity;
- status evidence and precedence without deriving process lifecycle from agent
  status;
- typed error/degraded results that cannot break Live Terminal;
- version/capability declarations needed later by Adapter Doctor/Repair.

Preserve the distinction:

```text
process lifecycle: starting/running/stopping/exited/killed/failed
agent status:       thinking/working/waiting/etc.
terminal stream:    raw PTY bytes
AgentEvent:         provider-neutral structured evidence
Transcript:         later T3 readable projection
```

## 5. Safety invariants

- Recorder remains the only PTY reader; raw subscriber/bootstrap bytes are
  unchanged.
- Unknown/malformed provider records fail safely and never crash a session.
- No raw terminal input, prompt content, source code, token, key, signature,
  bearer, full home path, or unrestricted JSONL is stored in common diagnostics.
- `detectApproval` cannot emit a positive result from unknown or low-confidence
  evidence. Include adversarial near-miss tests in the fixed harness.
- Common event/status evidence never becomes process lifecycle authority.
- Reads and metadata are bounded; cursors cannot cause unbounded replay.
- Provider failure degrades metadata only; Terminal, auth, revoke, lifecycle,
  and WS-ticket behavior remain operational.
- Shared wire/DTO contracts stay OS-neutral. Windows remains deferred; do not
  introduce ConPTY, CNG/TPM, DPAPI, Named Pipes, Windows Services, NTFS ACL, or
  speculative Windows abstraction work.

## 6. Fixed conformance harness

Build tests owned outside any future version-specific adapter write area. At a
minimum prove:

- all six operations satisfy the stable interface;
- deterministic normalization for valid fixtures;
- unknown event type and unknown fields return bounded safe results;
- malformed/truncated input cannot panic or fabricate an event;
- cursor advancement, duplicate suppression, read bounds, and stable ordering;
- positive and negative session-correlation cases;
- approval positive fixtures and adversarial near-miss negatives;
- status precedence and unknown/degraded fallback;
- diagnostics contain no raw secrets, prompts, terminal input, or unrestricted paths;
- a failing adapter cannot affect another adapter or Live Terminal;
- common DTO snapshots remain stable.

Use synthetic/redacted generic fixtures in T0. Provider-version fixtures belong
to T1/T2 and must not be reverse-engineered into T0.

## 7. Scope boundaries

Do not implement during T0:

- Codex JSONL/path/hook parsing (T1);
- Claude JSONL/path/hook parsing (T2);
- Adapter Doctor/Repair or coding-agent execution (D1);
- Transcript projector/storage/mobile integration (T3);
- rich status UI (S1), approval UI/actions (A1), orchestration (O1);
- new mobile lifecycle/auth UX;
- changes to Recorder/PTy framing or Terminal transport;
- Windows or iOS/Android authentication work.

Do not weaken accepted M3b behavior to accommodate the new model.

## 8. Suggested work order

1. Produce a code-path audit and explicit mapping from current types to T0.
2. Write the common schema/interface and validators in the existing agent layer.
3. Implement safe unknown/degraded defaults and bounded cursor/read semantics.
4. Add the fixed conformance harness using a minimal fixture adapter.
5. Adapt existing generic consumers only where required to compile against the
   stable contract; keep behavior additive and compatibility-preserving.
6. Run focused tests repeatedly, then the full build gate.
7. Write `docs/T0_COMMON_AGENT_EVENT_IMPLEMENTATION_REPORT.md` with changed
   files, contract table, evidence matrix, gates, limitations, and verifier prompt.
8. Commit and push one narrow T0 sequence, then stop for independent verification.

If the audit shows a choice that would materially change public DTOs, approval
semantics, or existing agent behavior, document the alternatives and stop for
direction rather than silently choosing a broader redesign.

## 9. Required gates

Run and report:

```sh
gofmt on changed Go files
go build ./...
go vet ./...
go test -race ./...
cd mobile && npm run typecheck && npm test -- --runInBand
cd .. && sh scripts/build-gate.sh
git diff --check
git status --short
```

If a clean checkout contains only the partial tracked `mobile/android` tree,
run the repository's clean Expo Android prebuild before claiming the native
compile gate. Do not claim an unavailable physical-device gate.

## 10. Completion and review protocol

The final implementation commit/report must explicitly say:

```text
REVIEW REQUEST: T0 Common AgentEvent Contract
baseline: <full SHA including 9cdf2f290... ancestor>
tip: <full SHA>
scope: T0 only
gate result: <exact summary>
known deferred items: <list>
```

Push to `feature/phase10-multi-adapter`, confirm the remote tip, leave a clean
worktree, and stop. Do not start T1 automatically. A fresh verifier should inspect
the production interfaces, fixed harness, unknown/approval safety, and full gate.

After independent ACCEPT, create the T1 Codex handoff from the accepted T0
contract. Do not pre-authorize T1 against an unaccepted interface.
