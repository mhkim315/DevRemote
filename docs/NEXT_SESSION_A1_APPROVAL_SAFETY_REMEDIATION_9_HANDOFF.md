# Next Session Handoff — A1 Approval Safety Remediation 9

Status: **READY — R9-A THEN R9-B ONLY; A1/PROVIDER/N1 BLOCKED**

Risk class: **authority canonicalization + bounded state + deterministic concurrency**

This handoff is written for a fresh execution agent. It is intentionally detailed
because prior rounds repeatedly passed direct unit tests while omitting the named
production path or contested intermediate state.

## 0. Canonical repository onboarding

```text
repository: https://github.com/mhkim315/DevRemote.git
required local root: /Users/mhk/Documents/codex/DevRemote
branch: feature/phase10-multi-adapter
authoritative handoff commit: populated by the commit containing this document
reviewed report HEAD: ebdc507134b022e17f73ff987541e3cc60efb225
reviewed implementation: 8ec3630ea
accepted S1.1 ancestor: 02c8385e3270fbbc4df45e0c71ccad6ebe11a076
```

Do not work from `/Users/mhk/Documents/codex` as though it were the repository.
`DevRemote` is the repository root.

Before editing, run and report:

```bash
pwd
git remote -v
git branch --show-current
git fetch origin feature/phase10-multi-adapter
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git merge-base --is-ancestor 02c8385e3270fbbc4df45e0c71ccad6ebe11a076 HEAD
git merge-base --is-ancestor 8ec3630ea HEAD
git status --short
```

The local and remote full SHAs must match and the worktree must be clean. Fetch and
fast-forward only. Never reset-hard, rewrite reviewed history, discard another
agent's work or force-push. If the branch has advanced, read the intervening commits
before editing and preserve them.

Read in this exact order:

1. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
2. `docs/A1_APPROVAL_SAFETY_PLAN.md`;
3. `docs/A1_APPROVAL_SAFETY_REVERIFICATION_8.md`;
4. this handoff;
5. remediation 8 contract note/report;
6. `companion-daemon/internal/mux/id_parser.go`;
7. `companion-daemon/internal/term/approval_execution.go`;
8. `companion-daemon/internal/term/approval_store_gen.go`;
9. `companion-daemon/internal/term/approval_delivery.go`;
10. `companion-daemon/internal/term/telemetry_service.go` and the relevant
    production tests.

Before production edits, write a short remediation-9 contract note. Include the
authority owner, canonical grammar and exact byte bound for every retained field,
a binding table, exact versus conservative accounting, capacity failure behavior,
the contested interleaving/linearization point, one counterexample per blocker and
explicit non-goals. Stop extending the note once those rows are complete.

## 1. Scope and allowed files

Allowed production scope:

- `companion-daemon/internal/term/approval_delivery.go`;
- the smallest necessary existing canonical identifier helper in
  `companion-daemon/internal/mux/` only if the exported functions are insufficient;
- `companion-daemon/internal/term/telemetry_service.go` only if a narrow test seam
  is strictly necessary for deterministic proof.

Allowed tests/documents are focused `internal/term` delivery/telemetry tests, one
compact contract note and the final report.

Do not modify the provider-neutral claim/store/handler contract unless a reproduced
counterexample proves a direct dependency and the expansion is reported first. Do
not touch T0/T1/T2 adapters, mobile DTOs, authentication, Transcript, Recorder/PTy
ownership or lifecycle semantics.

## 2. Preserve already-correct behavior

- server-derived requester context and immutable claim binding;
- payload digest checked before append;
- ActionDigest/PayloadDigest = 64 lowercase hex;
- ClaimToken = 32 lowercase hex;
- canonical idempotency key;
- invalid capacity fails with no endpoint publication;
- captured generation handles and typed defensive items;
- replacement retains accepted old-generation items;
- active or retired-nonempty endpoints are never evicted;
- same SessionID/RuntimeRef/capacity activation is idempotent;
- entropy/capacity exhaustion fail before mutation;
- no external I/O while holding the gate mutex;
- production capacity zero, provider mapping empty, actions hidden.

## 3. Ordered task packets

Complete, test and checkpoint-commit R9-A before starting R9-B.

### R9-A — finish canonical metadata and accounting

Required call graph:

```text
RuntimeDeliveryGate.Activate
  → validate endpoint identity/capacity
  → publish or fail without mutation

RuntimeDeliveryGate.Accept
  → validGateBindingMeta
  → endpoint/runtime equality
  → payload digest and resource admission
  → append typed item + receipt
```

Required implementation:

1. Use `mux.ParseSessionID`, exact canonical round trip,
   `SessionRef.Validate`, and `mux.ValidateAdapterName` for SessionID.
2. Validate Runtime.Adapter with the existing accepted identifier grammar.
3. Validate Runtime.Version with one bounded ASCII grammar such as
   `[A-Za-z0-9][A-Za-z0-9._-]{0,63}`; reject slash, backslash, control,
   whitespace and traversal-shaped values.
4. Keep ApprovalID's existing store bound; do not invent provider-specific rules.
5. Separate exact retained variable bytes, a named conservative fixed charge and
   charged item bytes. Do not call an estimate exact. Use checked addition or prove
   the bounded sum cannot overflow.
6. Preserve canonical digest/token/key validation and reject before entropy,
   receipt creation, append or map/order/accounting mutation.
7. Fix `TestDeliveryGate_ActivateRejectsBadSessionAndRuntime` to pass `x.session`.

Mandatory tests:

- SessionID, ApprovalID, Runtime.Adapter and Runtime.Version exact/one-over bounds;
- identity grammar/control/space/path/traversal failures;
- canonical/malformed digest, token and key controls;
- combined payload+metadata exact charged-item boundary and one-over;
- aggregate metadata exhaustion using multiple individually valid items;
- every rejection checks unchanged current map, endpoints, order, queue, endpoint
  bytes, global bytes and sequence/receipt state;
- payload aliasing remains impossible.

The aggregate test must reach the global bound. One per-item rejection is not
aggregate evidence.

R9-A checkpoint: run only formatting and focused canonical/bound tests under race,
self-audit every bullet, then commit. Do not run the full repository gate here.

### R9-B — real production path and deterministic contested-state proof

#### B1: real telemetry path

Drive this actual call graph:

```text
accepted controlled_pty session
→ TelemetryService.processSession
→ accepted adapter + managed-launch correlation
→ deliveryGate.Activate
```

Use `TelemetryService.processSession` and existing S1.1 production-test patterns.
Direct `gate.Activate` calls are forbidden as evidence for this test.

Prove repeated same-RuntimeRef polls preserve one handle/current mapping/order;
one real launch or stream generation change makes exactly one replacement; the next
same-generation poll makes none; correlation loss/deletion deactivates acceptance.

#### B2: explicit capacity states

Use separate fresh gates and never ignore `ok`, handle, receipt or drain results:

1. all slots active → extra activation fails with all state unchanged;
2. retired nonempty item at the bound → extra activation before drain fails and the
   exact item/receipt/bytes remain;
3. drain/deactivate one endpoint → deterministic eligible victim, while another
   endpoint/item remains unchanged.

#### B3: deterministic interleaving

Design accept versus activation/reclamation before code. Use barriers/channels or a
narrow nil-in-production hook; no sleeps. Pause at a named contested point and
inspect current mapping, endpoint ownership, queues, item count, sequence and byte
totals before releasing the conflict.

A sequential accept-then-drain test is forbidden. A payload check after drain is not
a negative control. Add a deliberately unsafe helper/fault mode or invariant checker
showing destructive/post-mutation admission makes the test fail. Document lock order
and keep external/provider I/O outside the gate lock.

### R9-C — preserve the provider blocker

Only reconfirm: `provenActionMapping` empty, production capacity zero, no heuristic
CTA/claim/delivery, A1/N1 BLOCKED. No provider research or implementation is
authorized. The unpushed Codex positive-path plan is not part of this remediation.

## 4. Prohibited shortcuts

- no “production” test that directly calls only the gate;
- no ignored operation result;
- no comment claiming an unexecuted branch is exercised elsewhere;
- no sleep-based race, later operation masking earlier mutation, or sequential test
  labeled as contested concurrency;
- no length-only identity called canonical or estimated accounting called exact;
- no fake provider/receipt, terminal Y/N, prompt/screen parsing or scope expansion.

## 5. Verification, commit and stop

Before each implementation commit, re-read this handoff and map every requirement to
exact production code and a non-vacuous test. Label production-wired, direct-unit,
test-only, skipped and blocked evidence separately.

After R9-A and R9-B:

1. run focused delivery/store/telemetry race tests;
2. run accepted T0/T1/T2/S1/S1.1/A1 regressions;
3. freeze HEAD and run the full build gate once;
4. commit the report and run documentation-sensitive checks on that exact report
   HEAD; run another full gate only if repository policy or executable/scanner input
   changes require it;
5. fetch/reconcile without history rewrite, rerun affected audits, push, verify
   local/remote full SHA equality and clean worktree.

Required marker:

```text
REVIEW REQUEST: A1 Approval Safety remediation 9 — <implementation SHA>
```

Stop for independent verification. Do not begin provider-positive work or N1.

## 6. Explicit exclusions

No Codex/Claude path, hook/app-server research, N1, Task/Dispatch, worker protocol,
Executor-Verifier, automatic policy, generic command, CLI redesign, ConPTY/Windows,
cloud relay, lock-screen action, O1 or O2.
