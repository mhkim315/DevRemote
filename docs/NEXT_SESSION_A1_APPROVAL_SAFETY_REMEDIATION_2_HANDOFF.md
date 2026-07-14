# Next Session Handoff — A1 Approval Safety Remediation 2

Status: **READY — R2-A THROUGH R2-E ONLY**

This handoff continues A1 after independent rejection of the B1-B8 remediation.
Preserve the accepted safety foundation; do not redesign A1 or begin N1/O1/O2.

## 0. Repository identity and stop condition

```text
repository: https://github.com/mhkim315/DevRemote.git
local path: /Users/mhk/Documents/codex/DevRemote
branch: feature/phase10-multi-adapter
reviewed report HEAD: 0ae85db89a60f1925dbbbcc2997b99fbf0bf5646
reviewed implementation: ed466094cd7a38d148d845d7057c63fe2019e1c9
accepted S1.1 ancestor: 02c8385e3270fbbc4df45e0c71ccad6ebe11a076
```

Before editing, run `pwd`, `git remote -v`, fetch the canonical branch, fast-
forward only, verify local/remote equality and accepted-S1.1 ancestry, and require
a clean worktree. Never work from `/Users/mhk/Documents/codex` as if it were the
repository; the repository root is the `DevRemote` child shown above. Do not
rewrite reviewed history or force-push.

Read in order:

1. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
2. `docs/A1_APPROVAL_SAFETY_PLAN.md`;
3. `docs/A1_APPROVAL_SAFETY_REVERIFICATION.md`;
4. this handoff;
5. the B1-B8 remediation report and touched code/tests.

This is authority and concurrency work. Sections 5 through 8 of
`docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md` are mandatory. Before editing
production code, write the bounded contract note and binding table required by
that protocol. Do not start R2-A until the note explicitly covers claim,
idempotency, delivery receipt, invalidation, capacity, retry, and restart.

## 1. Preserve without redesign

- `waiting_approval` and all heuristic/PTY/prompt/status evidence are display-only;
- only accepted correlated capability + `DetectApproval` + `SafeApprovalGate`
  evidence enters the store;
- no blind Y/N, raw prompt/payload or generic command queue;
- no client-supplied identity/permission/runtime authority;
- no legacy mobile transport fallback;
- no actionable production option until an exact provider mapping and delivery
  channel are proven;
- no public claim token, ActionDigest source material or delivery payload.

## 2. Sequential remediation packets

### R2-A — atomic canonical claim

Make the store, not the HTTP handler, the complete authority boundary. Bind an
immutable canonical selected action/input representation to the record or pass
canonical material that the store verifies under the same claim lock. Recompute
and exactly compare ActionDigest there. Always check the record's stored
permission; remove caller permission override. Require a bounded, non-empty,
canonical idempotency key. Preserve crypto-random opaque claim ownership.

Negative controls must show that arbitrary non-empty digests, weaker permission
requests and empty/malformed keys are rejected even when calling the store
directly.

### R2-B — approval-bound idempotency and bounded retry

Bind each idempotency record to ApprovalID, SessionID, RuntimeRef, ActionDigest,
requester authorization context and accepted receipt identity. Same key+same
digest means `already_accepted` only for the exact same approval and binding;
reuse against another approval is a conflict. Make capacity fail closed or prove
a safe deterministic eviction policy. Implement the frozen bounded manual retry
semantics explicitly, including ownership/lease/restart behavior, without
automatic retransmission of non-idempotent input.

### R2-C — full receipt binding and runtime linearization

Add RuntimeRef to the immutable receipt and exactly verify every receipt field:
claim ownership, ApprovalID, SessionID, adapter/provider/version, launch and
applicable stream generation, ActionDigest and idempotency key. Incomplete or
mismatched receipts never commit.

Linearize replacement/correlation loss/delete/unlink/termination against
executing claims and receipt commit. A runtime change after the handler's
pre-delivery check must still prevent stale success. Do not hold a mutex across
external delivery I/O; use the existing generations/high-water and the bound
receipt. Add deterministic barrier tests and race tests, not sleep-based tests.

### R2-D — mobile byte boundary and log safety

Make mobile limits use UTF-8 byte counts and remain byte-compatible with backend
DTO bounds. Apply closed safe identifier grammar or bounded diagnostic
sanitization/hashing before approval identifiers/actions reach logs. Add
multibyte-overflow, newline/control, absolute-path and token-like negative tests.
Do not expose additional public fields.

### R2-E — positive provider evidence or honest blocked result

Investigate only the already accepted provider/version approval evidence. A1 can
be accepted only with a controlled, redacted, redistributable fixture proving an
exact provider option/action mapping and a production-wired provider-specific
delivery boundary that returns a fully bound receipt. Do not synthesize Y/N or
infer a command from prompt/log/screen text.

If no such public/controlled evidence and channel are available, keep every
production approval non-actionable and report A1 as blocked. Do not substitute a
test-only actionable fixture and do not claim A1 complete. Stop; do not expand to
a new adapter, provider research milestone, CLI redesign or O1 protocol.

## 3. Acceptance gate

Run focused store/handler/delivery tests under `-race`, all A1 ingestion/auth/DTO
tests, T0/T1/T2/S1/S1.1 regression tests, backend build/vet/full race, mobile
TypeScript/full Jest, Android/native when the environment contains the checked
tooling, invariant/secret scans and `git diff --check`. Record environmental
skips as skips, never passes.

The final report must map every R2 finding to code and a non-vacuous negative
test, state whether the positive provider path is proven or blocked, provide one
REVIEW REQUEST marker for the exact implementation SHA, and confirm canonical
remote equality, accepted-S1.1 ancestry and a clean worktree.

Stop for independent A1 re-verification. N1 remains blocked.

## 4. Explicit exclusions

No N1 notifications, Task/Dispatch, worker acknowledgement/completion,
Executor-Verifier, automatic approval policy, generic terminal command execution,
CLI redesign, ConPTY/Windows, cloud relay, lock-screen action, O1 or O2 work.
