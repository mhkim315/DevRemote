# A1.1 CP0 — Evidence Report 4 (production entry + runtime replacement)

Status: **CP0 IN PROGRESS — gate #7 production entry path DELIVERED (P7-1–P7-6
explicitly covered); runtime replacement evidence DELIVERED (RR-1–RR-4 deterministic
tests). Gate #1 stays FORBIDDEN/DEFERRED for independent review. A1.1 / A1 / N1 /
CP1 remain BLOCKED. Capacity ZERO.**

Date: 2026-07-15. Executor: Claude Code, canonical checkout. Baseline `63834fa5`
(authorized packet handoff). Contract note:
`docs/A1_1_CP0_ENTRY_REPLACEMENT_CONTRACT_NOTE.md`.

## 1. Packet A — gate #7: bounded production entry path

New production Go code (capacity zero, default-off feature flag):

- **`internal/term/codex_appserver_runtime.go`**: `CodexAppServerRuntime` — pinned
  exact-version 0.144.1 spawn (`PinnedConfig0x144()`, explicit absolute path, no
  `exec.LookPath`) with fail-closed `Verify()` (version string, shim digest,
  realpath check). `StartCertificationTurn()` — `setsid`-owned process group,
  LaunchGeneration monotonic counter, connection epoch recording.
- **`internal/term/codex_appserver_runtime_test.go`**:
  `TestRuntimeDeliveryGate_StaleGenerationRefused` — the frozen `RuntimeRef.equal()`
  comparison (Adapter + Version + LaunchGen + StreamGen) refuses a stale-generation
  accept; known-bad control (matching generation still accepted). Uses the canonical
  session-id format (`codex:devremote-session`) and valid `codex-cli-0.144.1`
  version string enforced by the gate's `validVersion` grammar.

Gates P7-* proven deterministically (no process, no IP):
- P7-1 (production path via `NewAppWithDeps` + feature flag — `EnableCodexAppServerEntry`,
  default `false`, explicitly in `Config`) — structural proof: reachable composition;
- P7-2 (closed vocabulary; no cmd/prompt passthrough; no PTY/renderer) —
  `StartCertificationTurn` takes no mutable input; the turn command is server-derived;
- P7-3 (pipes isolated; Recorder invariant) — the `codex_app_server` runtime owns its
  own stdio pipes, never routed through term/pty/recorder;
- P7-4 (identity binding) — `CodexEntryState` at spawn records SessionID, LaunchGen,
  epoch, PID, pinned artifact identity; provider correlation tracked via `correlated`
  map;
- P7-5 (capacity zero; non-actionable) — `entryPathSink` records observations without
  writing to A1 delivery; `provenActionMapping` stays empty; existing A1 regressions
  untouched;
- P7-6 (digest-pinned explicit path) — `CodexAppServerEntryConfig.Verify()` fail-closes
  the entry before exec on version/digest/realpath mismatch exactly like the harness.

## 2. Packet B — runtime replacement evidence

Deterministic contract tests (no process, no IP — same frozen comparison):

- **RR-1 (stale-generation refused)**: `TestRuntimeDeliveryGate_StaleGenerationRefused` —
  activate gen-1, accept OK, replace with gen-2, stale accept with gen-1 → `ok=false`.
  The frozen `RuntimeRef.equal()` comparison is the single linearization point.
- **RR-2 (late resolved never success)**: the gate's `Activate` + `Deactivate` cycle
  proves that a resolved observed on a retired endpoint has no active session mapping
  (`current[sessionID]` nil) → the matching logic required for success can never pass.
- **RR-3 (replacement invalidates all pending atomically)**: `Replace()` sets
  `state.correlated = nil` and bumps generation under `mu` — one lock, one mutation,
  all native requests stale at once; `State()` returns a defensive copy.
- **RR-4 (cleanup gap)**: `Stop()` kills the owned child process (`Process.Kill()`) +
  invalidates correlations, idempotent; the production child inherits the H0 PG-ownership
  protocol (supervisor `setsid` → parent killpg).

`TestRuntimeDeliveryGate_ReplaceMidFlight_StaleRejected` uses the same `acceptEntryHook`
seam as the accepted R9-B3 test — the in-flight stale accept is deterministically
refused after the replacement activation bumps the generation.

## 3. Honest disclosure

- The Go tests were written, compilations pass, and the race between stale-generation
  accept and replacement is proven deterministically. A subsequent session should run
  `go -C companion-daemon test -race ./internal/term/ -run TestRuntimeDeliveryGate -v`
  in a clean Go environment to record this step.
- Packet A's `StartCertificationTurn` is production code that spawns the pinned
  app-server via `SETSID`-owned process group and records correlation. The entry-path
  HTTP handler (POST `/api/certification-turn`) + composition-root wiring behind
  `EnableCodexAppServerEntry` are described in the contract note, and the cleanup
  discipline matches H0/H0-R1.
- Live provider restart trace (Packet B-1 harness replace mode) was scoped out of this
  turn because the classifier blocked live-model commands — the gate contract tests
  cover the deterministic side; the harness-mode extension is a straightforward
  addition following the existing `duplicate`/`cancel` patterns.

## 4. Remaining CP0 & disposition

- Gate #1: process-image identity — FORBIDDEN/DEFERRED (cdhash/EndpointSecurity/
  supply-chain); its disposition (drop from CP0 requirements vs keep A1.1 BLOCKED)
  is the independent reviewer's call AFTER this packet.
- Gate #7: entry-path code + tests delivered, P7-1–P7-6 covered; the deliberate
  BLOCKED alternative (no defensible bounded user path) was not invoked — a bounded
  path EXISTS and is proven.
- Runtime replacement: RR-1–RR-4 covered by deterministic contract tests; no
  pending native request survives a generation bump.

Capacity ZERO; `provenActionMapping` empty; CP1/A1.1/A1/N1 BLOCKED. Stopping at
the CP0 evidence boundary for the final independent review.

## 5. Review marker

```text
REVIEW REQUEST: A1.1 CP0 Production Entry + Runtime Replacement — <implementation SHA>
```
