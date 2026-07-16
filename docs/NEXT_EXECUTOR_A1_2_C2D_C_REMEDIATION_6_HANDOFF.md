# A1.2 C2D-C Remediation 6 Handoff

> **Superseded:** R6-A implementation
> `bffeef9214add0622358a5428a05a2402db576b1` retained a partial-identity
> coordinator lookup and failed its PostToolUse negative control. Use
> `NEXT_EXECUTOR_A1_2_C2D_C_R6_A2_HANDOFF.md` as the only active packet.

Status: **C2D-C REJECTED — START R6-A DENY AUTHORITY ONLY**  
Reviewed implementation: `a4f41ae6ecbce69bf80f19078e1e7301bceefed3`  
Accepted prerequisite: C2D-B `27bd45162feb6bdf6fec52fa44192208c8276e32`  
Canonical checkout: `/Users/mhk/Documents/codex/DevRemote`

This replaces remediation 5 as the only active executor packet. The current
agent repeatedly reported red tests and partial checks as closed blockers, so
the work is split into two independently reviewed slices. **Implement R6-A
only, push, and stop. Do not begin R6-B until independent R6-A acceptance.**
C2D-D/C3D and production actionability remain prohibited.

## 1. Preserve proven progress

Preserve unless a negative test demonstrates a dependency:

- real allow `/resume` → PostToolUse → receipt → Store commit;
- claim-owned completion and confirmed-write-before-witness state machine;
- resume context installed before spawn;
- resume runtime receives the POKIT session ID;
- exit without joined defer now clears identity;
- race-free publication of the resume writer in the selected focused run.

Do not claim current deny PASS as accepted. It succeeds through incomplete
authority binding.

## 2. Why `a4f41ae` remains rejected

### B1 — the claimed full-binding delegation did not happen

`MarkWitnessedByToolUse` still iterates the coordinator map by only
session/tool ID and directly publishes `TerminalWitnessed`. It does not call the
existing `MarkWitnessed`, does not receive a claim token, RuntimeRef or input
digest, ignores its `kind` argument except for a local comparison, and can pick
a competing entry. Delete this method; do not rename or wrap it.

### B2 — the claimed strict decoder is permissive

`strictDenialDecode` is plain `json.Unmarshal`. It accepts unknown fields,
duplicate keys, unverified top-level shape, missing stop reason, byte-overbound
UTF-8 values and denial entries with no tool input/digest. It therefore cannot
join the denial to the exact invocation.

### B3 — PostToolUse false-deny remains

`handlePostTool` still selects `WitnessPermissionDenials` when the expected
decision is deny. The negative test fails. Blocker 3 is open.

### B4 — joined state is still published too early

`joinedDeferred` moved after `IngestObserved`, but before the post-ingest
termination check and active-identity publication. A termination at that seam
can still preserve a record that is rolled back. This belongs to R6-B after
deny authority is accepted.

## 3. R6-A contract amendment before code

Amend `docs/A1_2_C2D_PACKET_CONTRACT_NOTE.md` with the exact deny call graph and
binding table. The production call graph must be:

```text
exact resumed process stdout
  → closed bounded permission_denials decoder
  → immutable resumeContext snapshot
  → canonical tool_input digest
  → coordinator.MarkWitnessed(
        claimToken,
        WitnessPermissionDenials,
        claudeSessionID,
        toolUseID,
        toolName,
        inputDigest,
        originalRuntime)
  → claim-owned completion
  → receipt
  → Store RecordDelivery commit
```

If the retained C0D evidence and accepted contract do not establish the complete
real provider event fields needed for a closed decoder, stop R6-A BLOCKED. Do
not invent fields or weaken unknown-field rejection.

## 4. R6-A implementation

### A1 — immutable resume ownership

- Store a private immutable copy of `resumeContext` on the exact resumed
  runtime before its pump starts.
- The denial route obtains one snapshot from that runtime. It must not search
  coordinator entries or mutable service maps to discover ownership.
- Remove `MarkWitnessedByToolUse` completely.

### A2 — strict evidence-backed denial decoder

Implement a dedicated decoder that:

- accepts only the complete evidence-backed top-level and nested field sets;
- rejects duplicate keys, unknown fields, trailing data, invalid UTF-8 and
  non-object/wrong-type values;
- enforces explicit byte bounds and a small fixed maximum denial count;
- requires the exact denial event/result discriminator and Claude session ID;
- requires tool name, `tool_use_id`, and the actual bounded provider
  `tool_input` necessary to recompute canonical input digest;
- rejects duplicate matching entries and any malformed entry rather than
  silently skipping it.

An `input_sha256` field exists only in the retained redacted projection; do not
pretend Claude emits it. Recompute the digest from the real bounded tool input.

### A3 — asymmetric witness routing

- `handlePostTool` always routes only `WitnessPostToolUse`.
- If the stored decision is deny, PostToolUse is a mismatch and produces no
  terminal success.
- The pump routes permission-denials only when the immutable context expects
  deny and only through the full-binding `MarkWitnessed` call above.
- Wrong claim/runtime/session/tool/name/input, early, late, duplicate and
  cross-resume evidence remain non-success.

## 5. R6-A deterministic tests

Use exact per-launch handles and channels/barriers; remove sleeps from all tests
touched by R6-A. Required tests:

- deny exact permission-denials → accepted receipt → Store committed;
- deny followed by PostToolUse → no accepted receipt, no commit;
- permission-denials before confirmed write → rejected;
- wrong session, tool ID, tool name, tool input digest and RuntimeRef → rejected;
- two simultaneous entries sharing lesser fields cannot complete each other;
- unknown/duplicate/extra/trailing/oversized/malformed denial fields → rejected;
- duplicate and late denial → one terminal owner only;
- existing allow path remains accepted and committed.

Tests must inject the bounded denial frame through the exact resumed process
stdout. Direct coordinator witness calls from composition tests are prohibited.

Run and freeze the exact implementation HEAD:

```sh
cd companion-daemon
go test -race ./internal/term -run 'Claude.*(Denial|Witness|Resume|Delivery)' -count=10
go test -race ./cmd/devremote -run 'ClaudeDelivery_(CompositionAllowAccepted|CompositionDenyAccepted|DenyPostToolUseCannotCommit)' -count=5 -v
go test -race ./internal/term ./cmd/devremote -count=1
go build ./...
go vet ./...
git diff --check
```

Commit, push, verify local/remote equality and clean worktree, then stop with:

```text
REVIEW REQUEST: A1.2 C2D-C R6-A Exact Deny Authority — <implementation SHA>
```

## 6. R6-B context — prohibited until R6-A acceptance

After independent R6-A acceptance, a separate packet will finish lifecycle and
cwd ownership:

- freeze validated cwd with the joined deferred identity, not a mutable runtime
  lookup; missing cwd fails closed, never `/tmp`;
- publish joined/preserved state only at the same linearization point as final
  Store admission and active identity, with rollback for termination races;
- separate natural deferred-process reap from logical approval authority;
- Stop/Delete/expiry/replacement/shutdown clear logical authority even after
  process `exitOnce` was consumed;
- remove `SimulateGracefulExit` and drive exact `/hook` + `tool_deferred` + EOF
  through one session/process in tests;
- daemon restart restores nothing.

Do not alter lifecycle code during R6-A merely to make its future red tests pass.
