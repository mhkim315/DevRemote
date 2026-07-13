# Next Session Handoff — T3 Transcript Integration Remediation

Status: **HISTORICAL — T3 ACCEPTED AT `9ad6f834f70e88b800e60124c8e408d38bca9d2b`**

T3 is complete. This document is retained as remediation history and must not be
used to restart implementation. The acceptance record is
`docs/T3_TRANSCRIPT_INTEGRATION_FINAL_ACCEPTANCE.md`; the next authorized stage
is S1 through `docs/NEXT_SESSION_S1_RUNTIME_STATUS_HANDOFF.md`.

This was the authoritative continuation document for the T3 remediation.
It supplements `NEXT_SESSION_T3_TRANSCRIPT_INTEGRATION_HANDOFF.md`; where the two
documents differ about current implementation status or the remediation starting
point, this document wins. The original handoff remains authoritative for the
complete T3 privacy, product-path, source-arbitration, testing, and done criteria.

Do not begin S1, A1, O1, another adapter, or Windows work. Do not declare T3
complete after fixing only the backend cursor path.

## 1. Repository, branch, and exact starting point

```text
repository: https://github.com/mhkim315/DevRemote.git
branch: feature/phase10-multi-adapter
latest independently reviewed implementation tip: 0c82cf02ae2f48ef67ee66b5e2d609965e39d34c
review verdict at that tip: REJECT
accepted D1 / T3 baseline: 8f7c22def81abf0b932f6dbbacc07325ae2bb12e
accepted R2: 6f940b03bbb451dd0ddcfc6bf7ce5f86bbec3b50
accepted T2: ef4a162c7f9a5644fd52d89501e97f4e62301dfa
accepted T1: 162266f830caaf07bf701d9a1294557432855769
accepted T0: 3ce2604bd333dcb63142b5b1710185a823162efa
```

The handoff commit that adds this document will be newer than `0c82cf0`. Always
start from the remote branch tip, not from the reviewed implementation SHA.

Run before reading or editing code:

```sh
cd /Users/mhk/Documents/codex/DevRemote
git remote set-url origin https://github.com/mhkim315/DevRemote.git
git fetch origin feature/phase10-multi-adapter
git switch feature/phase10-multi-adapter
git merge --ff-only origin/feature/phase10-multi-adapter
git status --short
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git log --oneline -30
git merge-base --is-ancestor 0c82cf02ae2f48ef67ee66b5e2d609965e39d34c HEAD
git merge-base --is-ancestor 8f7c22def81abf0b932f6dbbacc07325ae2bb12e HEAD
git merge-base --is-ancestor 6f940b03bbb451dd0ddcfc6bf7ce5f86bbec3b50 HEAD
git merge-base --is-ancestor ef4a162c7f9a5644fd52d89501e97f4e62301dfa HEAD
git merge-base --is-ancestor 162266f830caaf07bf701d9a1294557432855769 HEAD
git merge-base --is-ancestor 3ce2604bd333dcb63142b5b1710185a823162efa HEAD
```

Every ancestry command must exit 0. Local and remote tips must match, and the
worktree must be clean. If this document is missing locally, fetch it before
asking the user to paste it:

```sh
git fetch origin feature/phase10-multi-adapter
git show origin/feature/phase10-multi-adapter:docs/NEXT_SESSION_T3_REMEDIATION_HANDOFF.md
```

Never reset, force-push, or discard another agent's work. If the remote advances,
fetch, rebase normally, rerun all gates, and report the final full remote SHA.

## 2. Read order

Read these before changing code:

1. this document;
2. `docs/NEXT_SESSION_T3_TRANSCRIPT_INTEGRATION_HANDOFF.md` in full;
3. `docs/T3_TRANSCRIPT_INTEGRATION_IMPLEMENTATION_REPORT.md`, treating its
   completion boxes and old review SHA as historical claims, not acceptance;
4. `docs/T0_COMMON_AGENT_EVENT_FINAL_ACCEPTANCE.md`;
5. `docs/T1_CODEX_ADAPTER_FINAL_ACCEPTANCE.md` and the production adapter/tests;
6. `docs/T2_CLAUDE_ADAPTER_IMPLEMENTATION_REPORT.md` and adapter/tests;
7. `docs/D1_ADAPTER_DOCTOR_REPAIR_FINAL_ACCEPTANCE.md`;
8. `companion-daemon/internal/agent/contract/`, especially opaque cursor and
   resource-bound rules;
9. `companion-daemon/internal/term/adapter_state.go`, `reader.go`,
   `telemetry.go`, and `telemetry_service.go`;
10. all `internal/transcript/` production code and tests;
11. Transcript HTTP/auth handlers and mobile DTO/read/render code;
12. every terminal input producer: WebSocket, IPC, local attach, host input, and
    snapshot/TUI capture.

## 3. Why `0c82cf0` was rejected

The commit improved provider-specific version parsing, restored discovered PID
passthrough, removed legacy semantic submission, and introduced
`PositionedRecord`. Those are useful changes. It did not connect the position to
the frozen T1/T2 adapter boundary.

### 3.1 Position is discarded before `ReadEvents`

`adapterState.buildAdapterInput()` converts `[]PositionedRecord` back to
`[][]byte`. The accepted adapters receive only array indices and cannot see
`Position`, `basePos`, or the original stream coordinate.

### 3.2 The opaque cursor is parsed outside its owner

`parseAdapterCursor()` assumes the version-specific string format
`<pos>:<anchor>`. T0 defines `contract.Cursor` as opaque. Production orchestration
must store and return it unchanged; it must not parse, forge, rebase, or depend on
an adapter-private encoding.

### 3.3 Offset is applied twice

The bridge uses cursor position to select a suffix and then passes the unchanged
absolute cursor with that suffix. The adapter applies `cur.nextPos` again to the
already-sliced input. This yields an empty/out-of-range suffix or anchor mismatch.

### 3.4 A sliding suffix is not a full-prefix snapshot

The accepted Codex and Claude implementations explicitly consume ordered
full-prefix snapshots. Keeping the last 2,000 records and changing their array
indices cannot preserve position-based IDs, Seq, anchors, or version authority.

### 3.5 Version conflict handling and PID need production proof

`updateVersion()` must not keep a previous accepted version after a conflicting,
malformed, or unsupported authority. Discovered PID must be passed to
`LaunchCorrelation` and tested for match, mismatch, and missing values.

### 3.6 No production regression tests accompanied the cursor series

The commits from `38b21df` through `0c82cf0` repeatedly changed production cursor
state without adding the required two-poll, long-stream, rotation, authority, or
Transcript end-to-end tests. A green adapter unit suite alone does not prove the
production bridge.

## 4. Frozen constraints — do not work around them

- T0 cursor is opaque, bounded, and adapter-owned.
- T1/T2 accepted history and conformance behavior must remain intact.
- Do not silently modify T0, the fixed harness, or accepted fixtures to make T3
  pass.
- Do not use timestamps, text equality, fuzzy matching, prompts, CWD, or process
  name alone to correlate sessions.
- Only correctly bound accepted adapter events may enter semantic Transcript.
- Legacy parsers may continue serving legacy telemetry/status, but their events
  must never be submitted to accepted semantic Transcript.
- Recorder remains the only PTY reader; raw Terminal bytes remain unchanged.
- Transcript must be bounded and may degrade safely. It does not need to pretend
  unlimited semantic retention.
- Unknown/malformed/version-drift input fails closed.

## 5. Recommended remediation design

Do not continue the `PositionedRecord + parse opaque cursor + sliding suffix`
approach. The smallest safe design that preserves frozen adapters is a bounded
immutable full-prefix ingestion epoch.

### 5.1 Before the bound is reached

For each stream generation:

```text
reader generation + path
→ append complete records to an ordered prefix beginning at source record 0
→ pass the entire retained prefix and the unchanged opaque adapter cursor
→ store returned opaque NextCursor unchanged
→ project only accepted, correctly correlated adapter events
```

The prefix must be bounded by both record count and bytes using contract-owned or
equally strict constants. Do not trim the beginning and do not reindex records.

### 5.2 When the bound would be exceeded

Fail safely instead of fabricating continuation:

```text
mark adapter semantic ingestion overflowed/degraded for this generation
stop calling the accepted adapter with incomplete/rebased input
emit at most one bounded, content-free degraded diagnostic
keep byte-stream/snapshot fallback explicitly separate
keep Live Terminal unaffected
do not project legacy parser events as semantic AgentEvents
```

This deliberately trades unlimited semantic continuation for correctness and
bounded resources. A future separately reviewed adapter API may support a base
offset/window cursor, but T3 must not invent that contract locally.

### 5.3 Generation reset

`ReadRawLines` must expose a typed generation change for inode replacement and
same-inode truncation. Path change is already known by the caller. Any one of
these must atomically reset:

- retained prefix and byte count;
- opaque adapter cursor;
- accepted version and confirmation/conflict state;
- overflow/degraded marker state;
- source generation/path/inode identity.

Partial EOF is not a generation change and must not advance the byte cursor.

### 5.4 Version and correlation state

Use provider-specific structural validation consistent with accepted T1/T2:

- Codex: exact `session_meta.payload.cli_version == 0.144.1` at the authority
  location; additional/conflicting session metadata fails closed.
- Claude: exact top-level `version == 2.1.202` according to its accepted adapter
  rules; missing/non-string/conflicting values fail closed.
- Never use substring matching.
- `confirmed`, `unconfirmed`, `conflicting`, and `rejected` must be distinct.
- A conflict/rejection immediately revokes correlation for that generation.
- Provider, version, session, generation, and actual PID binding must all match.

Prefer deriving authority from the accepted adapter result rather than maintaining
a weaker parallel parser. If a small production status wrapper is necessary, its
rules must be tested against the accepted fixtures and must never be more
permissive than the adapter.

### 5.5 Semantic source boundary

The production branch must be structurally equivalent to:

```text
if accepted adapter + bound correlation + accepted events:
    semantic Transcript projection
else:
    no semantic AgentEvent projection
```

Legacy events remain outside this branch. Adapter errors, zero events, unknown
events, cursor failures, overflow, or unavailable correlation must not activate a
legacy semantic fallback.

## 6. Required tests before another review request

Tests must call production functions/state, not copied mock logic.

### 6.1 Reader/generation

- complete line plus partial EOF, then completion: exactly once;
- inode replacement;
- same-inode truncation with immediate new complete records;
- path change;
- each generation change resets all adapter-derived state;
- partial EOF does not reset generation.

### 6.2 Accepted adapter production bridge

Run the same matrix for Codex and Claude:

- first poll contains authority plus events;
- second and third polls append events and use the opaque cursor unchanged;
- one-shot and paged output have identical `(ID, Seq, Type, SessionID)` tuples;
- no duplicate projection across polls;
- zero-event result with advanced `NextCursor` makes progress;
- malformed cursor/result fails closed without panic;
- unsupported/missing/conflicting version revokes authority;
- actual PID match succeeds; PID mismatch/missing fails closed;
- wrong session/provider/generation fails closed.

### 6.3 Resource bound and overflow

Use injectable small bounds in tests and at least one realistic 2,001+ record
case:

- prefix remains position 0 based until the bound;
- byte and record bounds are both enforced;
- overflow does not trim/rebase/replay events;
- overflow produces one bounded degraded indication, not one per poll;
- no legacy semantic event appears after overflow;
- Live Terminal and fallback remain operational.

### 6.4 Transcript production path

- accepted event reaches only its session's semantic store/API/mobile DTO;
- adapter failure and unavailable correlation produce zero semantic segments;
- semantic and byte/snapshot fallback channels remain separate;
- store pagination does not duplicate IDs;
- delete clears Transcript, while stop/kill/exit retain it as specified;
- authenticated API rejects missing/wrong-session credentials;
- mobile validator rejects malformed, cross-session, oversized, and mixed-channel
  DTOs;
- actual mobile rendering tests cover semantic-only, fallback-only, and both
  separated sections.

### 6.5 Privacy/input paths

Re-audit all production input sources, not only WebSocket. Prove that typed input
cannot re-enter Transcript through PTY echo for WebSocket, IPC/local attach, host
input, and snapshot paths. If a path lacks an explicit content-free boundary,
omit/degrade its projection. Do not use timing, chunk count, newline, command, or
prompt heuristics to remove echo.

## 7. Commands and gates

Use run-owned caches under `/tmp` and remove them after verification. Do not leave
thousands of `go-build*`, Doctor workspace, Expo prebuild, or Metro artifacts.

At minimum:

```sh
cd companion-daemon
gofmt -l internal/term internal/transcript
GOCACHE=/tmp/devremote-t3-handoff-go-cache go build ./...
GOCACHE=/tmp/devremote-t3-handoff-go-cache go vet ./...
GOCACHE=/tmp/devremote-t3-handoff-go-cache go test -race ./internal/agent/contract ./internal/agent/adapters/codex/v0_144_1 ./internal/agent/adapters/claude/v2_1_202 ./internal/transcript ./internal/term -count=1

cd ../mobile
npx tsc --noEmit
npx jest --runInBand

cd ..
sh scripts/build-gate.sh
git diff --check
git status --short
```

If sandbox networking prevents `httptest` from binding a local port, report that
environmental limitation and rerun the authoritative gate in the normal execution
environment. Do not call an unexecuted gate PASS.

Clean only artifacts created by the current run. Never delete user worktrees,
untracked user files, shared caches, or unrelated simulator/device data.

## 8. Commit, push, and review protocol

Keep the remediation focused and reviewable. A reasonable split is:

1. production reader/adapter-ingestion state plus focused tests;
2. remaining Transcript/mobile/privacy regression fixes;
3. final report update and review marker.

Before every push:

```sh
git fetch origin feature/phase10-multi-adapter
git rebase origin/feature/phase10-multi-adapter
git status --short
```

After the full gate passes, push normally and verify:

```sh
git push origin feature/phase10-multi-adapter
git fetch origin feature/phase10-multi-adapter
git rev-parse HEAD
git rev-parse origin/feature/phase10-multi-adapter
git status --short
```

Do not request review on an intermediate cursor-only commit. Update
`docs/T3_TRANSCRIPT_INTEGRATION_IMPLEMENTATION_REPORT.md` so it accurately names
the final production path, every test, honest M-track items, and the final full
remote SHA. Remove or supersede stale completion claims.

The final report and final pushed commit must contain:

```text
REVIEW REQUEST: T3 Transcript Integration — <full remote SHA>
```

Then stop for independent verification. Do not write an acceptance document and
do not begin S1.

## 9. Final done condition

T3 is reviewable only when both this remediation document and every done condition
in the original T3 handoff are satisfied. In particular:

- no external parsing or rewriting of opaque adapter cursors;
- no sliding/rebased suffix passed as a full-prefix snapshot;
- bounded overflow is explicit and fail-safe;
- provider/version/session/generation/PID correlation is proven;
- legacy parser cannot become semantic authority;
- production long-stream, rotation, conflict, and pagination tests pass;
- echo privacy is proven across every enabled input/fallback path;
- authenticated API and actual mobile read/render tests pass;
- T0/T1/T2/D1 regressions and the full build gate pass;
- local and remote tips match, worktree is clean, and no run-owned artifacts or
  processes remain.

Anything less remains T3 in progress.
