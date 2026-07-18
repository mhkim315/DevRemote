# PF — Post-Claude Accepted State Freeze

Status: **ACCEPT**

This document contains the complete ledger required by section 1 of the POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md.

## 1. Frozen State and Repository HEADs

- **Frozen implementation / rollback HEAD**: `cc86e36556754c0973f457ff202127fd282748bb`
- **PF document commit**: The enclosing Git commit reported in the REVIEW REQUEST (intentionally not embedded to avoid self-referential SHA drift)
- **Remote branch HEAD**: Equal to local HEAD (`feature/phase10-multi-adapter`)
- **Worktree**: clean

## 2. Accepted Evidence Anchors

| Artifact | SHA | Reference Document |
| --- | --- | --- |
| Accepted Codex implementation | `dd6d05c8ba0f92af196374226c01d72ce3ef6440` | `docs/SP1_P3_EVIDENCE_REPORT.md` |
| Accepted Codex final report | `2b940a6fce6e878ffa0da17b5df4d39438af144d` | `docs/SP1_NATIVE_APPROVAL_FINAL_ACCEPTANCE.md` |
| Accepted Claude implementation | `33cce5743d4004da3e5cc2d6e1c50576c9068b81` | `docs/A1_2_FINAL_ACCEPTANCE.md` |
| Accepted mobile allow/deny | `3ade9e3c49727e2b472cd9eb6924f879813ec08d` | `docs/A1_2_FINAL_ACCEPTANCE.md` / `docs/A1_2_C3D_C_LIVE_PACKET_CONTRACT_NOTE.md` |

## 3. Bounded Live Evidence Paths

- **Codex live allow/deny/exit**: `docs/SP1_P3_EVIDENCE_REPORT.md` (Pinned version: 0.144.1)
- **Claude live allow/deny/exit**: `docs/A1_2_C3D_C_EVIDENCE_REPORT.md` (Pinned version: `2.1.209 (Claude Code)`)

## 4. Authoritative Repository Gate Results

All gates were successfully executed on the frozen implementation HEAD `cc86e36556754c0973f457ff202127fd282748bb` using `sh scripts/build-gate.sh`.

| Gate | Execution Command | Result |
| --- | --- | --- |
| Backend build/vet | `go build ./...`, `go vet ./...` | PASS |
| Backend full race | `go test -race ./... -count=1` | PASS |
| Clean diff | `git diff --check` | PASS |
| Mobile TypeScript | `npm run typecheck` | PASS |
| Mobile Jest | `npm test -- --ci` | PASS |
| Android native gate | `./gradlew :pokit-device-key:compileReleaseKotlin` | BUILD SUCCESSFUL (not skipped) |
| Invariant scans | Vendor branch scan, ID inference scan | PASS |
| Secret scan | Canonical secret scan | PASS |

## 5. Ancestry Checks
The following accepted SHAs are verified as ancestors of the frozen implementation HEAD `cc86e36556754c0973f457ff202127fd282748bb`:
- `2b940a6fce6e878ffa0da17b5df4d39438af144d` (Codex final report)
- `4794ce7f42380c388c1a5614b4b2518bc1722870` (Claude C1D)
- `91160409c9fc7e41a0c60b1c97a6ec6a7c4cffb4` (Claude C2D)
- `3ade9e3c49727e2b472cd9eb6924f879813ec08d` (Claude C3D-B/mobile)
- `33cce5743d4004da3e5cc2d6e1c50576c9068b81` (Claude final ACCEPT)

## Exact rollback SHA for PA0/PA1
Rollback SHA: `cc86e36556754c0973f457ff202127fd282748bb`

## Stop condition
The exact implementation tree was frozen and verified. This document is committed separately and awaits independent PF review.
