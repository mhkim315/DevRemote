# Adapter Phase 5 Reverification 7

Date: 2026-07-07

Executor commit: `7429256c8`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope

This pass reviewed the Phase 5 correction after the previous verifier block at
`dceb620`.

Changed files since `dceb620`:

- `companion-daemon/internal/term/fixture_e2e_test.go`

Changed runtime code: none.

Changed tmux/cmux adapter files: none.

## Automated verification

Commands run:

```sh
GOCACHE=/tmp/devremote-phase5-7429256-go-cache go test ./internal/term -run 'TestFixture|Fixture' -count=20 -v
GOCACHE=/tmp/devremote-phase5-7429256-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase5-7429256-go-cache go test ./...
GOCACHE=/tmp/devremote-phase5-7429256-go-cache go test -race ./...
cd mobile && node_modules/.bin/tsc --noEmit
```

Results:

- Targeted Phase 5 fixture suite passed for 20 iterations with local listener
  permission.
- `go vet ./...` passed.
- `go test ./...` passed.
- `go test -race ./...` passed.
- `mobile` TypeScript check passed.

Sandbox note:

The first sandboxed targeted fixture run failed because `httptest.NewServer`
could not bind a local listener:

```text
bind: operation not permitted
```

The targeted fixture run passed with local listener permission.

## Improvements confirmed

### WebSocket input relay is now asserted

`fixtureFullSession` now implements `InputWriter` and captures the last input.
`TestFixtureE2E_WebSocket` sends:

```go
echo hello\n
```

and asserts that the handler-owned fixture session received that exact input.
This closes the previous WebSocket input-relay blocker.

### WebSocket output and lifecycle remain stable

The targeted 20-iteration run confirms:

- initial screen frame contains `fixture screen content`;
- live stream frame contains `FIXTURE_STREAM_CONTENT`;
- no previous `close of closed channel` server panic appears.

### Unsupported history, create/delete, telemetry, and API schema remain covered

The existing unsupported bare session, create/discover/delete mutation,
`TelemetryService` snapshot, and backend API schema tests remain in place.

## Blocking findings

### 1. Resize test uses a fake test-only route, not the production boundary

`TestFixtureE2E_Resize` now creates an HTTP server, but it registers a
test-local fake route:

```go
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    switch {
    case strings.Contains(r.URL.Path, "/size"):
        // Simulate /term/size handler...
```

This does not exercise the production handler stack. Runtime routing still has
no registered `/term/size` handler:

```go
serveMux.HandleFunc("/term/ws", h.AuthMiddleware(h.HandleWS))
serveMux.HandleFunc("/term/", h.AuthMiddleware(h.HandleHTML))
```

The client-side HTML and mobile code call `/term/size`, but the Go server has
no production resize endpoint. A test-only fake handler proves that resize
would work if a handler existed; it does not prove the current product boundary
works.

Phase 5 requires:

```text
최소 discovery + live stream + resize를 구현한다.
HTTP/WebSocket/telemetry/mobile schema 경계까지 end-to-end test를 만든다.
```

The current resize test is still not a valid E2E boundary proof.

Required executor action:

- Either add/identify the real production resize boundary and test fixture
  `Resize(rows, cols)` through that boundary, or explicitly update the Phase 5
  acceptance scope to say server-side resize is not currently exposed and is
  not required for accepting the fixture adapter.
- Do not simulate `/term/size` inside the test and count it as production
  boundary coverage.
- If `/term/size` is supposed to exist, add a real handler and route it through
  `Handlers`/`Registry`, then test that route.

### 2. Mobile schema is still only checked from Go backend tests

`TestFixtureE2E_MobileSchema` checks JSON keys in the Go response. That is
useful API schema coverage, but it still does not import or type-check the
mobile `SessionTelemetry` type with a fixture/third-adapter sample.

The mobile code appears adapter-name neutral, and `tsc --noEmit` passes, but
Phase 5 asked for the mobile schema boundary. A backend-only JSON key test is
weaker than a mobile typed fixture.

Required executor action:

- Add a small mobile-side typed fixture/sample importing the real
  `SessionTelemetry` type, or explicitly document that backend API golden
  coverage is the accepted mobile-schema boundary for this phase.
- Include:
  - `adapter: "fixture"`;
  - `displayId`;
  - `capabilities`;
  - missing or unknown optional capabilities.

## Verdict

Phase 5 is **not accepted** at `7429256c8`.

This commit resolves the WebSocket input blocker and the automated verification
suite is now clean. The remaining issue is narrower but still decisive:
resize is not proven through the real product boundary. The current test
creates a fake `/size` handler that production does not register.

Do not start the actual third backend phase until resize boundary semantics are
made real and tested, or the Phase 5 acceptance criteria are explicitly scoped
so that server-side resize is not part of the required E2E proof.
