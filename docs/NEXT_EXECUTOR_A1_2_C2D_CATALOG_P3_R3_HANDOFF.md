# Next Executor Handoff — A1.2 C2D Catalog P3-R3 Assertion Completion

Status: **P3-R2 REJECTED — ASSERTIONS ONLY — C3D PROHIBITED**

Reviewed R2: `ea3b559f0e7e85637f267eb953f3ccc904080a8e`
Parent handoff: `18a95297121e0132cd74c0e6796605ae526778b1`
Accepted P2B: `d841a88b5f371b41d78e5d2958c029b7114b8d6b`

R2 added both required catalog-bearing negative scenarios and its focused race suite
passes. It stops before submitting failure receipts to the Store and compares only a
subset of the positive immutable binding. R3 adds assertions to the existing tests;
do not add scenarios or change production.

## 1. Scope

Modify only
`companion-daemon/cmd/devremote/claude_delivery_composition_test.go`.

No production/mobile changes, new exports, callbacks, sleeps, live turns, fixtures
or test files.

## 2. Positive receipt helper and final state

Add a test-only helper used by catalog allow and deny. Because
`ApprovalExecutionBinding` contains only comparable value fields, compare the whole
binding directly:

```go
if receipt.Binding != claim.Binding { ... }
```

Also require:

- `receipt.ClaimToken == claim.Token`;
- `receipt.ReceiptID != ""`;
- `receipt.DeliveredPayloadDigest == claim.Binding.PayloadDigest`;
- first `RecordDelivery` returns `Committed=true` and the exact terminal state
  (`approved` for allow, `rejected` for deny);
- second `RecordDelivery` returns `Committed=false` and cannot alter that state;
- exact Safe DTO is found after commit with the expected terminal public state,
  catalog summary, and no raw command/provider response bytes in any marshalled DTO
  field.

Do not retain the current partial Adapter/OptionID-only comparison.

## 3. Mutated resume failure commit

After the mutated-input delivery returns its non-success receipt:

- call `RecordDelivery(receipt)`;
- require `Committed=false` and `State==ApprovalDeliveryFailed`;
- require the exact Safe DTO exists with public state `delivery_failed`;
- send the later correct PostToolUse witness through the already captured hook URL;
- require it cannot create a new accepted receipt or change the Store state;
- a repeated `RecordDelivery` remains non-committing.

Use bounded existing HTTP/delivery deadlines, not sleep.

## 4. Stale runtime failure commit

Require the production `Stop` call returns nil. After delivery returns:

- require outcome is non-success;
- submit the receipt to `RecordDelivery` and require `Committed=false` with
  `State==ApprovalDeliveryFailed` (or the exact accepted stale-runtime terminal state
  returned by the frozen Store contract);
- repeat submission and prove no commit/state restoration;
- find the Safe DTO and require no successful terminal state;
- prove catalog summary remains display-only and does not affect stale rejection.

Do not mutate runtime or Store internals directly.

## 5. Gate and stop

Run `gofmt`, `git diff --check`, catalog P3 race `-count=5`, frozen C2D-C regression,
P2A/P2B focused race, backend build/vet and changed-file secret scan. No live/mobile/
Android gate.

Commit, push, confirm equality and clean worktree, then stop with:

```text
REVIEW REQUEST: A1.2 C2D Catalog P3-R3 Assertion Completion — <SHA>
```

C3D remains unauthorized until independent final C2D ACCEPT.
