# A1.2 C2D-C R6-A2 Exact Deny Authority Handoff

> **Superseded:** implementation `5455e2de50a0a468299eee3e38eb8fc2a4166842`
> still substituted the stored context digest for missing provider evidence and
> failed the PostToolUse negative test. Use
> `NEXT_EXECUTOR_A1_2_C2D_C_R6_A3_HANDOFF.md`.

Status: **R6-A REJECTED — IMPLEMENT R6-A2 ONLY — R6-B/C2D-D/C3D PROHIBITED**  
Reviewed implementation: `bffeef9214add0622358a5428a05a2402db576b1`  
Accepted prerequisite: C2D-B `27bd45162feb6bdf6fec52fa44192208c8276e32`  
Canonical checkout: `/Users/mhk/Documents/codex/DevRemote`

This is the only active executor packet. Do not begin lifecycle/cwd R6-B. The
current allow path remains useful, but the deny PASS is a false green produced
by filling missing witness fields from coordinator state rather than from the
exact provider evidence.

## 1. Exact rejection findings

### B1 — `LookupClaimToken` preserves the rejected lesser-identity search

The new method searches every active entry by session/tool identity, returns
the claim token, input digest and RuntimeRef from that entry, and then calls
`MarkWitnessed`. This is not full-binding evidence validation: it discovers the
authority fields that the witness failed to prove. With competing entries, map
iteration may select the wrong owner.

Delete `LookupClaimToken`. No coordinator scan by partial provider identity may
exist in the deny path.

### B2 — denial input is not bound

`streamDenial` contains no provider tool input. `routeDenial` obtains the input
digest from the selected coordinator entry, so any denial carrying the same
session/tool ID can satisfy the stored digest. The digest must be recomputed
from the exact bounded tool input in the provider event.

### B3 — decoder is not strict enough or evidence-backed

`DisallowUnknownFields` does not reject duplicate known JSON keys. The decoder
also accepts a synthetic reduced top-level shape, does not require the exact
result/stop discriminator, and has no tool-input field/digest validation. No
new decoder tests or retained real-event fixture were committed.

If the retained accepted C0D evidence does not establish the complete raw
field vocabulary emitted by pinned Claude 2.1.209, stop BLOCKED and obtain a
new bounded/redacted non-actionable schema trace. Do not make a synthetic test
shape the production contract.

### B4 — PostToolUse false-deny remains open

`TestClaudeDelivery_DenyPostToolUseCannotCommit` fails because `handlePostTool`
still turns a PostToolUse hook into a denial witness when the stored decision is
deny. A red negative test is an open blocker, not an expected final result.

### B5 — mandatory note and adversarial tests were omitted

The commit changes only two production files. It does not amend the C2D contract
note and adds none of the R6-A cross-claim, malformed, duplicate, wrong-runtime,
wrong-input or ordering tests.

## 2. Pre-implementation contract note

Before production changes, amend `docs/A1_2_C2D_PACKET_CONTRACT_NOTE.md` with
this single allowed deny path:

```text
exact resumed process
  → exact stdout reader owned by that resume attempt
  → evidence-backed closed denial decoder
  → provider event session/tool/tool-input digest
  → immutable resumeContext already owned by that runtime
  → exact equality against resumeContext
  → coordinator.MarkWitnessed(
        resumeContext.claimToken,
        WitnessPermissionDenials,
        event.sessionID,
        event.toolUseID,
        event.toolName,
        event.inputDigest,
        resumeContext.originalRuntime)
```

No map scan, claim lookup, runtime lookup, Store lookup, FIFO assumption or
entry-derived witness field is permitted.

## 3. R6-A2 implementation

### A2-1 — immutable context on the exact runtime

- Add a private immutable resume-context snapshot to the resumed
  `claudeManagedRuntime` before `pump()` starts.
- Copy strings and RuntimeRef by value. Do not retain a caller-mutable object.
- Observation runtimes have no resume context and cannot route denial success.
- The pump reads its own context once; it never asks the coordinator who owns an
  event.

### A2-2 — closed denial decoder

- Freeze the complete real pinned-provider event allowlist from accepted
  evidence.
- Use token-level object parsing or an equivalent duplicate-key check at every
  authority-bearing object.
- Reject unknown/duplicate/trailing/wrong-type/non-UTF-8/over-bound data.
- Enforce bounded denial count and byte bounds for session ID, tool ID, tool
  name and tool input.
- Require the exact provider denial discriminator.
- Canonicalize the actual event `tool_input` and compute its SHA-256 digest.
- Reject the whole event if any authority-bearing denial entry is malformed or
  if multiple entries ambiguously target the bound invocation.

The redacted evidence field `input_sha256` is not a provider wire field and must
not be decoded as one.

### A2-3 — exact asymmetric routing

- For deny, require `resumeContext.expectedDecision == "deny"` and exact equality
  of event session ID, tool ID, tool name and computed input digest.
- Call only the existing full-binding `MarkWitnessed` with context claim token
  and original RuntimeRef.
- Delete `LookupClaimToken` and any replacement partial-identity lookup.
- `handlePostTool` always represents PostToolUse. It may complete only an allow
  entry; for deny it returns without witness success.

## 4. Mandatory non-vacuous tests

Add tests before claiming completion:

1. retained real bounded denial fixture decodes;
2. exact deny via resumed stdout → accepted receipt → Store committed;
3. deny plus PostToolUse → test PASS with no accepted receipt and no commit;
4. wrong/missing tool input and mismatched computed digest → rejected;
5. wrong session/tool ID/tool name/RuntimeRef/claim context → rejected;
6. duplicate known keys at top level and denial entry → rejected;
7. unknown, trailing, malformed, wrong-type, invalid UTF-8 and every bound+1 →
   rejected;
8. witness before confirmed write, duplicate and late witness → rejected;
9. two entries sharing lesser identity fields cannot complete each other;
10. observation runtime without resume context cannot route denial;
11. existing allow composition remains accepted and committed.

Composition tests must inject through the exact resumed stdout. Direct
coordinator calls are allowed only for coordinator unit tests, never as
production composition proof. Use barriers/channels, not sleeps.

## 5. Gate and stop

Freeze the exact implementation HEAD and run:

```sh
cd companion-daemon
go test -race ./internal/term -run 'Claude.*(Denial|Witness|Resume|Delivery)' -count=10
go test -race ./cmd/devremote -run 'ClaudeDelivery_(CompositionAllowAccepted|CompositionDenyAccepted|DenyPostToolUseCannotCommit)' -count=5 -v
go test -race ./internal/term ./cmd/devremote -count=1
go build ./...
go vet ./...
git diff --check
```

All three named composition tests must PASS. “RED expected” is not completion.
Commit, push, verify local/remote equality and clean worktree, then stop with:

```text
REVIEW REQUEST: A1.2 C2D-C R6-A2 Exact Deny Authority — <implementation SHA>
```

Do not start R6-B until independent R6-A2 ACCEPT.
