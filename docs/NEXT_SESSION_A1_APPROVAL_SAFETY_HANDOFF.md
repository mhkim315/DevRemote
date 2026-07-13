# Next Session Handoff — A1 Approval Safety

Status: **READY FOR A FRESH EXECUTION AGENT — A1 ONLY**

This handoff is written for a fresh Claude Code execution session backed by
DeepSeek V4 Pro. Follow the bounded packet protocol. Do not start N1, O1, O2,
terminal compatibility certification, local `pokit run` redesign, another
adapter, Windows, or distribution work.

## 0. Canonical repository identity

```text
remote: https://github.com/mhkim315/DevRemote.git
branch: feature/phase10-multi-adapter
accepted S1.1 implementation: 02c8385e3270fbbc4df45e0c71ccad6ebe11a076
accepted S1.1 report HEAD: 895a6b3fb0b5ff821b595dad1e0637133a3eb5bf
accepted S1: 70ef5df28dede7b0f3025eeaab7826f76a229fbf
accepted T3: 9ad6f834f70e88b800e60124c8e408d38bca9d2b
accepted D1: 8f7c22def81abf0b932f6dbbacc07325ae2bb12e
accepted T2: ef4a162c7f9a5644fd52d89501e97f4e62301dfa
accepted T1: 162266f830caaf07bf701d9a1294557432855769
accepted T0: 3ce2604bd333dcb63142b5b1710185a823162efa
```

The executor must fetch and fast-forward before claiming a commit or document is
missing. Verify top-level, canonical remote, branch, full local/remote SHA
equality, clean worktree, and every required ancestry. Do not reset, stash,
force-push, or reconstruct from chat. If the checkout is dirty, preserve it and
use a separate clean clone.

## 1. Read-before-edit order

1. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
2. this handoff;
3. `docs/S1_1_RUNTIME_STATUS_HARDENING_FINAL_ACCEPTANCE.md`;
4. frozen T0 approval contract and `SafeApprovalGate`;
5. accepted T1 Codex `DetectApproval` and conformance fixtures;
6. accepted T2 Claude no-approval-capability evidence;
7. `internal/term/approvalstore.go`, `approval_handler.go`,
   `telemetry_service.go`, `commandbroker.go`, and tests;
8. route/auth composition in `cmd/devremote/app.go` and device permission tests;
9. mobile `client.ts`, `ApprovalCard.tsx`, `DashboardScreen.tsx`,
   `agentActivity.ts`, and tests.

Before editing, produce an audit matrix with exact production caller, current
authority input, missing binding, unsafe fallback, smallest change, and negative
test for each packet below.

## 2. Confirmed baseline facts

- `waiting_approval` activity is already display-only on mobile; CTA visibility
  reads pending approvals, not runtime status.
- production approval ingestion currently comes from the legacy parser branch in
  `TelemetryService.processSession`, not accepted adapter `DetectApproval`.
- legacy ingestion creates `AgentApproval` with a derived ID, JSONL source, fixed
  confidence, terminal-capability-derived options, and no launch/stream generation
  binding.
- Codex 0.144.1 advertises `CapApprovalDetection`; Claude 2.1.202 explicitly does
  not and must continue returning no approval.
- the current store keys by session/approval ID and checks pending/expiry, but has
  no runtime generation, authoritative provenance, requested-action digest, or
  authorization-context binding.
- the current handler resolves the store before placing terminal fallback input;
  it cannot prove delivery and may record success when execution cannot occur.
- remote approval POST is device-auth protected by `PermTerminalInput`, but the
  mobile `resolveApproval` still uses legacy `checkedFetch`/token transport rather
  than the host-bound device transport.

These are audit starting points, not permission to preserve unsafe behavior.

## 3. Frozen authority and safety invariants

- `waiting_approval` status can display attention only. It must never create an
  ApprovalStore record, CTA, option, or action authority.
- approval authority begins only with accepted adapter `DetectApproval`, gated by
  declared `CapApprovalDetection`, exact source-event binding, `SafeApprovalGate`,
  authoritative provenance, and current runtime identity.
- exact session ID, approval ID, provider/adapter version, launch generation,
  stream generation, requested action, expiry, and current authorization context
  must still match at execution time.
- Claude remains no-approval until separately accepted structured evidence exists.
- process/CWD/PTY/screen/prompt text and legacy parser hints cannot manufacture an
  approval.
- unknown/malformed/expired/replayed/stale-generation requests fail closed.
- a user decision is at most once. A failed or unavailable delivery must not be
  falsely recorded as successfully executed.
- do not log or expose raw prompts, terminal input, secrets, paths, tokens, or
  provider payloads. Public DTO additions must be bounded and consumer-justified.
- A1 does not introduce Task/Dispatch, notifications, worker completion,
  orchestration, workspace isolation, Git operations, or automatic execution.

## 4. Sequential task packets

Complete in order. Commit focused checkpoints only after their packet tests pass.
Request independent acceptance once, after A1-A through A1-E pass on one final
tree.

### A1-A — authority audit and contract freeze

Map the actual path:

```text
accepted AgentEvents
→ capability-gated DetectApproval
→ bounded authoritative approval request
→ session-owned ApprovalStore
→ authenticated read/action API
→ exact action delivery
→ terminal resolution state
→ strict mobile consumer
```

Freeze a closed internal state machine and error vocabulary. Decide explicitly
how pending, executing, approved/rejected/resolved, delivery-failed, expired, and
invalidated states behave. Do not reuse runtime activity vocabulary.

Required evidence: current-vs-target call graph, DTO diff decision, threat matrix,
and tests that would fail for every later packet blocker.

### A1-B — generation-bound authoritative ApprovalStore

Replace or harden the store so each pending request is immutably bound to:

- canonical session ID and provider approval ID;
- accepted provider/adapter/version;
- current launch generation and stream generation;
- exact authoritative source event/provenance and bounded confidence;
- bounded allowed actions/options and any action/input contract;
- creation/expiry and current authorization context needed by the action route.

Store operations must be bounded, deterministic, immutable-by-copy, race-safe,
and session-isolated. Runtime replacement, stream generation change, correlation
loss, session delete/unlink/relink, or daemon restart must invalidate or begin
without prior pending authority. Older generation cannot update, resolve, or
restore the current request.

Required negative tests: cross-session ID, duplicate approval ID with changed
action set, older generation, expiry boundary, replay, mutation aliasing, bounded
eviction, delete/recreate, restart, and race churn.

### A1-C — accepted-adapter production ingestion

Wire the same accepted T1/T2 event batch used by Transcript/status into approval
detection. Call `DetectApproval` only when the descriptor declares
`CapApprovalDetection`; isolate error/panic to no approval plus bounded diagnostics.

Codex positive evidence must preserve exact ApprovalID and source-event binding.
Claude and adapters without the capability must produce zero approvals. Remove
legacy parser approval creation as authority; it may remain non-actionable
diagnostic history only if clearly separated.

Do not construct options merely from terminal input capability. Options must come
from accepted provider evidence or a separately frozen, exact provider mapping.
Near-miss, prompt text, PTY, screen, heuristic, id-less, cross-session, unsupported
version, and unavailable correlation cases must produce no actionable request.

### A1-D — authenticated exact-action execution boundary

Harden `HandleApprovalAction` and route composition so execution revalidates the
current store request and current runtime identity immediately before action.
Bind the request body to one exact allowed action and input schema; reject unknown
fields, oversized input, malformed UTF-8, duplicates, expiry, stale generation,
wrong device authorization, and already-terminal requests.

Define delivery semantics before coding. Do not mark an approval successfully
resolved before its required action is durably accepted by the owned delivery
boundary. If terminal fallback cannot confirm delivery, represent that limitation
honestly and fail closed for actions requiring confirmation. No automatic retry of
non-idempotent input. Concurrent double-submit must execute at most once.

Maintain device-auth remote routing with `PermTerminalInput`; legacy Supabase or
an arbitrary bearer must not authorize the paired-device route. Audit logs contain
IDs/outcome codes only, never raw input or prompt.

Required production tests: exact success, delivery failure, concurrent duplicate,
expired between lookup and commit, replacement between lookup and delivery,
revoked device, wrong permission, cross-host/session, and no-command-on-rejection.

### A1-E — strict mobile product path and integrated gate

Route approval actions through the existing host-bound device transport. Add a
strict bounded decoder for approval requests/options/results: closed states and
kinds, exact session binding, IDs, timestamps, field/array/string bounds, unknown
field policy, and generation/contract-version behavior justified by the frozen
DTO.

The UI must render actions only from pending authoritative ApprovalStore DTOs.
`waiting_approval` alone still shows no CTA. Session switches, disconnect,
reconnect, unmount, expiry, replacement, and stale responses must clear or ignore
old approval state. Submission remains disabled while one exact request is in
flight and preserves recoverable input on safe failure.

Run TypeScript typecheck and the complete mobile Jest suite. Add an end-to-end
production-path test from accepted Codex evidence through store/API/device bearer
to mobile decode/action result. Claude no-capability and status-only negatives
must remain green.

## 5. Prohibited shortcuts

- no approval from `agentActivity.status`;
- no approval from legacy parser text, screen/prompt matching, process name, or
  terminal capabilities;
- no static `orchestrationSafe` or generic workflow capability;
- no resolve-before-delivery success;
- no blind `y\n`/`n\n` synthesis without exact accepted provider action mapping;
- no unreviewed public DTO expansion, raw prompt exposure, automatic retry, or
  legacy auth fallback;
- no N1/O1/O2 implementation.

## 6. Gates, report, and stop

Focused tests precede each checkpoint. Final stable tree must pass:

```sh
cd companion-daemon
gofmt -l <touched-go-files>
go build ./...
go vet ./...
go test -race ./internal/agent/... ./internal/term/... ./internal/transcript/... -count=1
cd ..
sh scripts/build-gate.sh
git diff --check
```

The implementation report must contain authority/state matrices, action delivery
semantics, privacy/auth evidence, production call graph, exact tests and skips,
full baseline/implementation/report SHAs, and:

```text
REVIEW REQUEST: A1 Approval Safety — <full implementation SHA>
```

Commit and push without rewriting reviewed history, verify canonical local/remote
equality and clean worktree, then stop for independent A1 verification. Do not
start N1.
