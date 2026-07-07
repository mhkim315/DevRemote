# Adapter Phase 5 Reverification

Date: 2026-07-07

Executor commit: `f000ba731`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope

This pass reviewed the Phase 5 fixture adapter work introduced at
`f000ba731`.

Changed files since the Phase 4 verifier acceptance commit `666cc2c`:

- `companion-daemon/internal/mux/fixture_adapter_test.go`

Changed runtime code: none.

Changed tmux/cmux adapter files: none.

## Automated verification

Commands run:

```sh
GOCACHE=/tmp/devremote-phase5-f000ba7-go-cache go test ./internal/mux -run TestFixtureAdapter_Contract -count=20 -v
GOCACHE=/tmp/devremote-phase5-f000ba7-go-cache go test ./internal/mux -count=1
GOCACHE=/tmp/devremote-phase5-f000ba7-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase5-f000ba7-go-cache go test ./...
GOCACHE=/tmp/devremote-phase5-f000ba7-go-cache go test -race ./...
cd mobile && node_modules/.bin/tsc --noEmit
```

Results:

- `TestFixtureAdapter_Contract` passed for 20 iterations.
- `go test ./internal/mux` passed.
- `go vet ./...` passed.
- `go test ./...` passed when rerun outside the sandbox listener restriction.
- `go test -race ./...` passed when rerun outside the sandbox listener
  restriction.
- `mobile` TypeScript check passed with the locally installed `tsc`.

Sandbox note:

The sandboxed `go test ./...` and `go test -race ./...` runs failed because
`httptest` and IPC tests could not bind local sockets:

```text
bind: operation not permitted
```

The same commands passed with local listener permission.

The initial root-level `npx tsc --noEmit` attempt failed because there is no
root `node_modules/.bin/tsc` and `npx` attempted a registry lookup. The actual
project TypeScript check is under `mobile` and passed via
`mobile/node_modules/.bin/tsc --noEmit`.

## Improvements confirmed

### In-memory fixture adapter exists

`fixture_adapter_test.go` adds a test-only in-memory adapter named `fixture`.
It does not require an external tmux/cmux binary and provides deterministic
fixture sessions.

The fixture supports:

- discovery via `ListSessions`;
- screen content via `ScreenReader`;
- session creation via `CreateSession`;
- session termination via `TerminateSession`;
- live stream opening via `StreamOpener`;
- stream `Read`, `Write`, `Close`, and `Resize`.

### Common mux contract passes

The fixture is wired into the common mux contract harness:

- `RunAdapterContract`;
- `RunScreenHistoryContract`;
- `RunCreateDiscoverTerminateContract`;
- `RunLiveStreamContract`.

This proves the Phase 4 mux-level harness can accept a third adapter-like
implementation without modifying tmux/cmux adapter files.

### Production registration was not changed

The fixture is defined in a `_test.go` file only. There is no production default
registration for the fixture adapter.

## Blocking findings

### 1. HTTP/WebSocket/telemetry/mobile schema boundary E2E is missing

Phase 5 explicitly requires:

```text
HTTP/WebSocket/telemetry/mobile schema 경계까지 end-to-end test를 만든다.
```

`f000ba731` only adds a mux-package contract test. It does not add a
term-handler, app, HTTP, WebSocket, telemetry, or mobile schema test that
registers the fixture adapter through a test composition root and proves the
fixture reaches the external boundaries.

What is still unproven:

- `GET /api/sessions` returns fixture sessions as canonical IDs such as
  `fixture:f1`.
- the existing term handler can open a fixture-backed WebSocket session without
  production handler changes;
- WebSocket read/write/resize behavior works through the real HTTP/WebSocket
  boundary, not only through direct mux interfaces;
- telemetry shape remains stable for an unknown third adapter name;
- mobile parsing/rendering remains stable for a third adapter name and its
  capability set.

This is the main blocker. Passing the mux contract is useful, but it is not the
Phase 5 acceptance criterion.

Required executor action:

- Add a test-only fixture registration/composition root outside production
  default registration.
- Add an HTTP API test proving fixture sessions appear through
  `HandleSessionsAPI` or the app server with canonical fixture IDs and stable
  schema.
- Add a WebSocket test proving fixture sessions can be opened and streamed
  through `HandleWS`, including resize behavior if the WebSocket protocol
  exposes it.
- Add telemetry/schema coverage for fixture sessions so a third adapter name
  does not regress API/mobile assumptions.
- Add or update mobile typed fixture coverage for an unknown/third adapter
  response if the mobile side has a schema/parser boundary.

### 2. Unsupported capability behavior is skipped, not asserted

The fixture intentionally omits `HistoryReader`, `ProcessProvider`, and
`ProcessSnapshotProvider`. That is the right kind of Phase 5 fixture.

However, the current test does not assert a predictable unsupported result at
the UI/API boundary. The contract output shows `HistoryReader` is skipped:

```text
session does not implement HistoryReader
```

Skipping an optional capability inside the mux contract does not prove the user
visible unsupported path is predictable.

What is still unproven:

- history/API behavior for a fixture session without `HistoryReader`;
- WebSocket/API behavior for sessions missing a required live capability, if
  that is the intentionally unsupported capability under test;
- process/telemetry behavior when a session lacks `ProcessProvider` or the
  adapter lacks `ProcessSnapshotProvider`;
- mobile/UI behavior when a capability is absent.

Required executor action:

- Pick one unsupported capability as the explicit Phase 5 unsupported-path
  case.
- Assert the real API/UI-facing result, for example stable JSON error, stable
  empty capability metadata, disabled action state, or omitted telemetry field.
- Do not rely on mux contract skips as evidence for unsupported behavior.

### 3. “Term handler unchanged” is not proven

Phase 5 acceptance says:

```text
term handler 변경 없이 세션이 API와 WebSocket에 나타난다.
```

The commit does not change term handlers, which is good, but it also does not
exercise them with the fixture. Therefore the important claim remains unproven.

The evidence needed is a test that composes the existing handler stack with the
fixture adapter and reaches the handler through normal API/WebSocket entry
points. Direct calls to mux contracts are not enough because handler code is
where canonical IDs, capability metadata, telemetry shaping, and WebSocket
upgrade behavior are integrated.

Required executor action:

- Register the fixture only in test setup.
- Hit the existing handler entry points without editing production handler
  logic.
- Assert the fixture session is visible and usable through those entry points.

### 4. Extension proof is currently narrower than the Phase 5 gate

The current commit does prove one useful property:

```text
fixture 추가 시 tmux/cmux 파일 변경이 0줄이다.
```

But it only proves this at the mux contract layer. Phase 5 is the gate before a
real third backend. A real third backend will fail in practice if external
boundaries still assume only tmux/cmux even when mux contracts pass.

Required executor action:

- Keep the current mux fixture contract.
- Extend the proof upward to the HTTP/WebSocket/telemetry/mobile boundaries.
- Preserve the zero tmux/cmux production-file-change property while doing so.

## Verdict

Phase 5 is **not accepted** at `f000ba731`.

The fixture adapter is a good start and the basic quality gates pass, but the
work stops at the mux package. The Phase 5 acceptance gate requires end-to-end
boundary proof that a third adapter can flow through API, WebSocket, telemetry,
and mobile schema surfaces without handler rewrites.

Do not start the actual third backend phase until these Phase 5 boundary tests
exist and pass.
