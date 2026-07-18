# Next Executor Handoff — Post-Claude PF and PA0

Status: **PF EVIDENCE FREEZE IS NEXT; PA0 IS DOCUMENTATION-ONLY AND REQUIRES PF ACCEPT**

Intended execution agent: **Gemini 3.1 Pro**. The model change grants no extra
authority and does not change any acceptance contract.

## 1. Repository onboarding — do not assume the reviewer's local path

Canonical remote repository:

```text
https://github.com/mhkim315/DevRemote.git
```

Canonical branch:

```text
feature/phase10-multi-adapter
```

The prior reviewer used `/Users/mhk/Documents/codex/DevRemote`. Gemini may run in
a different workspace and **must not copy that absolute path into commands,
evidence or code**. Use the checkout supplied by the execution environment. If
no checkout exists, clone the canonical remote into the environment's assigned
writable workspace. After entering it, derive the repository root with:

```sh
git rev-parse --show-toplevel
```

Run every later command from that returned root or a documented subdirectory.
Do not use a scratch checkout, stale alternate clone or `/tmp` verification
worktree as the authoritative execution tree.

At startup:

1. verify `git remote get-url origin` exactly names the canonical repository;
2. fetch `origin`;
3. switch to `feature/phase10-multi-adapter`;
4. fast-forward only from `origin/feature/phase10-multi-adapter`;
5. require local HEAD == remote HEAD and an empty worktree;
6. require the accepted Codex and Claude SHAs below to be ancestors.

Never force-push, rebase accepted history, reset destructively or copy files from
another checkout. If the remote, branch, ancestry or clean-state check fails,
stop and report the mismatch.

## 2. Accepted anchors

- managed Codex final report: `2b940a6fce6e878ffa0da17b5df4d39438af144d`;
- managed Claude C1D: `4794ce7f42380c388c1a5614b4b2518bc1722870`;
- managed Claude C2D: `91160409c9fc7e41a0c60b1c97a6ec6a7c4cffb4`;
- managed Claude C3D-B/mobile: `3ade9e3c49727e2b472cd9eb6924f879813ec08d`;
- managed Claude final ACCEPT:
  `33cce5743d4004da3e5cc2d6e1c50576c9068b81`;
- current handoff: start from the current remote HEAD containing this document,
  which must be a descendant of every applicable anchor above.

## 3. Mandatory reading order

1. `docs/A1_2_FINAL_ACCEPTANCE.md`;
2. `docs/SP1_NATIVE_APPROVAL_FINAL_ACCEPTANCE.md`;
3. `docs/SP1_P3_EVIDENCE_REPORT.md`;
4. `docs/A1_2_C3D_C_EVIDENCE_REPORT.md`;
5. `docs/POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md`;
6. the current PF/PA0 rows in `docs/ROADMAP_AFTER_E10B.md`;
7. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
8. production composition and consumers named in the restructuring plan only as
   read-only audit input.

Preserve the accepted Codex and Claude runtime identity, generation, lifecycle,
approval, privacy, replay, stale-event and provider-consumption contracts.

## 4. Packet PF — accepted-state evidence freeze

PF is a documentation/evidence packet. Do not modify production, mobile or test
code. Create `docs/POST_CLAUDE_ACCEPTED_STATE_FREEZE.md` containing the complete
ledger required by section 1 of the restructuring plan:

- local and remote full HEADs and clean worktree;
- accepted Codex, Claude and mobile SHAs plus acceptance documents;
- bounded Codex and Claude allow/deny/exit evidence paths and pinned versions;
- backend build/vet/full-race result;
- mobile TypeScript/Jest result;
- Android native result, not skipped;
- invariant and secret-scan result;
- ancestry checks;
- exact rollback SHA for PA0/PA1.

Run one authoritative repository gate on a frozen implementation tree. Do not
repeatedly rerun successful suites and do not spend new live provider turns when
the accepted bounded live artifacts and ancestry already prove the result. If a
required gate result or evidence path is genuinely absent, report PF BLOCKED
instead of inventing or inferring it.

Commit the PF candidate separately, push it, and stop for independent PF review.
The execution agent must not label its own document `ACCEPT`; use `REVIEW
REQUEST` and report the exact SHA, local/remote equality and clean worktree.

## 5. Packet PA0 — only after independent PF ACCEPT

PA0 is documentation-only. It inventories the actual current tree before any
consumer migration. Create a contract note with a row for every production
consumer of:

- `mux.Registry` and adapter registration;
- tmux/cmux discovery, snapshots and pane/surface IDs;
- LocalPTY discovery/attach fallbacks;
- LinkStore and link/unlink routes;
- observer telemetry and screen/PTY/JSONL semantic status;
- REST, WebSocket, IPC and lifecycle lookup;
- Transcript and Activity source selection;
- mobile external/best-effort session branches;
- install scripts, flags, fixtures and active documentation.

Each row must record exact file/symbol, current owner, whether the managed path
uses it, target owner (`ManagedRuntime`, `ManagedRuntimeCatalog`,
`TerminalTransport`, provider-specific code, ApprovalAuthority or deletion),
planned packet PA1-PA4/PB, and an explicit deletion test.

Freeze only the minimum interfaces demanded by real call sites. Reuse or narrow
the existing managed registry. Do not design a provider plugin SDK, add attach
compatibility, or create a second runtime store.

Commit and push PA0 separately, then stop for independent review. PA1 production
work is prohibited until PA0 ACCEPT.

## 6. Scope exclusions

This handoff does not authorize:

- production migration in PA1-PA4;
- deleting tmux/cmux/localpty code;
- changing Codex or Claude provider protocols;
- changing ApprovalAuthority or public approval DTOs;
- Canonical Timeline implementation;
- N1, Grok/ACP, Navigator, O1 or O2;
- broad terminal UI, emulator or editor work;
- new live model research unless PF identifies a truly missing accepted artifact.

## 7. Completion report

For each authorized packet report:

- repository root actually used, without assuming the reviewer's path;
- remote URL, branch, final SHA and ancestry;
- documents changed;
- exact evidence or inventory produced;
- checks run and any skips/blocks;
- confirmation that production/mobile/test code did not change;
- local/remote equality and clean worktree.

Stop at the packet boundary. The next action is independent review, not PA1.
