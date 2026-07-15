# Next Executor Handoff — SP1 Native Approval

Status: **START WITH P1 ONLY; STOP AT EVERY CHECKPOINT**

This handoff onboards a new execution agent. Do not use summaries from a different
checkout as repository authority.

## 1. Canonical repository identity

- Repository: `https://github.com/mhkim315/DevRemote.git`
- Branch: `feature/phase10-multi-adapter`
- Canonical checkout: `/Users/mhk/Documents/codex/DevRemote`
- Accepted SP0.5 implementation: `4f9241ff263c39029a28fe3d83712f4dc4da354a`
- Accepted SP0.5 report baseline: `62652633eb56577f0308973f7581ac94498f19fe`
- The handoff/contract commit containing this document is the remote HEAD you must
  fetch and verify before work.

At startup run, without rewriting history:

```sh
cd /Users/mhk/Documents/codex/DevRemote
git remote -v
git fetch origin feature/phase10-multi-adapter
git switch feature/phase10-multi-adapter
git merge --ff-only origin/feature/phase10-multi-adapter
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git merge-base --is-ancestor 4f9241ff263c39029a28fe3d83712f4dc4da354a HEAD
git merge-base --is-ancestor 62652633eb56577f0308973f7581ac94498f19fe HEAD
git status --short
```

Stop if local and remote differ after the fast-forward, either ancestry check fails,
or the worktree is not clean. Do not work from an Antigravity/scratch checkout.

## 2. Mandatory reading order

1. `docs/SP1_NATIVE_APPROVAL_CONTRACT_NOTE.md` — corrected authority contract;
2. this handoff;
3. `docs/SP0_5_MANAGED_IO_EVIDENCE_REPORT.md`;
4. `docs/A1_1_CP0_EVIDENCE_REPORT_3.md` and current CP0 redacted evidence;
5. `companion-daemon/internal/term/approval_execution.go`;
6. `companion-daemon/internal/term/approval_store_gen.go`;
7. `companion-daemon/internal/term/approval_delivery.go`;
8. `companion-daemon/internal/term/approval_handler.go`;
9. `companion-daemon/internal/term/managed_codex.go` and managed event/API files;
10. `companion-daemon/cmd/devremote/app.go`.

The earlier draft commit `a41ebc2` is not authoritative where it conflicts with the
corrected contract.

## 3. Permanent invariants

- The pump is the sole provider stdout reader.
- Top-level JSON-RPC `id`, not `params.id`, is provider request identity.
- `waiting_approval`, PTY/screen/prompt/JSONL and generic terminal input create zero
  actionable approvals.
- Queue admission is not Codex consumption.
- No success before matching `serverRequest/resolved` observed after the exact write.
- Never infer an option from a digest or trust mobile-supplied runtime/provider data.
- No old non-actionable record is upgraded after capacity changes.
- Capacity and actionability stay zero on incomplete identity, mapping or delivery.
- Public/mobile DTOs never contain provider response bytes, raw commands, CWD,
  prompts, amendments, claim tokens or digest source material.

## 4. Execution discipline

Before each packet, write and commit a short packet contract note containing:

- authority owner and canonical inputs;
- immutable binding table;
- transition and linearization point;
- success evidence and invalidation paths;
- capacity behavior;
- one adversarial interleaving/counterexample per critical invariant;
- explicit non-goals.

Design deterministic failing tests before concurrency fixes. Use channels/barriers,
not sleeps. Freeze HEAD before the packet gate. Do not run the full repository gate
for every checkpoint: focused build/race tests are sufficient for P1/P2A/P2B; the
full build gate is mandatory at P3 finalization.

## 5. Authorized packet: P1 only

Implement only section 9/P1 of the corrected contract:

- strict structured projection of `item/commandExecution/requestApproval`;
- lossless bounded top-level request ID handling;
- exact session/epoch/thread/turn/item binding;
- exact pinned provider version and environment/fingerprint checks;
- bounded private pending-request state owned by the runtime/pump;
- safe non-actionable store/display record with no action options.

Required P1 negative evidence:

- `params.id` cannot substitute for top-level `id`;
- unknown/missing/oversized/wrong-type fields are rejected or non-actionable;
- wrong version/environment/decision fingerprint is non-actionable;
- duplicate request ID does not create a second authority record;
- old epoch, completed turn, exit and bounded-state exhaustion fail closed;
- raw command/CWD/prompt/amendment/JSON-RPC payload do not enter DTOs or logs;
- zero gate activation, zero provider response writes, zero mobile CTA.

P1 must not modify delivery capacity, produce `allow_once`/`deny` actionable options,
wire the mobile action handler, or implement provider response delivery.

Commit and push P1, report local/remote equality, ancestry and clean worktree, then
stop for independent verification. Do not begin P2A in the same session unless the
reviewer explicitly accepts P1 and authorizes continuation.

## 6. Later packets — not yet authorized

- **P2A:** exact internal delivery material plus provider write/resolved-consumption
  boundary, still non-actionable in production.
- **P2B:** atomic current-runtime capability activation and actionable ingest for new
  requests only.
- **P3:** existing authenticated mobile action path, real accept/deny turns, final
  full gate and report.

Each packet requires its own commit and independent verification. Intermediate
acceptance never means SP1 completion.

## 7. Scope exclusions

Do not start N1, A1.2/Claude, a generic provider SDK, Task/Dispatch, orchestration,
automatic approval policy, persistence/restart recovery, observer cleanup,
tmux/cmux deletion, Windows work, CLI redesign, terminal rendering or PTY approval
heuristics.

## 8. Honest stop conditions

Report **BLOCKED** and stop if any of these cannot be proven without weakening the
contract:

- exact top-level request identity or lossless response ID;
- exact current runtime/epoch binding;
- bounded safe projection;
- certified accept/decline mapping;
- write-before-resolved consumption proof;
- production actionability without queue-only success.

Unavailable, heuristic, fixture-only or test-only behavior is not production support.
