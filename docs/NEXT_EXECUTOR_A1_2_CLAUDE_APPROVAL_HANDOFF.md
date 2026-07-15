# Next Executor Handoff — A1.2 C1D Managed Claude Observation

Status: **START C1D ONLY — ACTIONABILITY ZERO — STOP FOR REVIEW**

## 1. Canonical repository

- Remote: `https://github.com/mhkim315/DevRemote.git`
- Branch: `feature/phase10-multi-adapter`
- Canonical checkout: `/Users/mhk/Documents/codex/DevRemote`
- Accepted C0D evidence: `e42d4c570e64462ce813861017cc635e338e68bf`
- Accepted SP1/Codex baseline: `2b940a6fce6e878ffa0da17b5df4d39438af144d`
- This plan/handoff commit must be the fetched remote HEAD before work begins.

Do not work from an Antigravity, scratch or duplicate checkout. At startup:

```sh
cd /Users/mhk/Documents/codex/DevRemote
git remote -v
git fetch origin feature/phase10-multi-adapter
git switch feature/phase10-multi-adapter
git merge --ff-only origin/feature/phase10-multi-adapter
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git merge-base --is-ancestor e42d4c570e64462ce813861017cc635e338e68bf HEAD
git merge-base --is-ancestor 2b940a6fce6e878ffa0da17b5df4d39438af144d HEAD
git status --short
```

Stop if HEAD differs from origin, either ancestry check fails, or the worktree is
not clean. Never rewrite or force-push history.

## 2. Mandatory reading order

1. `docs/A1_2_CLAUDE_APPROVAL_EXTENSION_PLAN.md` — authoritative plan;
2. this handoff;
3. `docs/A1_2_C0D_R5_DEFER_EVIDENCE_REPORT.md` and bounded R5 evidence;
4. `docs/A1_2_C0_CLAUDE_EVIDENCE_REPORT.md` — C0H remains BLOCKED;
5. `docs/A1_2_C0R_PERMISSION_PROMPT_TOOL_REPORT.md` — C0R remains BLOCKED;
6. `docs/SP1_NATIVE_APPROVAL_FINAL_ACCEPTANCE.md`;
7. `companion-daemon/internal/term/managed_codex.go` for managed-runtime ownership,
   not for Claude wire semantics;
8. `managed_approval.go`, `managed_approval_delivery.go`, and
   `managed_approval_activation.go` to understand the frozen Codex boundary;
9. `approval_execution.go`, `approval_store_gen.go`, `approval_handler.go`, and
   `approval_delivery.go` for the frozen provider-neutral authority core;
10. `create.go`, `profiles.go`, `managed_registry.go`, `managed_api.go`, and app
    composition in `cmd/devremote/app.go`.

Historical C0H/C0R reports are negative findings. Do not reuse their surfaces.

## 3. Authority-first packet note

Before changing production code, write and commit
`docs/A1_2_C1D_PACKET_CONTRACT_NOTE.md` containing:

- authority owner and canonical stored inputs;
- complete binding table showing creation, storage, comparison, invalidation and tests;
- observation/defer state transitions and exact linearization points;
- exact success evidence for C1D (observation only);
- timeout, exit, stop/delete, replacement and capacity behavior;
- adversarial mismatched-result, duplicate-ID and multi-tool counterexamples;
- evidence/privacy projection;
- explicit C1D non-goals.

Do not start implementation before this note is committed.

## 4. Authorized work: C1D only

Implement the smallest provider-specific managed Claude observation path:

1. A `ManagedClaudeService` (or equivalent) that owns direct process launch,
   provider stdout/result reading, process wait/reap and one runtime epoch.
2. The real structured `pokit run claude` preset routes to this service. The
   executable and argv are direct; no shell, command string, PTY inference,
   observer discovery or legacy JSONL authority.
3. Exact Claude Code 2.1.209 certification via an OS-neutral launcher/attestor seam.
   PATH lookup alone is not certification. Keep macOS-specific process details
   behind that seam; do not build Windows support.
4. Session-isolated Claude hook settings using ambient user authentication. Never
   copy credentials or change user/project/managed Claude settings.
5. A daemon-owned private local hook bridge. Give each runtime an unguessable,
   bounded capability and a private endpoint. Strictly decode only the certified
   `PreToolUse` fields and return `defer`; unknown/duplicate/oversized fields fail
   closed. Do not log the capability or raw payload.
6. Join the hook observation to the structured `tool_deferred` result using exact
   Claude session ID, tool-use ID, tool name and canonical input digest.
7. Store only bounded private pending observations. If the join succeeds, expose a
   safe **non-actionable** intervention record with no options. Clean it on timeout,
   child exit, stop/delete and epoch replacement.

Use canonical adapter identity `claude_headless`, authority version `2.1.209`, the
managed epoch as LaunchGeneration and StreamGeneration zero. ApprovalID remains a
daemon/A1 identity and must never be replaced with the provider tool-use ID.

Prefer new Claude-specific files over conditional branches inside Codex protocol
code. Extract only narrow provider-neutral process or registry helpers when both
accepted runtimes need exactly the same contract.

## 5. Required C1D tests

Write failing tests first for the critical boundaries.

- Production entry: real structured `pokit run claude` selects the managed Claude
  service and direct argv; arbitrary command/custom/interactive paths do not.
- Version/artifact: wrong, missing or changed certification fails before authority.
- Hook bridge: missing capability, wrong runtime, duplicate/unknown fields, malformed
  JSON, oversized input and invalid UTF-8 fail closed.
- Identity: wrong session, tool-use ID, tool name or input digest cannot join.
- Multiplicity: duplicate ID, two pending tools, batch/parallel ambiguity and bounded
  capacity exhaustion create no actionable state.
- Lifecycle: timeout, hook disconnect, child exit, stop/delete and replacement clear
  pending state and reject late results.
- Authority isolation: PTY, prompt, screen, JSONL, observer status and
  `waiting_approval` produce zero Claude approvals.
- Privacy: raw prompt, command, tool input, paths, hook capability, authentication
  data and provider payload never enter DTOs or logs.
- Composition: production store/registry/API path returns an observation with zero
  options; ClaimForExecution, provider resume, delivery and mobile CTA are never
  reached.
- One bounded live probe proves exact production defer observation on pinned 2.1.209.

Use deterministic channels/barriers for lifecycle races, never sleeps. Tests must
inspect intermediate state and include known-bad or fault-injection controls where
the result could otherwise be vacuous.

## 6. Gates and checkpoint

For C1D run:

- `gofmt`/`git diff --check`;
- focused `go build`, `go vet`, and `go test -race` for changed managed/runtime/API
  packages;
- relevant frozen A1/SP1 regression tests;
- documentation/repository secret scan;
- no mobile gate unless mobile code changes (mobile changes are not authorized).

Freeze HEAD before the authoritative checkpoint gate. Commit implementation and a
bounded C1D evidence report, push that exact verified tree, confirm local/remote
equality and clean worktree, then stop with:

```text
REVIEW REQUEST: A1.2 C1D Managed Claude Observation — <implementation SHA>
```

Do not begin C2D in the same session.

## 7. Permanent prohibitions

- no action options, ClaimForExecution, decision delivery, resume-for-decision,
  delivery capacity, mobile CTA or action handler wiring;
- no use of `PermissionRequest` or `--permission-prompt-tool`;
- no terminal prompt parsing, synthetic keys/Y/N, generic send-text or side-effect
  authority;
- no modification of accepted Codex semantics;
- no generic provider SDK, Agent SDK, Channels, N1, O1/O2, observer cleanup,
  tmux/cmux deletion, Windows implementation or cloud relay work.

If exact direct launch, certified identity, private hook bridge or exact defer join
cannot be proven on the production path, keep Claude non-actionable, report BLOCKED
and stop. Never substitute a fixture-only path for production support.

## 8. Later packets — context only, not authorization

- **C2D:** exact one-shot resume decision delivery plus matching provider-consumption
  routing; still production-non-actionable.
- **C3D:** atomic activation, existing authenticated mobile path, live allow/deny and
  final full gates.

Only an independent C1D ACCEPT may authorize a new C2D handoff.
