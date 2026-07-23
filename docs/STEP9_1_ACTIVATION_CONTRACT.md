# STEP 9.1 — Operational Timeline Staging Activation Contract

**Status:** IMPLEMENTATION CONTRACT — PENDING IMPLEMENTATION

**Branch:** `feature/canonical-timeline-foundation`
**HEAD:** `301f48544`
**PREREQUISITE:** Step 9.0 ACCEPTED at `62a50f0a8` (EVID: `STEP9_0_EVIDENCE.md`)

## 1. Scope and authority boundary

Step 9.1 activates minimal Operational Canonical Timeline staging — connecting
bounded, accepted provider-native producers to the existing `--enable-timeline-shadow`
path with fail-open semantics. This is NOT a production default change; the flag
remains `false` until staging evidence proves 7-day stable operation.

**What gets activated (event types):**
- `EventProviderInvocationStarted` — emitted when a managed Codex or Claude runtime starts
- `EventProviderInvocationFinished` — emitted on managed runtime graceful exit
- `EventApprovalRequested` — emitted when an approval is requested
- `EventApprovalResolved` — emitted when an approval is resolved (allow/deny)
- `EventToolCallStarted` / `EventToolCallFinished` — already present in contract
- `EventStreamObserved` — thinking/streaming observation
- `EventDegraded` — degradation indicator (disk full, write failure, drop)

**What stays unchanged:**
- `ManagedCodexService` and `ManagedClaudeService` lifecycle (create, stop, kill)
- `OwnedPTYRuntime` session lifecycle
- `ApprovalAuthority` approval ingestion, storage, delivery, resolution
- `TerminalTransport` generation-gated input, resize, output
- `Recorder` PTY byte stream (sole reader, no dual-feed)
- `Transcript` service API and consumer behavior
- Device trust, pairing, permission, bearer, WS ticket authority
- All existing REST and WebSocket handlers

## 2. Producer connection (bounded, accepted only)

Only the already-accepted managed runtimes may emit:

| Producer | Session Prefix | Event Source |
|----------|---------------|-------------|
| `ManagedCodexService` | `codex_app_server:` | Runtime lifecycle, tool call, approval |
| `ManagedClaudeService` | `claude_headless:` | Runtime lifecycle, tool call, approval, stream |

Each producer connects through the existing `writer.Writer.Append()` path.
The connection is:

```text
ManagedCodexService (create/stop/approval)
  → contract.Envelope (validate + marshal)
  → writer.Writer.Append(envelope)
  → ring buffer (128 capacity, bounded)
  → optional shadow file (when --enable-timeline-shadow)
```

No new goroutines, channels, callbacks, or observer patterns are introduced.
The ring-buffer mailbox model from Step 8 (R4) is the sole push path.

## 3. Fail-open guarantee

Timeline failure is ALWAYS degradation, never daemon crash or session failure:

- `Writer.Append` returns `bool` — false on validation/marshal/write/sync failure
- The caller logs the failure and continues; the managed runtime is unaffected
- Drops are counted in `writer.Stats.Dropped` and exposed via cockpit
- Disk full → drop, continue. Permission denied → drop, continue.
  Kill -9 restart → empty ring buffer after restart, continue.
- No production path may call `log.Fatal`, `panic`, or `os.Exit` from Timeline code
- Timeline initialization failure (missing path, permission) must not block daemon startup

**Composition rule:** `Open(Config{Path: ...})` errors are logged at the composition
root and the shadow writer remains `nil`. All call sites check `if w != nil` before
calling `Append`.

## 4. Mailbox / backpressure

The existing ring buffer (`writer.go`, `recentEnvelopes=128`) is the sole push consumer.
Overflow is silent drop with counter increment:

- `Append` pushes to ring buffer under mutex — non-blocking
- `Write` and `Sync` to the shadow file are under the same mutex
- No backpressure from cockpit consumers (polling, on-demand)
- Cockpit reads via `writer.ReadRecent(n)` on its own schedule
- Gaps are explicit: `Stats.Dropped` counter exposes every silent drop

## 5. Acceptance tests

Before the implementation is accepted, automated tests must prove:

1. **Graceful degradation on disk full** — `syscall.ENOSPC` on write → drop counted, runtime continues
2. **Graceful degradation on permission denied** — `syscall.EACCES` → drop counted, runtime continues
3. **Kill -9 restart** — after abrupt death, ring buffer empty, new appends succeed
4. **Drop exposure** — `Stats.Dropped` correctly increments on each failure mode
5. **No authority regression** — all existing tests pass; no production import change
6. **Bounded producer** — only Codex/Claude managed runtimes connect; generic events rejected
7. **Gap visibility** — cockpit exposes drops; no event fabricated to conceal gap
8. **Default-off** — without `--enable-timeline-shadow`, zero Timeline codepaths execute

## 6. Staging gate

The implementation is considered staging-complete when:

- [ ] All existing tests pass (`go test -race ./...`, `npx tsc --noEmit`, `npx jest --runInBand`)
- [ ] No production import regressions (Timeline packages are not imported from new callers)
- [ ] Staging daemon runs for 7 days with `--enable-timeline-shadow` enabled
- [ ] Cockpit shows real session/approval/event data from managed runtimes
- [ ] Zero daemon crashes, panics, or session failures attributed to Timeline code
- [ ] Drop counter increments on induced failures (disk full, permission denied)
- [ ] Separate evidence commit records staging results

After staging evidence is accepted, a separate reviewed change may alter the
default flag value. Step 9.1 itself does NOT change the default.

## 7. Implementation bounds

**Files that may change:**
- `internal/timeline/writer/` — existing ring buffer (already implemented)
- `cmd/devremote/app.go` — composition wiring (connect managed runtimes to writer)
- `internal/term/managed_codex.go` — emit envelope on create/stop (guarded by `--enable-timeline-shadow`)
- `internal/term/managed_claude.go` — emit envelope on create/stop/stream (same guard)

**Files that must NOT change:**
- `internal/term/pty.go` — terminal transport, recorder
- `internal/term/input_b_protocol.go` — acknowledged input
- `internal/transcript/` — Transcript service
- `internal/devicetrust/` — device trust
- `mobile/` — any mobile code (cockpit already reads via existing GET)
- `internal/agent/` — agent adapters (fixture-only per CT-P0)

**Additions:**
- New test file: `internal/timeline/writer/integration_test.go` — staging acceptance tests

## 8. Stop conditions

Stop and reject if the change:
- Makes Timeline a daemon startup, session creation, or shutdown prerequisite
- Changes any existing authority (runtime lifecycle, approval, input, transcript, PTY)
- Adds a callback, observer, dispatcher goroutine, or blocking channel
- Increases daemon goroutine count for non-Timeline code paths
- Introduces a production import of Timeline from `cmd/`, `term/`, `transcript/`, or `devicetrust/`
- Fails any existing test in the full gate (`go test -race ./...`, `npx tsc`, `npx jest`)
- Requires a mobile app update to function
- Changes the default value of `--enable-timeline-shadow`
