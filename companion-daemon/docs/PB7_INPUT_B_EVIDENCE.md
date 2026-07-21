# Input-B Evidence — Versioned Control-Request Protocol (R4)

**Input-B IMPL SHA (R4):** `042005af7`
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
mobile/src/screens/FeedScreen.tsx   — identity-bound inputId tracking, 2-frame ACK, command preservation
mobile/__tests__/feedScreenInput.test.ts — protocol test update
```

## Gate Results

```
go build ./...                    exit 0
go vet ./...                      exit 0
gofmt -l .                        0 files
go test -race ./... -count=1       ALL PASS
npx tsc --noEmit                   clean
npx jest --runInBand               476/476 pass, 35 suites
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
- Strict JSON: DisallowUnknownFields plus exact-one-document decoding;
  trailing JSON is `invalid_request` and never reaches `WriteInput`
- pokitMakeInputID: 32 bytes crypto random → 64 hex chars

### Production binary policy and connection binding

- Paired production (`InsecureLocalOnly == false`) rejects every legacy binary
  input frame before `WriteInput`; it returns `invalid_request`. Raw binary is
  retained only for the explicit local-development mode.
- `newConnectionID` uses crypto/rand and fails the upgrade closed when entropy
  cannot be read.
- hello, `input_pending`, and `input_result` carry the bound connection ID,
  session ID, and generation. The mobile accepts a result only when those
  identities and its `inputId` all match its pending request.

### 2-Frame submitLine
- Text + Enter sent as two independent control requests
- The delayed Enter is tied to its original operation and is cancelled if the
  first request fails or times out
- "Delivered to terminal" only after BOTH results are `accepted`, including
  when the first ACK arrives before the 40ms Enter request becomes pending
- Command is preserved for failure/timeout; the UI says "Not delivered" to
  represent possible partial delivery rather than claiming command success

## Focused Tests

```
TestInputA_DenialViaHandleWS                     PASS
TestInputB_HandleWSAcknowledgesExactGeneration   PASS
TestInputB_HandleWSWriteFailureDoesNotAcknowledge PASS
TestInputB_DuplicateInputIDReplaysCached         PASS
TestInputB_InputIDConflictDifferentPayload       PASS
TestInputB_ProductionRejectsLegacyBinaryBeforeWrite PASS
TestInputB_TrailingTerminalInputIsRejected       PASS
TestInputB_ConnectionIDEntropyFailureIsFailClosed PASS
feedScreenInput.test.ts — two-frame/identity/partial tests PASS
```
