# Adapter Phase 5 Reverification 2

Date: 2026-07-07

Executor commit: `71b9af32b`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope

This pass reviewed the Phase 5 correction after the previous verifier block at
`573823f`.

Changed files since `573823f`:

- `companion-daemon/internal/term/fixture_e2e_test.go`

Changed runtime code: none.

Changed tmux/cmux adapter files: none.

## Automated verification

Commands run:

```sh
GOCACHE=/tmp/devremote-phase5-71b9af3-go-cache go test ./internal/term -run 'TestFixture|Fixture' -count=20 -v
GOCACHE=/tmp/devremote-phase5-71b9af3-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase5-71b9af3-go-cache go test ./...
GOCACHE=/tmp/devremote-phase5-71b9af3-go-cache go test -race ./...
cd mobile && node_modules/.bin/tsc --noEmit
```

Results:

- Fixture E2E tests passed for 20 iterations.
- `go vet ./...` passed.
- `go test ./...` passed when rerun outside the sandbox listener restriction.
- `go test -race ./...` passed when rerun outside the sandbox listener
  restriction.
- `mobile` TypeScript check passed.

Sandbox note:

The sandboxed full Go test runs failed because `httptest` and IPC tests could
not bind local sockets:

```text
bind: operation not permitted
```

The same commands passed with local listener permission.

## Improvements confirmed

### Fixture is now exercised through term handler tests

The new `fixture_e2e_test.go` composes a test-only `fixture` adapter with
`Handlers` and exercises `HandleSessionsAPI` and `HandleWS`. This is a useful
step beyond the previous mux-only proof.

### API discovery now checks third-adapter canonical IDs

`TestFixtureE2E_GetSessions` verifies that fixture sessions appear in
`GET /api/sessions` as canonical IDs:

- `fixture:f1`
- `fixture:f2`

It also checks that fixture sessions have non-empty `displayId` values.

### Production registration still was not changed

The fixture remains test-only. There is still no production default
registration, and no tmux/cmux adapter file changed.

## Blocking findings

### 1. Unsupported history test calls the wrong API shape

`TestFixtureE2E_UnsupportedCapability` calls:

```text
GET /api/sessions/fixture:f1/history
```

But `HandleSessionsAPI` routes GET requests into `HandleSessionsV2`, whose
history API is query-based:

```text
GET /api/sessions?history=<session-id>
```

The handler does not route on `/api/sessions/{id}/history`. Therefore the test
does not exercise the unsupported history path. It receives the normal session
list path and passes because it only rejects HTTP 500.

This is a false positive for the Phase 5 unsupported-capability requirement.

Required executor action:

- Change the test to hit the real history endpoint:

  ```text
  GET /api/sessions?history=fixture:f1
  ```

- Use a fixture session that lacks both `HistoryReader` and any fallback
  capability if the goal is to prove a true unsupported history result.
  The current fixture implements `ScreenReader`, so the real history endpoint
  can legally fall back to screen content.
- Assert the exact expected status/body, not only "not 500".

### 2. WebSocket test does not establish a WebSocket connection

`TestFixtureE2E_WebSocket_Unsupported` calls `HandleWS` with
`httptest.NewRecorder()` and no WebSocket upgrade headers. The observed log is:

```text
WS upgrade err: websocket: the client is not using the websocket protocol
```

The test then passes as long as the response is not HTTP 500.

This does not prove the Phase 5 WebSocket boundary. It does not verify that a
fixture-backed session can:

- complete a real WebSocket upgrade;
- deliver fixture stream bytes to a WebSocket client;
- accept client input;
- handle resize protocol messages if resize is part of the WebSocket protocol.

Existing tests in `pty_ws_test.go` already show the project has the needed
pattern: `httptest.NewServer` plus `websocket.DefaultDialer.Dial`.

Required executor action:

- Add a real WebSocket E2E test using `httptest.NewServer` and Gorilla
  `websocket.DefaultDialer`.
- Register the fixture adapter in that server's handler.
- Dial `/term/ws?session=fixture:f1`.
- Assert the client receives known fixture stream content.
- Assert input and close behavior are exercised.
- If resize is encoded through WebSocket messages, assert `Resize` is invoked
  on the fixture stream.

### 3. Create/delete test does not prove create-discover-terminate state

`TestFixtureE2E_CreateAndDelete` checks only that POST returns an ID with a
`fixture:` prefix and that DELETE returns HTTP 200.

There are two concrete problems:

1. The fixture adapter's `CreateSession` returns the requested name but does
   not mutate adapter state, so the created session is never rediscovered.
2. The DELETE request uses:

   ```text
   DELETE /api/sessions?session=<id>
   ```

   but `HandleSessionCRUD` reads the `id` query parameter:

   ```text
   DELETE /api/sessions?id=<id>
   ```

   Therefore this test can pass without calling the fixture adapter's
   `TerminateSession` path.

Required executor action:

- Make the fixture API adapter stateful in the test.
- After POST, call `GET /api/sessions` and assert the created
  `fixture:<local-id>` appears.
- DELETE with `?id=<canonical-id>`, not `?session=`.
- After DELETE, call `GET /api/sessions` again and assert the session is gone.
- Track whether `TerminateSession` was actually called, or rely on state
  mutation that would fail if it was not.

### 4. Telemetry boundary is only a shallow fallback snapshot check

`TestFixtureE2E_Telemetry` calls `GET /api/sessions` with `Handlers.Telemetry`
left nil. That exercises `buildSimpleSnapshot`, not `TelemetryService.Snapshot`
or the background telemetry path.

It only asserts non-empty `ID` and `Adapter`, which duplicates the discovery
test and does not lock the fields that matter for a third backend:

- `displayId`;
- `capabilities`;
- `state`;
- `runner`;
- `runnerColor`;
- `events`;
- `stale` / error metadata when applicable.

Required executor action:

- Either explicitly scope this as API schema coverage and assert the full
  `SessionTelemetry` shape for fixture sessions, or wire a real
  `TelemetryService` in the test and assert `TelemetryService.Snapshot`.
- Include capability expectations for the fixture session, especially absence
  of unsupported capabilities such as `history` or `process`.

### 5. Mobile/schema boundary remains unproven

The mobile TypeScript compile passes, but no mobile schema/parser/fixture test
was added for a third adapter response. Phase 5 asks for the boundary through
mobile schema, not only that unrelated TypeScript still compiles.

Required executor action:

- Add a mobile typed fixture or parser test if the project has a mobile test
  harness.
- If there is no test harness, add a documented golden fixture or typed sample
  that the mobile `AgentCard` / session type accepts for an unknown adapter
  with fixture capabilities.
- The fixture should include an unknown adapter name and missing optional
  capabilities.

## Verdict

Phase 5 is **not accepted** at `71b9af32b`.

The correction moved in the right direction by adding term-handler fixture
tests. However, several tests are currently false positives:

- unsupported history uses the wrong endpoint shape;
- WebSocket does not perform a real WebSocket upgrade;
- delete uses the wrong query parameter;
- create/delete do not prove state mutation;
- telemetry/mobile boundary coverage is too shallow or absent.

Do not start the actual third backend phase until Phase 5 proves the real
HTTP/WebSocket/telemetry/mobile boundaries with assertions that would fail if
the fixture adapter were not actually integrated.
