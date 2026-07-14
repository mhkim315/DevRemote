# Next Session Handoff — A1 Approval Safety Remediation 3

Status: **READY — R3-A THROUGH R3-E ONLY**

Risk class: **authority + concurrency**

Preserve the accepted A1 foundation and remediation-2 corrections. Fix only the
independent re-verification findings below. Do not begin N1/O1/O2.

## 0. Canonical repository recovery

```text
repository: https://github.com/mhkim315/DevRemote.git
local path: /Users/mhk/Documents/codex/DevRemote
branch: feature/phase10-multi-adapter
reviewed report HEAD: 35c632ea1a6d81b1ee7dbaf1ecb82cc0df7f3ad7
reviewed implementation: 0521c3828b9d78028f64f9479f8269f469050f85
accepted S1.1 ancestor: 02c8385e3270fbbc4df45e0c71ccad6ebe11a076
```

The repository root is the `DevRemote` child above, not
`/Users/mhk/Documents/codex`. Before editing, report `pwd`, remote URL, branch,
local/remote full SHAs, accepted-S1.1 and reviewed-implementation ancestry, and
clean worktree. Fetch and fast-forward only. Never reset, rewrite reviewed
history, discard another agent's changes, or force-push.

Read in order:

1. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
2. `docs/A1_APPROVAL_SAFETY_PLAN.md`;
3. `docs/A1_APPROVAL_SAFETY_REVERIFICATION_2.md`;
4. this handoff;
5. both earlier A1 verification/remediation reports and the current code/tests.

Sections 5 through 8 of the execution protocol are mandatory. Before production
edits, write the bounded pre-implementation contract note and complete binding
table. It must cover requester context, idempotent replay, canonical action and
exact payload, delivery acceptance, receipt, supersession, capacity, retry,
restart, and logs. Include the concrete failing interleavings and adversarial
counterexamples from the re-verification. Do not start R3-A until the note is
complete; stop extending it once every row and transition is explicit.

## 1. Preserve these accepted properties

- `waiting_approval` and heuristic/legacy/PTY/prompt/status evidence remain
  display-only;
- only accepted correlated capability + `DetectApproval` + `SafeApprovalGate`
  evidence enters the authoritative store;
- production remains non-actionable until a controlled provider mapping and
  delivery channel are proven;
- no blind Y/N, provider payload inference, raw prompt, generic command queue,
  legacy mobile transport, or client-supplied authority;
- store recomputes ActionDigest and uses its stored permission;
- cross-Approval key reuse conflicts and ledger capacity fails closed;
- fully mismatched receipt fields cannot commit and superseded records cannot
  commit stale success;
- backend safe DTO and mobile UTF-8 byte bounds remain unchanged.

## 2. Ordered task packets

### R3-A — complete requester/idempotency authority

Move the idempotent replay decision behind the full current authority checks.
Bind one immutable canonical requester authorization context to the claim and
ledger: exact server-derived DeviceID, HostID, bearer/auth session, boot/auth
context, and canonical permission set. Do not retain mutable caller slices.

`already_accepted` is valid only for the exact same ApprovalExecutionBinding,
exact current requester authorization context, stored permission, and allowed
current runtime/supersession state. A stale generation or revoked/changed auth
context must not obtain success from the fast path.

Required direct-store negatives: each requester field changed independently,
permission removed/changed, mutable input slice changed after claim, launch/stream
superseded, cross-Approval, different digest, and restart. Include the reviewer's
same-DeviceID/different-host-bearer-boot/no-permission counterexample.

### R3-B — one canonical action and exact payload binding

The store claim must produce the only immutable delivery-semantic action. The
handler must not reconstruct payload authority from `ApprovalSnapshot`. Either:

- the delivery boundary derives exact bytes solely from an immutable canonical
  action owned by the claim; or
- a domain-separated exact payload digest is included in the binding and
  independently verified by delivery and receipt.

Whichever minimal design is selected, substituted bytes with an unchanged claim
binding must fail before daemon acceptance and cannot commit. Validate UTF-8,
closed input type/placement/schema, size, and normalization inside the store.
Unknown placement and invalid UTF-8 fail closed. Do not expose canonical action,
payload, or digest source material in public DTOs.

Required negative control: claim one input, attempt a different payload through
the real approval delivery operation, and prove no acceptance/receipt/commit.

### R3-C — production delivery linearization

The current standalone `RuntimeDeliveryGate.AcceptDelivery` check is not an
acceptable boundary. Demonstrate its check-then-write race first, then replace
or isolate the smallest unsafe abstraction.

Use a generation-owned endpoint/queue or equivalent daemon boundary that
atomically accepts/enqueues the exact bound request under the same per-session
transition gate used for activation, launch/stream replacement, correlation
loss, delete, unlink, and termination. External I/O may occur after the gate only
against the captured generation-owned endpoint; do not perform a mutable session
lookup and do not hold internal locks across external I/O. Create an accepted
receipt only after exact daemon-boundary acceptance.

Wire the real production owners/call graph, even though the current provider
delivery implementation remains unavailable. A helper referenced only by tests
does not close this blocker.

Tests must use deterministic barriers/hooks to force:

```text
claim -> delivery begins -> replacement/deactivate contests acceptance
```

Assert the contested intermediate state and exact accepted-byte count. Include a
negative control proving the test fails if the shared gate/binding is bypassed.
No sleeps and no final-state-only churn test.

### R3-D — conform to the frozen retry contract

The plan requires bounded manual retry with the original key/digest; remediation
2 may not redefine it as universal no-retry. Implement a bounded retry only for
a receipt/outcome that proves the daemon boundary accepted no action. Bind retry
attempt count or lease, owner, original execution binding, expiry and restart
policy. Never automatically retransmit non-idempotent input. An ambiguous
delivery outcome remains non-retryable and non-success.

If no safe state model satisfies the frozen plan, stop before implementation of
this packet and request an independent plan amendment. Do not change tests or
report prose to call a different rule “frozen.”

### R3-E — conservative logs and honest provider blocker

Replace raw identifier logging with bounded opaque hashes or the repository's
conservative diagnostic sanitizer. Capture actual emitted log output and prove
that controls, Unix and Windows absolute paths, token/secret patterns, long
values, and malformed UTF-8 do not appear. Do not merely unit-test newline
replacement.

Reconfirm the provider action mapping/delivery status without broad research or
adapter work. If the exact production channel is still unavailable, keep all
production approvals non-actionable, mark A1 `BLOCKED`, and stop. Test fixtures
may prove machinery only and cannot satisfy the positive production gate.

## 3. Required completion audit and gate

Before the implementation commit, re-read the plan and both this handoff and the
re-verification. Produce an invariant-by-invariant audit mapping every binding
field to production code and non-vacuous tests. Explicitly label
`production-wired`, `test-only`, `unavailable`, `skipped`, and `blocked`.

Run focused direct-store, handler, delivery and deterministic concurrency tests
under `-race`; accepted A1 ingestion/auth/DTO tests; T0/T1/T2/S1/S1.1 regressions;
backend build/vet/full race; mobile TypeScript/full Jest; Android/native when
available; invariant/secret scans; and `git diff --check`. Environmental skips
are skips, never passes.

Freeze HEAD before the final gate and do not commit/amend while it runs. If a
rebase or any later tree change occurs, repeat the audit and gate as required by
the execution protocol. The report must provide exact SHAs, blocker-to-proof
matrix, positive provider status, one REVIEW REQUEST marker for the implementation
SHA, local/remote equality, ancestry, and clean worktree.

Stop for independent A1 re-verification. N1 remains blocked.

## 4. Explicit exclusions

No N1 notifications, Task/Dispatch, worker acknowledgement/completion,
Executor-Verifier, automatic approval policy, generic terminal command execution,
CLI redesign, new adapter/research stage, ConPTY/Windows, cloud relay,
lock-screen action, O1, or O2 work.
