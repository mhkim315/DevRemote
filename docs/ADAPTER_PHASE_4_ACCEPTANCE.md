# Adapter Phase 4 Acceptance

Date: 2026-07-07

Executor commit: `6da12373d`

Verifier decision: **ACCEPT**

Next phase permission: **ALLOWED**

## Scope

This pass reviewed the Phase 4 correction after
`docs/ADAPTER_PHASE_4_REVERIFICATION_3.md`.

The accepted revision updates:

- `companion-daemon/internal/mux/adapter_contract_test.go`

No production code changed.

## Automated verification

Commands run:

```sh
GOCACHE=/tmp/devremote-phase4-6da1237-go-cache go test ./internal/mux -run 'Test(Tmux|Cmux)Adapter_Contract|Test.*Contract|TestRegistry_SlowAdapterDoesNotBlock|TestProbeAdapter' -count=20 -v
GOCACHE=/tmp/devremote-phase4-6da1237-go-cache go test ./internal/mux -count=1
GOCACHE=/tmp/devremote-phase4-6da1237-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase4-6da1237-go-cache go test ./...
GOCACHE=/tmp/devremote-phase4-6da1237-go-cache go test -race ./...
npx tsc --noEmit
```

Results:

- Targeted tmux/cmux contract tests passed for 20 iterations.
- `go test ./internal/mux` passed.
- `go vet ./...` passed.
- `go test ./...` passed when rerun outside the sandbox listener restriction.
- `go test -race ./...` passed when rerun outside the sandbox listener
  restriction.
- `npx tsc --noEmit` passed.

Sandbox note:

Sandboxed full test and race runs failed only because `httptest` could not bind
localhost ports:

```text
bind: operation not permitted
```

The same commands passed with local listener permission.

## Acceptance findings

### 1. Factory context cancellation and timeout are now hard contracts

Accepted.

`testFactoryCtxCancel` now requires:

- non-nil error;
- `errors.Is(err, context.Canceled)`;
- nil sessions.

`testFactoryCtxTimeout` already required a non-nil error for an already-expired
context. Together, these close the prior false-success path where broken
factory adapters could pass cancellation/timeout behavior.

### 2. Live stream contract now covers both stream and adapter input models

Accepted.

The live suite now covers:

- `OpenStream`;
- `Read`;
- `Close`;
- `Resize`;
- `TerminalStream.Write`;
- `InputWriter.WriteInput`.

tmux exercises `TerminalStream.Write`. cmux intentionally skips stream write
because `CmuxStream.Write` returns the documented "use InputWriter" error, and
then passes the `InputWriter` path. This is an acceptable expression of the two
supported input models.

### 3. Live read no longer accepts empty EOF-only behavior

Accepted.

The read test now fails if the stream returns `0, io.EOF`, which closes the
previous path where a stream with no useful output could satisfy the contract.

### 4. Process snapshot identity is defined and checked

Accepted.

The process snapshot suite now enforces:

- non-nil map;
- non-empty map;
- non-empty keys;
- every snapshot key must correspond to a discovered session ID.

The suite also documents the intended coverage rule: process snapshot keys are
a subset of discovered sessions because not every terminal session necessarily
has an agent process. That rule is coherent with the cmux telemetry model and
is sufficient for Phase 4.

### 5. tmux/cmux both use the common harness

Accepted.

tmux and cmux are both registered against the reusable required suite and
capability suites with explicit expectations. This satisfies the Phase 4 goal
that a future adapter author can register a factory with the shared suite and
get meaningful contract coverage.

## Non-blocking follow-ups

These do not block Phase 4:

- A future cleanup can replace the local `stringsContains` helper with
  `strings.Contains` for readability.
- A future fixture adapter in Phase 5 should include intentionally unsupported
  capabilities to prove unsupported UI/API behavior at the integration layer.
- The live stream suite can become even stricter once Phase 5 adds a fully
  deterministic in-memory or child-PTY fixture stream.

## Verdict

Phase 4 is **accepted** at `6da12373d`.

The contract harness is now strong enough to gate Phase 5 fixture adapter work.
Phase 5 may begin.
