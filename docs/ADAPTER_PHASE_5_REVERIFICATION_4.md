# Adapter Phase 5 Reverification 4

Date: 2026-07-07

Executor commit: `ee708b6fb`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope

This pass reviewed the Phase 5 correction after the previous verifier block at
`7e2c072`.

Changed files since `7e2c072`:

- `companion-daemon/internal/term/fixture_e2e_test.go`

Changed runtime code: none.

Changed tmux/cmux adapter files: none.

## Automated verification

Commands run:

```sh
GOCACHE=/tmp/devremote-phase5-ee708b6-go-cache go test ./internal/term -run 'TestFixture|Fixture' -count=20 -v
GOCACHE=/tmp/devremote-phase5-ee708b6-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase5-ee708b6-go-cache go test ./...
GOCACHE=/tmp/devremote-phase5-ee708b6-go-cache go test -race ./...
cd mobile && node_modules/.bin/tsc --noEmit
```

Results:

- `go vet ./...` passed.
- `go test ./...` passed in a single full-suite run with local listener
  permission.
- `go test -race ./...` passed in a single full-suite run with local listener
  permission.
- `mobile` TypeScript check passed.
- The targeted Phase 5 fixture suite **failed** under repeated execution:

  ```text
  --- FAIL: TestFixtureE2E_WebSocket
      fixture_e2e_test.go:269: WS read: websocket: close 1011 (internal server error): error: stream failed
  FAIL
  FAIL devremote/companion-daemon/internal/term
  ```

Sandbox note:

The first sandboxed targeted run failed because `httptest.NewServer` could not
bind a local listener:

```text
bind: operation not permitted
```

The repeated targeted run was rerun with local listener permission and still
failed intermittently. That failure is a real test/runtime issue, not a sandbox
artifact.

## Improvements confirmed

### Unsupported capability is now a real unsupported path

`TestFixtureE2E_UnsupportedCapability` now uses a bare session with no
`ScreenReader`, no `HistoryReader`, and no `StreamOpener`.

It calls the real history endpoint:

```text
GET /api/sessions?history=bare:bare
```

and asserts HTTP `404` plus a stable body containing `history unavailable`.
This closes the previous problem where screen fallback success was counted as
unsupported behavior.

### Create/delete state mutation remains covered

`TestFixtureE2E_CreateAndDelete` still proves create/discover/delete through
`HandleSessionsAPI` using the correct `?id=<canonical-id>` delete query.

### Telemetry and API/mobile schema checks were tightened

The telemetry test now checks non-empty `ID`, `State`, and capabilities for the
fixture adapter. The API schema test checks the expected JSON keys and confirms
the third adapter name `fixture` appears in the response.

These are useful improvements.

## Blocking findings

### 1. Targeted Phase 5 fixture suite is flaky/failing

The repeated targeted command failed:

```sh
GOCACHE=/tmp/devremote-phase5-ee708b6-go-cache go test ./internal/term -run 'TestFixture|Fixture' -count=20 -v
```

The failing path is `TestFixtureE2E_WebSocket`. The fixture stream returns EOF
after its fixed reader is exhausted:

```go
func (s *fixtureStream) Read(p []byte) (int, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.pr.Read(p)
}
```

`HandleWS` treats stream read errors as fatal stream failures and sends a 1011
close frame:

```text
websocket: close 1011 (internal server error): error: stream failed
```

The test sometimes receives the initial screen frame first and passes, and
sometimes receives the close/error first and fails. That makes the Phase 5 gate
non-deterministic.

Required executor action:

- Make the fixture stream deterministic and non-flaky.
- Do not allow normal fixture EOF to race the initial screen assertion.
- If the stream is meant to end normally, make the handler/test distinguish
  normal completion from fatal stream failure.
- Keep `go test ./internal/term -run 'TestFixture|Fixture' -count=20 -v`
  green.

### 2. WebSocket test still does not prove fixture stream content

`fixtureStream` emits:

```text
FIXTURE_STREAM_CONTENT
```

but `TestFixtureE2E_WebSocket` only asserts that the first WebSocket message is
non-empty:

```go
if len(msg) == 0 {
    t.Error("WS received empty message, want content")
}
```

`HandleWS` sends `ReadScreen` output as an initial frame before stream reads.
Therefore this assertion can pass by receiving `fixture screen content` even if
the live stream path never delivers `FIXTURE_STREAM_CONTENT`.

Required executor action:

- Assert the exact initial screen frame if that is part of the contract.
- Then assert the live stream frame contains `FIXTURE_STREAM_CONTENT`.
- The test must fail if `OpenStream().Read` is never relayed to the WebSocket
  client.

### 3. WebSocket input is sent but not verified

The WebSocket test sends:

```go
conn.WriteMessage(websocket.TextMessage, []byte("echo test\n"))
```

but no assertion checks whether that input reached either:

- `fixtureStream.Write`, or
- `fixtureFullSession.WriteInput`.

Since the fixture session does not implement `InputWriter`, `HandleWS` should
fall back to `stream.Write(msg)`. The fixture stream records writes in
`strings.Builder`, but the test does not retain or inspect the actual stream
instance used by `OpenStream`.

Required executor action:

- Make the fixture session expose the stream instance used by the handler, or
  record writes through a shared test sink.
- Assert the sent WebSocket input appears in the fixture stream/session.
- The assertion must fail if input relay is broken.

### 4. Resize is still not tested at a user-facing boundary

`TestFixtureE2E_ResizeAtBoundary` directly calls:

```go
stream := newFixtureStream()
stream.Resize(40, 120)
```

That is not an HTTP/WebSocket/IPC boundary test. It does not use `Handlers`,
`Registry`, `HandleWS`, or any registered route.

Runtime route search found no registered `/term/size` handler:

```go
serveMux.HandleFunc("/term/ws", h.AuthMiddleware(h.HandleWS))
serveMux.HandleFunc("/term/", h.AuthMiddleware(h.HandleHTML))
```

The browser HTML posts to `/term/size?...`, but that path is currently covered
by the generic `/term/` HTML handler pattern, not a resize handler. The Phase 5
test comment says resize is triggered by `/term/size`, but the test does not
prove such a boundary exists or works.

Required executor action:

- If resize is meant to be HTTP-based, add or identify the actual resize
  handler and test it through a real request against `Handlers`/`Registry`.
- If resize is meant to be IPC-based, test the IPC resize path with the fixture
  adapter.
- If resize is currently not exposed through a user-facing boundary, document
  that as a product gap and do not claim Phase 5 boundary resize proof.
- The test must fail if fixture `Resize(rows, cols)` is never invoked through
  the intended boundary.

### 5. Mobile schema is still checked only from the backend side

`TestFixtureE2E_MobileSchema` checks backend JSON keys in Go. That is useful API
schema coverage, but it still does not import or type-check the mobile
`SessionTelemetry` type with a third-adapter fixture.

The mobile side currently appears adapter-name neutral and `tsc --noEmit`
passes, so this is weaker than the WebSocket/resize blockers. Still, the Phase
5 mobile boundary is not as strong as requested.

Required executor action:

- Add a mobile-side typed fixture/sample importing the real mobile
  `SessionTelemetry` type, or explicitly document that the Go API golden is the
  accepted schema boundary for this phase.
- Include unknown adapter and missing/unknown optional capabilities.

## Verdict

Phase 5 is **not accepted** at `ee708b6fb`.

This commit closes the unsupported-history blocker and improves schema
assertions. However, the targeted fixture E2E suite is now actually failing
under repetition, and the remaining WebSocket/resize tests still do not prove
the required live stream, input, and resize behavior through real boundaries.

Do not start the actual third backend phase until:

- `go test ./internal/term -run 'TestFixture|Fixture' -count=20 -v` is stable;
- WebSocket assertions prove both initial screen and live stream content;
- WebSocket input relay is asserted;
- resize is invoked through the actual supported boundary;
- mobile/schema compatibility is pinned or explicitly scoped.
