# Next Session Handoff — S1 Rich Agent Runtime Status

Status: **COMPLETED — S1 ACCEPTED; DO NOT RE-EXECUTE**

S1 is independently accepted at implementation
`b6504bd7d5c634f0c0459ae87503b82d17c1537b`, canonical marker
`70ef5df28dede7b0f3025eeaab7826f76a229fbf`. This document is retained as
historical execution evidence. The authoritative next task is
`docs/NEXT_SESSION_S1_1_RUNTIME_STATUS_HARDENING_HANDOFF.md`.

> Historical notice: imperative instructions and “next” statements below record
> the completed S1 workflow and no longer authorize work.

This was the authoritative S1 execution and onboarding document. It is deliberately
split into five bounded stages for a fresh Claude Code execution session using
DeepSeek V4 Pro. Complete the stages sequentially, but request independent
acceptance only once, after S1-A through S1-E are all complete.

Do not infer the task from chat summaries, an old roadmap, or an unpushed
worktree. Do not begin A1, O1, Notifications, another adapter, or Windows work.

Current independently checked progress:

```text
S1-A audit:                 8d73a4a (checkpoint)
S1-B internal contract:    33c1fed (checkpoint)
S1-C production wiring:    e8d53d0 (checkpoint)
S1-B/C authority hardening: 2f3fdfcbff0fc21e78f9c077f8cf2cef61024d6d
S1-D API/mobile checkpoint: 17553e0106404bf75a75899c720ff7be0df3fad0
checkpoint result:         ACCEPT — proceed to revised S1-E only
```

This was a historical pre-final checkpoint. S1-E was subsequently completed and
accepted; use `docs/S1_FINAL_ACCEPTANCE.md` for the final decision and the S1.1
handoff named above for current work.

## 0. Repository identity and commit recovery

```text
canonical repository: https://github.com/mhkim315/DevRemote.git
branch: feature/phase10-multi-adapter
accepted T3 / S1 code baseline: 9ad6f834f70e88b800e60124c8e408d38bca9d2b
accepted S1-D checkpoint: 17553e0106404bf75a75899c720ff7be0df3fad0
accepted D1: 8f7c22def81abf0b932f6dbbacc07325ae2bb12e
accepted R2: 6f940b03bbb451dd0ddcfc6bf7ce5f86bbec3b50
accepted T2: ef4a162c7f9a5644fd52d89501e97f4e62301dfa
accepted T1: 162266f830caaf07bf701d9a1294557432855769
accepted T0: 3ce2604bd333dcb63142b5b1710185a823162efa
```

Run this before reading local files or editing:

```sh
cd /path/to/DevRemote
git remote set-url origin https://github.com/mhkim315/DevRemote.git
git fetch origin feature/phase10-multi-adapter
git switch feature/phase10-multi-adapter
git merge --ff-only origin/feature/phase10-multi-adapter
git status --short
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git log --oneline -30
git merge-base --is-ancestor 9ad6f834f70e88b800e60124c8e408d38bca9d2b HEAD
git merge-base --is-ancestor 17553e0106404bf75a75899c720ff7be0df3fad0 HEAD
git merge-base --is-ancestor 8f7c22def81abf0b932f6dbbacc07325ae2bb12e HEAD
git merge-base --is-ancestor 6f940b03bbb451dd0ddcfc6bf7ce5f86bbec3b50 HEAD
git merge-base --is-ancestor ef4a162c7f9a5644fd52d89501e97f4e62301dfa HEAD
git merge-base --is-ancestor 162266f830caaf07bf701d9a1294557432855769 HEAD
git merge-base --is-ancestor 3ce2604bd333dcb63142b5b1710185a823162efa HEAD
```

Every ancestry command must exit 0. Local and remote tips must match and the
worktree must be clean before implementation.

If this handoff is missing locally, do not ask the user to paste it and do not
guess. Recover it from the canonical remote first:

```sh
git fetch origin feature/phase10-multi-adapter
git show origin/feature/phase10-multi-adapter:docs/NEXT_SESSION_S1_RUNTIME_STATUS_HANDOFF.md
```

Never use `git reset --hard`, force-push, or discard another agent's changes. If
the remote advances, fetch and rebase or fast-forward normally, inspect the new
commits, preserve accepted ancestry, and rerun all gates before pushing.

## 1. DeepSeek V4 Pro execution protocol

The reusable operating rule is
`docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`. Read it before this section and
apply its task packet, evidence-level, remediation, and resource-hygiene rules.

S1-B/C showed that the main acceleration came from exact blocker-to-test
instructions, not from asking the model to "be more careful." Continue using an
exact production caller, prohibited behavior, required behavior, negative test,
and completion evidence for every remaining item.

This task has many nearby concepts with similar names. Work in small, explicit
units so a locally plausible change does not silently cross an authority boundary.

For each S1 stage:

1. restate the stage's exact input, production entry point, and prohibited scope;
2. inspect existing code before proposing new types;
3. write the negative/authority tests first or in the same focused change;
4. implement only the smallest production path that makes those tests pass;
5. run the focused tests and record their exact commands/results;
6. inspect `git diff --check` and the actual diff, not only test output;
7. create a narrow checkpoint commit whose message starts with `S1-A`, `S1-B`,
   `S1-C`, `S1-D`, or `S1-E`;
8. continue to the next stage without writing an intermediate REVIEW REQUEST.

At every checkpoint, explicitly answer:

```text
What is daemon lifecycle authority?
What is agent-activity authority?
What is merely connectivity/health?
What evidence was rejected or degraded?
Which production caller now uses this code?
Which accepted contract remained byte-for-byte or behaviorally unchanged?
```

Do not satisfy a production-path requirement with a new unused helper or copied
renderer. `rg` the final import/call graph and test the function/component actually
used by the product. Do not weaken a fixed test or gate to make implementation
pass. Do not report ALL GATES PASSED unless the command was run on the final tree.

## 2. Read-before-edit order

Read these in order:

1. this handoff;
2. `docs/T3_TRANSCRIPT_INTEGRATION_FINAL_ACCEPTANCE.md`;
3. `docs/T0_COMMON_AGENT_EVENT_FINAL_ACCEPTANCE.md`;
4. `docs/T1_CODEX_ADAPTER_FINAL_ACCEPTANCE.md` and implementation report;
5. `docs/T2_CLAUDE_ADAPTER_IMPLEMENTATION_REPORT.md`;
6. `docs/R1_RUNTIME_SIGNAL_MATRIX.md`, especially the authority/precedence section;
7. `docs/R2_MULTI_AGENT_EXPANSION_RESEARCH_REPORT.md`;
8. `docs/M3B_FINAL_ACCEPTANCE.md` and `docs/M3B_IMPLEMENTATION_REPORT.md`;
9. `docs/ROADMAP_AFTER_E10B.md`, S1 section;
10. `companion-daemon/internal/agent/models.go`;
11. `companion-daemon/internal/agent/contract/contract.go` and `validate.go`;
12. accepted T1/T2 `GetStatus` implementations and tests;
13. `companion-daemon/internal/term/telemetry.go` and
    `telemetry_service.go` with tests;
14. `companion-daemon/internal/term/adapter_state.go` and T3 production bridge;
15. lifecycle Catalog/service and `/api/sessions` handlers/tests;
16. mobile `client.ts`, `agentDisplay.ts`, lifecycle helpers, dashboard/session
    list, and `FeedScreen.tsx`;
17. device-principal authentication middleware and route registration.

Before implementation, add a code-path audit to the S1 implementation report.
It must identify every existing field called `state`, `status`, `agentStatus`,
`lifecycleState`, `stale`, `lastError`, and `confidence`, its producer, consumer,
and authority. Do not rename or reinterpret one until this map exists.

## 3. S1 objective

Add a bounded, provider-neutral, session-scoped **agent activity status** product
path driven by the frozen T0 contract and accepted T1/T2 adapters:

```text
accepted, session-bound AgentEvents / strong status evidence
  -> accepted version-specific adapter GetStatus
  -> session-owned status resolver/store
  -> additive authenticated read DTO
  -> strict mobile validation
  -> visibly separate agent-activity status UI
```

S1 must never collapse these independent dimensions:

| Dimension | Examples | Authority |
| --- | --- | --- |
| daemon lifecycle | starting, running, stopping, exited, killed, failed | managed-session Catalog / LifecycleService |
| agent activity | unknown, idle, thinking, working, waiting_input, waiting_approval, completed, failed, interrupted, degraded | frozen T0 `GetStatus`, accepted evidence/provenance |
| connectivity/observation health | fresh, stale, adapter_error, unavailable | registry/telemetry poll health |
| approval action state | pending, approved, rejected, expired | A1/approval store; **not implemented by S1** |

The same word, such as `failed`, may exist in more than one dimension. It must
remain namespaced and must not be copied across dimensions.

## 4. Non-negotiable authority rules

- `lifecycleState` remains daemon-authoritative. Agent status never enables Stop,
  Kill, Delete, reconnect, session retention, or process-exit decisions.
- Process exit does not prove agent `completed`; agent `completed` does not prove
  process exit.
- Transcript text, PTY text, screen strings, prompt matching, timing proximity,
  CWD, and process-name guesses cannot create agent status.
- T3 Transcript is a read-only consumer/history surface, never S1 authority.
- Use the frozen T0 `AgentStatus`, `StatusEvidence`, `StatusResult`, provenance
  ranking, confidence ceiling, and `ResolveStatus`. Do not fork this vocabulary.
- Advisory provenance may show bounded degraded/nonterminal information but cannot
  establish `completed`, `failed`, or `interrupted`.
- `waiting_approval` is display-only activity in S1. It cannot create an approval,
  expose an approval CTA, or authorize an action. A1 owns that boundary.
- Unknown/malformed/future evidence fails to `unknown` or `degraded`; it never
  falls back to a legacy parser, PTY heuristic, or provider-specific UI branch.
- Cross-session, stale-generation, version-conflict, and uncorrelated evidence is
  rejected or degraded without leaking event content or private paths.
- Never log prompts, thinking, commands, tool input/output, terminal input,
  tokens, signatures, raw JSONL, or private absolute paths.
- T0, T1, T2, D1, and T3 accepted contracts remain unchanged unless an actual
  compile-safe additive integration is explicitly required. Do not modify the
  T0 interface or conformance harness.

## 5. Staged implementation

### S1-A — Current status-path audit and frozen product contract

Goal: remove ambiguity before behavior changes.

Required outputs:

- `docs/S1_RUNTIME_STATUS_IMPLEMENTATION_REPORT.md` with the field/producer/
  consumer/authority audit;
- one versioned, additive internal/product DTO design;
- a written transition/evidence table mapping accepted AgentEvent kinds to
  possible `StatusEvidence` and explicitly listing non-mappings;
- a rollback description that removes S1 status exposure without changing
  lifecycle, T3, Recorder, or adapters.

Audit at minimum:

- `SessionTelemetry.State`, `AgentStatus`, `AgentConfidence`, `LifecycleState`,
  `Stale`, `LastSuccessAt`, and `LastError`;
- legacy detector/parser writes versus accepted adapter writes;
- session Catalog state and retained terminal rows;
- dashboard and FeedScreen labels/action policy;
- API authentication and host-bound mobile transport.

Do not make runtime behavior changes in S1-A. Do not create a second public
status vocabulary merely because legacy fields already exist.

Checkpoint commit example:

```text
docs(S1-A): audit status paths and freeze authority matrix
```

### S1-B — Internal status resolver and bounded session state

Goal: implement the provider-neutral status boundary without product wiring.

Required properties:

- consume only accepted, session-bound evidence;
- delegate precedence/final resolution to the accepted adapter `GetStatus` and
  frozen `contract.ResolveStatus` behavior;
- store one immutable current result per canonical session ID with observed time,
  provenance, confidence, degraded state, and generation/version binding;
- bound all strings/diagnostics and total state; clear on explicit history/session
  delete and replace atomically on session generation change;
- isolate adapter error/panic so telemetry, Recorder, Terminal, lifecycle, and
  other sessions continue;
- deterministic stale policy: elapsed time may make a prior result stale/unknown,
  but may never manufacture a new activity status;
- lifecycle and connectivity are inputs to the response envelope only as separate
  fields, never inputs to `GetStatus`.

Required negative tests:

- advisory terminal claim -> unknown + degraded;
- cross-session event -> rejected;
- unknown provenance/status -> safe unknown;
- old generation/version result -> cannot overwrite current session;
- adapter failure -> only that status degrades;
- lifecycle exited + activity working remain separate;
- agent completed + lifecycle running remain separate;
- delete clears status; recreation cannot inherit old status.

Do not add HTTP/mobile code yet.

### S1-C — Accepted T1/T2 production wiring

Goal: make actual telemetry polling feed S1 through accepted adapters.

Use the existing accepted-adapter production read path. Prefer reusing the exact
validated event batch, version authority, correlation, cursor/generation, and
adapter instance already established by T3/TelemetryService. Do not create a
second log reader, independent cursor, independent polling goroutine, or second
session-correlation implementation.

Production flow must prove:

```text
TelemetryService.processSession
  -> accepted Codex or Claude adapter read/normalize
  -> correlation/version gate
  -> bounded StatusEvidence from allowlisted event structure
  -> that adapter's GetStatus
  -> session-owned S1 state
```

Event-to-evidence mapping must be closed and conservative. Suggested candidates
must be justified by accepted structured events, not names alone:

- `thinking` -> thinking;
- `tool_call_started` -> working;
- authoritative `waiting_input` -> waiting_input;
- authoritative bound `approval_requested` -> waiting_approval display only;
- strong `completed`/`failed`/`interrupted` -> corresponding terminal agent
  activity, subject to frozen advisory downgrade;
- other events -> no new status unless the audit proves a provider-neutral rule.

Preserve provenance and confidence from the accepted event. Never upgrade either.

Required production-path tests must call `processSession`, not manually compose
only helper calls. Include Codex success, Claude safe limitations, version
conflict, unavailable correlation, malformed/unknown event, incremental polling,
stale response/generation, and failure isolation. Assert exact status,
provenance, confidence, degraded flag, session ID, and that lifecycle fields are
unchanged.

### S1-D — Authenticated API and actual mobile consumer

Goal: expose and render S1 without weakening existing lifecycle/mobile behavior.

Use an additive, versioned response. Prefer a nested status object rather than
overloading the legacy `state` string. It must include only bounded values needed
by the UI, for example:

```text
sessionId
contractVersion
activity { status, provenance, confidence, degraded, observedAt, stale }
lifecycle { state }             // existing daemon value, separate
observation { stale }           // health only, separate
```

The exact shape must follow the S1-A audit and repository conventions. Do not
expose raw `LastError`, raw evidence, prompts, metadata, private paths, or adapter
records.

Requirements:

- authenticated with device principal `sessions:read` in remote mode;
- host-bound mobile transport; no Supabase/legacy credential in paired mode;
- strict server and mobile DTO validation with count/string/time/confidence bounds
  and unknown-field handling;
- session-scoped response with no cross-session leakage;
- mobile visibly labels **Agent activity** separately from **Session lifecycle**;
- unknown/degraded/stale states remain honest and do not enable actions;
- `waiting_approval` is text/badge only—no CTA or action endpoint in S1;
- real product import/call/render chain, not an unused helper or copied component;
- generation/unmount guard prevents a previous session's response from rendering.

Required tests:

- actual production route with missing/invalid/insufficient/valid bearer;
- wrong-session and retained/deleted-session behavior;
- strict DTO rejection and bounded diagnostics;
- production mobile fetch through the host-bound transport;
- actual component/classifier imported by dashboard/FeedScreen;
- lifecycle and activity labels displayed independently;
- stale session response discarded;
- no action visibility change from agent status alone.

Run `npm run typecheck` and focused Jest whenever mobile code changes.

#### S1-D execution packet — use without broadening

Checkpoint: **S1-D only**. Start from
`2f3fdfcbff0fc21e78f9c077f8cf2cef61024d6d` or a descendant that preserves it.
Do not start S1-E until this checkpoint has been reviewed. This checkpoint review
is not the final S1 `REVIEW REQUEST`.

Required production chain (adapt names only where the S1-A audit proves the
repository uses a different accepted owner):

```text
TelemetryService accepted-adapter update
  -> session-owned S1 AgentStatusStore snapshot
  -> additive versioned session read DTO
  -> existing sessions:read authenticated production route
  -> mobile host-bound apiGet transport
  -> strict DTO validator
  -> actually imported Dashboard/FeedScreen status renderer
```

Use an additive nested `agentActivity`/`runtimeStatus` object. Do not reinterpret
legacy `state`, `lifecycleState`, or `agentStatus`, and do not use agent activity
to change lifecycle action policy.

| ID | Bad behavior to prevent | Required production behavior | Required regression proof |
| --- | --- | --- | --- |
| D1 | an unused endpoint/helper makes S1 appear reachable | the existing product session-read path calls the S1 snapshot owner and the actual mobile screen imports its renderer | route-to-client-to-component production call/import test |
| D2 | one session's activity is returned for another | canonical requested session ID must exactly match the stored snapshot; absent/mismatch returns safe unknown/absent status | wrong-session and cross-session leakage tests |
| D3 | stale activity is displayed as current thinking/working | preserve the stale observation explicitly; mobile renders stale/unknown/degraded, never an active label | stale-clock response and rendered-label negative test |
| D4 | agent activity changes Stop/Kill/Delete or approval actions | lifecycle remains the sole lifecycle-action authority; `waiting_approval` is label-only | identical action visibility across every agent activity status |
| D5 | remote read bypasses device authorization | use the existing `sessions:read` principal boundary; paired mobile uses host-bound device bearer only | missing, malformed, insufficient, and valid bearer route tests plus paired-transport test |
| D6 | malformed or future DTO is partially trusted | validate contract version, exact session ID, closed status/provenance vocabulary, finite confidence in range, RFC3339 time, bounds, channel separation, and unknown fields according to repository strict-validation convention | table tests for each malformed field and oversized payload |
| D7 | a previous request renders after session switch/unmount | bind response to session generation and mounted state before commit | deferred A->B session switch and unmount tests |
| D8 | delete/recreate inherits old status | production delete clears S1 state; retained history behavior follows the accepted lifecycle contract without fabricating activity | delete, retained row, and same-ID recreation tests |
| D9 | diagnostic/private adapter data crosses the API | expose only bounded product fields; never raw evidence, errors, records, prompts, paths, commands, tool I/O, or metadata | response snapshot/secret/path negative tests |

Implementation constraints:

- reuse `AgentStatusStore`; do not create a second poller, cursor, resolver, or
  correlation system;
- use the accepted S1-B/C status/provenance/confidence/degraded result without
  upgrading it in the API or UI;
- preserve the current `/api/sessions` wire fields byte-for-byte except for the
  explicitly additive versioned nested field;
- prefer the existing authenticated session-list route unless the audit proves an
  additive session-scoped route is required; do not expose two competing status
  APIs;
- do not edit T0/T1/T2 adapter semantics, T3 projection, Recorder, Terminal,
  lifecycle transitions, A1 approval state, or Windows behavior;
- do not add a status-derived approval CTA.

Focused evidence required before the S1-D checkpoint commit:

```text
Go: production route/auth/session/delete tests with -race
TypeScript: npm run typecheck
Jest: strict DTO, host-bound transport, session-race, and actual renderer tests
Call graph: rg evidence showing the product imports the tested renderer/client
Diff: git diff --check and reviewed final diff
```

Then run `sh scripts/build-gate.sh` on the final S1-D tree. Report native skips
honestly. Commit with an `S1-D` prefix, push, verify local/remote full SHA equality
and clean worktree, and stop for checkpoint review. Do not emit the final S1
`REVIEW REQUEST` and do not begin S1-E in the same execution turn.

### S1-E — Integrated regression, safety evidence, and final review request

Goal: prove the complete S1 contract without expanding scope.

The milestone boundary was revised after S1-D acceptance. Before executing this
section, read and follow `docs/NEXT_SESSION_S1_E_FINALIZATION_HANDOFF.md`. That
packet adds the required stream-generation invalidation, daemon restart/mobile
freshness, and cleanup evidence while explicitly deferring launch/process identity
hardening to S1.1. It does not authorize S1.1 or A1 implementation.

Required final regression matrix:

- T0 fixed conformance unchanged;
- accepted T1 Codex and T2 Claude suites unchanged;
- D1 fixed suite/review safety unchanged;
- T3 source arbitration, echo privacy, bounded store/API/mobile tests unchanged;
- lifecycle Stop/Kill/Delete authority unchanged;
- AgentStatus cannot mutate lifecycle or approval state;
- no PTY/Transcript/text heuristic status authority;
- cross-session/version/generation/error isolation;
- authenticated API and actual mobile rendering;
- secret/private-path scan;
- race tests under repeated status updates and session deletion/recreation.

Run on the final tree:

```sh
cd companion-daemon
gofmt -l ./internal/agent ./internal/term
go build ./...
go vet ./...
go test -race ./internal/agent/... ./internal/term/... -count=1

cd ../mobile
npm run typecheck
npm test -- --runInBand

cd ..
git diff --check
sh scripts/build-gate.sh
git status --short
```

If tests need local sockets, run them in the normal trusted development
environment rather than weakening/skipping them. Zero tests, skipped required
tests, a baseline-only pass, or a gate run before the final change is not PASS.

Update the report with a requirement-to-test matrix and one final marker:

```text
REVIEW REQUEST: S1 Rich Agent Runtime Status — <full implementation SHA>
```

Push all S1 commits, verify local and remote full SHA match, verify the worktree
is clean, then stop for independent S1 verification. Do not begin A1.

## 6. Prohibited scope

S1 does not authorize:

- changing the frozen T0 AgentEvent/AgentStatus interface or fixed harness;
- editing accepted T1/T2 version-specific semantics merely to produce richer
  status;
- treating legacy parser/detector output as accepted adapter authority;
- reading PTY, Transcript, prompts, terminal screen, CWD, or process names to
  infer thinking/waiting/completed/failed;
- adding approval CTA/actions, automatically approving, or changing approval
  authorization (A1);
- changing lifecycle actions/state transitions or session retention (M3b);
- changing Recorder, raw terminal bytes, WebSocket tickets, auth, or pairing;
- D1 repair activation, another production adapter, R2 research expansion;
- Notifications, O1 orchestration, iOS/Android auth redesign, distribution work;
- Windows/ConPTY/Named Pipe/Service/DPAPI/CNG/TPM work.

If completion appears to require one of these, stop and document the blocker
instead of expanding scope.

## 7. Resource and temporary-file hygiene

Long Go race tests, Android/Expo tasks, and isolated workspaces can consume large
disk space. Keep temporary output bounded:

```sh
export GOCACHE=/tmp/devremote-s1-go-cache
export GOTMPDIR=/tmp/devremote-s1-go-tmp
mkdir -p "$GOCACHE" "$GOTMPDIR"
```

After tests, inspect only paths created by this S1 session and remove those
temporary paths when no process is using them. Also check for runaway `go test`,
`jest`, `gradle`, `xcodebuild`, Metro, or coding-agent processes. Do not delete
user caches, other agents' worktrees, tracked build files, simulators, device
data, or arbitrary `/tmp` content. Do not run Expo prebuild with a destructive
clean flag unless the handoff's required gate explicitly needs it and the diff is
inspected afterward.

Before final push record:

```text
temporary directories created
temporary directories removed
long-running processes remaining
worktree status
```

## 8. Acceptance checklist

S1 is ready for independent review only when all are true:

- [ ] S1-A audit maps every existing status-like field and freezes separation.
- [ ] S1-B bounded session-owned status boundary passes authority negatives.
- [ ] S1-C actual `processSession` uses accepted T1/T2 adapter `GetStatus`.
- [ ] No second adapter/log/PTY/Transcript reader or cursor exists.
- [ ] Lifecycle, activity, observation health, and approval remain separate.
- [ ] Strong/advisory provenance and terminal downgrade follow frozen T0.
- [ ] S1-D authenticated, session-scoped API is production-wired.
- [ ] Paired mobile uses host-bound bearer and strict DTO validation.
- [ ] Actual production mobile component renders activity separately.
- [ ] Agent status alone cannot enable lifecycle or approval actions.
- [ ] Cross-session/version/generation/error isolation tests pass.
- [ ] T0/T1/T2/D1/T3 regressions and full build gate pass on final tree.
- [ ] Implementation report contains exact commands, evidence matrix, limitations,
      cleanup report, and final full-SHA REVIEW REQUEST.
- [ ] Local HEAD equals remote branch tip and worktree is clean.
- [ ] A1, O1, Notifications, another adapter, and Windows were not started.

## 9. Honest M-track deferrals

Physical Android/iOS display smoke may remain M-track if no device is available,
provided the executable backend/API/mobile product path and automated rendering
tests are complete. Do not claim a physical-device pass without running it.

Potential richer provider status that requires new managed hooks/protocol signals
must remain unknown/degraded until those signals are independently evidenced. It
is not a reason to add heuristics or weaken S1 acceptance.
