# PA2a — Link Subsystem Removal Evidence Report

Status: **REVIEW REQUEST** (R3 — remediation of the telemetry evidence blocker)

Implementation SHA: `1b92fc702148eb461fb58d9d9980e63e6230dab8`
Evidence/report SHA: (this commit)
Gate execution SHA: `1b92fc702148eb461fb58d9d9980e63e6230dab8`

## 1. Ancestry

| Ancestor | SHA | Role |
| --- | --- | --- |
| PA2a implementation | `76c20ff1a16d51f2a4731ab2c71be0d59b188d3b` | Link/LinkStore subsystem removal (accepted) |
| PA2a-R1 | `c4b7e0cd6dfba572c85312d4e786fe1bf4f8f276` | required acceptance evidence tests |
| PA2a-R2 | `f2ece73e130b0d6167c460bca0730e367a1831d7` | non-vacuous CLI/IPC/sentinel tests — **REJECTED** on the telemetry evidence blocker |
| PA2a-R3 | `1b92fc702148eb461fb58d9d9980e63e6230dab8` | this remediation (telemetry evidence only) |

All ancestry verified via `git log`; R3 is a linear descendant of the rejected
R2 candidate. All R1/R2 behavior outside the telemetry evidence blocker is
preserved untouched (only `internal/term/telemetry_test.go` changed).

## 2. The rejected blocker

PA2a focused test 5 (docs/PA2_LIFECYCLE_TRANSPORT_CONTRACT.md) requires:

> Positive control: valid process snapshot + normal process-based resolver →
> `LogRef` obtained without link → telemetry state/projection unchanged.
> Negative control: Antigravity external log previously resolved only via
> link is no longer resolved.

The R2 candidate's `TestPA2a_TelemetryProcessFallback` used **nonexistent PID
12345**, so `ResolveAgentLog` always failed and the test only asserted the
error text did not mention "link" — vacuous: no `LogRef` was ever obtained,
and nothing proved the production resolver path or downstream visibility.
The R2 negative control (`TestPA2a_TelemetryAntigravityNotResolved`) used a
nonexistent PID with an unrecognizable command — it never demonstrated an
actual link-equivalent Antigravity fixture failing to resolve.

## 3. Remediation mapping (blocker items 1–7)

All in `companion-daemon/internal/term/telemetry_test.go` at the
implementation SHA.

| # | Requirement | Remediation |
| --- | --- | --- |
| 1 | Replace vacuous PID-12345 positive test | `TestPA2a_TelemetryProcessFallback` deleted; replaced by `TestPA2a_TelemetryProductionResolverPositiveControl` |
| 2 | Bounded process fixture through real production `ResolveAgentLog` | `spawnBoundedAgentFixture` starts a REAL, live child process named `claude` (a copy of the test binary running the env-guarded `TestPA2aHelperSleep`; self-exits after 60s, killed+reaped at cleanup). The production `findAgentProcess` `ps` child scan discovers it. A copied system `sleep` binary cannot be used: modern macOS kills copied platform binaries on exec (observed: child dead immediately); the ad-hoc-signed test binary survives copying on darwin and linux |
| 3 | Valid process snapshot + normal production resolver | Snapshot `{sid: {PID: os.Getpid(), CWD: fixtureCWD}}` (live PID, batch-snapshot shape). Test asserts precondition `svc.logResolver == nil` — resolution runs through `resolveLog → ResolveAgentLog`, never a seam |
| 4 | Real `LogRef` for a supported production fixture | Claude chosen (deterministic resolver: session file by live agent PID + encoded CWD; no lsof/±5s timestamp dependence). Real `~/.claude/sessions/<live-pid>.json` and real Claude-shaped JSONL at `~/.claude/projects/<encoded-cwd>/<uuid>.jsonl` in a temp `HOME`. Asserts exact `LogRef{Agent: "claude", Session: <uuid>, Path: <log>}` (symlink-aware path equality, errors fail the test) |
| 5 | Evidence reaches event, state, projection and remains visible | After a full production poll (`reconcileSessions` + `processSession`): event store holds the 2 parsed events (`assistant_message` with fixture text, `tool_call_started`), state machine is `working/100` with `Runner "claude"` and a parser assigned, and `Snapshot(reg)` projects `runner claude / state working / 2 events`. A second poll (no new log lines) re-runs the production path and the projection assertions — evidence remains visible |
| 6 | Negative control: equivalent legacy Antigravity external-log fixture | `TestPA2a_TelemetryAntigravityLinkOnlyFixtureNotResolved`: real transcript at the exact link-served path `~/.gemini/antigravity/brain/<uuid>/.system_generated/logs/transcript.jsonl`, fixture-equivalence proven by the legacy per-UUID resolver (`AntigravityResolver.ResolveLink` — the exact component the removed LinkStore path invoked) resolving it, plus a live bounded `antigravity`-named process. Production `resolveLog` fails (non-link-flavored error); no events, no parser, runner stays `"agent"`, state deterministic `idle`, projection clean |
| 7 | Injected resolver = unit-seam only | `TestPA2a_TelemetryInjectedResolver` retained, re-documented as UNIT-SEAM test ONLY and explicitly excluded from acceptance evidence; the two controls above never touch the seam |

`TestPA2a_TelemetryNoLinkStore` (constructor evidence) is preserved unchanged.

## 4. Exact commands and results (executed at gate SHA)

```
$ go test ./internal/term -run "TestPA2a" -count=1 -v
--- PASS: TestPA2a_TelemetryNoLinkStore
--- SKIP: TestPA2aHelperSleep            (inert fixture body, env-guarded)
--- PASS: TestPA2a_TelemetryProductionResolverPositiveControl (0.13s)
--- PASS: TestPA2a_TelemetryInjectedResolver
--- PASS: TestPA2a_TelemetryAntigravityLinkOnlyFixtureNotResolved (0.09s)
--- PASS: TestPA2a_IPCLinkOperationsRemoved
PASS

$ go test -race ./internal/term -run \
    "TestPA2a_TelemetryProductionResolverPositiveControl|TestPA2a_TelemetryAntigravityLinkOnlyFixtureNotResolved" \
    -count=20
ok  devremote/companion-daemon/internal/term  8.721s   (20/20 stable, race-clean)
```

## 5. Gate results

| Gate | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go test -race ./internal/term ./cmd/devremote -count=1` | PASS (25.9s / 33.3s) |
| `go test -race ./... -count=1` (full backend) | PASS (exit 0, 11 packages ok) |
| New evidence tests `-count=20 -race` | PASS (stable) |
| `gofmt -l internal/term/telemetry_test.go` | CLEAN (empty) |
| `git diff --check` | PASS |
| Secret scan (changed files: `internal/term/telemetry_test.go`) | CLEAN |
| Static zero-production-reference gate (contract rg pattern, non-test `.go` under `cmd/ internal/`) | 0 matches |
| Invariant: vendor branch scan (`mobile/src`) | CLEAN |
| Invariant: ID inference scan (`mobile/src`) | CLEAN |
| Mobile `npx tsc --noEmit` | **not-run** — `mobile/node_modules` not installed in this environment (per CLAUDE.md: backend gate run, mobile reported not-run with reason). Change is backend-test-only; no mobile source touched |

## 6. Final state

| Condition | Value |
| --- | --- |
| Implementation commit | `1b92fc702148eb461fb58d9d9980e63e6230dab8` (only `companion-daemon/internal/term/telemetry_test.go`) |
| Evidence/report commit | this commit (docs only) |
| Worktree at push | clean |
| Local == Remote after push | verified in worker_done |
