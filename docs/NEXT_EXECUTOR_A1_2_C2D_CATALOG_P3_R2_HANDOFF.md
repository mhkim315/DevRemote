# Next Executor Handoff — A1.2 C2D Catalog P3-R2 Final Evidence Gaps

Status: **P3-R1 REJECTED — TWO NEGATIVE CASES + FINAL ASSERTIONS ONLY — C3D PROHIBITED**

Reviewed R1: `490246b6e3528f62b0f88a9280b4ee244d50f842`
Parent handoff: `60ec3568175af14f248197fd319153ad18000154`
Accepted P2B: `d841a88b5f371b41d78e5d2958c029b7114b8d6b`

R1 correctly installs the non-empty catalog ID in the private coordinator identity,
checks setup transitions, removes the new deny sleep, and adds catalog-bearing
wrong-witness/timeout tests. Two required catalog-bearing interleavings and positive
terminal assertions remain. Do not add another broad matrix.

## 1. Scope

Modify only
`companion-daemon/cmd/devremote/claude_delivery_composition_test.go`.

No production, mobile, Store, DTO, coordinator, delivery or lifecycle changes. No
new exported helpers, callbacks, sleeps or live Claude turns.

## 2. Positive allow and deny completion assertions

For both catalog allow and catalog deny:

- require the returned receipt is bound to the exact claim token, ApprovalID,
  SessionID, RuntimeRef, action digest/payload digest and option/decision binding
  exposed by the existing receipt contract;
- call `RecordDelivery` exactly once and require `Committed=true`;
- a second `RecordDelivery` or equivalent replay must not create a second commit;
- find the exact Safe DTO after commit and assert its final state, exact catalog
  summary, and absence of raw command/provider payload;
- use a `found` assertion so an empty DTO list cannot pass.

Reuse existing receipt-binding assertions/helpers where available. Do not expose new
production fields solely for this test.

## 3. Catalog-bearing mutated resume input

Using `catalogMakeSetup`, start delivery for either option and POST a resume hook with
the same session/tool-use/tool name but a one-byte-mutated `tool_input` command.

Assert:

- the certified allow/deny response is not accepted for the mismatched invocation;
- delivery ends in the existing non-success outcome after the bounded poll deadline;
- `RecordDelivery` cannot commit;
- the Store record does not reach the successful delivered state;
- no later correct witness can retroactively turn that failed attempt into a commit.

Use the existing delivery timeout/channel behavior; no sleep.

## 4. Catalog-bearing stale runtime

Using `catalogMakeSetup`, claim the decision and then invalidate the exact runtime
through the production-owned lifecycle boundary (`Stop`, `Kill`, or the existing
runtime-generation invalidation used by accepted C2D-C tests) before the resume
response can be accepted.

Assert:

- lifecycle operation succeeds;
- stale resume/witness cannot yield `DeliveryAccepted`;
- zero successful Store commit;
- late witness/response cannot restore the claim;
- catalog summary remains display evidence only and does not bypass runtime
  invalidation.

Do not simulate staleness by mutating private fields directly.

## 5. Frozen regression and production-off proof

Run, rather than duplicate, the accepted C2D-C mutated denial, timeout, replay and
lifecycle tests. Also run the accepted P2A/P2B production hook test proving ordinary
managed observation remains non-actionable with empty options. Since P3 changes only
tests, no production capability or composition-root installation may appear in the
diff.

## 6. Gates and stop

Run:

- `gofmt` / `git diff --check`;
- all catalog P3 tests under `-race -count=5`;
- frozen C2D-C delivery/lifecycle/replay subset;
- P2A/P2B focused race;
- backend build/vet and changed-file secret scan.

Commit, push, confirm local/remote equality and clean worktree, then stop with:

```text
REVIEW REQUEST: A1.2 C2D Catalog P3-R2 Final Evidence — <SHA>
```

C3D remains unauthorized until independent final C2D ACCEPT.
