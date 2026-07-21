# Input-B Evidence — Versioned Control-Request Protocol (R4 remediation)

**Input-B R4 remediation IMPL SHA:** `514bf83ce`
**Input-B R5 edge-case IMPL SHA:** `78b9090ce`
**Prior Input-B R4 IMPL SHA:** `042005af7`
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
npx jest --runInBand               478/478 pass, 35 suites
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
  retained only for the explicit local-development mode. The rejection carries
  the bound connection/session/generation plus `reason: "update_required"`.
- `newConnectionID` uses crypto/rand and fails the upgrade closed when entropy
  cannot be read.
- hello, `input_pending`, and `input_result` carry the bound connection ID,
  session ID, and generation. The mobile accepts a result only when those
  identities and its `inputId` all match its pending request.
- Raw frames above 8192 bytes and decoded payloads above 4096 bytes return the
  closed `input_too_large` result rather than being silently ignored.
- The per-connection result cache does not evict live accepted entries. At 64
  entries it rejects a new request before writing, preserving retry safety;
  only TTL expiry frees capacity. Permission-denied results are burst-limited
  to three and refill at ten per minute.
- Legacy binary is classified before the permission gate in paired production,
  so both authorized and unauthorized old clients receive `update_required`.

### 2-Frame submitLine
- Text + Enter sent as two independent control requests
- The delayed Enter is tied to its original operation and is cancelled if the
  first request fails or times out
- "Delivered to terminal" only after BOTH results are `accepted`, including
  when the first ACK arrives before the 40ms Enter request becomes pending
- Command is preserved for failure/timeout; the UI says "Not delivered" to
  represent possible partial delivery rather than claiming command success
- A reconnect hello clears all old pending IDs, and a failed/timeout line
  abandons every known sibling ID so a late accepted result cannot overwrite
  its partial-delivery state. A second Send cannot overlap a live line.
- Soft-keyboard Enter uses the same preservation path as Send. The page tags
  each line frame with an operation ID and `text`/`enter` role, so macro/paste
  traffic cannot claim a line's ACK slots. A completed line clears the editor
  only when its acknowledged text still equals the current editor content.

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
TestInputB_ExactRawAndDecodedBounds               PASS
TestInputB_StrictParserRejectsMalformedUnknownAndTrailing PASS
TestInputB_ClosedOutcomesAndPermissionLimiter     PASS
TestInputB_CacheCapacityNeverEvictsAcceptedReplay PASS
TestInputB_ReplacedCapturedTransportCannotWriteNewGeneration PASS
feedScreenInput.test.ts — reconnect/late-sibling/overlap tests PASS
```
