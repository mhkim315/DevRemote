# Next Executor Handoff — A1.2 C2D Exact Claude Decision Delivery

Status: **C1D ACCEPTED — START C2D-A ONLY — ACTIONABILITY ZERO — C3D PROHIBITED**

This is the authoritative onboarding document for the next execution agent.
Earlier C1D remediation handoffs are historical and must not be used as active
instructions.

## 1. Canonical repository and accepted state

- Remote: `https://github.com/mhkim315/DevRemote.git`
- Branch: `feature/phase10-multi-adapter`
- Canonical checkout: `/Users/mhk/Documents/codex/DevRemote`
- Accepted C0D evidence: `e42d4c570e64462ce813861017cc635e338e68bf`
- Accepted SP1/Codex baseline: `2b940a6fce6e878ffa0da17b5df4d39438af144d`
- Accepted C1D implementation: `4794ce7f42380c388c1a5614b4b2518bc1722870`
- Accepted C1D report HEAD: `e9e661c550c0a78f8f6544f8db911bea9fd5cac1`
- This handoff commit must be the fetched remote HEAD before work starts.

Do not use an Antigravity, scratch, temporary, or duplicate checkout. Run:

```sh
cd /Users/mhk/Documents/codex/DevRemote
git remote -v
git fetch origin feature/phase10-multi-adapter
git switch feature/phase10-multi-adapter
git merge --ff-only origin/feature/phase10-multi-adapter
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git merge-base --is-ancestor e42d4c570e64462ce813861017cc635e338e68bf HEAD
git merge-base --is-ancestor 2b940a6fce6e878ffa0da17b5df4d39438af144d HEAD
git merge-base --is-ancestor 4794ce7f42380c388c1a5614b4b2518bc1722870 HEAD
git merge-base --is-ancestor e9e661c550c0a78f8f6544f8db911bea9fd5cac1 HEAD
git status --short
```

Stop if local and remote differ, an ancestry check fails, or the worktree is not
clean. Never rewrite or force-push history.

## 2. Mandatory reading order

1. `docs/A1_2_CLAUDE_APPROVAL_EXTENSION_PLAN.md`;
2. this handoff;
3. `docs/A1_2_C1D_INDEPENDENT_ACCEPTANCE.md`;
4. `docs/A1_2_C1D_EVIDENCE_REPORT.md`;
5. `docs/A1_2_C0D_R5_DEFER_EVIDENCE_REPORT.md` and its bounded evidence;
6. `docs/SP1_NATIVE_APPROVAL_FINAL_ACCEPTANCE.md`;
7. `companion-daemon/internal/term/managed_claude.go` and its tests;
8. `managed_approval_delivery.go` for the accepted Codex delivery pattern only;
9. `approval_execution.go`, `approval_store_gen.go`, `approval_delivery.go`, and
   `approval_handler.go` for the frozen provider-neutral A1 authority core;
10. production composition in `cmd/devremote/app.go`.

C0H and C0R remain BLOCKED. Do not use `PermissionRequest` or
`--permission-prompt-tool`. Codex `serverRequest/resolved` semantics are not
Claude semantics.

## 3. Frozen boundary

C1D is accepted and must remain observation-only in production:

- `provenActionMapping` remains empty for Claude;
- Claude observations expose zero options and zero delivery material;
- no Claude `ClaimForExecution` is reachable from a handler;
- no Claude delivery is installed in `app.go`;
- no mobile CTA or action route is enabled;
- no production capacity is activated;
- terminal text, PTY, screen, JSONL, prompt text and `waiting_approval` remain
  non-authoritative.

C2D may build and test an uninstalled provider-specific delivery boundary. Only
C3D, after independent C2D acceptance, may atomically activate actionability.

## 4. Mandatory authority note before implementation

The only work immediately authorized is **C2D-A**. Before changing production
code, create and commit `docs/A1_2_C2D_PACKET_CONTRACT_NOTE.md` containing:

- authority owner and canonical stored inputs;
- complete immutable binding table showing where every field is created, stored,
  compared, copied into delivery/receipt, invalidated and tested;
- exact provider state machine and one shared linearization point;
- exact C0D allow and deny consumed-decision witnesses;
- timeout, cancellation, exit, stop/delete, replacement and restart behavior;
- capacity behavior and one adversarial counterexample per critical invariant;
- public/private data classification and safe-review feasibility;
- explicit non-goals.

At minimum the binding table must include:

- A1 ApprovalID and opaque claim token;
- POKIT SessionID;
- `RuntimeRef{Adapter: claude_headless, Version: 2.1.209,
  LaunchGen: epoch, StreamGen: 0}`;
- exact Claude `session_id` and `tool_use_id`;
- certified tool name (`Bash` only in this slice);
- canonical private tool-input bytes and their digest;
- stored option ID (`allow_once` or `deny`);
- delivery schema `claude.pretooluse.decision.v1`;
- A1 ActionDigest, PayloadDigest and idempotency key;
- daemon-generated one-shot resume nonce;
- provider consumption witness identity;
- receipt ID and complete immutable receipt binding.

Do not implement until the note demonstrates that no field is inferred from
ordering, timing, command equality, terminal output or caller-supplied authority.
Commit the note, push it, and stop with:

```text
REVIEW REQUEST: A1.2 C2D-A Contract and Code-Path Audit — <commit SHA>
```

Do not start C2D-B until the verifier approves C2D-A.

## 5. C2D-B — private one-shot resume coordinator

This section is context for the next approved checkpoint, not current authority.

Implement the smallest Claude-specific private coordinator beside the C1D runtime.
It must preserve the exact deferred invocation and serialize:

```text
observed_and_joined
  → decision_reserved                 # one linearization point
  → resume_started
  → repeated_pretooluse_matched
  → decision_written_once
  → allow_witness | deny_witness
  → terminal
```

Cancellation, timeout, exit, stop/delete, runtime replacement and provider-side
resolution must transition to a non-success terminal state. A stale or duplicate
operation must never regain ownership.

Requirements:

- exactly one owner for one runtime/tool-use/claim/resume nonce;
- exact full-binding validation before the repeated hook receives a decision;
- `allow_once → allow` and `deny → deny` only;
- zero decision for mismatched session, epoch, tool-use ID, tool name or input digest;
- no lock held across process spawn, hook IPC or provider I/O;
- no automatic retransmission after an ambiguous write;
- no generic terminal input, command queue or `RuntimeDeliveryGate` acceptance as
  Claude success evidence;
- bounded entries and fail-closed exhaustion;
- daemon restart restores no pending/executing authority.

Use deterministic barriers/channels. The tests must inspect the state immediately
after reservation, during resume, after write and after witness routing. Include a
known-bad check-then-write or duplicate-owner control proving the tests are
non-vacuous.

Commit and stop for independent C2D-B verification. Do not continue to C2D-C in
the same checkpoint.

## 6. C2D-C — uninstalled Claude ApprovalDelivery and consumption routing

> **Current status:** the implementation at
> `cf877aaa64460b1e996d8756464ffb1ce41eb36a` is independently rejected. Before
> any C2D-D work, execute the bounded remediation packet in
> `NEXT_EXECUTOR_A1_2_C2D_C_REMEDIATION_HANDOFF.md`. Its real-write,
> fail-closed cleanup, and non-vacuous A1 composition requirements amend this
> section without changing the frozen C2D authority contract.
>
> The first remediation at `08be1ad3711cb0549e157413ea8ba4cef5798834` is also
> rejected. The current authoritative executor packet is
> `NEXT_EXECUTOR_A1_2_C2D_C_REMEDIATION_4_HANDOFF.md`. Remediation
> `bc7145f529dc205e74a61c5c57bb000192d59e5e` added claim-owned completion but
> still did not prove an accepted+committed allow or deny flow.

After C2D-B approval, implement an uninstalled `ApprovalDelivery` boundary (name
may differ) that:

- accepts only the frozen Claude RuntimeRef, tool shape, options and schema;
- receives the provider-neutral immutable `ApprovalExecutionBinding` without
  trusting a handler to reconstruct provider identity;
- delivers the exact private decision through the C2D-B coordinator;
- returns `accepted` only after a matching Claude-native witness:
  - allow: matching `PostToolUse`/tool result for the same `tool_use_id`;
  - deny: matching `permission_denials` entry for the same `tool_use_id` and input
    digest;
- returns a fully bound receipt with the same claim token, binding, payload digest,
  idempotency key and a fresh bounded ReceiptID;
- reports stale, mismatch, unavailable, conflict, rejected or ambiguous outcomes
  honestly;
- never treats queue admission, response write, process exit, side effects or
  absence of execution as acceptance.

Production composition must still not install this boundary. Controlled
production-composition tests may exercise it with the frozen A1 store and exact
redacted C0D fixtures. Do not substitute a fake provider witness for acceptance.

Required interleavings include duplicate decision, allow-vs-deny, stop/delete,
epoch replacement, timeout, malformed provider output, result before reservation,
result during write, result after write, duplicate/late witness and cross-session
substitution.

Commit and stop for independent C2D-C verification.

## 7. C2D-D — safe review projection and final C2D evidence

After C2D-C approval, freeze the initial `Bash` review projection. It must be:

- bounded and deterministic;
- structurally derived from the certified input, not display text;
- sufficient to distinguish the exact action whose digest is claimed;
- free of secrets, raw provider payload, hook capability and unsafe absolute paths;
- fail-closed when truncation or redaction could hide execution meaning.

If the frozen public A1 DTO cannot safely and unambiguously represent the Bash
action, report **C2D BLOCKED**. Do not weaken the DTO or enable a vague CTA.

C2D final evidence must show the production code remains uninstalled and
non-actionable while controlled composition proves exact allow/deny routing and
all negative interleavings. Update a bounded C2D evidence report, freeze HEAD,
run the final gates, push, and stop with:

```text
REVIEW REQUEST: A1.2 C2D Exact Claude Decision Delivery — <implementation SHA>
```

C3D remains prohibited until independent C2D acceptance.

## 8. Required gates

At each checkpoint:

- `gofmt` and `git diff --check`;
- focused build/vet/race for changed term and command packages;
- repeated deterministic Claude delivery/concurrency tests;
- frozen C1D and Codex SP1 regressions;
- documentation and repository secret scan;
- exact local/remote equality, ancestry and clean worktree.

At the final C2D checkpoint, run the complete repository build gate including
backend race, mobile TypeScript/Jest, Android Kotlin, invariants and secret scan.
Mobile production code must remain unchanged in C2D.

Do not run additional paid/live provider turns merely to validate internal state
machinery. C0D already freezes the native lifecycle. A new bounded live probe is
allowed only if a concrete C2D contract question cannot be resolved from accepted
evidence and production-composition tests; document the reason before running it.

Freeze HEAD before every authoritative gate. If the tree changes afterward, rerun
the proportional gate on the new exact tree.

## 9. Hard exclusions

- no C3D activation or handler installation;
- no mobile changes or CTA;
- no modification of the frozen provider-neutral A1 state machine;
- no Codex semantic changes;
- no generic provider/plugin/hook SDK;
- no Agent SDK, Channels, PermissionRequest or permission-prompt-tool path;
- no terminal prompt parsing, synthetic keys, send-text or side-effect authority;
- no multiple parallel Claude approvals in the first slice;
- no N1, O1/O2, Executor-Verifier, tmux/cmux cleanup, CLI redesign, cloud relay or
  Windows implementation.

If exact identity, one-shot ownership, safe review, or matching consumed-decision
evidence cannot be proven, leave Claude actionability disabled, report BLOCKED and
stop. Never replace missing production authority with heuristics or fixture-only
success.
