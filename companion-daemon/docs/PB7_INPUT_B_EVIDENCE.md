# Input-B Evidence — Versioned Control-Request Protocol (R3)

**Input-B IMPL SHA (R3):** (this commit)
**PA4 ACCEPT SHA:** `74560edd`
**PB Ancestry Baseline SHA:** `abe4df1d6`

## Ancestry

```
$ git merge-base --is-ancestor 74560edd8 HEAD
PA4 ACCEPT: ANCESTOR OK
$ git merge-base --is-ancestor abe4df1d6 HEAD
PB BASELINE: ANCESTOR OK
```

## Diff Scope

```
internal/term/input_b_protocol.go  — versioned protocol, cache, handleTerminalInput (NEW)
internal/term/input_b_ack_test.go  — WS integration tests
internal/term/pty.go               — TextMessage handler hook, terminal HTML protocol
internal/term/terminal_transport.go — atomic WriteInput lock
mobile/src/screens/FeedScreen.tsx   — inputId tracking, 2-frame ACK, command preservation
mobile/__tests__/feedScreenInput.test.ts — protocol test update
```

## Gate Results

```
go build ./...                    exit 0
go vet ./...                      exit 0
gofmt -l .                        0 files
go test -race ./... -count=1       ALL PASS (11 packages)
npx tsc --noEmit                   clean
npx jest --runInBand               474/474 pass, 35 suites
```

## Protocol Details

### Client → Server (TextMessage JSON)
```json
{
  "type": "terminal_input",
  "version": 1,
  "sessionId": "controlled_pty:session",
  "generation": 7,
  "inputId": "64-char-lowercase-hex",
  "payload": "base64-encoded-input-bytes"
}
```

### Server → Client (TextMessage JSON)
```json
{
  "type": "input_result",
  "inputId": "64-char-lowercase-hex",
  "connectionId": "32-char-hex",
  "sessionId": "controlled_pty:session",
  "generation": 7,
  "sequence": 1,
  "outcome": "accepted|permission_denied|stale_generation|session_not_found|transport_closed|write_failed|invalid_request|input_too_large"
}
```

### Safety Bounds
- Raw frame: 8192 bytes max
- Decoded payload: 4096 bytes max
- inputId: exactly 64 lowercase hex chars
- Cache: 64 entries, 30s TTL, LRU eviction
- Strict JSON: DisallowUnknownFields + trailing data check
- pokitMakeInputID: 32 bytes crypto random → 64 hex chars

### 2-Frame submitLine
- Text + Enter sent as two independent control requests
- "Delivered to terminal" only after BOTH accepted
- Command preserved until both ACKs arrive
- Partial delivery recognized when one ACK fails/times out

## Focused Tests

```
TestInputA_DenialViaHandleWS                     PASS
TestInputB_HandleWSAcknowledgesExactGeneration   PASS
TestInputB_HandleWSWriteFailureDoesNotAcknowledge PASS
TestInputB_DuplicateInputIDReplaysCached         PASS
TestInputB_InputIDConflictDifferentPayload       PASS
feedScreenInput.test.ts — all 5 tests            PASS
```
