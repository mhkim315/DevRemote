# Agent Phase A8 Executor Onboarding

Date: 2026-07-07

Audience: a fresh **execution agent** with no previous conversation context.

This document exists because the prior execution agent lost context. Use this as
the bootstrap instruction set for continuing Agent Adapter Layer work from the
latest accepted state.

## Your role

You are the implementation/execution agent.

There is a separate verifier role. The verifier reviews your commits and decides
whether a phase is accepted. Do not mark your own work accepted. Your job is to
make the smallest coherent implementation commit, push it, and report the commit
hash for verification.

## Branch and synchronization

Work on:

```text
feature/phase10-multi-adapter
```

Before editing:

```sh
git fetch origin feature/phase10-multi-adapter
git status --short --branch
git log --oneline -12
```

Fast-forward to `origin/feature/phase10-multi-adapter` before making changes.
Do not force-push. Do not discard user/verifier commits.

## Current accepted state

Terminal Adapter Layer:

- Phase 7 accepted.
- tmux, cmux, and LocalPTY can flow through API, WebSocket, telemetry, and
  mobile without backend-specific behavior at the product boundary.

Agent Adapter Layer:

- Phase A0 accepted — scope and non-goals.
- Phase A1 accepted — real log inventory and redacted fixture policy.
- Phase A2 accepted — common Agent model.
- Phase A3 accepted — parser contract harness.
- Phase A4 accepted — detector/resolver foundation.
- Phase A5 accepted — Claude Agent Backend Foundation.
- Phase A6 accepted — Codex Backend Slice.
- Phase A7 accepted — Agent Agnostic UX/Product Gate.
- Phase A8-prep accepted — Antigravity actual-log fixture collection.

Important commits:

- `108525a` — A7 Agent Agnostic UX/Product Gate acceptance.
- `a9830df` — roadmap updated after A7 acceptance.
- `de3197dbc` — A8-prep Antigravity fixtures from actual CLI logs.

Current verifier decision for `de3197dbc`:

```text
ACCEPT — A8-prep only.
Not accepted as full A8 Third Agent Backend Slice.
Next phase permission: A8 implementation ALLOWED.
```

## Required reading order

Read these before editing:

1. `docs/AGENT_PHASE_A8_EXECUTOR_ONBOARDING.md` — this file.
2. `docs/AGENT_ADAPTER_LAYER_PLAN.md`
3. `docs/NEXT_SESSION_AGENT_ADAPTER_HANDOFF.md`
4. `docs/AGENT_PHASE_A8_PREP_SCOPE.md`
5. `docs/AGENT_PHASE_A7_ACCEPTANCE.md`
6. `docs/AGENT_UX_RELEASE_GATE_FOLLOWUPS.md`
7. `docs/AGENT_PHASE_A6_BACKEND_ACCEPTANCE.md`
8. `docs/AGENT_PHASE_A5_BACKEND_FOUNDATION_ACCEPTANCE.md`

Then inspect code:

1. `companion-daemon/internal/agent/parser_contract_test.go`
2. `companion-daemon/internal/agent/claude_adapter.go`
3. `companion-daemon/internal/agent/codex_adapter.go`
4. `companion-daemon/internal/agent/testdata/antigravity/`
5. `companion-daemon/internal/term/resolver.go`
6. `companion-daemon/internal/term/gemini_resolver.go`
7. `companion-daemon/internal/term/gemini_parser.go`
8. `companion-daemon/internal/term/telemetry_service.go`
9. `companion-daemon/internal/term/tracker.go`
10. `companion-daemon/cmd/devremote/app.go`
11. Mobile files only for regression checks, not for vendor-specific behavior:
    - `mobile/src/components/AgentCard.tsx`
    - `mobile/src/screens/FeedScreen.tsx`

## What A8-prep already proved

`de3197dbc` established that Antigravity has real observed logs and redacted
fixtures:

- source path:
  `~/.gemini/antigravity/brain/<uuid>/.system_generated/logs/transcript.jsonl`
- fixture files:
  - `user_agent.jsonl`
  - `tool_call.jsonl`
  - `malformed.jsonl`
- observed types:
  - `USER_INPUT`
  - `VIEW_FILE`
  - `PLANNER_RESPONSE`
  - `SEARCH_WEB`
  - `LIST_DIRECTORY`
  - `CHECKPOINT`
  - `ERROR_MESSAGE`
  - `GENERIC`
  - `EPHEMERAL_MESSAGE`

The prep commit did **not** complete production A8. It only made A8
implementation permissible.

## Your active task: Phase A8 — Third Agent Backend Slice

Implement the smallest production backend slice that puts Antigravity on the
Agent Adapter contract without breaking Claude, Codex, or A7 agnostic UX.

Target outcome:

```text
Antigravity actual-log evidence
→ detector/resolver/parser
→ common AgentEvent / AgentStatus / AgentIdentity
→ production telemetry boundary
→ /api/sessions agent fields/events
```

## A8 implementation requirements

Minimum required work:

1. Add an Antigravity production parser or clearly named Antigravity adapter in
   the agent layer.
2. Use the actual observed fixture types from `de3197dbc`.
3. Map Antigravity output to common contract values:
   - `user_message`
   - `assistant_message`
   - `tool_call_started`
   - `unknown`
   - conservative status such as `working`, `idle`, `degraded`, or `unknown`
     only if supported by evidence.
4. Add detector/resolver connection for Antigravity evidence.
5. Connect the new agent path to production telemetry so `/api/sessions`
   receives `AgentKind`, `AgentStatus`, `AgentConfidence`, and events.
6. Preserve Claude A5 and Codex A6 behavior.
7. Preserve A7 unknown/future graceful fallback.
8. Add tests that would fail if Antigravity only existed as fixture data but was
   not connected to the production path.

If structured Antigravity logs are insufficient for a full parser, implement a
lower-confidence screen/process/log-evidence path and document the degradation.
Do not invent fields or semantics not present in actual logs.

## Required tests

Add or update tests in proportion to the implementation. At minimum, prove:

1. Antigravity parser contract passes against redacted fixtures.
2. malformed/unknown Antigravity records do not panic and degrade safely.
3. detector/resolver finds or represents Antigravity evidence without requiring
   raw user paths in committed fixtures.
4. production telemetry path can expose Antigravity agent fields/events.
5. Claude and Codex parser/detector regressions still pass.
6. mobile/common code did not gain vendor-specific behavior branches.

Recommended verification commands:

```sh
cd companion-daemon
GOCACHE=/tmp/devremote-agent-a8-go-cache go vet ./...
GOCACHE=/tmp/devremote-agent-a8-go-cache go test ./...
GOCACHE=/tmp/devremote-agent-a8-go-cache go test -race ./...

cd ../mobile
node_modules/.bin/tsc --noEmit
```

If `go test ./...` fails inside a sandbox because `httptest` cannot bind local
ports, rerun it in the approved unsandboxed environment and mention that in your
handoff.

## Hard prohibitions

Do not:

- claim A8 complete if only fixtures were changed;
- create a fake third agent;
- design parser behavior from imagination instead of actual logs;
- add mobile/common UX behavior branches for `claude`, `codex`, `antigravity`,
  `gemini`, `qwen`, or any future vendor;
- make `agentKind` decide approval CTA, degraded copy, or session availability;
- hide terminal sessions when agent parsing fails;
- expose raw prompt, token, absolute user path, source code, or command payload
  in fixtures, push, diagnostics, or docs;
- change common AgentEvent/AgentStatus contract without documenting the reason
  and making the change vendor-agnostic;
- rewrite Terminal Adapter Layer behavior as part of A8;
- implement A9 Approval UX or A10 Diagnostics unless explicitly requested after
  A8 acceptance.

Vendor-specific literals are allowed inside:

- parser implementation;
- detector/resolver registration;
- fixture metadata;
- tests that prove vendor-specific input maps to common output.

Vendor-specific literals are not allowed to drive common/mobile UX behavior.

## Redaction rules

Commit only redacted fixtures. Use stable tokens:

| Sensitive data | Replacement |
| --- | --- |
| home path | `<HOME>` |
| username | `<USER>` |
| repo/project name | `<PROJECT>` |
| UUID/session ID where sensitive | `<UUID>` |
| API token/secret | `<TOKEN>` |
| user prompt | `<PROMPT>` |
| command | `<CMD>` |
| absolute file path | `<PATH>` |
| source code/body output | `<REDACTED_CONTENT>` or `<REDACTED_OUTPUT>` |

Before committing:

```sh
rg -n "/Users/|/home/|Bearer |sk-[A-Za-z0-9]|api[_-]?key|token|secret|mhk|Documents|Downloads|OPENAI|ANTHROPIC|GEMINI" \
  companion-daemon/internal/agent/testdata

rg -n "/Users/|/home/|Bearer |sk-[A-Za-z0-9]|api[_-]?key|mhk|Documents|Downloads|OPENAI|ANTHROPIC" \
  docs/AGENT_PHASE_A8_*.md docs/NEXT_SESSION_AGENT_ADAPTER_HANDOFF.md
```

Expected result: no raw sensitive values in fixtures. Documentation may contain
redaction policy words such as `token` or `secret`; those are acceptable when
they are not real values.

## Vendor-branch regression search

Before committing:

```sh
rg -n "agentKind.*===|agentKind.*==|switch.*agentKind|case ['\"]claude|case ['\"]codex|case ['\"]antigravity|case ['\"]gemini|case ['\"]qwen" \
  mobile companion-daemon/internal companion-daemon/cmd
```

Expected interpretation:

- agent-specific parser/detector/resolver code may contain vendor literals;
- mobile/common behavior must not branch on vendor names;
- existing historical backend literals are not automatically failures, but any
  new common/mobile behavior branch is a blocker.

## Commit and handoff format

Use a focused commit message:

```text
feat: Phase A8 — Antigravity backend slice
```

If you discover A8 still cannot be implemented safely, stop at a scoped prep
commit:

```text
docs: Phase A8 blocker — Antigravity production gap
```

Report back with:

```text
Phase A8 implementation — <commit>

Scope:
- ...

Production path:
- detector:
- resolver:
- parser:
- telemetry/API:

Regression:
- Claude:
- Codex:
- A7 unknown/future:
- mobile vendor-branch:

Verification:
- command: result
- command: result

Known limitations:
- ...
```

## Verifier will judge strictly

The verifier will reject if:

- the commit is fixture-only but claims A8 implementation;
- Antigravity is not connected to a production path;
- common contract output is bypassed;
- Claude/Codex regressions are introduced;
- mobile/common vendor-specific behavior branches are added;
- raw sensitive data is committed;
- unknown or malformed Antigravity data can crash or hide terminal sessions.

Implement the smallest defensible A8 slice. Leave A9 approval actions and A10
diagnostics for later phases.
