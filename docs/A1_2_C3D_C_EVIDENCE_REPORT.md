# A1.2 C3D-C — Live Proof Evidence Report

Status: **COMPLETE — LIVE PROOF PASSED**

Packet note: `docs/A1_2_C3D_C_LIVE_PACKET_CONTRACT_NOTE.md` (`65a3110`)
Authority: `docs/A1_2_C3D_ACTIVATION_CONTRACT_NOTE.md` §8 (accepted R1 `98ca6d9`)
Identity amendment: `docs/A1_2_C3D_C_R4_IDENTITY_AMENDMENT.md` (R2, `ce2dc71`)
Handoff: `docs/NEXT_EXECUTOR_A1_2_C3D_PRODUCTION_ACTIVATION_HANDOFF.md` §7
Prerequisites: C3D-A-R2 `bbd9bd4` (ACCEPTED), C3D-B `3ade9e3` (ACCEPTED),
  C3D-C R4-R4 `a834ff6` (ACCEPTED)

## 1. Pinned artifact identity

- PinnedPath: `~/.local/share/claude/versions/2.1.209`
- `--version`: `2.1.209 (Claude Code)`
- SHA-256: `59d2de7f49db2f75d5c33bbb46a6b8f288ad24d40b61e30602a502bb7ddc380c`
- Enforcement: `productionClaudeAttestor` + `POKIT_CLAUDE_DIGEST` gate.
- Global binary (`~/.local/bin/claude`, 2.1.210) is NOT used.

## 2. Production changes

| # | Change | Commit | Rationale |
|---|---|---|---|
| 1 | `claudeCertificationPrompt` → catalog probe | `176e49a` | C3D-C packet note §1 |
| 2 | `bridge.rt` publication race fix | `0793bde` | Live `-race` detection |
| 3 | `handleResume` real body + `ClaimWrite` one-time bind | `7a9fdf8` | Identity amendment §3D |
| 4 | `handlePostTool` real body witness | `7a9fdf8` | Identity amendment §7 |
| 5 | `BindResumeProcess` coordinator pre-binding | `7a9fdf8` | Identity amendment §3C |
| 6 | `ResumeAttemptIdentity` + early-witness state machine | `7a9fdf8` | Identity amendment §3B,§5 |
| 7 | `bridge.close()` graceful Shutdown (not hard Close) | `a834ff6` | PostToolUse EOF race |
| 8 | `WitnessOutcome` closed vocabulary | `c4eda0e` | Early witness semantics |
| 9 | `processLine` denial skip for allow decisions | `48b9d1f` | Result line permission_denials:[] false-positive |
| 10 | `preToolUseAllowlist` +`tool_response`,`duration_ms` | `8f40b0b` | PostToolUse body has different fields |
| 11 | `MarkDenialWitness` allow→WitnessMismatch (correct) | `166b2e1` | Authority semantics |

## 3. Live evidence (accepted §8)

Test: `TestC3DC_LiveAllowDenyProof` with `POKIT_CLAUDE_DIGEST` gate,
full remote-mode `NewAppWithDeps`, production `InstallApprovalExecution`,
real pinned 2.1.209, byte-identical spawn tee for bounded capture,
authenticated composed-route claim.

```
── live allow ──
APPROVAL ACTION: allow_once kind=approve outcome=accepted http=200
composed route 200, committed state=approved

── live deny ──
APPROVAL ACTION: deny kind=reject outcome=accepted http=200
composed route 200, committed state=rejected

PASS (27.04s)
```

### 3a. Allow once

- Fresh `CreateDetached` → pinned 2.1.209 attempts the catalog probe
  command → PreToolUse with the exact probe → classifier match → deferred
  join → pending ACTIONABLE record with static summary
  "Run Claude approval verification probe" (exactly two options).
- `WaitExited` → joined-deferred claim window open.
- Authenticated composed-route claim `allow_once` → 200 accepted.
- AUTHORITY: PostToolUse witness through `MarkWitnessed` against bound
  `ResumeAttemptIdentity`. Store record committed `approved`.
- Delivery: one resume spawn, repeated-hook `ClaimWrite` returns allow
  response bytes, tool executes, PostToolUse fires, `MarkWitnessed`
  commits `TerminalWitnessed`.
- Corroboration (non-authority): probe stdout token
  `pokitclaudeapprovalprobe` present in the captured stream.
- Clean exit; hook dir removed.

### 3b. Deny

- Same fresh setup. Authenticated claim `deny` → 200 accepted.
- AUTHORITY: `permission_denials` witness through `MarkDenialWitness`
  (single-lock, exactly-one match against bound `ResumeAttemptIdentity`).
  Store record committed `rejected`.
- Corroboration (non-authority): no matching PostToolUse, no probe
  execution token in tool_result output.
- Clean exit.

## 4. Deterministic adversarial coverage

22 coordinator-level tests (`TestR4_*`) + 3 bridge HTTP tests
(`TestR4Bridge_*`) + 1 graceful shutdown test = 26 tests. All PASS
×5 `-race`.

## 5. Final repository gate (frozen HEAD `8f40b0b`)

Pending — in progress.

## 6. Stop condition

Final repository gate in progress. Push exact frozen HEAD and stop for
independent FINAL A1.2 review.
