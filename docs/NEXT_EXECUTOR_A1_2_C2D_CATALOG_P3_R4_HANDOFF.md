# Next Executor Handoff — A1.2 C2D Catalog P3-R4 Exact Assertion Replacement

Status: **P3-R3 REJECTED — EXACT ASSERTION REPLACEMENT ONLY — C3D PROHIBITED**

Reviewed R3: `393eed83852b92baeb069505ec06810576bc15ea`
Parent handoff: `a3516d7b6f59d391bf53c805510e52dc279525f8`

R3 tests pass but again compare only selected binding fields and only check
`Committed=false` on failures. R4 must use the exact conditions below. Do not
reinterpret them, add scenarios, or modify production.

## 1. Scope

Modify only
`companion-daemon/cmd/devremote/claude_delivery_composition_test.go`.

No production/mobile/docs-result changes, new helpers outside this test file,
exports, callbacks, sleeps or live turns.

## 2. Replace partial positive binding checks

In both catalog allow and deny, delete the Adapter/Version/OptionID/digest-nonempty
partial comparisons and use all of these exact assertions:

```go
if receipt.ClaimToken != claim.Token {
    t.Fatal("receipt claim token mismatch")
}
if receipt.Binding != claim.Binding {
    t.Fatalf("receipt binding mismatch: got=%+v want=%+v", receipt.Binding, claim.Binding)
}
if receipt.ReceiptID == "" {
    t.Fatal("receipt ID empty")
}
if receipt.DeliveredPayloadDigest != claim.Binding.PayloadDigest {
    t.Fatal("delivered payload digest mismatch")
}
```

`ApprovalExecutionBinding` is comparable. Do not replace equality with non-empty or
subset checks.

Capture the first commit result and require:

```go
commit.Committed == true
commit.State == term.ApprovalApproved // allow
commit.State == term.ApprovalRejected // deny
```

The second `RecordDelivery(receipt)` must have `Committed=false` and the Safe DTO
must still have the same terminal state and exact catalog summary. Marshal each DTO
and reject the raw catalog command and both encoded provider response payloads.

## 3. Complete mutated-input terminal assertions

After `commit := store.RecordDelivery(receipt)`, require exactly:

```go
commit.Committed == false
commit.State == term.ApprovalDeliveryFailed
```

Find the target Safe DTO and require public state `delivery_failed`, exact catalog
summary, and no raw payload. Capture `posttoolURL` before sending the mutated resume.
After the failed commit, send the later correct PostToolUse witness through that URL.
Then require:

- repeating `RecordDelivery(receipt)` remains non-committing;
- the target DTO remains `delivery_failed`;
- no second delivery attempt produces `DeliveryAccepted`.

No sleep.

## 4. Complete stale-runtime terminal assertions

Keep the checked production `Stop` call. For its receipt commit require:

```go
commit.Committed == false
commit.State == term.ApprovalDeliveryFailed
```

Require a repeated submission remains non-committing. Find the exact Safe DTO and
assert `delivery_failed`; it may retain the catalog summary as display evidence but
must not show approved/rejected/resolved. Do not leave Store/DTO variables assigned
to `_` instead of asserting them.

## 5. Gate and stop

Run `gofmt`, `git diff --check`, catalog P3 race `-count=5`, frozen C2D-C regression,
P2A/P2B focused race, backend build/vet and changed-file secret scan. No live/mobile/
Android gate.

Commit, push, confirm equality and clean worktree, then stop with:

```text
REVIEW REQUEST: A1.2 C2D Catalog P3-R4 Exact Assertions — <SHA>
```

C3D remains unauthorized until independent final C2D ACCEPT.
