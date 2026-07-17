# Next Executor Handoff — A1.2 C2D Catalog P2A-R4 Final Mechanical Fix

Status: **P2A-R3 REJECTED — THREE MECHANICAL FIXES ONLY — P2B/P3/C3D PROHIBITED**

Reviewed R3: `da3ee9f24b55851012339a3769d23334592db1f3`
Parent handoff: `b29058d985a741ee686ca45c41f7a7648dd4aa38`

R3 correctly added a real digest selector, a production Store generation-mismatch
rejection, exact lifecycle descriptions, and stronger DTO assertions. Its focused
race suite passes. Do not redesign or add new scenarios. Close only the following.

## 1. Keep the selector private

`SelectCatalogActionID` is an exported test API even though its tests are in package
`term` and can call an unexported function. This violates the R3 constraint.

- remove the exported wrapper;
- retain one unexported `selectCatalogActionID` implementation;
- call that same unexported function from `handleHook` and its table test;
- do not add a test-only export, callback, mutable seam, or interface.

## 2. Make the capacity assertion exact

The current check compares a generated `approvalID` against the literal session value
`"sess-cap-overflow"`; it can never identify an overflow approval and is vacuous.

Before the overflow hook/join, capture exact snapshots of:

- active approval IDs;
- coordinator identity IDs/bindings already created;
- safe Store DTO IDs for the runtime.

After the overflow join, compare exact before/after sets and counts. They must be
identical. Also assert:

- the overflow pending observation remains pending at the capacity boundary and has
  not become an active approval or coordinator identity;
- termination clears that pending observation and all existing identities.

Do not infer the overflow approval ID and do not compare an ApprovalID to SessionID.
No production code change is needed for this item.

## 3. Format and gate

Run `gofmt` on every touched Go file. Current R3 has an over-indented assignment in
`claude_hook_bridge.go` and an extra blank line in `managed_claude_test.go`.

Then run only:

- `git diff --check` and empty `gofmt -d`;
- all `TestP2A_` tests under `-race -count=5`;
- `go build ./...` and `go vet ./...`;
- changed-file secret scan.

Production Store, DTO, coordinator, delivery, lifecycle, mobile and composition root
remain unchanged. No live Claude turn or broad mobile/Android gate is required.

Commit, push, confirm local/remote equality and clean worktree, then stop with:

```text
REVIEW REQUEST: A1.2 C2D Catalog P2A-R4 Final Mechanical Fix — <SHA>
```

P2B remains unauthorized until independent P2A ACCEPT.
