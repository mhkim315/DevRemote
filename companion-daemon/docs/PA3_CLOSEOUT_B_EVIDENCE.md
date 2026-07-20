# PA3 Closeout B — Evidence

**Implementation SHA:** `ed02f108965eb7732b29122e19cd187fe8f7ed36`

**R2 Fix:** TOCTOU race closed — `processChunk`, `emitDegradedGuarded`, and
`flushBytesGuarded` hold `Service.mu` across generation check AND mutation.
`matchGeneration` rejects gen=0 when `currentGen` exists (fail-closed).

## Gate Output

```
=== Backend ===
(BUILD: PASS)
(VET: PASS)

=== Tests ===
ok  	devremote/companion-daemon/cmd/devremote	33.385s
?   	devremote/companion-daemon/cmd/signald	[no test files]
ok  	devremote/companion-daemon/internal/agent	4.805s
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	4.994s
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	8.767s
ok  	devremote/companion-daemon/internal/agent/contract	2.326s
ok  	devremote/companion-daemon/internal/agent/doctor	123.646s
ok  	devremote/companion-daemon/internal/devicetrust	5.473s
?   	devremote/companion-daemon/internal/models	[no test files]
ok  	devremote/companion-daemon/internal/mux	8.815s
ok  	devremote/companion-daemon/internal/sessionid	2.645s
ok  	devremote/companion-daemon/internal/term	19.971s
ok  	devremote/companion-daemon/internal/transcript	3.352s
ok  	devremote/companion-daemon/internal/watcher	3.767s

=== Format ===
(FORMAT: PASS)

=== GATE COMPLETE ===
```

## Specific Gate Results

### Closeout B Transcript Tests (`-race -count=20`) — 9 tests
```
PASS: TestCloseoutB_CurrentLeaseSuccess (x20)
PASS: TestCloseoutB_StaleFeedBytesDropped (x20)
PASS: TestCloseoutB_StaleCloseSessionQueueNoOp (x20)
PASS: TestCloseoutB_ReplaceTranscriptAtomic (x20)
PASS: TestCloseoutB_FailedReplacementRollback (x20)
PASS: TestCloseoutB_ConcurrentGenerationCreation (x20)
PASS: TestCloseoutB_GuardedMethodsRejectStaleGeneration (x20)
PASS: TestCloseoutB_ZeroLeaseRejectedAfterGeneration (x20)
PASS: TestCloseoutB_GuardedFlushRejectsStaleGeneration (x20)
```

### Closeout B Recorder Tests (`-race -count=20`) — 3 tests
```
PASS: TestRecorder_CloseoutB_StaleFeedAfterReplace (x20)
PASS: TestRecorder_CloseoutB_StaleStopAfterReplace (x20)
PASS: TestRecorder_CloseoutB_QueueGenCapture (x20)
```

## R2 Findings Addressed

| Finding | Fix |
|---------|-----|
| TOCTOU: worker check/use split | `processChunk`, `emitDegradedGuarded`, `flushBytesGuarded` all acquire `Service.mu` before generation check and hold it through mutation |
| matchGeneration accepts gen=0 unconditionally | Now rejects gen=0 when `currentGen` exists (cur==0 gate only) |
| Tests missing interleaving proof | 3 new tests directly call guarded methods with stale generations post-ReplaceTranscript |

## Changes Summary

| File | Change |
|------|--------|
| `transcript/service.go` | `currentGen` map; generation-bound API; atomic ReplaceTranscript; `matchGeneration` fail-closed; guarded `processChunk`/`emitDegradedGuarded`/`flushBytesGuarded` |
| `transcript/chunk_queue.go` | Generation lease; worker calls guarded methods (no split check/use) |
| `transcript/closeout_b_test.go` | 9 race-stable contract tests including TOCTOU-proof and zero-lease rejection |
| `transcript/transcript_test.go` | Updated FeedBytes callers (gen=0 backward compat) |
| `term/recorder.go` | `queueGen` field; capture from EnableQueue; pass to all transcript calls |
| `term/recorder_test.go` | 3 integration tests + `pipeStream` helper |
| `term/step3_fixture_test.go` | Updated EnableQueue/FeedBytes/CloseSessionQueue callers |
| `term/process_session_test.go` | Updated AddSnapshotSegment caller |
