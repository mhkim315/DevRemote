# A1.2 C2D-C R6-A3 Deny Evidence Handoff

Status: **R6-A2 REJECTED — IMPLEMENT R6-A3 ONLY**  
Reviewed implementation: `5455e2de50a0a468299eee3e38eb8fc2a4166842`  
Current docs parent: `36192f79e425d95e72193bbc62d8703bac7a259c`  
Accepted prerequisite: C2D-B `27bd45162feb6bdf6fec52fa44192208c8276e32`

R6-B/C2D-D/C3D remain prohibited. R6-A3 has only two production outcomes:

1. PostToolUse can prove allow and can never prove deny.
2. Permission-denials can prove deny only with an input digest recomputed from
   the exact provider event.

Do not report completion unless all required tests PASS. “RED expected” means
the packet is incomplete.

## 1. Rejection findings

- `LookupClaimToken` was removed, which is useful.
- However, `routeDenial` reads `ctx.inputDigest` and passes it back to
  `MarkWitnessed`. The provider denial event carries no decoded tool input, so
  the event does not prove the digest.
- The pump reads `bridge.resumeCtx` rather than an immutable context privately
  owned by the exact resumed runtime.
- `handlePostTool` still converts PostToolUse into
  `WitnessPermissionDenials` when the expected decision is deny.
- No decoder/adversarial/contract-note tests were added.
- Independent race execution: Allow PASS, Deny PASS through incomplete
  evidence, DenyPostToolUse negative FAIL.

## 2. Gate A3-0 — freeze real denial schema or stop BLOCKED

Before production edits, establish the complete bounded Claude 2.1.209
stream-json result shape containing `permission_denials`, including the actual
denied entry tool-input field. Use retained accepted evidence if sufficient.
If it is not sufficient, capture one isolated, bounded, redacted,
non-actionable schema trace with the pinned executable. Preserve field names,
types, session/tool IDs and a digest of the harmless bounded input; remove raw
command/cwd/prompt/token content.

Commit the contract-note/schema fixture first. Stop BLOCKED if the exact input
cannot be joined from the provider event. Do not invent a wire field and do not
substitute coordinator state.

## 3. Production changes

### A3-1 — runtime-owned immutable resume context

- Clone the approved resume context by value onto the exact resumed runtime
  before `pump()` starts.
- The denial pump reads only that private snapshot.
- Do not read `bridge.resumeCtx`, service maps, Store records or coordinator
  maps to discover claim/runtime/digest authority.

### A3-2 — evidence-derived denial digest

- Extend the evidence-backed denial decoder with the actual bounded provider
  tool input.
- Canonicalize that raw JSON exactly as the initial deferred/PreToolUse input is
  canonicalized and compute its digest.
- Compare event session ID, tool ID, tool name and computed digest with the
  private resume context.
- On exact equality and expected decision deny, call existing
  `MarkWitnessed(ctx.claimToken, WitnessPermissionDenials, event fields,
  eventDigest, ctx.originalRuntime)`.
- Never pass `ctx.inputDigest` as the witness digest; it is comparison state,
  not evidence.

### A3-3 — PostToolUse asymmetry

In `handlePostTool`:

```text
if expectedDecision != allow: return without witness
kind = WitnessPostToolUse
MarkWitnessed(exact context and decoded hook evidence)
```

Delete the branch that maps PostToolUse to `WitnessPermissionDenials`.

### A3-4 — real strict decoding

- token-level duplicate-key rejection for top-level and denial entries;
- complete evidence-backed allowlist;
- unknown/trailing/wrong-type/invalid UTF-8 rejection;
- explicit byte and array bounds;
- exact result/denial discriminator;
- malformed entry rejects the entire authority event.

`DisallowUnknownFields` alone is insufficient because it accepts duplicate
known keys.

## 4. Mandatory tests added in this commit

- real bounded denial fixture accepted;
- exact deny stdout → receipt accepted → Store committed;
- deny + PostToolUse → test PASS, no accepted receipt, no commit;
- missing/wrong/mutated tool input → rejected;
- same session/tool ID with different input digest → rejected;
- wrong claim/runtime context → rejected;
- observation runtime without private resume context → rejected;
- duplicate known keys at both object levels → rejected;
- unknown/trailing/wrong-type/invalid UTF-8/bound+1 → rejected;
- early/duplicate/late/cross-resume denial → rejected;
- allow composition remains accepted and committed.

Use exact resumed stdout and deterministic barriers. No sleeps and no direct
coordinator call in composition tests.

## 5. Gate and stop

```sh
cd companion-daemon
go test -race ./internal/term -run 'Claude.*(Denial|Witness|Resume|Delivery)' -count=10
go test -race ./cmd/devremote -run 'ClaudeDelivery_(CompositionAllowAccepted|CompositionDenyAccepted|DenyPostToolUseCannotCommit)' -count=5 -v
go test -race ./internal/term ./cmd/devremote -count=1
go build ./...
go vet ./...
git diff --check
```

All named tests must PASS. Push a frozen clean HEAD and stop with:

```text
REVIEW REQUEST: A1.2 C2D-C R6-A3 Deny Evidence — <implementation SHA>
```

Do not start R6-B.
