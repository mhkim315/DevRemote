# Adapter Phase 7 Implementation Review

Date: 2026-07-07

Executor commit: `6bcb93e90`

Verifier decision: **BLOCKED**

Next phase permission: **BLOCKED**

## Scope reviewed

- `companion-daemon/internal/term/phase7_golden_test.go`
- `mobile/src`
- `docs/ADAPTER_PHASE_7_SCOPE.md`
- `docs/ADAPTER_PHASE_7_SCOPE_ACCEPTANCE.md`

This review evaluated the first Phase 7 implementation commit.

## Automated verification

```sh
git diff --check
GOCACHE=/tmp/devremote-phase7-6bcb93e-go-cache go test ./internal/term -run 'Test(StatusTaxonomy|CapabilityGolden|Diagnostics|MobileLegacy)' -count=10 -v
GOCACHE=/tmp/devremote-phase7-6bcb93e-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase7-6bcb93e-go-cache go test ./...
GOCACHE=/tmp/devremote-phase7-6bcb93e-go-cache go test -race ./...
cd mobile && node_modules/.bin/tsc --noEmit
```

Results:

- `git diff --check`: PASS
- Targeted Phase 7 Go tests repeated run: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- mobile TypeScript compile: PASS

Sandbox note:

Full Go tests require local `httptest` listener binding, so they were run with
local listener permission.

## Accepted progress

- The commit adds a Phase 7-specific Go golden/status test file.
- It starts covering the intended vocabulary: empty, unavailable, ended,
  unsupported, legacy no-capabilities, unknown adapter, and LocalPTY.
- It documents the existing `adapter !== 'native'` mobile display exception as
  intended to be display-only.

This is useful groundwork, but it does not yet satisfy the Phase 7 acceptance
gates.

## Blocking findings

### 1. Capability golden tests do not prove absence of unsupported capabilities

`TestCapabilityGolden_AllBackends` only checks that each expected capability is
present:

```go
for _, want := range tt.wantCaps {
    found := false
    for _, got := range s.Capabilities {
        if got == want {
            found = true
            break
        }
    }
    if !found { ... }
}
```

It does not assert exact equality.

This matters because `capFullSession` always implements every optional
capability method:

- `ReadScreen`
- `ReadHistory`
- `OpenStream`
- `ProcessInfo`

So the `localpty` and `future` fixtures configured as `["live_stream"]` can
still expose `screen`, `history`, and `process` through type assertions. The
test would still pass because it only checks that `live_stream` is included.

Relevant lines:

- `phase7_golden_test.go:113-121`
- `phase7_golden_test.go:146-164`
- `phase7_golden_test.go:291-301`

Expected correction:

- Make fixtures implement only the capabilities being tested, or create
  separate session structs per capability shape.
- Assert exact capability sets, including absence of `screen` and `history`
  for LocalPTY.
- Prove unknown adapter rendering does not accidentally inherit tmux/cmux-like
  affordances.

### 2. Diagnostics proof does not show `/api/sessions` exposes unavailable adapter details

`TestDiagnostics_APISufficient/adapter_lastError_visible` has no failure if no
matching stale session appears:

```go
for _, s := range sessions {
    if s.Adapter == "unavail" && s.Stale {
        return
    }
}
```

If `sessions` is empty, the subtest passes. With the current
`unavailableAdapter`, `ListSessions` returns no sessions, so there may be no
per-session telemetry item carrying `stale` or `lastError`.

That means the test does not prove the Phase 7 claim that `/api/sessions`
alone lets a client distinguish "healthy but empty" from "adapter unavailable".
It proves the registry snapshot has `LastError`, but not that the mobile/API
boundary exposes a usable adapter-level diagnostic when there are zero stale
sessions.

Relevant lines:

- `phase7_golden_test.go:198-216`
- `phase7_golden_test.go:230-234`

Expected correction:

- Add an assertion that fails when the API response does not expose the
  unavailable adapter condition.
- If `/api/sessions` cannot represent unavailable adapters with zero sessions,
  revise the diagnostics conclusion and document the need for an adapter-level
  diagnostics endpoint/field.

### 3. Mobile UX smoke is simulated in Go, not verified in mobile code

Phase 7 scope required mobile capability-driven UX smoke:

- no supported-backend allowlist or behavior branch exists in mobile;
- unknown adapter responses render safely;
- legacy responses without `capabilities` render safely;
- LocalPTY live-only sessions render without history/screen affordance
  confusion.

This commit does not add mobile fixtures, mobile unit tests, or a documented
manual/mobile smoke result. It only adds a Go test that simulates the
`adapter !== 'native'` display rule.

Relevant gate:

- `docs/ADAPTER_PHASE_7_SCOPE_ACCEPTANCE.md:143-149`

Expected correction:

- Add mobile-side fixture/test coverage if test infrastructure exists, or add a
  concrete documented smoke procedure with sample payloads and expected UI
  behavior.
- The evidence must cover unknown adapter, legacy no-capabilities response,
  and LocalPTY live-only response.

### 4. Adapter operational requirements are not finalized as documentation

Phase 7 requires adapter-specific install/permission/socket/LaunchAgent
documentation for `tmux`, `cmux`, and `localpty`.

This commit does not add or update operational documentation. The only
operational table remains in the scope document, which was accepted as scope,
not final evidence.

Relevant gate:

- `docs/ADAPTER_PHASE_7_SCOPE_ACCEPTANCE.md:150`

Expected correction:

- Add a durable operations document or finalize the Phase 7 scope table into a
  Phase 7 operations/diagnostics document.
- It should explain how users distinguish "no sessions" from "adapter
  unavailable" for each adapter.

## Non-blocking observations

- The targeted and full automated test suites pass.
- No new backend was added.
- No common runtime feature expansion was introduced.

## Verdict

Phase 7 is not accepted at `6bcb93e90`.

The commit is useful as a first Go-level taxonomy test, but it does not yet
prove the actual Phase 7 product/operations requirements. The next revision
should tighten capability golden assertions, prove or revise the diagnostics
claim at the API/mobile boundary, add real mobile smoke evidence, and finalize
adapter operational documentation.
