# Next Session Handoff — S1-E Final Correctness and Acceptance

Status: **READY FOR A FRESH EXECUTION AGENT — S1-E ONLY**

This is the authoritative next execution packet for completing S1. It applies
the accepted milestone-boundary revision after S1-D. Read it together with the
umbrella contract in `docs/NEXT_SESSION_S1_RUNTIME_STATUS_HANDOFF.md`; if an old
sequence description conflicts with this packet, this newer boundary revision
controls the remaining S1-E scope.

Do not begin S1.1, A1, O1, O2, Notifications, another adapter, or Windows work.

## 0. Canonical repository and exact baseline

```text
repository: https://github.com/mhkim315/DevRemote.git
branch: feature/phase10-multi-adapter
accepted S1-D checkpoint: 17553e0106404bf75a75899c720ff7be0df3fad0
accepted S1-B/C authority hardening: 2f3fdfcbff0fc21e78f9c077f8cf2cef61024d6d
accepted T3 baseline: 9ad6f834f70e88b800e60124c8e408d38bca9d2b
```

Before reading local files or editing:

```sh
cd /path/to/DevRemote
git remote set-url origin https://github.com/mhkim315/DevRemote.git
git fetch origin feature/phase10-multi-adapter
git switch feature/phase10-multi-adapter
git merge --ff-only origin/feature/phase10-multi-adapter
git status --short
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git merge-base --is-ancestor 17553e0106404bf75a75899c720ff7be0df3fad0 HEAD
git merge-base --is-ancestor 2f3fdfcbff0fc21e78f9c077f8cf2cef61024d6d HEAD
git merge-base --is-ancestor 9ad6f834f70e88b800e60124c8e408d38bca9d2b HEAD
```

All ancestry commands must exit 0. Local and remote full SHA must match and the
worktree must be clean. If this document is missing, fetch it from the canonical
remote before asking the user or reconstructing it.

## 1. Read-before-edit order

1. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
2. this handoff;
3. `docs/NEXT_SESSION_S1_RUNTIME_STATUS_HANDOFF.md`;
4. `docs/S1_RUNTIME_STATUS_IMPLEMENTATION_REPORT.md`;
5. `docs/S1_1_RUNTIME_STATUS_HARDENING_PLAN.md` for the explicit deferred
   boundary only—do not execute it;
6. frozen T0 contract and accepted T1/T2 adapter status tests;
7. `agent_status_store.go`, `adapter_state.go`, `reader.go`, and
   `telemetry_service.go` with their S1 tests;
8. mobile `agentActivity.ts`, `sessionPoller.ts`, `DashboardScreen.tsx`, and
   their production-path tests;
9. lifecycle delete, Registry disappearance, link/unlink, and launch-binding
   cleanup paths.

Before changing code, record the exact current production caller and a failing
or missing regression proof for each item below. Do not redesign a path that is
already safe.

## 2. Already accepted — regression only

S1-D is accepted at `17553e0106404bf75a75899c720ff7be0df3fad0`.
Do not reimplement it. Preserve and rerun these properties:

- backend uses frozen `contract.ContractVersion` (`t0.1`);
- mobile requires all DTO fields and rejects unknown fields/vocabulary;
- empty provenance is rejected;
- `observedAt` is bounded and calendar-valid RFC3339;
- unsupported/future contract versions fail closed and render an explicit
  non-current/unavailable result without a new public status enum;
- stale/degraded activity is not rendered as current `Working`/`Thinking`;
- a malformed accepted DTO never falls back to legacy heuristic authority;
- session polling is singleflight and drops post-unmount results;
- lifecycle and approval action visibility do not derive from agent activity;
- authenticated sessions-read and delete routes retain their accepted boundaries.

The backend golden/fixture-to-mobile-decoder compatibility proof remains an S1-E
integration requirement. It must assert `t0.1` and the complete field shape; do
not introduce a generated schema system merely to share one physical constant
across Go and TypeScript.

## 3. S1-E objective

Close only current frozen-contract correctness gaps and produce the final S1
review evidence. Recovery may temporarily show absent, unknown, unavailable, or
degraded status, but it must never confidently show prior activity as current.

The existing authority split remains frozen:

```text
daemon lifecycle       -> Catalog / LifecycleService
agent activity         -> accepted T1/T2 GetStatus over correlated AgentEvents
observation health     -> polling/connectivity freshness
approval action state  -> ApprovalStore, deferred to A1
```

Process, CWD, PTY, prompt, Transcript, and screen content cannot become agent
status evidence.

## 4. Required correctness packets

### E1 — Stream-generation invalidation

Current reusable mechanisms are `ReadRawLines.GenerationChanged`,
`adapterState.streamGen`, the accepted adapter cursor, and event `Seq`. Do not
create a second cursor or ordering system.

When `streamGen` changes, invalidate any positive record from the prior
generation immediately. This must happen even if the new generation contains
only version metadata, non-status events, partial data, or no authoritative
status event. Until new correlated status evidence arrives, the product result
must be absent or explicitly unknown/degraded/non-current.

Required production regression:

```text
generation N -> working or waiting_approval
inode/path/truncation generation change
generation N+1 -> no status-producing event
API/mobile observation -> never the generation-N positive current status
```

Also prove an older generation update/revoke cannot overwrite N+1. Do not add a
same-generation revision mechanism here unless a reachable production test
proves that the existing sequential poll + opaque cursor can regress.

### E2 — Connectivity and restart freshness

A mobile polling failure or expired last-success freshness must make previously
displayed activity non-current. `DashboardScreen` must not keep a previously
fresh `working`/`thinking` card authoritative indefinitely while the daemon is
unreachable. Reconnect must accept only the new successful response.

After a daemon restart, the in-memory status store is empty by design. An
accepted Codex/Claude product session with no current `agentActivity` must not
silently present legacy heuristic `agentStatus` as accepted authority. Choose the
smallest repository-consistent presentation (`Unavailable`, unknown, degraded,
or suppressed); do not invent a new public AgentStatus.

Required tests:

- successful fetch -> activity current -> network failure/freshness expiry ->
  activity non-current;
- disconnect -> reconnect -> only the post-reconnect response is committed;
- daemon restart/new TelemetryService -> no inherited positive status;
- accepted session after restart with absent DTO -> no legacy-authority fallback;
- unsupported contract version -> explicit unavailable/decoder outcome and no
  legacy fallback.

### E3 — Cleanup evidence

Confirm through production owners, not only direct map manipulation:

- lifecycle/history delete clears `AgentStatusStore`;
- Registry disappearance clears the exact canonical session record;
- link, unlink, and relink clear the exact canonical status record;
- same-ID recreation starts without the previous result;
- clearing one session does not affect another.

Reuse `TelemetryService.Clear`, `LifecycleService.Delete`, and existing canonical
ID migration. Do not add another cleanup registry. Existing tests may be extended
where they check only `TelemetryService.sessions` and not `statusStore`.

### E4 — Final integration and frozen-contract regression

Add or retain focused coverage for:

- backend DTO fixture/golden decoded by the production mobile validator;
- daemon restart;
- mobile disconnect/reconnect and freshness expiry;
- stream generation and stale-event ordering;
- termination/delete/link/unlink/relink cleanup;
- unsupported contract version;
- T0/T1/T2/D1/T3 invariants, lifecycle authority, and approval-action separation;
- race tests under repeated update, generation reset, clear, and recreate.

## 5. Approval boundary that S1-E must preserve

`waiting_approval` is display-only. It must never create an ApprovalStore entry,
show or authorize an approval action by itself, or become authorization input.

Future A1 authority must come from an ApprovalStore record bound to exact session
ID, approval ID, authoritative approval provenance, and current generation. Do
not implement that A1 work during S1-E.

## 6. Explicitly prohibited S1-E scope

- new T0 status/provenance values or modified `ResolveStatus` precedence;
- process/CWD/PTY/prompt/screen/Transcript status inference;
- raw cursors or new public degraded reasons;
- launch-generation/process-start identity hardening (S1.1);
- same-generation replay framework beyond a demonstrated current correctness bug;
- approval CTA/action/authorization changes;
- broker, orchestrator, worktree, Git, automated execution, rework, or merge work;
- S1.1, A1, O1, O2, Notifications, another adapter, or Windows implementation.

## 7. Required gates and final report

Run focused tests first, then on the final tree run the complete commands from
the umbrella S1 handoff, including:

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

Update `docs/S1_RUNTIME_STATUS_IMPLEMENTATION_REPORT.md` with:

- S1-A through S1-E commit/SHA traceability;
- requirement-to-production-path-test matrix;
- confirmed reuse of cursor/generation/sequential polling;
- restart/reconnect/cleanup behavior;
- unsupported-version behavior;
- exact commands, counts, skips, and cleanup evidence;
- limitations and physical-device M-track items;
- one final marker:

```text
REVIEW REQUEST: S1 Rich Agent Runtime Status — <full implementation SHA>
```

Commit and push S1-E, verify local/remote full SHA equality and a clean worktree,
then stop for independent S1 verification. Do not start S1.1 or A1.
