# PA3 Closeout C — Evidence

**Implementation SHA:** `5be4b0cd7a2d55d6a1f5f07e17d75e02fadbb9d4`

**Scope:** `term/` test files only. No production code changes.

## Gate Output

```
=== Backend ===
(BUILD: PASS)
(VET: PASS)

=== Tests ===
ok  	devremote/companion-daemon/cmd/devremote	35.620s
?   	devremote/companion-daemon/cmd/signald	[no test files]
ok  	devremote/companion-daemon/internal/agent	2.780s
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	2.859s
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	10.342s
ok  	devremote/companion-daemon/internal/agent/contract	2.548s
ok  	devremote/companion-daemon/internal/agent/doctor	123.378s
ok  	devremote/companion-daemon/internal/devicetrust	5.746s
?   	devremote/companion-daemon/internal/models	[no test files]
ok  	devremote/companion-daemon/internal/mux	9.447s
ok  	devremote/companion-daemon/internal/sessionid	4.995s
ok  	devremote/companion-daemon/internal/term	21.265s
ok  	devremote/companion-daemon/internal/transcript	3.350s
ok  	devremote/companion-daemon/internal/watcher	4.310s

=== Format ===
(FORMAT: PASS)

=== GATE COMPLETE ===
```

## Specific Gate Results

### Step3 + ProcessSession Tests (`-race -count=20`)
```
PASS: TestStep3_ActivityEndpoint_Returns410 (x20)
PASS: TestStep3_HistoryEndpoint_Returns410 (x20)
PASS: TestStep3_NormalList_Returns200 (x20)
PASS: TestStep3_TranscriptAPI_HistoryAvailable (x20)
PASS: TestStep3_TranscriptAPI_HistoryAvailable_AfterOutput (x20)
PASS: TestProcessSession_AcceptedAdapterIndependent (x20)
PASS: TestProcessSession_VersionConflictNoSegments (x20)
PASS: TestProcessSession_SnapshotSuppressedAfterInput (x20)
```

### Closeout A Tests (`-race -count=20`)
```
PASS: TestRecorder_CloseoutA_StaleEOFCannotMarkReplacement (x20)
PASS: TestRecorder_CloseoutA_StaleStopCannotAffectReplacement (x20)
PASS: TestRecorder_CloseoutA_MatchingRecordsTermination (x20)
```

### Closeout B Tests (`-race -count=20`)
```
PASS: TestRecorder_CloseoutB_StaleFeedAfterReplace (x20)
PASS: TestRecorder_CloseoutB_StaleStopAfterReplace (x20)
PASS: TestRecorder_CloseoutB_QueueGenCapture (x20)
PASS: TestCloseoutB_* (9 transcript tests, x20)
```

## Fixes Applied

| Category | Issue | Fix |
|----------|-------|-----|
| Gen=0 calls | `step3_fixture_test.go:112` FeedBytes(0) silently dropped | Use EnableQueue + real gen |
| Gen=0 calls | `process_session_test.go:169` AddSnapshotSegment(0) silently dropped | Use EnableQueue + real gen |
| ActivityBuffer shim | 10 tests using nil `ActivityBuffer` stub (Append no-op, List returns nil) | Removed stub types; all tests use real `transcript.Service` via `SetTranscriptService` |
| t.Log assertions | `DeltaMarkerNotVisible` stripANSI check, `NormalANSINotDelta` classification | Changed to `t.Error`/`t.Errorf` |
| Self-checking no-ops | `TerminalInput_NoRawText` self-constructed `ActivityEvent` and checked against itself | Rewrote to use `BeginInput`/`EndInput` on real Transcript Service |
| Self-checking no-ops | `StreamOnlyInputFallback` manual `ActivityEvent` with hardcoded values | Removed; stream.Write verification already covers the path |
| Missing transcript flush | `MultipleSubscribers` checked transcript before queue drain | Added `CloseSessionQueue` call + newline to test data |

## Production Files Preserved

No changes to: `recorder.go`, `service.go`, `chunk_queue.go`, or any other production file.
