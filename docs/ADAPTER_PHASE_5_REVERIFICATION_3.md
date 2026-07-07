# Adapter Phase 5 Reverification 3

Date: 2026-07-07

Executor commit: `bbb8c116e`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope

This pass reviewed the Phase 5 correction after the previous verifier block at
`62157c6`.

Changed files since `62157c6`:

- `companion-daemon/internal/term/fixture_e2e_test.go`

Changed runtime code: none.

Changed tmux/cmux adapter files: none.

## Automated verification

Commands run:

```sh
GOCACHE=/tmp/devremote-phase5-bbb8c11-go-cache go test ./internal/term -run 'TestFixture|Fixture' -count=20 -v
GOCACHE=/tmp/devremote-phase5-bbb8c11-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase5-bbb8c11-go-cache go test ./...
GOCACHE=/tmp/devremote-phase5-bbb8c11-go-cache go test -race ./...
cd mobile && node_modules/.bin/tsc --noEmit
```

Results:

- Fixture E2E tests passed for 20 iterations when run with local listener
  permission.
- `go vet ./...` passed.
- `go test ./...` passed with local listener permission.
- `go test -race ./...` passed with local listener permission.
- `mobile` TypeScript check passed.

Sandbox note:

The sandboxed fixture WebSocket test failed because `httptest.NewServer` could
not bind a local listener:

```text
bind: operation not permitted
```

The same targeted and full Go commands passed with local listener permission.

## Improvements confirmed

### Create/discover/delete is now stateful

The fixture API adapter is now mutable. `TestFixtureE2E_CreateAndDelete`:

- records the initial API session count;
- creates `fixture:test-create`;
- verifies the count increases;
- deletes using the correct `?id=<canonical-id>` query parameter;
- verifies the count returns to the initial value.

This closes the previous false-positive where DELETE used `?session=` and the
fixture create path did not mutate state.

### History endpoint now uses the real API shape

`TestFixtureE2E_UnsupportedCapability` now calls:

```text
GET /api/sessions?history=fixture:f1
```

This closes the previous false-positive where the test used
`/api/sessions/fixture:f1/history`, which is not routed by `HandleSessionsAPI`.

### WebSocket test now performs a real upgrade

`TestFixtureE2E_WebSocket` now uses `httptest.NewServer` and
`websocket.DefaultDialer.Dial`, so it does perform a real WebSocket handshake
against `HandleWS`.

### TelemetryService is now exercised

`TestFixtureE2E_Telemetry` now creates a real `TelemetryService`, runs it, and
asserts fixture sessions appear in `TelemetryService.Snapshot`.

These are real improvements over `71b9af32b`.

## Blocking findings

### 1. Unsupported capability is still not actually tested

Phase 5 requires:

```text
선택 capability 하나는 의도적으로 지원하지 않아 unsupported 경로를 검증한다.
unsupported capability가 예측 가능한 UI/API 결과를 낸다.
```

The current unsupported test no longer calls the wrong endpoint, but it changed
the expected behavior to a supported fallback:

```go
// fixture session has ScreenReader but NOT HistoryReader.
// GET /api/sessions?history=fixture:f1 should use ScreenReader fallback.
```

That proves screen fallback works. It does not prove an unsupported capability
path. Because the fixture session implements `ScreenReader`, the history API is
expected to succeed even without `HistoryReader`.

Required executor action:

- Add a second fixture session, or a second fixture adapter, that intentionally
  lacks the chosen capability and lacks any fallback that makes the request
  succeed.
- Hit the real API/UI boundary for that unsupported operation.
- Assert the exact stable result: status code, content type, JSON error body,
  or stable capability metadata that disables the action.
- Do not count `ScreenReader` fallback as unsupported behavior.

Concrete acceptable example:

- A `fixture:no-history` session that implements neither `HistoryReader` nor
  `ScreenReader`.
- `GET /api/sessions?history=fixture:no-history` returns the expected stable
  unsupported/not-available response.
- The response shape is asserted, not only "non-500".

### 2. WebSocket live stream is opened but live output is not proven

The WebSocket test now connects, but the fixture stream returns `io.EOF`
immediately:

```go
func (s *fixtureE2EStream) Read(p []byte) (int, error) { return 0, io.EOF }
```

The test accepts any read error:

```go
_, msg, err := conn.ReadMessage()
if err != nil {
    t.Logf("WS read (expected for mock): %v", err)
}
_ = msg
```

The actual targeted run repeatedly logged:

```text
WS stream read err: EOF
WS read (expected for mock): websocket: close 1011 (internal server error): error: stream failed
```

This proves the handler can complete a WebSocket handshake and then close on
stream failure. It does not prove the Phase 5 live-stream requirement.

What remains unproven:

- known fixture stream bytes reach the WebSocket client;
- client input reaches `WriteInput` or `TerminalStream.Write`;
- stream close behavior is clean for the fixture path;
- resize is invoked at the boundary.

Required executor action:

- Make the fixture stream emit deterministic content, e.g.
  `fixture stream content`.
- Assert the WebSocket client receives that content.
- Send a client message and assert it reaches the fixture stream/session.
- Assert the connection can close without treating normal fixture EOF as a
  stream failure if that is the intended behavior.

### 3. Resize is still not exercised at the HTTP/WebSocket boundary

Phase 5 requires minimum:

```text
discovery + live stream + resize
```

The mux-level fixture contract has a `Resize` call, but the Phase 5 boundary
test does not call resize through the user-facing boundary. Searching runtime
code shows resize exists on `TerminalStream`, and the browser HTML attempts to
call:

```text
POST /term/size?session=...&rows=...&cols=...
```

but no handler test in this Phase 5 commit verifies fixture resize through an
HTTP/WebSocket boundary.

Required executor action:

- If `/term/size` is a supported endpoint, register it in a test server and
  assert the fixture stream's `Resize(rows, cols)` is called.
- If resize is not currently exposed as a backend HTTP endpoint, document that
  explicitly and add the correct boundary test for the actual resize path
  before accepting Phase 5.
- The assertion must fail if fixture `Resize` is never invoked.

### 4. Mobile/schema boundary is still only a compile check

The mobile TypeScript check passes, and existing code is adapter-name neutral:

- `AgentCard` renders `displayId ?? id`;
- `adapter` is a generic optional string;
- `capabilities` is an optional string array.

However, this commit did not add a mobile typed fixture, parser test, or golden
sample for a third adapter response. Phase 5 asks for the boundary through
mobile schema, not just that TypeScript compiles.

Required executor action:

- Add a mobile typed fixture/sample for a session like:

  ```json
  {
    "id": "fixture:f1",
    "displayId": "f1",
    "adapter": "fixture",
    "capabilities": ["live_stream", "screen"]
  }
  ```

- Include missing/unknown optional capabilities.
- If there is no mobile test harness, add a small documented typed sample or
  compile-time fixture under `mobile` that imports the real `SessionTelemetry`
  type and would fail on schema incompatibility.

### 5. Telemetry assertions are still shallow

The test now uses `TelemetryService`, which is good. But it still only asserts
that a fixture adapter appears and has non-empty `ID`/`Adapter`.

For Phase 5, this is weaker than the API schema assertion. It should also lock
the fields that third adapters depend on:

- canonical `id`;
- `displayId`;
- `capabilities`, including absence of unsupported capabilities;
- `state`;
- `runner`;
- `runnerColor`;
- `events`.

This is not the primary blocker by itself, but it should be tightened while
fixing the unsupported/mobile gaps.

## Verdict

Phase 5 is **not accepted** at `bbb8c116e`.

This commit fixed several previous false positives, especially create/delete
state and real WebSocket upgrade. The remaining gaps are still material:

- unsupported capability behavior is not tested; the current test verifies
  screen fallback success;
- WebSocket live stream content/input/resize is not proven;
- mobile/schema boundary is still not pinned by a typed fixture or test.

Do not start the actual third backend phase until a fixture adapter proves
discovery, live output, input, resize, unsupported capability behavior,
telemetry shape, and mobile schema compatibility through assertions that would
fail if the fixture were not truly integrated.
