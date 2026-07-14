# Next Session Handoff — A1.1 Codex Provider-Positive Path

Status: **INDEPENDENT PLAN ACCEPT — EXECUTE CP0 ONLY; CAPACITY MUST REMAIN ZERO**

Date: 2026-07-14

## 1. Canonical repository identity

Use only this checkout and branch:

```text
repository: /Users/mhk/Documents/codex/DevRemote
remote:     https://github.com/mhkim315/DevRemote.git
branch:     feature/phase10-multi-adapter
```

Do not work from a scratch clone, `/tmp` checkout, the stale
`local/a1-codex-positive-plan` branch, or a similarly named parent directory.
That local branch contains the historical planning source only; this handoff and
the plan on the canonical branch supersede it.

At the start of every turn:

```sh
cd /Users/mhk/Documents/codex/DevRemote
git fetch origin feature/phase10-multi-adapter
git status --short --branch
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git merge-base --is-ancestor 997a697b55b421aa8590aa05a672c604762fd0d2 HEAD
```

Stop if local HEAD differs from the remote branch, ancestry fails, or the worktree
contains changes that were not created by the active task. Never force-push or
rewrite the accepted history.

## 2. Accepted and blocked state

- S1.1 accepted ancestor: `02c8385e3270fbbc4df45e0c71ccad6ebe11a076`.
- Frozen provider-neutral A1 implementation: `2e70512345718bef47e83a08b1ac8af5a6294aaf`.
- R11 evidence remediation: `d5a965cad4d9b499a5c3bc132be2aecd64f91e86`.
- Independently reviewed R11 report HEAD: `997a697b55b421aa8590aa05a672c604762fd0d2`.
- R11 is ACCEPT. The provider-neutral safety core is frozen.
- The A1 product milestone is still BLOCKED because no real provider-positive path
  exists.
- N1 is BLOCKED until independent A1.1 ACCEPT.

Production must remain non-actionable while A1.1 is incomplete:

- `provenActionMapping` remains empty;
- production approval delivery capacity remains zero;
- no terminal, PTY, prompt, screen or `waiting_approval` signal creates a CTA;
- no generic text/key delivery substitutes for a provider response.

## 3. Mandatory read order

Read these documents before proposing or changing code:

1. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
2. `docs/A1_APPROVAL_SAFETY_PLAN.md`;
3. `docs/A1_APPROVAL_SAFETY_REMEDIATION_11_REPORT.md`;
4. `docs/A1_APPROVAL_SAFETY_REVERIFICATION_11.md`;
5. `docs/A1_CODEX_PROVIDER_POSITIVE_PATH_PLAN.md`;
6. `docs/A1_1_PLAN_INDEPENDENT_VERIFICATION.md`;
7. `docs/ROADMAP_AFTER_E10B.md`;
8. this handoff.

Then inspect the current production boundaries named in the plan rather than
relying on earlier remediation summaries.

## 4. Immediate next action

Independent A1.1 plan verification is recorded in
`docs/A1_1_PLAN_INDEPENDENT_VERIFICATION.md`. The executor is authorized to perform
**CP0 only** after syncing the canonical commit containing that verdict.

CP0 begins with the required contract note and filled binding table, then the exact-
version redacted evidence spike. CP0 changes no production capacity and does not
authorize CP1. Stop BLOCKED if any CP0 hard gate cannot be proven.

## 5. Frozen authority boundary

The provider-neutral A1 core remains the sole owner of:

- authenticated requester and permission derivation;
- ApprovalID and current SessionID;
- LaunchGeneration and StreamGeneration binding;
- canonical selected-action digest and idempotency;
- atomic claim ownership;
- expiry, supersession and retry policy;
- delivery receipt comparison and final commit;
- bounded public/mobile DTO policy.

Codex-specific code may only:

- receive one structured native request from a certified runtime;
- bind it to the exact current RuntimeRef and connection epoch;
- retain a bounded provider-native request identity;
- produce a bounded redacted summary and fixed supported options;
- deliver the exact native allow/deny response on the owning provider channel;
- prove that the same provider invocation consumed or resolved that response.

It must not mint requester authority, weaken generation checks, bypass the store,
or report success from queue admission or process-byte writes.

## 6. Pre-implementation contract note

Before each authorized packet, follow
`docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md` and write a short contract note that
contains:

- authority owner;
- canonical stored inputs;
- complete immutable provider and A1 binding;
- state transitions and one shared linearization point;
- exact success evidence;
- invalidation, restart and capacity behavior;
- one adversarial counterexample for every critical invariant;
- explicit non-goals;
- a binding table showing where every field is created, stored, checked, copied,
  invalidated and tested.

Design deterministic failing interleavings before concurrency implementation. Use
barriers/channels, inspect contested intermediate state and include a non-vacuous
known-bad control. Do not use sleeps as race evidence.

## 7. A1.1 packet order

Execute only in this order after plan acceptance:

```text
CP0 evidence freeze and production-path spike
→ CP1 managed app-server runtime, capacity zero
→ CP2 native request registry and safe ingestion, capacity zero
→ CP3 delivery/resolution bridge, capacity zero
→ CP4 authenticated product integration, capacity zero
→ CP5 exact-tuple certification and focused activation
```

Each packet receives a focused checkpoint commit only after its focused tests pass.
Intermediate checkpoints are not milestone acceptance requests. CP5 and the final
review request cover the complete A1.1 contract.

## 8. CP0 hard stop rules

CP0 changes no production capacity. It must use a real, exact supported Codex
binary and pinned schema/source evidence to prove all of the following:

- local stdio app-server initialization and lifecycle;
- TOCTOU-hardened binary identity (regular file, not symlink; digest bound to
  device+inode; verified artifact == spawned artifact) and a schema bundle generated
  WITHOUT `--experimental` with per-file digests + a deterministic manifest digest
  (plan §4.1). Adjacent path re-verification is not sufficient; the actual process
  image must be bound to the verified digest;
- stable native request ID plus thread/turn/item identity;
- exact command-approval request shape for the initial supported request, including
  the optional-field freeze (`approvalId == null`, `environmentId == null`/absent;
  no network/policy amendment or unsupported semantic fields; `commandActions` is
  display/corroboration only) (plan §6);
- allow-once and deny response shape;
- ordering and meaning of matching `serverRequest/resolved`;
- provider cancellation, timeout, duplicate response and child cleanup behavior;
- a bounded real product path that starts/resumes a thread and starts a turn without
  creating a terminal replacement or generic Task/Dispatch API.

Treat app-server as shipped-but-experimental and `0.144.1`-only; do not assume any
other version is compatible.

Stop BLOCKED if identity, response consumption, cleanup or the bounded user path
cannot be proven. Do not weaken the acceptance criteria, fabricate a nonce where a
provider identity is required, use a fixture as production proof, or enable a CTA.

Fixtures must be redacted, reproducible and record binary/version/schema/source
provenance. They must contain no prompt, secret, token, personal path or private
repository data.

## 9. Initial certified scope

The only candidate tuple is the one frozen by the plan:

```text
provider:          codex
provider version:  0.144.1 exactly (shipped-but-experimental app-server; no other version)
transport:         local app-server v2 stdio JSONL
profile:           explicit managed codex_app_server profile
binary identity:   regular-file digest bound to device+inode; verified == spawned
schema identity:   per-file digests + deterministic manifest digest (no --experimental)
request:           normal commandExecution requestApproval (approvalId == null)
actions:           allow_once and deny
```

Delivery routing (plan §3.1) is frozen before CP3: the default `ApprovalDelivery` is
always unavailable; only the exact server-derived certified runtime tuple routes to the
Codex bridge; every other runtime/provider stays capacity zero; bridge register/replace/
delete is atomic with runtime supersession.

Unsupported versions, WebSocket, PermissionRequest hooks, attached TUI sessions,
ordinary `pokit run codex`, experimental fields, file/network/MCP/session-wide
approvals and unknown actions remain non-actionable.

Production capacity stays zero through CP4. CP5 activation must be a separate
focused change limited to the independently certified tuple.

## 10. Scope exclusions

Do not begin or redesign:

- N1 notifications;
- A1.2 Claude implementation;
- generic provider/plugin SDK;
- T0/T1/T2 or the frozen A1 core;
- generic `pokit run` or CLI redesign;
- Task, Dispatch, worker acknowledgement/completion, O1 or O2;
- terminal UI, PTY prompt parsing, generic send-text/send-key approval;
- automatic approval policy;
- cloud relay, WebSocket app-server, ConPTY, Windows or SSH expansion.

## 11. Optional A1.2 boundary

A1.2 is a possible later **Claude Approval Extension**, not part of A1.1 and not
automatically an N1 prerequisite. Do not extract a generic interface merely because
Claude may be added later.

A future A1.2 review must independently prove exact Claude hook invocation identity,
one-response ownership, timeout/cancellation and evidence that the exact invocation
consumed the response. If it cannot, Claude remains non-actionable without changing
the frozen A1 core.

## 12. Gate and disk discipline

Run only focused checks during a packet. Run the full repository gate once on the
frozen final implementation tree and again only if a later commit changes that tree.
Do not run parallel full gates or leave background test processes alive.

Use dedicated caches under `/tmp`, record their paths and remove only caches created
for this work after verification. Check for runaway `go`, `node`, `jest`,
`gradle` or daemon processes before handoff. Never delete user-owned caches or files
without explicit authorization.

Before commit, re-read the plan and produce an invariant-by-invariant self-audit.
Repeat it before push only if the tree changed after the audit. Freeze HEAD before
the authoritative final gate and push that exact verified tree.

## 13. Review and stop condition

The plan-review result is recorded as ACCEPT. Each implementation packet must clearly
label behavior as production-wired, test-only, unavailable, skipped or blocked. A
green test suite is not proof of provider consumption.

The final A1.1 review request may be made only after real production allow-once and
deny paths pass end to end and capacity activation is confined to the exact certified
tuple. Until then report A1.1, A1 and N1 as BLOCKED and stop at the packet boundary.
