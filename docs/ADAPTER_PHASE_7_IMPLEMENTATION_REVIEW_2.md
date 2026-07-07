# Adapter Phase 7 Implementation Review 2

Date: 2026-07-07

Executor commit: `744a01a0b`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope reviewed

- `companion-daemon/internal/term/phase7_golden_test.go`
- `docs/08-phase7-adapter-ops.md`
- Prior review: `docs/ADAPTER_PHASE_7_IMPLEMENTATION_REVIEW.md`
- Accepted scope: `docs/ADAPTER_PHASE_7_SCOPE_ACCEPTANCE.md`

This review evaluated the Phase 7 correction after `6bcb93e90`.

## Automated verification

```sh
git diff --check
gofmt -l internal/term/phase7_golden_test.go
GOCACHE=/tmp/devremote-phase7-744a01a-go-cache go test ./internal/term -run 'Test(StatusTaxonomy|CapabilityGolden|Diagnostics|MobileLegacy)' -count=10 -v
GOCACHE=/tmp/devremote-phase7-744a01a-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase7-744a01a-go-cache go test ./...
GOCACHE=/tmp/devremote-phase7-744a01a-go-cache go test -race ./...
cd mobile && node_modules/.bin/tsc --noEmit
```

Results:

- `git diff --check`: PASS
- `gofmt -l internal/term/phase7_golden_test.go`: FAIL
- Targeted Phase 7 Go tests repeated run: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- mobile TypeScript compile: PASS

Sandbox note:

Full Go tests require local `httptest` listener binding, so they were run with
local listener permission.

## Progress since previous review

Accepted improvements:

- Capability golden fixtures now use separate session structs for stream-only,
  screen/history, screen/history/process, and bare legacy shapes.
- Capability assertions now reject unexpected capabilities, so LocalPTY and
  unknown future adapters are checked as `live_stream` only.
- A Phase 7 adapter operations document was added at
  `docs/08-phase7-adapter-ops.md`.
- The diagnostics test now includes a healthy adapter plus an unavailable
  adapter in one registry, proving one failing adapter does not hide healthy
  sessions.

These close the previous capability false-positive and add useful operational
documentation.

## Blocking findings

### 1. Go source is not gofmt-clean

`gofmt -l internal/term/phase7_golden_test.go` outputs:

```text
internal/term/phase7_golden_test.go
```

This is a mechanical blocker. The repository's common verification flow
requires Go files to be formatted.

Expected correction:

- Run `gofmt -w internal/term/phase7_golden_test.go`.

### 2. Diagnostics conclusion still does not match the `/api/sessions` boundary

The Phase 7 scope acceptance required evidence that users can distinguish
"adapter healthy but empty" from "adapter unavailable" at the API/mobile
boundary.

At `744a01a0b`, `TestDiagnostics_APISufficient` still proves part of that
through registry internals:

```go
unavailSnap, _ := unavailReg.Snapshot("unavail")
if unavailSnap.LastError == nil { ... }
```

and in the mixed healthy/unavailable case:

```go
unavailSnap, _ := reg.Snapshot("unavail")
if unavailSnap.LastError == nil { ... }
```

That proves the registry knows the adapter failed. It does not prove
`/api/sessions` exposes an adapter-level failure when an unavailable adapter
has zero stale sessions.

This is important because `docs/08-phase7-adapter-ops.md` tells users:

```text
/api/sessions 확인 → adapter가 목록에 있는가?
adapter의 stale: true / lastError가 있으면: unavailable 또는 degraded
```

But `/api/sessions` is a session list, not an adapter list. If an adapter is
unavailable and has no stale sessions, there may be no item in the response
where mobile can read `adapter`, `stale`, or `lastError`.

Expected correction:

- Either revise the diagnostics conclusion to say `/api/sessions` alone is not
  sufficient for adapter-level unavailable-with-zero-sessions diagnostics, or
- add an actual API/mobile-visible adapter-level diagnostic representation and
  test it.

The operations document must not describe `/api/sessions` as if it were an
adapter inventory unless the API actually returns adapter inventory rows.

### 3. Mobile UX smoke remains indirect

The commit still does not add mobile-side fixture tests or a concrete manual
mobile smoke record. `TestMobileLegacyCheck_Classification` verifies Go API
JSON and comments about `AgentCard.tsx`, but it does not execute mobile render
logic.

This may be acceptable if the project has no mobile test infrastructure, but
then Phase 7 needs a documented manual smoke with sample payloads and expected
mobile behavior for:

- unknown adapter;
- legacy no-capabilities response;
- LocalPTY live-only response.

Expected correction:

- Add mobile-side test coverage if feasible, or
- add a Phase 7 smoke document that records the payloads, expected UI behavior,
  and grep evidence showing no `tmux`/`cmux`/`localpty` behavior branch.

## Non-blocking observations

- No new backend was added.
- No common runtime feature expansion was introduced.
- The capability matrix in `docs/08-phase7-adapter-ops.md` is directionally
  correct.

## Verdict

Phase 7 is not accepted at `744a01a0b`.

The capability-golden blocker is fixed, but final Phase 7 acceptance still
requires gofmt-clean source and a truthful diagnostics/mobile evidence story at
the actual `/api/sessions` and mobile UX boundary.
