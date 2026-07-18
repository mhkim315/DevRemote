# A1.2 Managed Claude Approval — Final Acceptance

Verdict: **ACCEPT**

Final reviewed repository HEAD: `33cce5743d4004da3e5cc2d6e1c50576c9068b81`

Final review date: 2026-07-18

## Accepted chain

- C1D managed observation: `4794ce7f42380c388c1a5614b4b2518bc1722870`;
- C2D closed catalog: `91160409c9fc7e41a0c60b1c97a6ec6a7c4cffb4`;
- C3D-B authenticated API/mobile evidence: `3ade9e3c49727e2b472cd9eb6924f879813ec08d`;
- C3D-C frozen implementation/gate tree:
  `995fbc5b680d3fc3cd4bcbd39c0ecb4e4b509db2`;
- final test/report correction and formatting HEAD:
  `33cce5743d4004da3e5cc2d6e1c50576c9068b81`.

The accepted scope is the single closed catalog action
`claude.bash.approval_probe.v1`. Arbitrary Bash actionability remains rejected.
C0H and C0R remain historical BLOCKED results; neither is reclassified by C0D.

## Authority and live evidence

The accepted production path preserves the provider-neutral A1 claim,
idempotency, receipt and commit authority while binding Claude's original
approval identity to one resume-attempt identity. Provider-native consumption
evidence is required before commit.

The bounded live report is
`docs/A1_2_C3D_C_EVIDENCE_REPORT.md`:

- allow: authenticated route returned accepted, Store committed `approved`,
  and the harmless probe executed exactly once (`tokenHits=1`);
- deny: authenticated route returned accepted, Store committed `rejected`,
  and the harmless probe did not execute (`tokenHits=0`);
- pinned Claude Code: `2.1.209` with the recorded artifact digest;
- both resume processes reached bounded cleanup/final-exit handling.

## Independent final checks

At final review:

- local HEAD equaled `origin/feature/phase10-multi-adapter`;
- the worktree and `git diff --check` were clean;
- accepted C3D-A, C3D-B and identity-amendment SHAs were ancestors;
- `gofmt` was clean;
- `TestDrain_*` passed five race-enabled repetitions;
- `go vet ./internal/term ./cmd/devremote` passed;
- `go test -race ./internal/term ./cmd/devremote` passed;
- the frozen full gate recorded backend full race, mobile TypeScript/Jest,
  Android Kotlin, invariant and secret-scan PASS results.

The evidence report's `Local == Remote` row names the frozen implementation gate
SHA, while this document names the final reviewed report/formatting HEAD. No
semantic production change occurred between those two points.

## Consequence

A1.2 is complete. Post-Claude PF evidence freeze may begin. This acceptance does
not itself authorize managed-only consumer migration or tmux/cmux deletion.
