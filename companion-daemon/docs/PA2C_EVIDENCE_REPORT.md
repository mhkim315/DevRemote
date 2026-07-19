# PA2c — Managed Lifecycle Ownership Evidence Report

Status: **REVIEW REQUEST** (R6 — production adapter tests with deterministic seam)

Implementation SHA: `HEAD` (R6; to be replaced by commit SHA below)
Evidence/report SHA: (this commit)
Gate execution SHA: (see R6 section)

## Ancestry — all PA2c remediation rounds

PA2b final ACCEPT (rollback): `2897a9e0943656883a885e75b08513982de507b7`

| Round | Impl SHA | Fate |
| --- | --- | --- |
| PA2c initial | `5d655775e` | REJECTED (three blockers) |
| PA2c-R1 | `5af5095bf` | REJECTED (three R2 findings) |
| PA2c-R2 | `f3c3ef770` | Eviction (subsumed by R3) |
| PA2c-R3 | `82d53d58f` | Eviction (R4 fallback removal + R5 vacuous barrier tests superseded it) |
| PA2c-R4 | `cd53f6d01` | Eviction (non-atomic fallback removed; vacuous pre-lock barrier tests) |
| PA2c-R5 | `84062341e` | REJECTED (fake-adapter algorithm; need production adapter + shared detach) |
| **PA2c-R6** | **this commit** | **REVIEW REQUEST** |

No round was independently ACCEPTed. R1→R5 are sequential rejections;
R6 is the current candidate. No PA2d/PA3 work started.

## R3 baseline (accepted R3 changes that R4→R6 preserve)

- `SessionIdentityTerminator` adapter capability (`internal/mux/adapter.go`)
- `Registry.CompareAndTerminateSession` delegates to adapter's `CompareAndTerminate` under the adapter lock
- `controlledPTYAdapter.CompareAndTerminate` implements the atomic boundary
- `OwnedPTYRuntime.newCleanup` calls `CompareAndTerminateSession` once — no separate `FindSession`, no unlocked check-then-act
- `ErrStaleSessionIdentity` sentinel
- `lcAdapter` and `fixtureAdapter` implement the capability

## R4: non-atomic fallback removed (cd53f6d01)

The snapshot-cache comparison + id-addressed `TerminateSession` fallback in
`Registry.CompareAndTerminateSession` was **deleted** (R4).  Adapters that
do not implement `SessionIdentityTerminator` fail closed with
`ErrUnsupported` — no fallback exists.  Current supported adapters all
implement the capability.

## R5: deterministic barrier tests (84062341e)

`prodShapedBarrierAdapter` with lock-acquired signal + post-detach barrier
replaced vacuous pre-lock barriers.  5/5 mux tests pass.

## R6: production adapter tests + shared detach primitive (this round)

### Finding 1 — exercise real production adapter, not fake

- `testCloseBarrier`: nil-in-production package-level variable in
  `controlled_pty_adapter.go`.  Zero-overhead in prod (nil → no-op);
  tests set it to a channel barrier that gates the Close/process/PTY I/O
  boundary **after** detach+unlock, deterministically.
- Four production-adapter tests in `controlled_pty_r6_test.go`:
  1. `TestPA2cR6_ProductionCompareAndTerminate_MatchTerminates` — real
     `controlledPTYAdapter`, real `CompareAndTerminate` via Registry, real
     identity comparison under `a.mu`, detach, Close-barrier called.
  2. `TestPA2cR6_DetachBeforeClose_ReplacementSurvives` — lock acquired,
     identity matched, session detached under lock, lock released; Close
     blocked at barrier; same-id replacement inserted; replacement
     survives after Close completes.
  3. `TestPA2cR6_StaleIdentity_ProductionAdapterReturnsStale` —
     replacement first, atomic call returns `ErrStaleSessionIdentity`,
     **zero** Close barrier calls.
  4. `TestPA2cR6_KnownBad_CheckThenDelete_NegativeControl` — known-bad
     FindSession-then-id-delete negative control correctly rejected by
     atomic path.

### Finding 2 — shared detach primitive

`controlledPTYAdapter.detachLocked` is the single primitive both
`TerminateSession` and `CompareAndTerminate` call.  Both detach under
`a.mu`, unlock, then call `native.Close()` outside every lifecycle
lock.  No duplicate lock-held-Close logic.

### Finding 3 — evidence corrections

- R3 impl SHA: `82d53d58f` (this report).  Not `da299248d` (that was the evidence commit).
- R4 impl SHA: `cd53f6d01`.  Not `cd53f6d…` (the old evidence had it wrong).
- R5 impl SHA: `84062341e`.
- Historical `classifyProviderErr` reference removed (deleted in R1).
- R1→R5 explicitly marked as sequential rejections; no independent ACCEPT claimed.
- Non-atomic fallback marked as removed in R4; current adapters fail closed without it.

## Gate results (R6)

```
$ go test -race ./internal/mux -run "TestPA2cR6" -count=1 -v       → 4/4 PASS
$ go test -race ./internal/mux -run "TestPA2cR6" -count=20          → ok (1.2s)
$ go test -race ./internal/mux -run "TestPA2cR5" -count=1           → ok (5/5 preserved)
$ go test -race ./internal/term -run "TestPA2c|TestLifecycle" -count=1 → 16/16 PASS
$ go test -race ./internal/term -run "TestPA2c|TestLifecycle" -count=20 → ok (21.0s)
$ go test ./internal/term ./cmd/devremote -count=1                  → ok / ok (24.0s / 32.0s)
$ go test -race ./... -count=1                                      → exit 0, 12 packages ok
```

| Gate | Result |
| --- | --- |
| `go build ./...` / `go vet ./...` | PASS |
| `gofmt -l` / `git diff --check` | CLEAN |
| Secret scan (changed files) | CLEAN |
| PA2a zero-reference / PA2b duplicate-parser | 0 / 0 |
| PA2b focused tests | 2/2 PASS |
| Mobile invariant + tsc | CLEAN / not-run (zero mobile changes) |

## Final state

| Condition | Value |
| --- | --- |
| Pushed HEAD | (this commit) |
| Local == Remote | verified in worker_done |
| Worktree | clean |
| Rollback SHA | `2897a9e0943656883a885e75b08513982de507b7` (PA2b final ACCEPT) |
