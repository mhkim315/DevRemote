# A1.2 C2D-C Remediation 5 Handoff

> **Superseded:** implementation `a4f41ae6ecbce69bf80f19078e1e7301bceefed3`
> did not close the claimed deny-authority blockers. Use
> `NEXT_EXECUTOR_A1_2_C2D_C_REMEDIATION_6_HANDOFF.md` and execute R6-A only.

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

### Partial checkpoint `64a65af` — red evidence accepted, implementation rejected

Checkpoint `64a65affbc3945b690fe844dfd9b15c7cc365388` usefully exposes the
expected deny and lifecycle failures, but it is not a reviewable implementation
checkpoint. Independent `-race` execution found a data race between
`compFakeLauncher.Launch` appending `procs` without the mutex and the test
reading `procs[1]`. It also confirms:

- no production permission-denials decoder exists, so `processLine` ignores the
  injected `stop_reason:end_turn` result and delivery times out;
- `DenyPostToolUseCannotCommit` correctly fails against the still-present false
  PostToolUse-as-denial implementation; this is red evidence, not a regression
  to be weakened;
- all-exit `deferredExit=true` remains, so exit without joined defer preserves
  identity;
- lifecycle tests create a second detached session, inject into `procs[0]`, and
  Stop/Delete the second session. They do not exercise one exact session;
- the tests hold `launcher.mu` across blocking `io.PipeWriter.Write` and use
  sleeps, creating the reported hangs and violating the deterministic-barrier
  requirement;
- cwd lookup is mutable and fail-open: delivery re-reads `svc.runtimes` and
  defaults to `/tmp`. Original cwd must instead be frozen with the joined
  deferred identity; missing cwd is unavailable/rejected, never `/tmp`.

The next implementation order is mandatory:

1. Repair the test launcher first: allocate and publish each launch record under
   one mutex, return its exact process handle over a buffered channel, never
   hold the launcher mutex across pipe I/O, and provide explicit
   frame-consumed/EOF/reaped barriers. Remove all polling sleeps.
2. Split setup helpers:
   - delivery composition may install the controlled Store/identity fixture;
   - lifecycle tests must drive the production `/hook` observation and exact
     `tool_deferred` stdout join for one returned POKIT session/process. Do not
     call `ReserveIdentity` manually in lifecycle proof and do not create a
     second session.
3. Make the negative tests deterministically red/green with the fixed harness.
4. Implement the strict denial decoder and route it from the exact resumed
   runtime's `processLine` using an immutable resume context. The test frame must
   include the actual bounded tool input needed to recompute the canonical
   input digest; do not invent an `input_sha256` provider field.
5. Fix logical deferred lifecycle and immutable cwd ownership.

Do not proceed while `go test -race` reports any harness race. Do not report a
named PASS merely because the corresponding red test still fails as expected;
record expected-red and final-green states separately.

### R5-A/B checkpoint `40e77d6` — progress preserved, authority still rejected

Checkpoint `40e77d67e00f1171d821b6c2bcc5fcfd3ca5bf98` makes useful progress:

- the selected focused race run no longer reports the prior launcher slice
  race;
- allow still traverses the real resume/PostToolUse path and commits;
- a resumed stdout denial frame now reaches a production pump path;
- exit without a joined defer now clears identity;
- original session ID/cwd fields are carried into the resume runtime.

Preserve those improvements. However, the current deny green is not authority
safe and the following changes are mandatory before any review request:

1. **Remove `MarkWitnessedByToolUse`.** It searches the coordinator map by only
   session/tool ID, ignores its `kind` parameter, omits claim token, RuntimeRef,
   input digest and resume-attempt ownership, and may select a conflicting map
   entry. The resumed runtime must snapshot its immutable `resumeContext` and
   call the existing full-binding `MarkWitnessed(claimToken,
   WitnessPermissionDenials, sessionID, toolUseID, toolName, inputDigest,
   originalRuntime)`.
2. **Make denial decoding closed and bounded.** Plain `json.Unmarshal` into
   `streamDenial` currently accepts unknown/duplicate fields, unbounded arrays
   and strings, missing stop reason, and evidence without tool input/digest.
   Add a dedicated strict decoder. Canonicalize the exact bounded provider
   `tool_input` to recompute the input digest; the controlled frame must include
   that input. Reject rather than skip malformed entries.
3. **Fix PostToolUse immediately.** `handlePostTool` still derives witness kind
   from expected decision. It must always route only `WitnessPostToolUse`, and
   a deny entry must reject it. The currently failing
   `DenyPostToolUseCannotCommit` is the required red counterexample.
4. **Move `joinedDeferred=true` after successful Store admission and final
   post-ingest termination check.** It is currently set before
   `IngestObserved`; capacity/store rejection can therefore preserve authority
   on exit despite no admitted record. Publish the joined/preserve state at the
   same logical point as the admitted active identity, with rollback on every
   failure.
5. **Rewrite lifecycle proof around one production-created identity.** Current
   setup manually calls `ReserveIdentity`, creates a second detached session,
   injects deferred output into the first process, and Stop/Delete targets the
   second session. Drive `/hook` PreToolUse and matching `tool_deferred` through
   one exact session/process, observe Store/coordinator admission, close that
   process, then Stop/Delete that same logical session.
6. **Separate process exit from logical cleanup.** Remove the exported
   `SimulateGracefulExit`. Stop/Delete must clear logical identity after the
   initial process has already consumed `exitOnce`; do not rely on calling
   `terminate()` a second time.
7. **Freeze cwd at deferred identity creation.** Current delivery re-reads the
   mutable runtime map and falls back to `/tmp`. Store the validated original
   cwd privately with the joined identity. Missing/mismatched cwd returns
   unavailable/rejected; never substitute `/tmp`.
8. Replace remaining polling sleeps and the single shared `resumeW` with exact
   per-launch records and named barriers before the repeated-count gate.

Independent checkpoint reproduction:

```text
AllowAccepted                         PASS
CompositionDenyAccepted               PASS, but unsafe incomplete binding
DenyPostToolUseCannotCommit           FAIL (required blocker)
ExitWithoutJoinedDeferredClears       PASS
DeferredExitThenStop/DeleteClears     FAIL (fixture does not test one session)
```

Do not request independent acceptance until every line is green for the correct
authority reason and `-race` passes the complete focused suite repeatedly.

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
