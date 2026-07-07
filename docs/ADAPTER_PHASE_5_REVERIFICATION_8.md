# Adapter Phase 5 Reverification 8

Date: 2026-07-07

Executor commit: `6fe6b13b9`

Verifier decision: `BLOCKED`

Next phase permission: `BLOCKED`

## Scope reviewed

- `companion-daemon/internal/term/fixture_e2e_test.go`
- Runtime route registration and resize call sites
- Mobile TypeScript compile boundary

This review did not change runtime implementation.

## Automated verification

The first sandboxed full Go test attempt failed because the sandbox denied `httptest` local listener binding. The same test was rerun with local listener permission.

- `GOCACHE=/tmp/devremote-phase5-6fe6b13-go-cache go test ./internal/term -run 'TestFixture|Fixture' -count=20 -v`: PASS
- `GOCACHE=/tmp/devremote-phase5-6fe6b13-go-cache go vet ./...`: PASS
- `GOCACHE=/tmp/devremote-phase5-6fe6b13-go-cache go test ./...`: PASS
- `GOCACHE=/tmp/devremote-phase5-6fe6b13-go-cache go test -race ./...`: PASS
- `cd mobile && node_modules/.bin/tsc --noEmit`: PASS

## Improvements since the previous review

- The fake test-only `/term/size` route was removed.
- The fixture E2E WebSocket test now exercises real WebSocket stream output and input forwarding.
- Create/delete, unsupported capability, API visibility, telemetry snapshot, stream content, and backend JSON schema checks are covered.
- The resize test now avoids pretending that a server-side resize route exists.

These are valid improvements and reduce the previous false-positive risk.

## Blocking findings

### 1. Resize is still not closed at the Phase 5 contract level

The Phase 5 plan requires minimum fixture discovery, live stream, and resize, and asks for E2E coverage through the HTTP/WebSocket/telemetry/mobile schema boundary before a real third backend is introduced.

At `6fe6b13b9`, resize is only tested by directly calling `fixtureStream.Resize(40, 120)` in Go test code. That proves the fixture stream method can record dimensions, but it does not prove the product boundary.

The commit adds comments saying server-side resize is out of scope because the current architecture is client-side only. That may be an acceptable scope decision, but it is not yet reflected in the adapter expansion plan, handoff, or an explicit Phase 5 acceptance contract.

The mismatch matters because product code still attempts server resize calls:

- `companion-daemon/internal/term/pty.go` calls `fetch('/term/size?...')`.
- `mobile/src/screens/FeedScreen.tsx` calls `fetch('/term/size?...')`.
- `companion-daemon/cmd/devremote/app.go` registers `/term/ws` and `/term/`, but no `/term/size` handler.

Therefore, Phase 5 cannot be accepted on a test comment alone. One of these must happen:

1. implement and test the real resize boundary, or
2. update the plan/contract/handoff documentation to explicitly state that server-side resize is out of Phase 5, and define the current `/term/size` calls as intentionally unimplemented/no-op for this phase.

### 2. Mobile schema boundary remains weaker than the stated Phase 5 acceptance

The current test checks backend JSON keys in Go. That is useful, but it is not a mobile-side typed fixture or mobile schema regression test.

If backend JSON golden coverage is intended to be the accepted mobile boundary for Phase 5, that must be explicitly documented. Otherwise, add a mobile-side schema fixture/check that consumes the fixture adapter session shape.

## Non-blocking observation

The repeated fixture WebSocket test run logs `WS stream read err: io: read/write on closed pipe` during normal close. It did not fail tests or race checks, so this is not a blocker for Phase 5. Cleaning or documenting normal-close logging would make future verification easier.

## Verdict

Phase 5 is not accepted at `6fe6b13b9`.

The implementation is materially closer than the previous attempts, and the automated test suite passes. The remaining gap is contract clarity: resize and mobile schema acceptance must either be implemented at the intended boundary or explicitly scoped down in the project documentation before Phase 5 can be accepted.
