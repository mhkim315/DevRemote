# PF — Post-Claude Accepted State Freeze

Status: **REVIEW REQUEST**

This document contains the complete ledger required by section 1 of the POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md.

| Evidence | Recorded value |
| --- | --- |
| Canonical repository HEAD | `cc86e36556754c0973f457ff202127fd282748bb` |
| Remote branch HEAD | `cc86e36556754c0973f457ff202127fd282748bb` |
| Worktree | clean |
| Accepted Codex implementation | `2b940a6fce6e878ffa0da17b5df4d39438af144d` (Document: `docs/SP1_P3_EVIDENCE_REPORT.md` / `docs/SP1_NATIVE_APPROVAL_FINAL_ACCEPTANCE.md`) |
| Accepted Claude implementation | `33cce5743d4004da3e5cc2d6e1c50576c9068b81` (Document: `docs/A1_2_FINAL_ACCEPTANCE.md`) |
| Accepted mobile allow/deny | `3ade9e3c49727e2b472cd9eb6924f879813ec08d` (Document: `docs/NEXT_EXECUTOR_A1_2_C3D_B_R4_HANDOFF.md`) |
| Codex live allow/deny/exit | `docs/SP1_P3_EVIDENCE_REPORT.md` (Pinned version: 0.144.1) |
| Claude live allow/deny/exit | `docs/A1_2_C3D_C_EVIDENCE_REPORT.md` (Pinned version: `2.1.209 (Claude Code)`) |
| Backend full race | PASS on frozen HEAD |
| Mobile TypeScript/Jest | PASS on frozen HEAD |
| Android native gate | PASS (`android module kotlin compile ... OK`), not skipped |
| Invariant/secret scan | PASS on frozen HEAD |

## Ancestry checks
- `2b940a6fce6e878ffa0da17b5df4d39438af144d` is ancestor: PASS
- `4794ce7f42380c388c1a5614b4b2518bc1722870` is ancestor: PASS
- `91160409c9fc7e41a0c60b1c97a6ec6a7c4cffb4` is ancestor: PASS
- `3ade9e3c49727e2b472cd9eb6924f879813ec08d` is ancestor: PASS
- `33cce5743d4004da3e5cc2d6e1c50576c9068b81` is ancestor: PASS

## Exact rollback SHA for PA0/PA1
Rollback SHA: `cc86e36556754c0973f457ff202127fd282748bb`

## Stop condition
The exact implementation tree was frozen and verified. This document is committed separately and awaits independent PF review.
