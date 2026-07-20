# PA3 Closeout B — Evidence

**Implementation SHA:** `d49ecb7490b8ff1378e73b6d3716c9792dbfc5d7`

## Gate Output

```
=== Backend ===
(BUILD: PASS)
(VET: PASS)

=== Tests ===
ok  	devremote/companion-daemon/cmd/devremote	33.333s
?   	devremote/companion-daemon/cmd/signald	[no test files]
ok  	devremote/companion-daemon/internal/agent	3.608s
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	4.136s
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	9.031s
ok  	devremote/companion-daemon/internal/agent/contract	3.397s
ok  	devremote/companion-daemon/internal/agent/doctor	121.560s
ok  	devremote/companion-daemon/internal/devicetrust	5.735s
?   	devremote/companion-daemon/internal/models	[no test files]
ok  	devremote/companion-daemon/internal/mux	7.639s
ok  	devremote/companion-daemon/internal/sessionid	3.725s
ok  	devremote/companion-daemon/internal/term	19.450s
ok  	devremote/companion-daemon/internal/transcript	1.639s
ok  	devremote/companion-daemon/internal/watcher	1.993s

=== Format ===
(FORMAT: PASS)

=== GATE COMPLETE ===
```

## Specific Gate Results

### Closeout B Transcript Tests (`-race -count=20`)
```
PASS: TestCloseoutB_CurrentLeaseSuccess (x20)
PASS: TestCloseoutB_StaleFeedBytesDropped (x20)
PASS: TestCloseoutB_StaleCloseSessionQueueNoOp (x20)
PASS: TestCloseoutB_ReplaceTranscriptAtomic (x20)
PASS: TestCloseoutB_FailedReplacementRollback (x20)
PASS: TestCloseoutB_ConcurrentGenerationCreation (x20)
```

### Closeout B Recorder Tests (`-race -count=20`)
```
PASS: TestRecorder_CloseoutB_StaleFeedAfterReplace (x20)
PASS: TestRecorder_CloseoutB_StaleStopAfterReplace (x20)
PASS: TestRecorder_CloseoutB_QueueGenCapture (x20)
```

## Changes Summary

| File | Change |
|------|--------|
| `transcript/service.go` | `_gens` → `currentGen`; generation-bound API on FeedBytes/EnableQueue/CloseSessionQueue/AddSnapshotSegment/BeginTUIBurst/EndTUIBurst; atomic ReplaceTranscript |
| `transcript/chunk_queue.go` | Generation lease field; worker checks `isGenerationCurrent()` before processing; stale final-flush skip |
| `transcript/closeout_b_test.go` | 6 race-stable contract tests |
| `transcript/transcript_test.go` | Updated FeedBytes callers (gen=0 backward compat) |
| `term/recorder.go` | `queueGen` field; capture from EnableQueue; pass to all transcript calls |
| `term/recorder_test.go` | 3 integration tests + `pipeStream` helper |
| `term/step3_fixture_test.go` | Updated EnableQueue/FeedBytes/CloseSessionQueue callers |
| `term/process_session_test.go` | Updated AddSnapshotSegment caller |
