# Next Executor Handoff — A1.2 C2D Catalog P2A-R3 Test Integrity

Status: **P2A-R2 REJECTED — R3 TEST-ONLY REMEDIATION — P2B/P3/C3D PROHIBITED**

Reviewed implementation: `fa992cc753c1b1b63b6ee0c96793c6a720925658`
Accepted production implementation under review: `11656a5330c8534fc727c12bbfc76b0243d647d5`

P2A-R2 added the requested test names and its focused race suite passes, but three
tests do not create the failure condition they claim to prove. R3 is a narrow test
integrity packet. Do not redesign the catalog, runtime, Store, DTO, delivery, mobile,
or lifecycle contracts.

## 1. Startup and scope

Use `/Users/mhk/Documents/codex/DevRemote` on
`feature/phase10-multi-adapter`. Fetch and fast-forward, then require local/remote
equality, both SHAs above as ancestors, and a clean worktree.

Authorized files:

- `companion-daemon/internal/term/managed_claude_test.go`;
- `companion-daemon/internal/term/claude_hook_bridge.go` only if a small pure
  digest-selection helper is necessary to test the existing cross-check;
- the corresponding focused test file if moving that pure helper test makes the
  production-path intent clearer;
- this handoff may receive a result/status amendment.

No new test callback, channel, sleep, exported test API, raw provider field, or
production capability is authorized.

## 2. R3-A — non-vacuous digest mismatch

Current `TestP2A_DigestMismatchDoesNotStoreCatalogID` executes only the successful
matching path. Its negative half consists of comments and never supplies unequal
digests to the selection condition.

Make the existing decision independently testable with the smallest pure internal
helper, for example an equivalent of:

```text
selectCatalogActionID(id, classifierDigest, bridgeDigest, matched) string
```

The production `handleHook` must call the same helper. It returns the ID only when
`matched` is true, the ID is non-empty, and both canonical digests are equal.

Required assertions:

1. matched + same digest returns the exact catalog ID;
2. matched + different digest returns empty;
3. unmatched + same digest returns empty;
4. empty ID returns empty;
5. the real HTTP hook positive path still carries the ID through deferred join.

Do not add a mutable seam that changes classifier output in production.

## 3. R3-B — capacity and Store rejection intermediate state

### Capacity

The current assertion `IdentityCount() > maxActiveApprovals` can pass even if the
overflow attempt incorrectly replaces or adds an identity within the bound.

Before the overflow join, capture:

- exact coordinator identity count;
- stable identity keys or exact known existing identities;
- overflow pending observation and its catalog ID.

After the overflow join, assert:

- count is exactly unchanged;
- every prior identity is unchanged;
- no identity exists for the overflow attempt;
- no Store record or active approval was created for the overflow attempt;
- the later cleanup removes the pending catalog metadata.

### Store rejection

Do not simulate failure by assigning `svc.approvals = nil` and `rt.approvals = nil`
after a production runtime is configured. That state is prohibited by the immutable
service composition contract.

Cause the real `AuthoritativeApprovalStore.IngestObserved` call to reject using a
valid production condition, preferably a current-generation mismatch or an existing
bounded-capacity condition. Then assert the already-reserved catalog-bearing private
identity is rolled back, no active approval is installed, and no safe DTO appears.

## 4. R3-C — truthful lifecycle and privacy assertions

Keep the existing Stop/Kill/Delete/exit/epoch tests, but make their claims exact:

- check every returned error;
- Delete may prove that a previously cleared identity is not restored, because the
  accepted lifecycle requires terminal state before Delete; do not claim Delete was
  the original clearing linearization point;
- no-join exit proves pending catalog metadata is cleared and no identity is ever
  created; do not call it cleanup of a previously catalog-bearing identity;
- epoch replacement must exercise the production generation transition when one is
  available. If no production same-session replacement entry point exists, state
  that limitation and verify the deepest production-owned `ClearRuntime` boundary
  without describing the manual setup as a complete replacement E2E.

For the privacy test, require the target DTO to be found before accepting its
summary assertion. Check the exact generic summary, empty options, and
`Actionable=false`. Retained runtime/coordinator fields must exclude command,
description, token and path sentinels. If this path emits no application log or
failure text, document that confirmed behavior rather than inventing a logging seam.

## 5. Gates and stop

Run only:

- `gofmt` / `git diff --check`;
- all `TestP2A_` tests under `-race`, repeated at least five times;
- the directly affected Claude hook/runtime/coordinator lifecycle subset;
- `go build ./...` and `go vet ./...`;
- changed-file secret scan.

Do not run live Claude turns, mobile, Android, or unrelated full-repository gates.
Freeze the implementation SHA, commit, push, confirm local/remote equality and a
clean worktree, then stop with:

```text
REVIEW REQUEST: A1.2 C2D Catalog P2A-R3 Test Integrity — <implementation SHA>
```

P2B remains unauthorized until independent P2A ACCEPT.
