# Next Executor Handoff — A1.2 C2D Catalog P2B-R3 Non-Vacuous Store Evidence

Status: **P2B-R2 REJECTED — REPLACE THREE VACUOUS TESTS ONLY — P3/C3D PROHIBITED**

Reviewed R2: `2ae0f6c3197d9899d748b54632acd4108adcf28b`
Accepted P2B production implementation: `17cb86b3842829af2f559972e32551e02d21afeb`
Parent handoff: `ff9a787bc95139149c2e2338b6b71b84e5d80c16`

Production tuple validation remains sound and unchanged. R2 tests pass, but three
tests do not execute the counterexample described in their comments. R3 replaces
those tests rather than adding more overlapping names.

## 1. Scope

Modify only `companion-daemon/internal/term/managed_claude_test.go`.

Do not modify production code, Store behavior, DTO schema, hook/runtime/coordinator,
delivery, lifecycle, mobile or composition root. Do not add test seams, sleeps,
exports or callbacks.

Tests are in package `term` and may inspect Store-private state under the Store mutex.
Add a small test helper equivalent to:

```go
func storedCatalogActionID(t *testing.T, s *AuthoritativeApprovalStore, sessionID, approvalID string) string {
    t.Helper()
    s.mu.Lock()
    defer s.mu.Unlock()
    sess := s.sessions[sessionID]
    if sess == nil || sess.records[approvalID] == nil {
        t.Fatalf("missing stored approval %s/%s", sessionID, approvalID)
    }
    return sess.records[approvalID].catalogActionID
}
```

Return a copied string while holding the lock. Do not expose this helper in
production.

## 2. Replace invalid-metadata normalization evidence

Pure `boundCatalogID` unit checks are useful but insufficient. Add one table-driven
Store-ingest test for:

- empty ID;
- unknown ID;
- over-bound ID;
- control/non-printable ID;
- valid Claude ID + wrong provider;
- valid Claude ID + wrong version.

For each case create a fresh Store/session/ApprovalID and call `IngestObserved`.
Assert all of the following, not substitutes:

1. `IngestObserved` returns true;
2. `storedCatalogActionID(...) == ""`;
3. exactly one Safe DTO exists for that ApprovalID;
4. summary equals the exact generic summary for the supplied provider;
5. `Actionable == false` and `len(Options) == 0`;
6. marshalled Safe DTO contains none of the supplied invalid ID or bounded privacy
   sentinels.

Delete or rewrite `TestP2B_RecordCatalogActionID_Empty`; DTO behavior alone does not
prove the private record was normalized because DTO projection independently
revalidates the tuple.

## 3. Replace the idempotent re-offer test

The current test says “different ID” but sends the same valid catalog ID twice.
Replace it with this exact order:

1. current generation, ApprovalID `dup-1`, `CatalogActionID=""` → admitted true;
2. capture private stored ID (must be empty) and exact generic Safe DTO;
3. same generation and identical approval shape/ApprovalID, but now supply the valid
   `claude.bash.approval_probe.v1` tuple → `IngestObserved` returns false because it is
   an idempotent re-offer, not a new record;
4. private stored ID remains empty;
5. Safe DTO remains byte-for-byte/equivalent-field generic, non-actionable and with
   empty options.

This proves a later re-offer cannot upgrade display metadata.

## 4. Replace the stale-generation test

The current test advances generation and checks state but never sends an older
catalog-bearing ingest. Replace it with:

1. install/current generation 2;
2. ingest ApprovalID `current-1` at generation 2 with empty catalog ID and confirm
   generic DTO/private empty ID;
3. call `IngestObserved` at older generation 1 with a valid Claude catalog tuple
   (same ApprovalID or a distinct `stale-1` whose absence is asserted);
4. require the older call returns false;
5. current record's private ID and Safe DTO are exactly unchanged;
6. no stale record was installed and no catalog summary appears.

Do not use `SupersedeRuntime` as a substitute for the stale ingest.

## 5. Privacy test correction

Use `storedCatalogActionID` to assert the invalid path/token sentinel was normalized
to empty inside the private record. Keep the existing DTO and snapshot non-exposure
checks. Avoid secret-shaped literal patterns in source and failure messages.

Remove the duplicated identical assertion in the older forged-provider test if it is
still present. Prefer one exact generic-summary assertion.

## 6. Gate and stop

Run:

- `gofmt` and `git diff --check`;
- P2A/P2B and ApprovalStore focused race with `-count=5`;
- affected-package build/vet;
- changed-file secret scan.

No live Claude, mobile or Android gate. Commit, push, confirm equality and clean
worktree, then stop with:

```text
REVIEW REQUEST: A1.2 C2D Catalog P2B-R3 Non-Vacuous Store Evidence — <SHA>
```

P3 remains unauthorized until independent P2B ACCEPT.
