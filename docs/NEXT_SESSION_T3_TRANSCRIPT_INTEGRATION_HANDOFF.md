# Next Session Handoff — T3 Transcript Integration

Status: **IN PROGRESS — USE THE REMEDIATION HANDOFF**

Current continuation: read
`docs/NEXT_SESSION_T3_REMEDIATION_HANDOFF.md` first. The implementation at
`0c82cf02ae2f48ef67ee66b5e2d609965e39d34c` received an independent **REJECT**;
the remediation handoff records the exact remaining cursor/window, authority,
privacy, product-path, and test work. This original document remains authoritative
for the complete T3 contract and done conditions, but its earlier "ready" status
is superseded.

This is the authoritative T3 execution handoff. It begins at the independently
accepted D1 tip and authorizes T3 only. Do not reconstruct T3 from an older
Transcript plan, an unpushed worktree, or a summary in chat.

## 1. Repository recovery and commit visibility

```text
repository: https://github.com/mhkim315/DevRemote.git
branch: feature/phase10-multi-adapter
accepted D1 / T3 baseline: 8f7c22def81abf0b932f6dbbacc07325ae2bb12e
accepted R2: 6f940b03bbb451dd0ddcfc6bf7ce5f86bbec3b50
accepted T2: ef4a162c7f9a5644fd52d89501e97f4e62301dfa
accepted T1: 162266f830caaf07bf701d9a1294557432855769
accepted T0: 3ce2604bd333dcb63142b5b1710185a823162efa
```

Run before editing:

```sh
git remote set-url origin https://github.com/mhkim315/DevRemote.git
git fetch origin feature/phase10-multi-adapter
git switch feature/phase10-multi-adapter
git merge --ff-only origin/feature/phase10-multi-adapter
git status --short
git log --oneline -30
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git merge-base --is-ancestor 8f7c22def81abf0b932f6dbbacc07325ae2bb12e HEAD
git merge-base --is-ancestor 6f940b03bbb451dd0ddcfc6bf7ce5f86bbec3b50 HEAD
git merge-base --is-ancestor ef4a162c7f9a5644fd52d89501e97f4e62301dfa HEAD
git merge-base --is-ancestor 162266f830caaf07bf701d9a1294557432855769 HEAD
git merge-base --is-ancestor 3ce2604bd333dcb63142b5b1710185a823162efa HEAD
```

Every ancestry command must exit 0. Local and remote tips must match before work
starts, and the worktree must be clean. If this document is missing locally, do
not guess or ask for a pasted copy before checking the remote:

```sh
git fetch origin feature/phase10-multi-adapter
git show origin/feature/phase10-multi-adapter:docs/NEXT_SESSION_T3_TRANSCRIPT_INTEGRATION_HANDOFF.md
```

Never use `git reset --hard`, force-push, or discard another agent's changes. If
the remote advances, fetch and rebase normally, preserve all accepted ancestry,
rerun the full gate, and report the final full SHA. The GitHub repository name is
case-sensitive in the canonical URL: `mhkim315/DevRemote.git`.

## 2. Read before editing

Read in this order:

1. this handoff;
2. `docs/D1_ADAPTER_DOCTOR_REPAIR_FINAL_ACCEPTANCE.md`;
3. `docs/T0_COMMON_AGENT_EVENT_FINAL_ACCEPTANCE.md`;
4. `docs/T1_CODEX_ADAPTER_FINAL_ACCEPTANCE.md`;
5. `docs/T1_CODEX_ADAPTER_IMPLEMENTATION_REPORT.md`;
6. `docs/T2_CLAUDE_ADAPTER_IMPLEMENTATION_REPORT.md`;
7. `docs/R2_MULTI_AGENT_EXPANSION_RESEARCH_REPORT.md`;
8. `docs/T0_TRANSCRIPT_CONTRACT_RESET_PLAN.md`;
9. `docs/R1_RUNTIME_SIGNAL_MATRIX.md`;
10. `docs/MOBILE_SESSION_LIFECYCLE_AND_TRANSCRIPT_PLAN.md`;
11. `docs/ROADMAP_AFTER_E10B.md`, especially `T0-R2-D1-T3`;
12. `companion-daemon/internal/agent/contract/`;
13. accepted Codex and Claude adapter implementations and tests;
14. `companion-daemon/internal/term/recorder.go` and its tests;
15. `companion-daemon/internal/term/activity.go` and its tests;
16. `companion-daemon/internal/mux/transcript_capture.go`;
17. `companion-daemon/internal/mux/cmux_delta.go`;
18. current Activity/Transcript HTTP, IPC, mobile read, and retained-session paths.

`T0_TRANSCRIPT_CONTRACT_RESET_PLAN.md` and
`NEXT_SESSION_T0_TRANSCRIPT_HANDOFF.md` are historical T3 inputs. Their old phase
names and old instruction to avoid implementation are superseded. Their Recorder,
raw-input, byte-stream, snapshot, overflow, and migration safety rules remain
authoritative.

Before changing code, produce a short code-path audit in the implementation
report that names every current Transcript producer, store, reader, DTO, mobile
consumer, feature/capability check, and heuristic. Do not implement from the
architecture sketch alone.

## 3. T3 objective

Integrate the accepted common `contract.AgentEvent` model with a bounded,
readable Transcript projection while retaining a safe, explicitly separate
generic terminal fallback:

```text
authoritatively correlated adapter events ──> primary semantic Transcript

Recorder byte-stream projection input ──────> separate fallback/degraded terminal-output channel

Recorder raw PTY bytes ───────────────────────> Live Terminal (unchanged)
```

This must be a production-path integration, not only a DTO, helper, fixture, or
unit-test package. Trace at least one accepted adapter event and one controlled
PTY byte-stream fallback through the actual session-owned producer, projection,
store/read API, and current product consumer or explicitly versioned shadow
consumer. If staged rollout requires a feature flag, the accepted path must still
be executable in production code and covered through its real controller/handler.

T3 does not implement S1 Status, A1 approval UX/authorization, O1 orchestration,
or another production adapter.

### 3.1 Source arbitration

For a session with correctly correlated accepted AgentEvents, those AgentEvents
are the primary semantic Transcript source. Recorder byte-stream projection is a
separate fallback or visibly degraded terminal-output channel; it is not a second
semantic agent-message source.

Do not merge, correlate, suppress, or equate AgentEvent and PTY content through:

- timestamp proximity;
- text equality or substring matching;
- fuzzy matching;
- prompt/command recognition;
- ordering guesses;
- shared CWD or process-name heuristics.

Do not present duplicate semantic and fallback representations as if they were
independent agent messages. Source selection/arbitration must be explicit,
session-owned, deterministic, and testable from declared correlation,
capabilities, and degradation state. When safe arbitration is unavailable, retain
source separation and label the fallback rather than inventing a merge.

## 4. Hard invariants

These are release blockers, not follow-ups:

```text
one session → one Recorder → one PTY reader
Recorder subscriber stream = raw PTY bytes only
raw PTY bytes → Live Terminal unchanged
projection enqueue = copied, bounded, non-blocking
projection failure/overflow → never blocks or corrupts Live Terminal
raw terminal input text → never stored, logged, or projected
PTY echo of terminal input → never allowed to re-enter Transcript
Transcript → readable history only; never lifecycle/input/approval authority
adapter events → accepted only with correct session correlation and provenance
cmux snapshot rules → never run on byte_stream input
WebSocket disconnect → never process-lifecycle authority
Stop/Kill/Delete → lifecycle state, not inferred from Transcript
```

Do not add a second PTY reader. Do not put typed JSON/control events into Recorder
raw subscribers, bootstrap ring, Activity history, or terminal output. Do not
change Terminal bytes to make Transcript prettier.

The raw-input invariant includes terminal echo. Merely avoiding storage of bytes
written through `InputWriter` is insufficient: a PTY may echo the same typed bytes
back through Recorder output. Do not remove echoed text through prompt matching,
string equality, shell parsing, timing windows, or other content heuristics. Use
an explicit content-free input-boundary/echo-suppression mechanism whose state is
owned by the session projection path, or omit/degrade projection whenever input
and output cannot be separated safely. The mechanism may retain bounded counts,
timestamps, or opaque boundaries, but never the input content itself.

The current protocol/mobile authentication contracts must remain operating-system
neutral. Windows support remains deferred. Do not add ConPTY, CNG/TPM, DPAPI,
Named Pipes, Windows Services, NTFS abstractions, or speculative Windows-ready
interfaces. Do not weaken accepted Mac/Android/iOS behavior for future Windows.

## 5. Authority and correlation rules

The frozen T0 `AgentEvent`, provenance tiers, confidence, sequence, cursor bounds,
approval gate, and status precedence are accepted inputs. T3 must not modify them
to simplify projection.

- Only an event bound to the requested Pokit session through accepted correlation
  evidence may enter that session's semantic Transcript.
- `CorrelationUnknown` or cross-session events must not be attributed to a
  Transcript. They may produce only bounded degraded diagnostics that contain no
  event body or private path.
- Advisory, heuristic, prompt-derived, or unknown evidence may be rendered only
  as non-authoritative Transcript material when safe; it cannot establish
  completion, failure, ownership, approval, or lifecycle state.
- Unknown event types fail safely. Preserve ordering and provenance; never invent
  a semantic event to avoid an unknown.
- Approval requests/results may be represented as readable historical segments
  only when the accepted event is authoritative and bound. T3 must not create an
  approval action, authorization decision, or false-positive detector. A1 owns
  approval product behavior.
- Adapter status may be recorded as historical metadata only. S1 owns runtime
  status and its authority policy.

The accepted Codex and Claude adapters currently have deliberate correlation and
approval limitations. Do not bypass them by matching CWD, prompt text, process
name, timestamp proximity, or raw PTY strings.

## 6. Transcript projection boundary

Use repository naming conventions, but keep these responsibilities separate.

### 6.1 Versioned event/store contract

Define an additive, versioned internal Transcript event/segment contract. It must
have immutable per-session ordering, stable IDs, explicit source/provenance,
bounded metadata, observation time, and a closed kind vocabulary including safe
unknown/gap/UI-omitted representation. It may reference the accepted AgentEvent
identity, but must not expose raw provider records or private metadata.

Do not silently mutate or reinterpret existing Activity events. Keep the legacy
read path available during migration unless a production-path compatibility proof
justifies an atomic replacement. Rollback must disable the new projection path,
not alter Recorder ownership or raw broadcast.

### 6.2 Agent-event projector

Project only allowlisted non-secret fields from a validated `contract.AgentEvent`.
Never expose prompts, thinking, command strings, tool input/output, source code,
tokens, signatures, raw JSONL, or private absolute paths merely because adapter
metadata contains them. Apply explicit text and metadata bounds before storage.

Preserve stable event order and deterministic deduplication across incremental
reads/reconnects without losing newly appended events. Cursor corruption,
rotation, unsupported versions, partial records, and adapter errors must become a
bounded degraded/gap result, not replay of an unbounded history or a crash.

### 6.3 Byte-stream fallback projector

For `controlled_pty`, `tmux`, and `localpty`, feed copied Recorder chunks into a
bounded asynchronous queue. Preserve parser/decoder state across arbitrary chunk
boundaries. Commit ordinary printable output at stable boundaries such as newline
or explicit input boundary.

Treat CR, backspace, erase operations, cursor movement, and alternate-screen/TUI
bursts as terminal operations. If safe readable projection is not possible, emit
one bounded `terminal_ui_omitted` marker and resume; never flatten repaint traffic
into giant lines. Preserve ordinary repeated log lines—do not globally deduplicate
them.

Model terminal input as an explicit content-free boundary. Prove that PTY echo
cannot make typed command content enter the Transcript. If the adapter/PTY mode
cannot distinguish echo from output without inspecting or retaining the input
string, omit or degrade that projection region. Never infer an echo by comparing
the output with a command, prompt, or recently typed string.

Queue capacity must have explicit event and byte bounds. A full queue must never
block Recorder. Coalesce a bounded projection-gap indication and expose a metric
or diagnostic without logging content.

### 6.4 Snapshot isolation

`screen_snapshot_delta`/cmux remains a separate best-effort projector with visibly
degraded provenance. Existing sentinel framing is migration input, not a new
universal protocol. Snapshot cleanup, stable-prefix logic, and timeouts must never
run on byte-stream or agent-event input. T3 must not claim cmux Transcript is
reliable.

### 6.5 Storage, API, and required product consumer

The store must be session-isolated, bounded, deterministically ordered, and safe
under concurrent append/read/delete. Retained exited sessions may retain readable
history according to the accepted lifecycle contract; Delete clears the selected
session's history without affecting another session.

Any new public DTO/endpoint must be additive and strictly validated. Do not leak
bearers, tickets, host keys, provider records, raw input, or private paths in DTOs,
logs, errors, snapshots, analytics, or crash reports. Authentication and
permissions must reuse the accepted central REST transport/server authorization;
do not introduce a Transcript-specific credential path.

The mobile/product path must select Transcript availability from declared
capabilities and lifecycle state, not host OS names. Do not make projected text a
fallback for Live Terminal input/control.

T3 completion requires the complete executable product path:

```text
production session-owned producer
→ bounded projector and session-isolated store
→ authenticated session-scoped read API
→ central authenticated mobile transport
→ actual mobile Transcript read and rendering path
```

A helper-only package, daemon-only shadow projection, uncalled controller, or
backend test endpoint is not sufficient. A feature flag is acceptable only when
the production path can be enabled and exercised end to end, fails closed when
disabled or unauthorized, and has automated production-path coverage. Ticket,
bearer, pairing, permissions, and host-origin binding must reuse the accepted
authentication architecture; no credential may enter Transcript content or a
navigation URL.

## 7. Required fixtures and automated evidence

Fixtures must be controlled, redacted, reproducible, bounded, and record source,
version, capture mode, chunk boundaries, expected projection, and whether each
claim is observed or inferred. They must contain no user prompts, secrets, tokens,
private repository/home paths, or personal data.

At minimum cover:

1. bash `pwd`, `ls`, and `echo hello` with sanitized paths/output;
2. UTF-8 and ANSI sequences split at every relevant chunk boundary;
3. CR progress and backspace/erase-line repaint ending in one stable line;
4. alternate-screen/cursor-heavy burst collapsed to one bounded omission marker;
5. repeated ordinary log lines preserved;
6. queue overflow and projector failure while raw terminal delivery continues;
7. one accepted, validated, correctly bound AgentEvent sequence;
8. unknown, malformed, oversized, advisory, and cross-session AgentEvents;
9. cursor resume/replay/dedup/rotation and deterministic ordering;
10. cmux snapshot isolation and visibly degraded output;
11. session delete/history retention and cross-session isolation;
12. credential, raw-input, prompt, tool-I/O, signature, and path leakage negatives.
13. PTY echo of a controlled typed command, proving the command text cannot enter
    Transcript through Recorder output.

Production-path tests—not helper-only tests—must prove:

- Recorder remains the only controlled-PTY reader;
- raw subscriber/bootstrap bytes are byte-for-byte unchanged;
- slow, failed, or full Transcript projection cannot delay raw delivery;
- terminal input is metadata-only at most and raw typed text is absent everywhere;
- a controlled production-path PTY echo test proves typed command text cannot
  re-enter Transcript through terminal output;
- byte-stream and snapshot projectors cannot be selected for the wrong mode;
- valid correlated AgentEvents reach only the intended session Transcript;
- unbound/cross-session events cannot enter it;
- Transcript failures do not change session lifecycle or close a socket/process;
- API and product consumer show ordered bounded output and safe degraded markers;
- the authenticated read API and real mobile controller/component render the
  accepted session Transcript end to end, while unauthorized/wrong-session/error
  results render no stale or cross-session content;
- accepted T0/T1/T2/D1 tests and public DTO snapshots do not regress.

## 8. Required staged implementation order

Implement T3 sequentially. Each checkpoint is an internal implementation boundary,
not an acceptance boundary:

```text
T3-A  code-path audit, versioned Transcript contract, bounded store
T3-B  accepted AgentEvent production projection and explicit source arbitration
T3-C  bounded byte-stream fallback, echo privacy, and snapshot isolation
T3-D  authenticated API and actual mobile read/render consumer
T3-E  integrated production-path regression, privacy, race, and safety gates
```

Do not request independent acceptance for T3-A through T3-D. Intermediate commits
may be pushed for recovery/reviewability, but must be labelled as incomplete and
must not contain the final `REVIEW REQUEST: T3` marker. The only independent
acceptance request covers the complete T3-A through T3-E contract.

## 9. Scope exclusions

Do not start or implement:

- S1 runtime Status state machine or status UX;
- A1 approval actions, approval authorization, or notification UX;
- O1 orchestrator or multi-agent control;
- a Goose, Gemini, OpenCode, Cline, OpenHands, or Aider production adapter;
- D1 activation or an unrestricted repair runner;
- prompt/thinking/tool payload capture;
- transcript persistence across daemon restart unless separately authorized;
- a full VT100/xterm screen emulator;
- iOS/Android authentication changes;
- Windows runtime support or Windows-specific abstraction layers.

If implementing an additive DTO is unavoidable, keep it narrowly Transcript-only
and document why the existing Activity DTO cannot safely carry the contract. Do
not revise the frozen T0 common AgentEvent contract during T3. A genuine
cross-provider gap requires a separate reviewed proposal.

## 10. Disk, process, and temporary-workspace safety

Previous D1 verification accidentally created recursive test execution and more
than 6,000 `go-build*`/`d1-workspace-*` directories, consuming about 86 GiB. T3
must not repeat this failure.

Before tests, record:

```sh
df -h .
pgrep -af 'go test|build-gate|d1-workspace|t3-workspace' || true
find /private/tmp -maxdepth 1 -type d -name 'devremote-t3-*' -print
```

Use one dedicated temporary root, for example
`/private/tmp/devremote-t3-<short-sha>`, and put `TMPDIR`, `GOTMPDIR`, and
`GOCACHE` beneath it. Install a shell `trap` or test cleanup that removes it on
success and failure. Every Go test that creates a workspace or goroutine must use
`t.Cleanup` and prove shutdown.

Never invoke a recursive wildcard test command from a test that includes the
calling package. Use explicit package lists. Do not run the full build gate and a
race/E2E suite concurrently. Run focused tests first, then package tests, then one
full gate sequentially. Do not leave background `go test`, daemon, WebView, or
coding-agent processes.

After tests, report:

```text
disk usage before/after
dedicated temp root removed: yes/no
remaining go-build/t3-workspace directories created by this run: <count>
runaway test/build processes: <count>
```

Do not delete unrelated user caches or worktrees. Cleanup is limited to artifacts
created by the current run unless the user explicitly authorizes more.

## 11. Required gates

Run focused tests while developing, then sequentially run:

```sh
gofmt -l <changed-go-files>            # must print nothing
cd companion-daemon
go build ./...
go vet ./...
go test -race ./internal/agent/contract/... -count=1
go test -race ./internal/agent/adapters/codex/v0_144_1/... -count=1
go test -race ./internal/agent/adapters/claude/v2_1_202/... -count=1
go test -race ./internal/agent/doctor/... -count=1
go test -race ./internal/mux/... ./internal/term/... -count=1
cd ..
sh scripts/build-gate.sh
git diff --check
git status --short
```

Use the dedicated temp/cache root described above. If a pre-existing flaky test
fails, reproduce it on the accepted baseline in a separate clean worktree before
classifying it; do not simply rerun until green or omit it.

Also run repository secret/credential leakage checks and verify no generated
fixture contains private content. Run Android/iOS native gates only if T3 changes
native code; T3 should not require native authentication changes. Physical-device
Transcript smoke may be recorded as M-track only when the automated production
path is complete; it may not excuse a helper-only integration.

Because T3-D requires mobile production code, run and report the mobile TypeScript
typecheck and Jest/unit gates explicitly even if the repository build gate also
runs them. Add production controller/component tests for successful read/render,
loading/error/no-content behavior, session switch, stale response, unmount, retry,
authorization failure, and cross-session isolation. If any native mobile file is
changed, run the corresponding native compile/gate as well.

## 12. Commit, push, and review request

Keep implementation and unrelated cleanup separate. Before committing:

```sh
git diff --check
git status --short
git diff --stat
git diff -- <security- and production-path files>
```

Commit and push normally. Never force-push. Then verify:

```sh
git fetch origin feature/phase10-multi-adapter
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git status --short
git merge-base --is-ancestor 8f7c22def81abf0b932f6dbbacc07325ae2bb12e HEAD
```

Create `docs/T3_TRANSCRIPT_INTEGRATION_IMPLEMENTATION_REPORT.md` containing:

- baseline and final full SHAs;
- code-path audit and legacy-heuristic disposition table;
- final production data flow and ownership diagram;
- AgentEvent correlation/authority mapping;
- byte-stream, snapshot, queue, store, API, and mobile/product boundaries;
- exact fixtures and test-to-invariant evidence matrix;
- DTO/auth/permission compatibility statement;
- gate results and disk/process/temp cleanup evidence;
- honestly deferred physical-device work;
- a literal final marker:

```text
REVIEW REQUEST: T3 Transcript Integration — <full remote SHA>
```

The final implementation commit message or immediately following report commit
must also clearly contain `REVIEW REQUEST: T3`. This prevents a verifier from
reviewing an intermediate commit as the finished result.

Stop after push. Request independent T3 verification. Do not begin S1, A1, O1,
another adapter, Windows work, or a T3 acceptance/handoff document yourself.

## 13. Done condition

T3 is ready for independent review only when all of the following are true:

- the accepted D1 baseline is an ancestor and remote/local tips match;
- the production Transcript path consumes correctly bound common AgentEvents and
  uses them as its primary semantic source, with a safe explicitly separate
  generic byte-stream fallback;
- raw Terminal bytes, Recorder single-reader ownership, lifecycle, authentication,
  and approval authority remain unchanged;
- PTY echo cannot cause typed input content to enter Transcript, proven through
  the production session-owned path without content-matching heuristics;
- projection/store/API/product paths are bounded, session-isolated, non-secret,
  and fail safely;
- the authenticated session-scoped API and actual mobile read/render path work
  end to end and pass TypeScript/mobile tests;
- byte-stream and snapshot behavior are separated;
- production-path race/overflow/failure/correlation tests pass;
- accepted T0/T1/T2/D1 regression gates pass;
- no recursive tests, runaway processes, or run-owned temporary artifacts remain;
- T3-A through T3-E are all complete; no intermediate checkpoint is presented as
  independently acceptable;
- the report and final pushed commit contain the required review marker;
- the worktree is clean and the final full remote SHA is reported.

Anything less is an intermediate T3 commit, not a completion claim.
