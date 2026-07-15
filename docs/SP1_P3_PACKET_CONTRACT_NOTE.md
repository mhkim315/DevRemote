# SP1 P3 — Packet Contract Note (authenticated mobile path + live production proof)

Status: **PACKET CONTRACT — P3 ONLY, the FINAL SP1 packet. Only independent
P3/full-contract review may ACCEPT SP1/A1.1.**

Baseline: `190f699eefe1346029b96726c1d7618f291a8811` (P2B + R1 independently
ACCEPTED). Executor: SP1 execution agent, canonical checkout
`/Users/mhk/Documents/codex/DevRemote`, branch `feature/phase10-multi-adapter`.

## 1. Scope and authority

- **No new mobile code**: the mobile client already consumes exactly the
  contract §8 surface — the ApprovalStore-backed `SafeApproval` DTO from
  `/api/sessions` (A1-E strict decoder, `actionableApprovals` validation,
  `ApprovalCard`) and `resolveApproval` POSTing only
  `{action, input?, idempotencyKey}` to
  `/api/sessions/{id}/approvals/{approvalId}` over the host-bound
  authenticated transport. Managed rows now carry that DTO (P1/P2B), so the
  existing UI renders the CTA with Pokit-owned labels. P3 adds NO second DTO,
  action list, or CTA authority.
- **Live proof harness**: one env-gated test file
  (`POKIT_SP1_LIVE=1`, cmd/devremote) driving the REAL production
  composition: `NewAppWithDeps(EnableManagedCodex)` with NO injected service
  — the pinned `PinnedConfig0x144` identity (fail-closed Verify: version,
  realpath, shim+native sha256), the real exec launcher, the atomic P2B
  activation, the real IPC `pokit run codex` structured create, and the real
  authenticated REST routes. Ambient `~/.codex` auth only; no credential
  copies; session CWD is a private temp dir removed afterwards.

## 2. Mandatory live evidence (reviewer-mandated closure)

For EACH of accept and deny, on a real pinned-0.144.1 managed turn whose
prompt requests a file-writing shell command (untrusted approval policy +
read-only sandbox force `item/commandExecution/requestApproval` — the CP0
recipe):

1. the actionable safe DTO (exactly `allow_once`/`deny`) appears on the
   authenticated `/api/sessions` read;
2. the authenticated action POST returns success ONLY after the daemon wrote
   the exact decision response AND the pump observed the matching
   `serverRequest/resolved` strictly after the write (the P2A
   `resolved-after-written` success witness — HTTP 200 with
   outcome accepted IS that witness, since no other path can commit);
3. the store commits `approved` (allow_once) / `rejected` (deny);
4. provider consumption is corroborated: the probe file EXISTS after
   allow_once and does NOT exist after deny (command execution /
   non-execution);
5. duplicate tap: a second POST with the SAME idempotency key replays
   `already_accepted` without a second provider write; a later action on the
   resolved record is refused;
6. stale epoch / deletion: after stop/kill the action fails closed
   (`stale_runtime`); after delete the record is gone (`not_found`);
7. DTO privacy: the full authenticated `/api/sessions` body contains no
   command text, cwd, amendment, decision/schema material, or raw JSON-RPC.

**Honest stop condition (reviewer-mandated)**: if the real provider
consistently resolves in the during-write window (ambiguous conflict) and no
resolved-after-written success can be observed for accept AND deny, the
positive production path is NOT proven and SP1 must be reported **BLOCKED** —
safety alone is not acceptance.

## 3. Final gate (frozen final HEAD)

After the live evidence lands, the FULL repository gate runs once on the
frozen final HEAD (`sh scripts/build-gate.sh`): backend build/vet/race
tests/format, mobile TypeScript + Jest (+ Android compile per the gate
script), vendor-branch and ID-inference invariant scans, and the secret
scan. The final evidence report commit is docs-only on top of that verified
tree.

## 4. Non-goals

No new mobile UI, no notification work, no persistence/restart recovery, no
N1/A1.2/CP1, no automatic policy, no provider SDK, no Windows. The physical
mobile device smoke stays out of scope (the authenticated transport and
decoder are proven by the existing A1-E/SP0.5 suites); this follows the
accepted SP0.5 precedent.

## 5. Adversarial notes

- The live harness never trusts silence: each step has a bounded poll with an
  explicit failure, and deny's non-execution is asserted only AFTER the
  provider's turn completed (absence-of-file at turn end, not mid-turn).
- The probe command and CWD are unique per run; cleanup removes the temp dir
  and stops the session; the harness fails loudly if a child survives.
- All live outputs quoted in the evidence report are bounded and contain no
  home paths, tokens, or prompt echoes beyond the fixed probe file name.
