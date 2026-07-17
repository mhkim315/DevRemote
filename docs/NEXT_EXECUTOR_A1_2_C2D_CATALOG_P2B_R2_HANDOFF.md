# Next Executor Handoff — A1.2 C2D Catalog P2B-R2 Evidence Completion

Status: **P2B-R1 IMPLEMENTATION SOUND, EVIDENCE INCOMPLETE — TEST-ONLY R2 — P3/C3D PROHIBITED**

Reviewed R1: `17cb86b3842829af2f559972e32551e02d21afeb`
Parent handoff: `f68e1747532a40d049b6f60b54950355bb70bf22`
Accepted P2A: `f93e81ef126c2f8f6e1013697a66ca363a442cf3`

R1 correctly validates `CatalogActionID + Provider + Version` at Store ingestion,
revalidates it during DTO projection, normalizes invalid metadata to empty, removes
the field from `ApprovalSnapshot`, and passes focused race/build/vet. No production
redesign is authorized. R2 completes the missing adversarial evidence only.

## 1. Authorized scope

Modify only directly corresponding `_test.go` files. Production code changes are
prohibited unless a test exposes an actual defect, in which case stop and report the
counterexample before changing production.

Do not touch hook/deferred identity, Store state transitions, DTO schema, claim,
delivery, receipt, witness, lifecycle, mobile or composition root.

## 2. Required table-driven invalid-metadata proof

Ingest otherwise valid non-actionable records directly through the real
`AuthoritativeApprovalStore` with each of:

- empty ID;
- unknown ID;
- ID longer than the accepted coordinator/catalog bound;
- ID containing a control or non-printable byte;
- valid Claude ID with wrong provider;
- valid Claude ID with wrong version.

For every case assert:

- the approval observation is admitted;
- the stored private `approvalRecord.catalogActionID` is exactly empty;
- the Safe DTO exists and has the exact generic summary `Approval requested`;
- `Actionable=false` and options are empty;
- no supplied ID or sentinel appears in any Safe DTO field.

Remove the duplicated identical assertion in
`TestP2B_ForgedCatalogID_StoreRejects`. Its replacement must check the exact generic
summary rather than merely checking that it differs from the catalog label.

## 3. Immutable re-offer and stale-generation proof

Add non-vacuous Store tests using the same ApprovalID:

1. first admit a generic record, then idempotently re-offer it with a valid catalog
   tuple at the same generation; the existing record must remain generic and its
   private catalog ID empty;
2. first admit a current-generation generic record, then offer an older-generation
   record carrying a valid catalog tuple; the older ingest must be rejected and must
   not change the record, DTO summary, actionability or options.

Capture the exact private record and Safe DTO before and after. Do not infer success
only from record count.

## 4. Positive and privacy controls

Keep the real Claude hook positive test proving the exact certified tuple selects
`Run Claude approval verification probe`. It must still assert non-actionable and
empty options.

Add or reuse bounded sentinels to prove no raw command, description, token or path is
stored in the approval record or exposed by any Safe DTO field. Do not add raw values
to failure messages or logs.

## 5. Gates and stop

Run only:

- `gofmt` / `git diff --check`;
- P2A/P2B and ApprovalStore focused race with `-count=5`;
- affected-package build/vet;
- changed-file secret scan.

No live Claude, mobile or Android gate is needed. Commit, push, verify local/remote
equality and clean worktree, then stop with:

```text
REVIEW REQUEST: A1.2 C2D Catalog P2B-R2 Evidence Completion — <SHA>
```

P3 remains unauthorized until independent P2B ACCEPT.
