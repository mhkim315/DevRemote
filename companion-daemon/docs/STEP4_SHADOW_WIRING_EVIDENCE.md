# STEP4 Evidence — Operational Canonical Timeline Shadow Wiring

Implementation commit: `5a6a600c5de774184cd6f2ec5ed666a0a00af6e6`

STEP4 R2 implementation commit: `6d1a72d35c7d2f3f0680b7602e82c0eae1c3d82e`

## Scope and authority boundary

STEP4 adds only an explicitly constructed, default-off `internal/timeline/writer`
component and the smallest daemon composition/configuration surface needed to
own it. `--enable-timeline-shadow` is false by default; when enabled, the
optional `--timeline-shadow-path` selects an absolute file path, otherwise the
writer uses the daemon user's `.pokit/timeline-shadow.jsonl` path.

The component is not CT-P2: it has no provider normalizer, no event producer,
no adapter, and no call into a Timeline reader, projection, or authority. The
existing terminal transport, approval, input, lifecycle, recorder, handlers,
Transcript, REST/WS routes, and mobile DTOs contain no new Timeline call.

`rg -n 'timeline/(contract|writer)' cmd/devremote internal/term ../mobile`
returns only the explicit daemon composition import and its composition test;
there is no terminal or mobile import.

## Fail-open behavior

- `writer.Open` is explicitly constructed with a selected path. It has no
  package initialization, global singleton, default constructor side effect, or
  runtime goroutine.
- `Writer.Append` validates and frames a bounded CT-P1 envelope, then holds
  only its writer-owned mutex around append/`Sync`. It returns `false` on every
  invalid, closed, write, or sync condition; it does not return an error into a
  primary authority path.
- The writer records appended/dropped/failure counts. A drop or failure is
  retained as a gap signal for equivalence invalidation; it is never retried by
  or reported as a daemon authority failure.
- `NewAppWithDeps` attempts writer construction only when explicitly enabled.
  An unavailable path is logged as `fail-open` and produces a functional App
  with `timelineWriter == nil`. Shutdown logs a writer-close error but does not
  join it into primary shutdown errors.
- No writer invocation is wired into an authority path in this packet. Any
  future producer must submit after its primary commit and without an authority
  lock; Timeline has no authority callback.

## R2 blocker corrections

- `NewAppWithDeps` now names its return values and installs a rollback defer
  before fallible construction continues. If a later constructor step fails,
  the already-open optional Timeline writer is closed immediately. Its close
  error is logged only, preserving fail-open semantics. The regression forces
  an invalid listen address after a real writer opens, then proves a valid
  subsequent append is dropped because the descriptor was closed.
- `Writer.Append` now requires `Write` to return the complete framed JSONL
  record length. A short write with nil error is a dropped failure and does not
  call `Sync` or increment `Appended`; a truncated record can therefore never
  be reported as successful evidence.

## Tests

`internal/timeline/writer/writer_test.go` covers:

- successful append to an actual temporary JSONL file;
- simulated `ENOSPC` and `EACCES` write failure isolation;
- abrupt descriptor loss modeling a kill-9/restart boundary, which drops rather
  than retries evidence;
- concurrent append under `-race`;
- explicit no-new-goroutine assertion;
- rejection of implicit/relative output paths.
- short-write-with-nil-error rejection before `Sync`.
- failed App construction closes the already-open Timeline writer.

`cmd/devremote/step4_shadow_wiring_test.go` proves that the flag is default-off
and that a configured but unavailable Timeline writer still permits full daemon
startup and shutdown.

## Full gate

All passed from `companion-daemon`:

```text
go build ./...
go vet ./...
go test -race ./... -count=1 -timeout 300s
go test ./internal/timeline/contract -run=^$ -fuzz=FuzzEnvelopeValidator -fuzztime=2s
go test ./internal/timeline/contract -run=^$ -fuzz=FuzzComputedEventIDStable -fuzztime=2s
test -z "$(gofmt -l .)"
cd ../mobile && npx tsc --noEmit && npm test -- --runInBand
```

Implementation files:

```text
M companion-daemon/cmd/devremote/app.go
M companion-daemon/cmd/devremote/main.go
A companion-daemon/cmd/devremote/step4_shadow_wiring_test.go
A companion-daemon/internal/timeline/writer/writer.go
A companion-daemon/internal/timeline/writer/writer_test.go
```
