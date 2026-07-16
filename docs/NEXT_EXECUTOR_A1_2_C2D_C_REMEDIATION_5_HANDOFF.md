# A1.2 C2D-C Remediation 5 Handoff

Status: **C2D-C REJECTED — deny witness and deferred-lifecycle authority are unsound**  
Reviewed implementation: `cdea8bd30059d72c2e4bcf1b33979318a3147f75`  
Accepted prerequisite: C2D-B `27bd45162feb6bdf6fec52fa44192208c8276e32`  
Canonical checkout: `/Users/mhk/Documents/codex/DevRemote`

This is the only active C2D-C executor packet. C2D-D/C3D and production
actionability remain prohibited. Use the existing execution agent, but execute
the slices below strictly in order and stop for independent review afterward.

## 0. Status checkpoint `6d58deb` is not implementation evidence

The docs-only checkpoint
`6d58deb7f1b9471e2b754d2e0de33e9524ea92cd` honestly reports BLOCKED, but its
diagnosis and proposed test shortcuts are not accepted:

- it contains no production or test changes; the tree still has the R4 false
  deny-positive test and does not contain the reported
  `TestClaudeDelivery_DenyPostToolUseCannotCommit` test;
- an empty resume-runtime `rt.sessionID` is a real binding defect, but it cannot
  itself terminate `bufio.Scanner`: the scanner reads only `proc.Stdout()`;
- `bytes.Buffer`/`strings.Reader` preload is not an adequate lifecycle harness:
  it delivers immediate EOF and cannot prove the named write/witness/exit
  interleavings;
- calling `coordinator.MarkWitnessedByToolUse` directly from a composition test
  is prohibited because it bypasses the required resumed-stdout decoder and
  production routing boundary.

Continue from the production baseline at `cdea8bd`, but first commit the R5-0
tests and deterministic launcher harness. Use a per-launch process handle plus
channels/barriers (or a channel-backed scripted reader) so the test can select
the exact resumed process, publish one bounded frame, observe its consumption,
and then close stdout at an explicit barrier. A correctly coordinated `io.Pipe`
is also acceptable; an uncoordinated blocking write is not.

Set the resume runtime's private POKIT session identity from the immutable
`resumeContext` as part of the binding fix, but do not claim that this alone
repairs pump I/O or deny routing.

## 1. Preserve the real progress

Do not rewrite the accepted structural work without a demonstrated dependency:

- the actual allow composition traverses the production bridge `/resume` and
  `/posttool` endpoints;
- the exact Store payload is written and the matching PostToolUse witness can
  produce an accepted receipt and committed Store result;
- claim-owned completion replaced global-count polling;
- canonical response bytes and payload digest remain bound through delivery;
- `resumeContext` is available before process launch;
- the original approval RuntimeRef remains separate from the resume attempt;
- response write count/error is checked and ambiguous writes are non-retryable.

The remediation is narrow: correct deny authority, deferred-exit lifecycle,
working-directory binding, decoder boundaries, and deterministic proof.

## 2. Blocking findings

### B1 — PostToolUse is falsely promoted to a deny witness

`internal/term/claude_hook_bridge.go` currently selects the witness kind from
the expected decision. When the expected decision is deny, a PostToolUse hook
is relabelled `WitnessPermissionDenials`. PostToolUse indicates execution and
must never prove denial.

The frozen asymmetric contract is:

- allow: authenticated matching PostToolUse;
- deny: matching `permission_denials` evidence from the exact resumed Claude
  stream-json result.

`managed_claude.go` still decodes only `tool_deferred`; there is no production
permission-denials decoder/routing call. The retained evidence shape is in
`docs/a1_2_c0d_r5_evidence/deny_live/denial_projection.json` and the contract is
recorded in `docs/A1_2_C2D_PACKET_CONTRACT_NOTE.md`.

### B2 — every pump exit preserves approval authority

The pump defer sets `deferredExit = true` for every stdout close, malformed
stream, crash, and ordinary exit. `terminate` then skips coordinator cleanup.
Only an exit after an exact, successfully joined `tool_deferred` event may
preserve the bounded deferred identity. Every other exit must invalidate it.

### B3 — deferred-exit cleanup is not proven and can be defeated by `exitOnce`

`TestClaudeDelivery_StopAfterDeferredExitInvalidatesIdentity` does not simulate
a deferred exit before Stop. `SimulateGracefulExit` is an exported test-only
production method. After it consumes process `exitOnce`, later Stop can call the
same no-op termination path and leave the preserved logical approval identity.

Process reaping and logical approval authority need distinct ownership. Stop,
Delete, expiry, replacement, and shutdown must clear logical authority even
when the original process was already reaped.

### B4 — resume cwd is not the approved invocation cwd

`ResumeForApproval` still uses `/tmp`. Capture the original validated cwd in a
private logical-session/deferred-identity record and resume in exactly that
directory. Never expose it in public approval DTOs.

### B5 — PostToolUse has no dedicated evidence-backed decoder

`handlePostTool` reuses the PreToolUse decoder. Freeze a dedicated bounded
PostToolUse decoder from retained C0D evidence. Unknown, duplicate, missing,
malformed, and over-bound fields fail closed.

### B6 — positive tests still use polling and test-only production APIs

The simulator polls with sleeps and the earliest-hook test does not establish a
deterministic pre-launch barrier. Replace polling with channels/barriers. Remove
`SimulateGracefulExit` from production and drive exit through the fake process's
real stdout/process lifecycle. Avoid exporting other internals solely for tests.

## 3. R5-0 — reproduce the failures before production edits

Add these deterministic tests first and run them against reviewed SHA
`cdea8bd30059d72c2e4bcf1b33979318a3147f75`:

- `TestClaudeDelivery_DenyPostToolUseCannotCommit`
- `TestClaudeDelivery_DenyPermissionDenialAccepted`
- `TestClaudeDelivery_ExitWithoutJoinedDeferredClearsIdentity`
- `TestClaudeDelivery_DeferredExitThenStopClearsIdentity`
- `TestClaudeDelivery_DeferredExitThenDeleteClearsIdentity`
- `TestClaudeDelivery_ResumeUsesOriginalCWD`
- `TestClaudeDelivery_PostToolUnknownFieldRejected`
- `TestClaudeDelivery_EarliestHookBarrier`

Record the expected red results in the contract-note amendment. The first test
must reproduce the current false success: deny decision followed by PostToolUse
must never yield an accepted receipt or committed Store state. The second must
fail until the actual denial projection is routed from resumed stdout.

Use barriers/channels, not sleeps. A later successful operation must not mask a
contested intermediate state.

## 4. R5-A — exact deny evidence routing

1. Make `handlePostTool` always and only route `WitnessPostToolUse`. If the
   expected decision is deny, PostToolUse is a mismatch/non-witness.
2. Add a closed, bounded stream-json denial decoder based on the retained C0D
   evidence. Require the exact result/event type, Claude session ID, denied
   tool name, `tool_use_id`, and canonical input digest.
3. Route denial only from the stdout of the exact resume attempt bound to the
   claim's immutable context.
4. Route `WitnessPermissionDenials` only after the decision write reached the
   coordinator's confirmed written state.
5. Wrong session/tool/input, early/late/duplicate evidence, unknown fields,
   malformed arrays, and cross-claim evidence remain non-success.

No FIFO, command equality, timing, process exit, or missing side effect may be
used as denial authority.

## 5. R5-B — logical deferred authority lifecycle

1. Set the preserve-after-exit flag only after exact `tool_deferred` decoding,
   `joinDeferred` success, and private identity/Store admission.
2. Separate one-shot process reap state from logical approval authority.
3. Preserve only that bounded logical identity across the expected headless
   deferred exit.
4. Stop, Delete, expiry, runtime replacement, correlation loss, and shutdown
   clear the logical identity even if the process is already reaped.
5. Daemon restart restores no pending authority.
6. Retain and bind the original validated cwd privately; use it for resume.
7. Remove the exported `SimulateGracefulExit`; tests close the fake process
   stdout and observe real exit/reap behavior.

Do not hold state locks across spawn, hook HTTP, provider I/O, or process wait.

## 6. R5-C — deterministic composition proof

The external simulator must emit exact bounded provider fixtures through the
real production ingestion surface:

- initial `tool_deferred`, then process exit;
- repeated PreToolUse through `/resume`;
- allow PostToolUse through `/posttool`;
- deny `permission_denials` through resumed stdout.

Required positive assertions for both allow and deny:

```text
HTTP decision bytes == exact Store canonical payload
DeliveryReceipt.Outcome == accepted
receipt binding/digests == claim binding/digests
RecordDelivery(receipt).Committed == true
coordinator/logical authority entry removed
resume process terminated and reaped
```

Required negative coverage includes PostToolUse-after-deny, malformed denial,
wrong identity/digest, witness-before-write, duplicate/late witness, exit before
joined defer, deferred-exit then Stop/Delete, replacement at each named barrier,
wrong cwd, short write, concurrent claims, and capacity exhaustion.

Preserve and rerun the existing accepted allow path. Do not run a live model;
this packet is production-composition proof with bounded retained evidence.

## 7. Completion and stop condition

Before the implementation commit, re-read this handoff and map B1-B6 to exact
production symbols and non-vacuous tests. Freeze HEAD before the final gate:

```sh
cd companion-daemon
go test -race ./internal/term -run 'Claude.*(Delivery|Resume|Hook|Coordinator)' -count=5
go test -race ./cmd/devremote -run 'Claude.*(Delivery|Composition)' -count=3 -v
go test -race ./internal/term ./cmd/devremote -count=1
go build ./...
go vet ./...
git diff --check
```

Verbose output must show:

- allow accepted and Store committed;
- deny accepted only from exact permission-denials evidence and Store committed;
- deny plus PostToolUse rejected;
- exit without joined defer invalidated;
- deferred exit followed by Stop and Delete invalidated.

Commit, push, verify local/remote equality and clean worktree, then stop with:

```text
REVIEW REQUEST: A1.2 C2D-C Remediation 5 — <implementation SHA>
```

C2D-D/C3D remain prohibited until independent acceptance.
