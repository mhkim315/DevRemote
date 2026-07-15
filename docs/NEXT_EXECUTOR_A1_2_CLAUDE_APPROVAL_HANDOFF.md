# Next Executor Handoff — A1.2 Claude Approval Extension C0

Status: **C0 EVIDENCE ONLY — MAX ONE DAY — NO PRODUCTION CODE — NO
ACTIONABILITY**

Repository: `/Users/mhk/Documents/codex/DevRemote`

Branch: `feature/phase10-multi-adapter`

Accepted baseline: `2b940a6fce6e878ffa0da17b5df4d39438af144d`

## 1. Startup checks

Use only the canonical checkout above. Do not work from an Antigravity/scratch
clone.

```sh
cd /Users/mhk/Documents/codex/DevRemote
git fetch origin feature/phase10-multi-adapter
git rebase origin/feature/phase10-multi-adapter
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git merge-base --is-ancestor 2b940a6f HEAD
git status --short
```

Require local==remote, ancestry exit 0, and clean worktree. If not, stop.

## 2. Required reading

1. `docs/A1_2_CLAUDE_APPROVAL_EXTENSION_PLAN.md`
2. `docs/SP1_NATIVE_APPROVAL_FINAL_ACCEPTANCE.md`
3. `docs/SP1_NATIVE_APPROVAL_CONTRACT_NOTE.md`
4. `docs/A1_APPROVAL_SAFETY_PLAN.md`
5. `docs/T2_CLAUDE_ADAPTER_IMPLEMENTATION_REPORT.md`
6. `docs/EXECUTOR_AUTHORITY_CONCURRENCY_GUIDELINES.md` if present, otherwise
   the authority/concurrency section in the current handoff guidance.

Then inspect the exact production `pokit run` create path, controlled PTY
profile launch, launch binding, A1 store and current Codex dispatcher. Do not
copy the Codex implementation before understanding the Claude provider
surface.

## 3. Mandatory pre-implementation contract note

Before running live evidence, add
`docs/A1_2_C0_CLAUDE_SURFACE_CONTRACT_NOTE.md` containing:

- authority owner;
- candidate immutable binding table;
- exact success/consumption witness;
- state transitions and linearization point;
- timeout/cancellation/restart behavior;
- capacity behavior;
- adversarial identical-concurrent-request example;
- privacy projection;
- explicit non-goals.

C0 may add a bounded test/evidence harness and docs only. It must not change
production actionability, A1 store, mobile code, provider dispatcher, or
runtime DTOs.

## 4. C0 questions to answer with evidence

1. Can exact `claude` `2.1.209` (or another explicitly reviewed pin) be
   installed/verified outside the repository without changing the user's
   global binary or copying credentials?
2. Can `pokit run claude` launch it directly with a session-only `--settings`
   file and no user/project settings mutation?
3. Can settings sources be constrained so unrelated rules/hooks cannot skip
   the PermissionRequest hook? How do managed organization policies interact?
4. Is one PermissionRequest command-hook process an exact provider invocation
   boundary despite the documented absence of `tool_use_id`?
5. Can a daemon nonce be bound to that exact blocking process/connection
   without timing, input-equality or transcript heuristics?
6. What event proves Claude Code consumed the exact allow/deny response?
   Hook stdout/write/exit alone is not automatically sufficient.
7. What happens for two identical concurrent tool requests, local terminal
   decision racing mobile, timeout, hook crash, Claude exit and daemon exit?
8. Does `PermissionRequest` cover the intended request under the exact
   permission mode, or can earlier allow/deny rules bypass it?

## 5. Required controlled traces

Use one harmless, uniquely named temp-file Bash command in a dedicated temp
directory. Capture bounded/redacted evidence for:

- allow once → exact command executes;
- deny → exact command does not execute;
- timeout/no mobile response;
- hook process cancellation and Claude termination;
- duplicate mobile response;
- two identical requests if the provider can produce them;
- local-terminal response race if the dialog remains reachable.

Evidence must record exact version/artifact identity, hook process lifecycle,
daemon nonce lifecycle, direction/sequence, response, provider outcome and
cleanup. Never commit credentials, prompt text, raw command, home paths,
transcript paths, tool payloads or tokens.

## 6. Surface decision

Assess in this order:

1. stable `PermissionRequest` command hook;
2. only if blocked, document whether print-mode
   `--permission-prompt-tool` or Agent SDK could close the exact missing
   invariant;
3. do not use Channel permission relay as the production answer: it is
   research preview, requires development/allowlist handling, and exposes a
   truncated preview rather than full canonical input.

Do not silently pivot into Agent SDK, a generic managed runtime, plugin SDK or
Channel product. Those require a revised plan.

## 7. Gate and stop condition

Run only focused harness checks, format checks, leak scan, process/temp cleanup,
`git diff --check`, ancestry and worktree checks. Do not run the full repository
gate in C0.

Commit and push the contract note, bounded harness/evidence and C0 report. End
with exactly one verdict:

- `C0 PASS — stable hook exact invocation and consumption proven`, or
- `C0 BLOCKED — <exact missing invariant>`.

Then stop for independent review. Do not begin C1, production code, N1, O1,
O2, A1 core changes, mobile changes, Claude actionability or Channel
integration.

