# A1.2 C2D-C Remediation 2 Handoff

Status: **C2D-C REJECTED again — structural provider wiring required**  
Reviewed implementation: `08be1ad3711cb0549e157413ea8ba4cef5798834`  
Prior remediation handoff: `e9f1c40cf0f6f08b30595d41bc556279c3c027bb`  
Accepted prerequisite: C2D-B `27bd45162feb6bdf6fec52fa44192208c8276e32`  
Canonical checkout: `/Users/mhk/Documents/codex/DevRemote`  
Branch: `feature/phase10-multi-adapter`

> **Superseded:** implementation `4e38933b698ede578e0be69454dfe829237c9e23`
> was independently rejected because it inferred success from global identity
> count and had no accepted+committed allow/deny composition. Continue only from
> `NEXT_EXECUTOR_A1_2_C2D_C_REMEDIATION_3_HANDOFF.md`.

This is a replacement execution packet for the rejected C2D-C implementation.
Complete all three packets below before requesting another independent review.
Do not start C2D-D/C3D, mobile work, production activation, or a live model turn.

## 1. Why `08be1ad` is rejected

The implementation improved Store composition and basic cleanup, but still does
not exercise a Claude provider boundary.

### B1 — delivery is still a test callback, not a hook response

`ClaudeResponseWriter` has no implementation outside
`claude_approval_delivery_test.go`. `Deliver` calls it directly; no resumed
Claude process is launched and no repeated PreToolUse HTTP request owns the
response writer. The two witness routes likewise have no implementation outside
the delivery file/tests. Therefore a recorder plus two fixture callbacks can
still create `DeliveryAccepted` without Claude.

Affected code:

- `companion-daemon/internal/term/claude_approval_delivery.go:28-40`
- `companion-daemon/internal/term/claude_approval_delivery.go:158-203`
- `companion-daemon/internal/term/claude_approval_delivery_test.go:14-65`

The existing `claudeHookBridge.handleHook` still always returns `defer`; it has
no resume nonce, no `ClaimWrite`, no decision response, and no authenticated
PostToolUse route. `ManagedClaudeService` has no approval-resume spawn path.

### B2 — the delivered bytes and receipt digest are different objects

The Store-issued payload is JSON such as
`{"permissionDecision":"allow"}`, but `WriteResponse` receives only the token
`"allow"`. The receipt nevertheless sets `DeliveredPayloadDigest` from the
Store JSON payload. This can commit a receipt that does not describe the bytes
accepted by the provider boundary.

Also, `ClaimWrite` returns the coordinator-owned `WriteHandle`, but `Deliver`
discards it and independently re-derives the decision from `OptionID`.

Affected code:

- `companion-daemon/internal/term/claude_approval_delivery.go:114-117`
- `companion-daemon/internal/term/claude_approval_delivery.go:147-160`
- `companion-daemon/internal/term/claude_approval_delivery.go:212-218`

### B3 — post-write failures leak coordinator capacity

After `ConfirmWrite(true)`, witness timeout or mismatch calls
`cleanupPreWrite`. That helper calls `CancelEntry`, but `CancelEntry` does
nothing for `stateDecisionWritten`. The entry remains in `entries` until a later
sweep even though identity is removed. Tests assert `pendingCount`, which omits
`stateDecisionWritten`, instead of `entryCount`, so the leak is masked.

Affected code:

- `companion-daemon/internal/term/claude_approval_delivery.go:190-207`
- `companion-daemon/internal/term/claude_approval_delivery.go:241-250`
- `companion-daemon/internal/term/claude_resume_coordinator.go:478-501`
- `companion-daemon/internal/term/claude_resume_coordinator.go:673-683`

### B4 — required tests remain vacuous or absent

- `recordingResponseWriter` and `stubWitnessRoute` bypass the hook bridge and
  resumed process.
- `TestClaudeDelivery_FakeConfirmation` tests a nil dependency; it does not
  reproduce the former `ConfirmWrite(true)`-without-provider-write bypass.
- `TestClaudeDelivery_WitnessRace` still uses `time.Sleep(50ms)`.
- The self-audit says the test uses channels rather than sleep, contradicting
  the source.
- The required `cmd/devremote` controlled-composition gate reports
  `[no tests to run]`.

The focused internal race suite passing is not acceptance evidence for these
boundaries.

## 2. Mandatory call-graph correction before more tests

Do not add another generic writer around the current `Deliver` method. The
provider HTTP response exists only when Claude invokes the repeated PreToolUse
hook. Therefore the ownership must be:

```text
A1 ClaimForExecution
  -> ClaudeManagedApprovalDelivery.Deliver
     -> coordinator.ReserveEntry (decision and nonce frozen)
     -> ManagedClaudeService starts exact pinned `claude --resume <session_id>`
        with the isolated resume-hook configuration and nonce
     -> Deliver waits for terminal coordinator result without holding locks

repeated PreToolUse HTTP request
  -> authenticated claudeHookBridge resume route
  -> strict request decode + exact private identity + nonce comparison
  -> coordinator.ClaimWrite
  -> encode the exact C0D-certified hook response from WriteHandle.Decision()
  -> write that exact byte slice to http.ResponseWriter
  -> coordinator.ConfirmWrite(write fully accepted)

allow consumption
  -> authenticated PostToolUse hook for the same invocation
  -> coordinator.MarkWitnessed(PostToolUse, exact identity/runtime)

deny consumption
  -> bounded permission_denials projection from that resumed process
  -> coordinator.MarkWitnessed(PermissionDenials, exact identity/runtime)

terminal coordinator result
  -> Deliver constructs receipt using the digest of the exact response bytes
  -> A1 RecordDelivery
```

This flow is uninstalled controlled composition. It must not be wired into
`app.go` yet.

## 3. Packet R2-A — resume and authenticated hook wiring

Write the contract-note amendment first. Then implement the minimum concrete
path:

1. Add a managed-service method that consumes the opaque `ResumeHandle` and
   launches the pinned, attested Claude executable with `--resume` and an
   isolated resume hook. Do not use a shell wrapper or PATH lookup.
2. Bind the hook capability, resume nonce, Claude session/tool identity, POKIT
   session, exact RuntimeRef, and claim token without exposing them publicly.
3. Extend or isolate `claudeHookBridge` with a closed resume mode. Normal C1D
   observation mode must continue returning only `defer`.
4. In resume mode, the HTTP handler — not `Deliver` — performs `ClaimWrite`,
   obtains `WriteHandle.Decision()`, encodes the closed response schema, writes
   the exact bytes, then calls `ConfirmWrite`.
5. Add an authenticated PostToolUse handler with a closed field allowlist. It
   must share exact `session_id`, `tool_use_id`, `tool_name`, input digest,
   RuntimeRef, nonce/claim ownership where applicable.
6. Route only the bounded deny projection from the exact resumed process to
   `MarkWitnessed`; stdout ordering or process exit is not a witness.
7. `Deliver` reserves, initiates resume, and waits for a terminal coordinator
   result. It must not receive an arbitrary response writer or witness callback.
8. No lock may be held across spawn, HTTP I/O, stream read, process wait, or
   teardown.

If the accepted Claude surface cannot support this exact call graph, stop as
`C2D-C BLOCKED`; do not restore callback-based success.

## 4. Packet R2-B — one canonical response and terminal cleanup

Freeze one internal response representation:

- derived only from `WriteHandle.Decision()`;
- strict, deterministic JSON encoding matching the C0D-certified hook response;
- the exact encoded bytes are written to the hook response;
- the Store delivery material and `PayloadDigest` represent those same bytes;
- `DeliveryReceipt.DeliveredPayloadDigest` hashes those exact bytes.

No independent OptionID mapping may be used after `ReserveEntry`. A deliberately
faulted coordinator decision/option mismatch must fail before write.

Add one coordinator terminalization operation that owns cleanup after
`stateDecisionWritten`. It must:

- atomically remove or replace the entry with the minimum bounded replay
  tombstone;
- distinguish confirmed consumption, timeout, malformed/wrong witness,
  cancellation, runtime replacement, stop/delete/exit, and daemon shutdown;
- never delete a newer runtime/claim entry;
- never permit retransmission after a possibly accepted write;
- release capacity deterministically rather than relying on a later sweep.

Tests must assert `entryCount`, identity count, replay/tombstone state, and Store
state for every failure after reservation, after claim, after response write,
and after confirmation.

## 5. Packet R2-C — production-shaped composition and adversarial tests

Create the required `cmd/devremote` controlled-composition test. It may use a
fake external process/launcher, but must use the real production objects and
routes up to that final boundary:

- real `ManagedClaudeService` composition;
- real hook HTTP server and authenticated endpoint;
- real strict PreToolUse/PostToolUse decoders;
- real resume launch request and captured exact argv/settings;
- real coordinator transitions;
- real `AuthoritativeApprovalStore` ingestion, `ClaimForExecution`, delivery,
  receipt, and `RecordDelivery`;
- real resumed-output deny projector.

Positive tests:

1. allow_once: repeated PreToolUse receives exact allow response, matching
   authenticated PostToolUse follows, receipt commits;
2. deny: repeated PreToolUse receives exact deny response, matching bounded
   permission denial follows, receipt commits.

Mandatory negative/interleaving tests:

- known-bad handler that calls `ConfirmWrite(true)` without writing must fail;
- short/failed response write cannot commit;
- Store payload bytes, HTTP response bytes, and receipt digest mismatch cannot
  commit;
- wrong/missing nonce, capability, session, tool ID/name, input digest, runtime,
  option, schema, or claim token writes zero decision bytes;
- witness before confirmation, during write, duplicate/late witness, wrong-kind
  witness, and cross-session witness;
- stop/delete/exit/epoch replacement at reservation, resume launch, repeated
  hook, response write, and witness barriers;
- resume launch failure, timeout, malformed output, capacity exhaustion, and
  daemon restart;
- each non-success path leaves no live entry and no successful Store state.

Use deterministic named barriers/channels. Remove sleep-based synchronization
from C2D-C tests. Run a negative control that fails when the hook write is
bypassed.

## 6. Executor discipline and stop condition

Before coding, re-read:

1. `docs/A1_2_C2D_PACKET_CONTRACT_NOTE.md` sections 2.2, 2.3, 3, 5, 6, 10.3;
2. `docs/NEXT_EXECUTOR_A1_2_C2D_C_REMEDIATION_HANDOFF.md`;
3. this document;
4. `claude_hook_bridge.go`, `managed_claude.go`,
   `claude_resume_coordinator.go`, and the frozen A1 execution/store code.

Complete R2-A through R2-C as one review packet. Do not request another review
after merely introducing interfaces or test recorders.

Before commit, produce an invariant self-audit that distinguishes:

- concrete production code;
- external-process test double;
- test-only seam;
- unavailable/skipped behavior;
- uninstalled production composition.

The audit must not call an interface "production-shaped" unless a concrete
non-test implementation exists and is exercised through the production call
graph.

Run on a frozen HEAD:

```sh
cd companion-daemon
go test -race ./internal/term -run 'Claude.*(Delivery|Resume|Hook|Coordinator)' -count=5
go test -race ./cmd/devremote -run 'Claude.*(Delivery|Composition)' -count=1 -v
go test -race ./internal/term ./cmd/devremote -count=1
go build ./...
go vet ./...
git diff --check
```

The `cmd/devremote` command must execute named tests; `[no tests to run]` is a
gate failure.

Freeze HEAD before the authoritative gate. If a report commit changes HEAD,
rerun the gate on the report HEAD. Push and stop with:

```text
REVIEW REQUEST: A1.2 C2D-C Remediation 2 — <implementation SHA>
```

C2D-D/C3D remain prohibited until independent acceptance.
