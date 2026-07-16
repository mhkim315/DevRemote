# A1.2 C2D-C Remediation 4 Handoff

Status: **C2D-C REJECTED — positive provider exchange still absent**  
Reviewed implementation: `bc7145f529dc205e74a61c5c57bb000192d59e5e`  
Previous verifier packet: `2a23fec0c10342b6a0e2d8575b824ffc87455b35`  
Accepted prerequisite: C2D-B `27bd45162feb6bdf6fec52fa44192208c8276e32`  
Canonical checkout: `/Users/mhk/Documents/codex/DevRemote`

This packet is intentionally test-first. Do not modify more production code
until R4-0 exists and demonstrably fails against `bc7145f` for the expected
missing provider exchange. Complete R4-0 through R4-3 before requesting review.
C2D-D/C3D and production activation remain prohibited.

## 1. What improved and is preserved

Preserve these R3 changes unless a demonstrated dependency requires adjustment:

- a claim-owned completion channel replaced global identity-count polling;
- `ConfirmWrite(true)` is intermediate, not terminal success;
- canonical full hook response bytes are validated against Store payload;
- the original approval RuntimeRef is carried separately from the resume
  attempt epoch;
- response write checks byte count and error;
- receipt construction uses the coordinator's stored binding and response
  digest.

Do not rewrite these accepted structural improvements.

## 2. Remaining blockers

### B1 — the mandatory positive tests were not implemented

The prior handoff required `CompositionAllowAccepted` and
`CompositionDenyAccepted`, including real hook exchange and
`RecordDelivery.Committed == true`. The current tests still use a fake process
that invokes no hook and expect `DeliveryConflict` timeout.

Passing these tests proves only failure cleanup:

- `companion-daemon/cmd/devremote/claude_delivery_composition_test.go:62-151`
- `companion-daemon/cmd/devremote/claude_delivery_composition_test.go:153-222`

### B2 — earliest repeated hook still races bridge initialization

`resumeContext` is installed before spawn, but `handleResume` still requires
`b.rt` and `rt.coordinator`. `b.rt` is installed only after the launcher
returns and `newClaudeManagedRuntime` executes. A provider invoking the hook
during process startup receives `defer`, leaving the claim to time out.

Use the already-installed immutable `resumeContext` directly for resume and
PostToolUse authority. Observation-mode `/hook` may continue to use `b.rt`.

### B3 — deny witness routing is still missing

`claudeManagedRuntime.processLine` recognizes only `tool_deferred`. There is no
strict bounded `permission_denials` decoder and no
`MarkWitnessed(WitnessPermissionDenials, ...)` production call. Deny cannot
reach `TerminalWitnessed`.

### B4 — the initial deferred identity does not survive the real headless exit

C0D established that headless defer ends the process. The current pump defers
`rt.terminate()`, and `terminate` calls `coordinator.ClearRuntime`, removing the
private identity required for later mobile claim/resume. Current tests hide
this by keeping the first fake process open.

Expected `tool_deferred` process completion and explicit stop/delete/runtime
replacement are different lifecycle events. The former must preserve only the
bounded deferred approval identity needed for resume; the latter must
invalidate it. Daemon restart still restores nothing.

### B5 — resume execution directory is hard-coded

`ResumeForApproval` uses `"/tmp"` rather than the original managed session's
certified working directory. This can execute an approved tool action in a
different workspace. Capture and bind the original validated cwd privately and
resume in exactly that directory. Do not expose it in public approval DTOs.

### B6 — PostToolUse parsing is not evidence-backed

`handlePostTool` reuses `strictPreToolUseDecode`. The accepted C0D evidence did
not authorize assuming identical field sets. Freeze a dedicated decoder from
the retained bounded C0D fixture. Unknown fields, missing required identity,
malformed input, and over-bounds fail closed.

## 3. R4-0 — red positive harness, no production changes

First change tests only. Build one deterministic external-process simulator at
the launcher boundary. It must:

1. capture the exact resume executable, argv, cwd, settings path, and hook
   scripts;
2. wait on a test channel until the bridge context is installed;
3. invoke the real `/resume` endpoint with the exact repeated PreToolUse fixture;
4. capture the exact HTTP response bytes;
5. for allow, invoke the real authenticated `/posttool` endpoint;
6. for deny, write the exact bounded `permission_denials` event to the resumed
   process stdout;
7. expose process exit/reap state through deterministic channels.

Add these exact tests:

- `TestClaudeDelivery_CompositionAllowAccepted`
- `TestClaudeDelivery_CompositionDenyAccepted`
- `TestClaudeDelivery_EarliestHookAfterSpawn`
- `TestClaudeDelivery_DeferredExitPreservesResumeIdentity`
- `TestClaudeDelivery_StopAfterDeferredExitInvalidatesIdentity`

The first two must require all of:

```text
HTTP response == exact Store payload
DeliveryReceipt.Outcome == accepted
DeliveryReceipt.DeliveredPayloadDigest == claim.Binding.PayloadDigest
RecordDelivery(receipt).Committed == true
coordinator live entry count == 0
resume process reaped == true
```

Run them against `bc7145f` and record the expected failures in the contract-note
amendment. Do not weaken assertions to timeout/conflict. Do not start R4-1 until
these red tests are non-vacuous.

## 4. R4-1 — bridge authority and lifecycle correction

Make the smallest code changes needed for the red tests:

1. `handleResume` reads one immutable snapshot of `resumeContext` under the
   bridge mutex and uses `ctx.coordinator`; it must not depend on `bridge.rt`.
2. It verifies query claim/nonce against the context before `ClaimWrite`.
3. It compares decoded Claude session/tool/input identity against the context
   as well as the coordinator entry.
4. `handlePostTool` uses the same context snapshot and a dedicated strict
   PostToolUse decoder.
5. Store the original validated cwd with the private deferred identity or
   logical managed-session record; resume uses that exact cwd.
6. Split expected deferred-process completion from destructive termination:
   - expected `tool_deferred` exit closes/reaps the process and observation
     resources but preserves the bounded private identity and pending Store
     record until expiry/decision;
   - stop/delete/replacement/expiry explicitly clear Store and coordinator;
   - daemon restart restores neither.
7. Every resume attempt is terminated and reaped on witnessed success and all
   failure outcomes without clearing an unrelated/newer logical runtime.

No global counts, timing, process exit, or absence of execution may become
success authority.

## 5. R4-2 — strict deny consumption

Implement a closed decoder for the exact accepted C0D deny result. It must
extract and validate only the evidence-proven fields needed for:

- Claude session identity;
- exact `tool_use_id`;
- tool name;
- canonical input digest;
- explicit denial/error result.

Route it only from the stdout of the exact resume attempt bound to the claim.
Call `MarkWitnessed(WitnessPermissionDenials, ...)` with the original approval
RuntimeRef from the immutable context.

Malformed, duplicate, missing, wrong-request, wrong-session, wrong-input,
unknown-field, oversized, late, or post-replacement denial events must never
publish `TerminalWitnessed`.

If the retained evidence does not contain enough fields for this exact join,
stop `C2D-C BLOCKED`; do not infer from FIFO, command equality, timing, or
process exit.

## 6. R4-3 — adversarial completion and cleanup

After the positive tests pass, add deterministic negative tests for:

- hook arrival before `bridge.rt` assignment;
- wrong capability, claim, nonce, session, tool ID/name, input digest;
- short/failed response write;
- witness before confirmation, duplicate/late witness, cross-claim witness;
- runtime replacement, stop, delete, expiry, and shutdown at every named
  barrier;
- resume launch failure and process exit before hook;
- payload/response/receipt digest substitution;
- two concurrent claims proving one completion cannot satisfy the other;
- capacity exhaustion and timeout leaving no live entry/process/hook directory;
- expected initial deferred exit preserving authority, followed by explicit
  lifecycle invalidation removing it.

Use channels/barriers only. Remove timeout-as-success Allow/Deny tests rather
than retaining them under positive names. A separate explicitly named timeout
test is appropriate.

## 7. Completion discipline

Before implementation commit, produce a self-audit mapping B1-B6 and every R4-0
assertion to exact production code and tests. Include the recorded initial red
failures and final green results.

Do not export internal coordinator/schema helpers merely to simplify tests.
Composition may use a narrow test harness, but authority must remain in the
production bridge/service/coordinator call graph.

Freeze HEAD and run:

```sh
cd companion-daemon
go test -race ./internal/term -run 'Claude.*(Delivery|Resume|Hook|Coordinator)' -count=5
go test -race ./cmd/devremote -run 'Claude.*(Delivery|Composition)' -count=3 -v
go test -race ./internal/term ./cmd/devremote -count=1
go build ./...
go vet ./...
git diff --check
```

The verbose output must contain both `CompositionAllowAccepted` and
`CompositionDenyAccepted`, and both must commit the Store. Otherwise the gate is
failed even if `go test` exits zero.

Push and stop with:

```text
REVIEW REQUEST: A1.2 C2D-C Remediation 4 — <implementation SHA>
```

C2D-D/C3D remain prohibited until independent acceptance.
