# Next Executor Handoff — A1.2 C2D Catalog P3-R1 Controlled Composition

Status: **P3 REJECTED — TEST-ONLY COMPOSITION REMEDIATION — C3D PROHIBITED**

Reviewed P3: `a95a290c05c5c49433ca983d82410fc7651c5fc8`
Accepted P2B: `d841a88b5f371b41d78e5d2958c029b7114b8d6b`
Accepted C2D-C implementation: `48ce57d81781646fc1c6c7445b5900b58fd5cef3`

The three P3 tests pass, but their catalog setup reserves an empty private catalog
ID despite claiming otherwise, and they add no catalog-bearing negative
interleavings. P3-R1 corrects the controlled tests only. Production installation,
capacity and actionability remain off.

## 1. Scope

Modify only:

- `companion-daemon/cmd/devremote/claude_delivery_composition_test.go`;
- a P3 result/contract amendment if needed.

No production file, mobile file, Store/coordinator/delivery implementation or DTO
schema may change. Do not add exports, callbacks, sleeps or live Claude turns.

## 2. Correct the catalog setup

In `catalogMakeSetup`:

1. call `ReserveIdentity` with the exact
   `claude.bash.approval_probe.v1` catalog ID, not `""`;
2. require `ReserveIdentity` returns true;
3. require `IngestObserved` returns true;
4. use the identical provider/version/session/runtime/tool/input digest in private
   identity and Store ingest;
5. keep manual `Actionable=true` strictly inside this controlled test setup;
6. require `ClaimForExecution` is granted and its binding remains the exact accepted
   runtime/action/payload binding.

The test-only actionable record does not authorize production activation. Add a
clear comment distinguishing controlled composition from production wiring.

For every DTO assertion, require the target ApprovalID is found. Check the exact
summary, state, actionability and option shape expected at that stage rather than
only asserting the catalog label is absent.

## 3. Positive controlled paths

Retain exact catalog allow and deny tests, but prove the full sequence:

```text
catalog-bearing private identity
→ catalog-bound Store record
→ claim
→ exact resume response write
→ exact provider-native witness
→ bound DeliveryReceipt
→ RecordDelivery committed once
```

Allow requires matching PostToolUse. Deny requires matching strict
permission_denials. Assert receipt binding and final committed Store state, not only
receipt outcome. The exact static summary must remain present and no raw command or
provider payload may enter the Safe DTO.

Replace the new deny-path `time.Sleep(100ms)` with a deterministic completion
channel and bounded select. A timeout is a test failure; do not use a sleep as an
ordering primitive.

## 4. Catalog-bearing negative composition

Add the smallest non-overlapping matrix proving accepted C2D-C failures remain
fail-closed when catalog metadata is present:

1. wrong provider/version/tool with the catalog ID makes `ReserveIdentity` return
   false and creates no claimable setup;
2. mutated resume `tool_input` cannot produce an accepted receipt or committed
   delivery;
3. allow response followed by a mismatched PostToolUse witness cannot commit;
4. deny response followed by a mismatched denial input/tool-use ID cannot commit;
5. stale runtime replacement between claim and response cannot accept/commit;
6. duplicate/late witness cannot create a second commit;
7. timeout/cancel/exit yields the accepted non-success outcome and no commit.

Where an accepted C2D-C composition test already proves an identical interleaving,
reuse it through a catalog-bearing setup or run it as a frozen regression; do not
copy large helper implementations. At minimum, mutated input, wrong witness and
stale runtime must be exercised with the non-empty catalog identity.

## 5. Production-off proof

Keep a real managed hook/deferred regression showing normal production observation
remains `Actionable=false` with empty options. The controlled helper is the only
place P3 may manually install actionability and delivery material. No composition
root or provider capability is activated.

Non-catalog production observation must retain the exact generic summary. A
manually actionable non-catalog fixture is not a substitute for this production-off
proof.

## 6. Gates and stop

Run:

- `gofmt` / `git diff --check`;
- P3 composition tests under `-race -count=5`;
- the frozen C2D-C allow/deny/lifecycle/timeout/replay regression subset;
- P2A/P2B focused race;
- backend build/vet and changed-file secret scan.

No live Claude, mobile or Android gate. Commit, push, verify local/remote equality
and clean worktree, then stop with:

```text
REVIEW REQUEST: A1.2 C2D Catalog P3-R1 Controlled Composition — <SHA>
```

C3D remains unauthorized until independent final C2D ACCEPT.
