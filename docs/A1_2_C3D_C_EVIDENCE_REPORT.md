# A1.2 C3D-C — Live Proof Evidence Report

Status: **DRAFT — LIVE RESULTS PENDING**

Packet note: `docs/A1_2_C3D_C_LIVE_PACKET_CONTRACT_NOTE.md` (`65a3110`)
Authority: `docs/A1_2_C3D_ACTIVATION_CONTRACT_NOTE.md` §8 (accepted R1 `98ca6d9`)
Handoff: `docs/NEXT_EXECUTOR_A1_2_C3D_PRODUCTION_ACTIVATION_HANDOFF.md` §7
Prerequisites: C3D-A-R2 `bbd9bd4` (ACCEPTED), C3D-B `3ade9e3` (ACCEPTED)

## 1. Pinned artifact identity (recorded)

- PinnedPath: `~/.local/share/claude/versions/2.1.209` (regular executable)
- `--version`: `2.1.209 (Claude Code)`
- SHA-256: `59d2de7f49db2f75d5c33bbb46a6b8f288ad24d40b61e30602a502bb7ddc380c`
- Enforcement: production `productionClaudeAttestor` (version + realpath +
  digest, fail-closed) via `NewClaudeAttestor`; the live gate additionally
  refuses to run unless `POKIT_CLAUDE_DIGEST` equals the recorded digest.
- The drifted global binary (`~/.local/bin/claude`, 2.1.210) is NOT used.

## 2. Production changes

1. `claudeCertificationPrompt` aligned to the frozen catalog probe
   (`echo pokitclaudeapprovalprobe`, catalog `claude.bash.approval_probe.v1`) —
   `176e49a`, rationale in packet note §1.

2. **Live-found production data race, fixed** (`0793bde`): the first live
   run under `-race` detected unsynchronised publication of the runtime
   pointer to the hook bridge. Fix: mutex-guarded `getRT()`/`publishRuntime(rt)`
   AFTER every field assignment. A hook arriving before publication observes
   nil and answers the safe defer. Deterministic `-race` green after fix.

3. **Live-found claim-write identity mismatch, fixed** (`1f0ad68`):
   `handleResume` passed the hook body's `tool_use_id` to `ClaimWrite`,
   which validates against the coordinator entry reserved from the ORIGINAL
   identity. Real Claude `--resume` generates a NEW `tool_use_id` every
   time — the identity's stored `tool_use_id` is authoritative. Fix:
   `ClaimWrite` called with `ctx.tool_useID` etc.

4. **Live-found witness identity mismatch, fixed** (`85e8830`): SAME
   defect in `handlePostTool` → `MarkWitnessed`. The witness was silently
   dropped (return value was never checked) and the entry timed out →
   `DeliveryConflict` (409). Fix: use context's original identity fields.

   Deterministic fake-launcher tests never caught 3 or 4 because the test
   fixtures pass the same `tool_use_id` everywhere — real Claude doesn't.

No other production backend or mobile change in this packet.

## 3. Live evidence (accepted §8)

## 3. Live evidence (accepted §8) — PENDING

- Run 1 (pre-fix): FAILED — `-race` detector fired on the publication race
  above during the allow session's `CreateDetached`; the same run's allow
  claim returned 409 `conflict` (under a detected race, downstream state is
  not interpretable; re-run after the fix is the evidence source).
- allow once: PENDING
- deny: PENDING

## 4. Deterministic negative coverage map

See packet note §4 — every §7 negative maps to named accepted tests; all ran
green inside the full `-race` suite (6 consecutive clean runs at `176e49a`
work tree: 3 single-package `./cmd/devremote`, 2 combined
`./internal/term ./cmd/devremote`, 1 initial combined re-run). One initial
combined run failed once before any of those; the failing test name was not
captured (only the tail of the log was kept) and the failure did not
reproduce in 6 subsequent attempts, including the exact same combined form.
Recorded honestly here; the final frozen-HEAD gate re-runs the full suite.

## 5. Final repository gate — PENDING

## 6. Stop condition

Stop for independent FINAL A1.2 review after §3 and §5 complete.
