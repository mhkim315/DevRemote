# Adapter Phase 5 Reverification 6

Date: 2026-07-07

Executor commit: `51b6f911c`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope

This pass reviewed the Phase 5 correction after the previous verifier block at
`d533603`.

Changed files since `d533603`:

- `companion-daemon/internal/term/fixture_e2e_test.go`

Changed runtime code: none.

Changed tmux/cmux adapter files: none.

## Automated verification

Commands run:

```sh
GOCACHE=/tmp/devremote-phase5-51b6f91-go-cache go test ./internal/term -run 'TestFixture|Fixture' -count=20 -v
GOCACHE=/tmp/devremote-phase5-51b6f91-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase5-51b6f91-go-cache go test ./...
GOCACHE=/tmp/devremote-phase5-51b6f91-go-cache go test -race ./...
GOCACHE=/tmp/devremote-phase5-51b6f91-go-cache go test ./cmd/devremote -run TestApp_ShutdownOrder -count=20 -v
cd mobile && node_modules/.bin/tsc --noEmit
```

Results:

- Targeted Phase 5 fixture suite passed for 20 iterations with local listener
  permission.
- `go vet ./...` passed.
- `go test ./...` failed once in `cmd/devremote TestApp_ShutdownOrder`, then
  passed on rerun.
- `go test -race ./...` passed.
- `TestApp_ShutdownOrder` passed for 20 targeted iterations on rerun.
- `mobile` TypeScript check passed.

Sandbox note:

The first sandboxed targeted fixture run failed because `httptest.NewServer`
could not bind a local listener:

```text
bind: operation not permitted
```

The targeted fixture run passed with local listener permission.

## Improvements confirmed

### WebSocket lifecycle panic is fixed

The fixture stream now uses `sync.Once` in `Close`:

```go
func (s *fixtureStream) Close() error {
    s.closeOnce.Do(func() {
        close(s.done)
        s.pr.Close()
    })
    return nil
}
```

The previous `close of closed channel` server panic no longer appeared in the
targeted 20-iteration run.

### WebSocket now asserts initial screen and live stream frames

`TestFixtureE2E_WebSocket` now asserts:

- frame 1 contains `fixture screen content`;
- frame 2 contains `FIXTURE_STREAM_CONTENT`.

This closes the previous gap where a non-empty first frame could pass without
proving live stream delivery.

### Unsupported history and create/delete remain covered

The bare unsupported session and create/discover/delete state checks remain in
place and continue to pass.

## Blocking findings

### 1. WebSocket input relay is still not asserted

`TestFixtureE2E_WebSocket` sends input:

```go
testInput := []byte("echo hello\n")
conn.WriteMessage(websocket.TextMessage, testInput)
time.Sleep(200 * time.Millisecond)
```

but there is still no assertion that the input reached the actual stream or
session used by `HandleWS`.

The fixture stream has an `input strings.Builder`, and direct stream tests
prove `stream.Write` records input. But the WebSocket E2E test does not retain
or inspect the stream instance returned by `fixtureFullSession.OpenStream`.
Therefore the test would still pass if `HandleWS` stopped forwarding client
messages to `stream.Write`.

Required executor action:

- Make `fixtureFullSession.OpenStream` expose the created stream through a
  shared sink, channel, or test-owned session object.
- After `conn.WriteMessage`, assert the actual handler-owned stream received
  `echo hello\n`.
- Remove the naked `time.Sleep` as proof; use polling with timeout or a channel
  that fails deterministically.

### 2. Resize remains a direct interface test, not a boundary test

`TestFixtureE2E_Resize` directly calls:

```go
stream := newFixtureStream()
stream.Resize(40, 120)
```

The test also documents:

```text
No server-side Go handler exists for this route
```

That means the test is honest, but it still does not satisfy the Phase 5
boundary requirement. The plan requires minimum `discovery + live stream +
resize`, and asks for HTTP/WebSocket/telemetry/mobile schema boundary tests.

Runtime routing still shows no registered `/term/size` handler:

```go
serveMux.HandleFunc("/term/ws", h.AuthMiddleware(h.HandleWS))
serveMux.HandleFunc("/term/", h.AuthMiddleware(h.HandleHTML))
```

Required executor action:

- If resize is required for Phase 5 acceptance, add or identify the actual
  server/IPC/WebSocket boundary and assert fixture `Resize(rows, cols)` is
  invoked through that boundary.
- If there is intentionally no server-side resize boundary, update the Phase 5
  acceptance scope explicitly and do not claim boundary resize proof.
- A direct `TerminalStream.Resize` test can remain as a contract check, but it
  is not an E2E boundary proof.

### 3. Mobile schema remains backend-side only

`TestFixtureE2E_MobileSchema` checks backend JSON keys from Go. This is useful
API schema coverage, but it still does not import the mobile
`SessionTelemetry` type or add a mobile-side typed fixture.

This is not as severe as the WebSocket input/resize blockers, because the
mobile code is already adapter-name neutral and TypeScript passes. Still, it is
weaker than the Phase 5 wording.

Required executor action:

- Add a small mobile-side typed fixture importing the real `SessionTelemetry`
  type, or explicitly document that backend API golden coverage is the accepted
  mobile-schema boundary for this phase.
- Include `adapter: "fixture"`, `displayId`, known capabilities, and missing or
  unknown optional capabilities.

## Verdict

Phase 5 is **not accepted** at `51b6f911c`.

This is the closest Phase 5 has been so far: the targeted fixture suite is now
stable, WebSocket screen/live output is asserted, and the previous server panic
is gone. The remaining blockers are narrower and concrete:

- WebSocket input relay is still not asserted at the boundary.
- Resize is still a direct stream method call, not an E2E boundary test.
- Mobile schema is still checked only from backend Go tests.

Do not start the actual third backend phase until WebSocket input and resize
boundary semantics are either proven by tests or explicitly scoped out in the
Phase 5 acceptance criteria.
