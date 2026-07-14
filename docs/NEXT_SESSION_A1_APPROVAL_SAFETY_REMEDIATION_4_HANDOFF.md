# Next Session Handoff — A1 Approval Safety Remediation 4

Status: **READY — R4-A THROUGH R4-D ONLY; R4-E REMAINS AN HONEST BLOCKER**

Risk class: **authority + concurrency + security-gate integrity**

Preserve accepted A1 remediation work. Fix only the independent
re-verification-3 findings. Do not begin N1/O1/O2 and do not invent the missing
provider capability.

## 0. Canonical repository recovery

```text
repository: https://github.com/mhkim315/DevRemote.git
local path: /Users/mhk/Documents/codex/DevRemote
branch: feature/phase10-multi-adapter
reviewed report HEAD: c34d8ce4c57ae13e952e96c8550fcea333698e38
reviewed implementation: 0ac1acf38
accepted S1.1 ancestor: 02c8385e3270fbbc4df45e0c71ccad6ebe11a076
```

The repository root is the `DevRemote` child, not
`/Users/mhk/Documents/codex`. Before editing, print and report `pwd`, remote URL,
branch, full local/remote SHAs, accepted-S1.1 and reviewed-implementation
ancestry, and worktree status. Fetch and fast-forward only. Never reset, rewrite
reviewed history, discard another agent's work or force-push.

Read in order:

1. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
2. `docs/A1_APPROVAL_SAFETY_PLAN.md`;
3. `docs/A1_APPROVAL_SAFETY_REVERIFICATION_3.md`;
4. this handoff;
5. remediation-3 contract note/report and current production code/tests.

The authority/concurrency contract-note and invariant-audit rules in the task
packet protocol are mandatory. Before code changes, append a short remediation-4
contract note covering complete requester presence, every cleanup owner,
bounded acceptance capacity, queue/drain lock ownership and secret-scan behavior.
Include the two reviewer counterexamples verbatim as failing tests before fixing
them.

## 1. Preserve these accepted properties

- `waiting_approval`, prompt, screen, PTY, process and heuristic evidence remain
  display-only and can never create action authority;
- only accepted correlated capability + `DetectApproval` + `SafeApprovalGate`
  evidence enters the authoritative store;
- production stays non-actionable while provider mapping/delivery is unproven;
- ClaimForExecution recomputes action and payload from stored canonical data;
- every idempotent replay follows permission/runtime/supersession checks and
  exact requester-auth comparison;
- payload digest, claim token, receipt and full execution binding remain exact;
- bounded manual retry, capacity fail-closed, safe DTOs, device transport and log
  redaction remain unchanged;
- no blind Y/N, generic CommandBroker delivery, fake receipt or fixture-based
  production claim.

## 2. Ordered task packets

### R4-A — complete requester presence at the store boundary

The store is the deepest authority boundary. Define the exact mandatory
server-authenticated context and require non-empty DeviceID, HostID,
BearerSessionID and BootID plus the stored permission before initial claim,
manual retry or `already_accepted`. Do not let a handler convention substitute
for store validation and do not add client identity fields.

First add the reviewer's direct-store failing cases for empty HostID and empty
BootID. Add a table test for every individually empty identity field and missing
required permission. Preserve changed-field, mutable-permission-slice,
cross-Approval, restart and stale launch/stream tests.

### R4-B — close every production cleanup path

Fix `TelemetryService.reconcileSessions` so registry disappearance deactivates
the exact generation-owned delivery endpoint in the same cleanup operation that
clears telemetry/status/approval state. Preserve the distinct `Clear` path used
by delete/link/unlink and existing launch/stream/correlation invalidation.

First add the reviewer's production-path counterexample: active endpoint +
tracked session → `reconcileSessions(nil)` → old RuntimeRef must accept zero
bytes. Then prove each owner independently: registry disappearance, explicit
delete, unlink/relink, correlation loss, launch replacement and stream
replacement. Tests must observe the delivery gate/accepted-byte count, not only
approval-store state.

### R4-C — bounded internal acceptance, external drain outside the gate

Do not call an arbitrary `DeliverySink` interface while holding the runtime
transition mutex. Replace or isolate the smallest unsafe boundary with a
production-owned, bounded, non-blocking in-memory endpoint/queue. The under-lock
linearization operation must be fully controlled by the gate; queue-full returns
non-acceptance and writes nothing. Provider/external I/O drains only the captured
immutable generation endpoint after acceptance and cannot hold the transition
gate.

Keep the exact payload and generation binding. Use deterministic barriers, not
sleeps, to prove:

```text
accept reservation/enqueue contests replacement
queue full contests acceptance
external drain blocks while replacement still completes
old generation cannot enqueue after replacement/deactivation
```

Include a known-bad/bypass negative control. Do not register a production
provider sink or claim a positive provider path in this packet.

### R4-D — restore secret-scan integrity

Remove `grep -v "Risk-proportional"` from `scripts/build-gate.sh`. Resolve the
heading substring false positive without a content-based scan bypass. The
smallest safe choice is to reword that active protocol heading while preserving
its meaning; do not rewrite historical acceptance reports.

Add or run a focused negative control showing a secret-shaped value is still
detected and a harmless heading does not fail the gate. Do not broaden exclusions
or hide test fixtures without explicit, narrow justification.

### R4-E — provider capability remains blocked

Reconfirm only from the existing accepted adapters and production mapping. If no
controlled exact provider action mapping and delivery endpoint exists, retain
empty `provenActionMapping`, register no sink, keep every production approval
non-actionable, report A1 **BLOCKED**, and stop. No new adapter research,
synthetic Y/N, prompt parsing or provider implementation is authorized.

## 3. Required completion audit and gate

Before the implementation commit, re-read the plan, re-verification and this
handoff. Map every requester field and cleanup path to production code and a
non-vacuous test. Map gate lock ownership, capacity behavior, acceptance receipt
and external drain. Label every path `production-wired`, `test-only`,
`unavailable`, `skipped` or `blocked`.

Run focused store, telemetry cleanup, gate and deterministic race tests under
`-race`; accepted A1 ingestion/auth/DTO tests; T0/T1/T2/S1/S1.1 regressions;
backend build/vet/full race; mobile TypeScript/full Jest; Android/native when
available; invariant and secret scans; and `git diff --check`. Environmental
skips are skips, never passes.

Freeze HEAD before the final gate. If the tree changes afterward, repeat the
audit/gate as required by the executor protocol. Commit and push focused changes,
report exact SHAs/local-remote equality/ancestry/clean tree, include one REVIEW
REQUEST marker for the implementation SHA, and stop for independent
re-verification. N1 remains blocked.

## 4. Explicit exclusions

No N1 notifications, Task/Dispatch, worker acknowledgement/completion,
Executor-Verifier, automatic approval policy, generic terminal command
execution, CLI redesign, new adapter/research stage, ConPTY/Windows, cloud relay,
lock-screen action, O1 or O2 work.
