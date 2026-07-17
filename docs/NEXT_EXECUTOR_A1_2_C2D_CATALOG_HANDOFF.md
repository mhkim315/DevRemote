# Next Executor Handoff — A1.2 C2D Closed Catalog

Status: **START P1 ONLY — P2/P3/C3D PROHIBITED — ACTIONABILITY ZERO**

This is the active handoff. The executor implements the planner-owned catalog
contract; it does not choose a projection strategy or reopen the D1 decision.

## 1. Start state and reading order

- Canonical checkout: `/Users/mhk/Documents/codex/DevRemote`
- Branch: `feature/phase10-multi-adapter`
- Accepted C2D-C R6-B: `48ce57d81781646fc1c6c7445b5900b58fd5cef3`
- Honest C2D BLOCKED record: `defeb9de2e73d8a821fc49a28a027529b05a60e3`
- This planner amendment/handoff commit must be the fetched remote HEAD.

Require local/remote equality, clean worktree and ancestry for both SHAs. Read:

1. `docs/A1_2_C2D_D_PLANNER_CATALOG_AMENDMENT.md`;
2. this handoff;
3. `docs/A1_2_C2D_D1_R1_BLOCKED.md`;
4. `docs/A1_2_C2D_PACKET_CONTRACT_NOTE.md`;
5. accepted Claude hook/coordinator code at `48ce57d`.

## 2. P1 only — strict classifier

Write the short pre-implementation contract note, then implement only a pure,
provider-private classifier for the single frozen catalog entry. P1 may touch the
smallest Claude provider file plus `_test.go`; it may not wire the classifier into
runtime observation yet.

The classifier input is the raw bounded `tool_input`, provider/version and tool
name. Output is either the exact catalog action ID or no match. It must not return
the command, description, label or partially decoded material.

Mandatory tests:

- exact object with command only;
- exact command plus bounded description;
- command mutation by one byte;
- missing/wrong-type/duplicate command;
- duplicate description;
- unknown nested field;
- non-object, trailing JSON, invalid UTF-8 and exact/over byte bounds;
- provider, version and tool mismatch;
- description containing path/token/control sentinels proves none appears in
  output, logs or failure text;
- deterministic repeated classification;
- known-bad last-key-wins control demonstrating why ordinary `json.Unmarshal`
  is insufficient.

Use immutable fixtures; no sleeps, live Claude turns, production callbacks,
exported `ForTest` methods or observation channels. Run format, diff-check, focused
tests/race and changed-file secret scan only. Commit, push and stop with:

```text
REVIEW REQUEST: A1.2 C2D Catalog P1 Strict Classifier — <implementation SHA>
```

## 3. Future packets — not authorized

P2, after independent P1 ACCEPT, carries the bounded catalog ID through private
Claude identity and the internal display-only `SafeReviewID`, with generic fallback
for every mismatch. It must prove no raw command/description survives and production
actionability stays zero.

P3, after independent P2 ACCEPT, performs final controlled composition with the
real store, delivery and witnesses. It adds no new semantics and runs the complete
gate. C3D remains a separate independently planned activation/mobile/live packet.

## 4. Immediate stop conditions

Stop without expanding scope if strict nested decoding requires changing accepted
delivery/lifecycle code, if the observed provider schema cannot match the frozen
entry, if any raw provider value must be stored/projected, or if actionability must
turn on. Report BLOCKED rather than changing the catalog or DTO.
