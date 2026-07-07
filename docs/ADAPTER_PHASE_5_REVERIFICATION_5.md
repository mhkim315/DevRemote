# Adapter Phase 5 Reverification 5

Date: 2026-07-07

Executor commit: `2676476e1`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope

This pass reviewed the Phase 5 correction after the previous verifier block at
`402834d`.

Changed files since `402834d`:

- `companion-daemon/internal/term/fixture_e2e_test.go`

Changed runtime code: none.

Changed tmux/cmux adapter files: none.

## Automated verification

Commands run:

```sh
GOCACHE=/tmp/devremote-phase5-2676476-go-cache go test ./internal/term -run 'TestFixture|Fixture' -count=20 -v
GOCACHE=/tmp/devremote-phase5-2676476-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase5-2676476-go-cache go test ./...
GOCACHE=/tmp/devremote-phase5-2676476-go-cache go test -race ./...
cd mobile && node_modules/.bin/tsc --noEmit
```

Results:

- `go vet ./...` passed.
- `go test ./...` passed in a single full-suite run with local listener
  permission.
- `go test -race ./...` passed in a single full-suite run with local listener
  permission.
- `mobile` TypeScript check passed.
- The targeted Phase 5 fixture suite returned exit 0 under repeated execution,
  but printed repeated HTTP server panics:

  ```text
  http: panic serving 127.0.0.1:...: close of closed channel
  devremote/companion-daemon/internal/term.(*fixtureStream).Close(...)
      companion-daemon/internal/term/fixture_e2e_test.go:128
  devremote/companion-daemon/internal/term.(*Handlers).HandleWS(...)
      companion-daemon/internal/term/pty.go:271
  ```

Sandbox note:

The first sandboxed targeted run failed because `httptest.NewServer` could not
bind a local listener:

```text
bind: operation not permitted
```

The repeated targeted run was rerun with local listener permission. The command
exited 0, but the server-side panic output is still a real correctness issue.

## Improvements confirmed

### Targeted suite no longer exits non-zero under repetition

The previous commit intermittently failed `TestFixtureE2E_WebSocket` with a
1011 close frame. This commit changed the fixture stream to an `io.Pipe` and
the repeated targeted command now exits 0.

### Stream helper test checks fixture stream content and write capture

`TestFixtureE2E_StreamContent` directly verifies:

- `newFixtureStream().Read` returns `FIXTURE_STREAM_CONTENT`;
- `newFixtureStream().Write` records `test input`.

This is useful at the fixture-stream unit level.

## Blocking findings

### 1. WebSocket test still causes server-side panics

Even though the targeted command exits 0, repeated runs produce many server
panic logs:

```text
panic: close of closed channel
devremote/companion-daemon/internal/term.(*fixtureStream).Close(...)
```

The cause is fixture stream `Close`:

```go
func (s *fixtureStream) Close() error {
    close(s.done)
    s.pr.Close()
    return nil
}
```

`HandleWS` can close the stream through more than one path:

- deferred close after `OpenStream`;
- explicit close at the end of the WebSocket loop.

Closing `done` twice panics. Because the panic happens in the HTTP server
goroutine, the test can still pass while printing `http: panic serving ...`.

This cannot be accepted as a Phase 5 gate. A fixture intended to prove safe
third-adapter integration must be idempotent under the handler's lifecycle.

Required executor action:

- Make fixture `Close` idempotent, e.g. with `sync.Once`.
- Re-run:

  ```sh
  GOCACHE=/tmp/devremote-phase5-2676476-go-cache go test ./internal/term -run 'TestFixture|Fixture' -count=20 -v
  ```

- The output must not contain `panic serving`, `close of closed channel`, or
  similar server-side panics.

### 2. WebSocket E2E still does not assert live stream content

`TestFixtureE2E_WebSocket` reads only one WebSocket message and asserts only
that it is non-empty:

```go
_, msg, err := conn.ReadMessage()
if len(msg) == 0 {
    t.Error("WS received empty message")
}
```

But `HandleWS` sends a `ReadScreen` preflight frame before stream frames. That
means this test can pass by receiving `fixture screen content` even if the live
stream payload `FIXTURE_STREAM_CONTENT` never reaches the WebSocket client.

`TestFixtureE2E_StreamContent` proves the stream object can emit the content
when read directly, but it does not prove `HandleWS` relays that content through
the WebSocket boundary.

Required executor action:

- In `TestFixtureE2E_WebSocket`, read until both expected boundary messages are
  observed, or otherwise assert the exact sequence:
  - initial screen frame contains `fixture screen content`;
  - live stream frame contains `FIXTURE_STREAM_CONTENT`.
- The test must fail if the stream reader goroutine is removed or broken.

### 3. WebSocket input is still not verified at the boundary

`TestFixtureE2E_WebSocket` writes:

```go
conn.WriteMessage(websocket.TextMessage, []byte("echo hello\n"))
```

but no assertion checks whether that input reached the stream or session used
by the handler. The separate `TestFixtureE2E_StreamContent` proves direct
`stream.Write` records input, but it does not prove WebSocket input relay.

Required executor action:

- Share the actual stream instance used by `fixtureFullSession.OpenStream` with
  the test, or record writes through a shared sink.
- Assert that `echo hello\n` appears in that sink after the WebSocket write.
- The test must fail if `HandleWS` stops forwarding client messages.

### 4. Resize is still not tested through a real boundary

`TestFixtureE2E_ResizeAtBoundary` still calls the stream method directly:

```go
stream := newFixtureStream()
stream.Resize(40, 120)
```

The test comment now explicitly says there is no server-side Go handler for
`/term/size`. That means this is not an HTTP/WebSocket boundary test and cannot
satisfy the Phase 5 requirement by itself:

```text
최소 discovery + live stream + resize를 구현한다.
HTTP/WebSocket/telemetry/mobile schema 경계까지 end-to-end test를 만든다.
```

Runtime routing confirms there is no registered resize HTTP handler:

```go
serveMux.HandleFunc("/term/ws", h.AuthMiddleware(h.HandleWS))
serveMux.HandleFunc("/term/", h.AuthMiddleware(h.HandleHTML))
```

`/term/size` is only referenced by client-side HTML/React code. There is no Go
handler path proving resize reaches `TerminalStream.Resize`.

Required executor action:

- If resize is required at the server boundary, add/identify the server
  boundary and test fixture `Resize` through it.
- If server-side resize is intentionally unsupported today, explicitly adjust
  the Phase 5 claim. Do not name the direct method test
  `ResizeAtBoundary`.
- A direct stream method call is acceptable as a mux/contract test, but it is
  not a Phase 5 boundary proof.

### 5. Mobile schema is still not pinned from mobile code

`TestFixtureE2E_MobileSchema` checks backend JSON keys from Go. This is useful
API schema coverage, but it still does not import the mobile `SessionTelemetry`
type or add a mobile-side fixture.

This remains weaker than requested. It is not the primary blocker compared with
WebSocket/resize, but should be addressed before acceptance or explicitly
scoped in the plan.

## Verdict

Phase 5 is **not accepted** at `2676476e1`.

This commit improves repeated test exit status and adds direct stream content
coverage, but the Phase 5 proof still has material gaps:

- WebSocket test output contains server-side panics.
- WebSocket boundary does not assert live stream content.
- WebSocket boundary does not assert input relay.
- Resize is still a direct method call, not a boundary test.
- Mobile schema is still backend-side only.

Do not start the actual third backend phase until the fixture E2E tests prove
the real WebSocket lifecycle, stream output, input relay, and resize boundary
without hidden server panics.
