# Next Executor Handoff — A1.2 C2D Catalog P2B-R1 Bound Display Metadata

Status: **P2B REJECTED — R1 BINDING REMEDIATION ONLY — P3/C3D PROHIBITED**

Reviewed P2B: `11c34897ccf960810190cb5d99a5fb1c2c128d95`
Accepted P2A: `f93e81ef126c2f8f6e1013697a66ca363a442cf3`

The normal Claude catalog path projects the intended static label and all focused
race tests pass. The remaining defect is that the Store and DTO projector trust a
catalog ID independently of provider/version. A valid Claude ID supplied on another
provider/version record therefore selects a Claude label. P2B-R1 closes only this
display-metadata binding; it does not add actionability or delivery authority.

## 1. Frozen behavior

The safe-review tuple is exactly:

```text
CatalogActionID + Provider + Version
```

All three values must match one compiled catalog entry. Exact match produces the
Pokit-owned static summary. Empty, unknown, malformed, over-bound, wrong-provider or
wrong-version metadata is rejected as safe-review metadata and normalized to empty.
The approval observation itself remains admitted and uses the existing generic
summary. Invalid display metadata must not advance actionability, add options, or
reject an otherwise valid non-actionable observation.

## 2. Deep-boundary validation

Add one internal value-copy lookup/resolver accepting all three tuple fields. It must
perform exact comparisons without truncation or prefix matching. Reuse the compiled
catalog; do not create a second mapping.

At `AuthoritativeApprovalStore.ingest`:

- validate the optional tuple before building `approvalRecord`;
- store the ID only when the exact tuple is certified;
- otherwise store an empty ID;
- do not use `boundStr` to transform an invalid ID into a different identifier;
- preserve the existing first-record/idempotent re-offer semantics. A later re-offer
  must not mutate an existing record's display identity.

At `projectSafeApproval`, resolve the summary using the record's provider, version
and ID again as defense in depth. Invalid tuples return the existing generic summary.

`catalogActionID` remains a private `approvalRecord` field. Remove it from exported
`ApprovalSnapshot` unless an existing production consumer is demonstrated; P2B has
no need to expose it through snapshot/pre-validation APIs.

## 3. Required non-vacuous tests

Use direct Store ingestion for adversarial caller cases and the real Claude hook for
the positive path.

1. exact Claude provider/version/ID → exact static summary;
2. same ID + wrong provider → generic summary;
3. same ID + wrong version → generic summary;
4. empty, unknown, over-bound and non-printable ID → generic summary;
5. invalid tuple is normalized internally and never appears in snapshot/DTO;
6. valid observation remains non-actionable with empty options;
7. idempotent re-offer with a changed ID/tuple cannot mutate the first record;
8. stale-generation ingest cannot replace display metadata;
9. raw command, description, token and path sentinels remain absent from retained
   Store fields and every Safe DTO field;
10. accepted P2A production hook and cleanup tests remain green.

Include a direct counterexample test that would fail on reviewed P2B: ingest a record
with provider/version not matching the Claude catalog but with
`claude.bash.approval_probe.v1`; its summary must remain generic.

## 4. Scope and gates

Authorized production files:

- `internal/term/claude_catalog_classifier.go` for the tuple resolver;
- `internal/term/approval_store_gen.go` for validation/private storage;
- `internal/term/approval_dto.go` for defense-in-depth tuple projection;
- directly corresponding tests and a short remediation amendment.

Do not modify hook/deferred identity wiring, claim, delivery, receipt, witness,
lifecycle, mobile, composition root or public DTO shape. Do not activate options or
actionability.

Run:

- `gofmt` and `git diff --check`;
- P2A/P2B plus ApprovalStore focused race, at least five repeats;
- affected-package build/vet;
- changed-file secret scan.

No live Claude turn, mobile or Android gate is required. Commit, push, verify
local/remote equality and clean worktree, then stop with:

```text
REVIEW REQUEST: A1.2 C2D Catalog P2B-R1 Bound Display Metadata — <SHA>
```

P3 remains unauthorized until independent P2B ACCEPT.
