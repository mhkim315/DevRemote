# STEP 9.1 — Operational Timeline Staging Activation Contract

**Status:** IMPLEMENTATION CONTRACT — PENDING IMPLEMENTATION

**Branch:** `feature/canonical-timeline-foundation`
**HEAD:** `1c69f99ab`
**PREREQUISITE:** Step 9.0 ACCEPTED at `62a50f0a8`

> **SUPERSEDED — producer behavior.** Sections 1 (only its `EventDegraded`
> activation bullet), 2, 3, 4, 5, 5a, 7 items 1–3 and 7, 8, and 10 are not
> authoritative for the Step 9.1 producer implementation. They are retained
> as T1 planning history only. The authoritative replacement is
> [STEP9_1_PRODUCER_CONTRACT.md](../companion-daemon/docs/STEP9_1_PRODUCER_CONTRACT.md).
>
> In particular, prefix-derived `ProducerAuth`, the prohibition on the minimal
> neutral `internal/term` seam, a claimed unconditional five-second close, and
> recursive `EventDegraded` persistence are superseded. New implementation and
> tests must follow the producer contract's composition-owned Bind/revocation,
> post-commit panic-contained seam, truthful close outcome, and non-recursive
> in-memory degradation boundary.

## 1. Scope and authority boundary

Step 9.1 activates minimal Operational Canonical Timeline staging — connecting
bounded, accepted provider-native producers to a non-blocking shadow write path
with fail-open semantics. The `--enable-timeline-shadow` flag remains `false` until
staging evidence proves 7-day stable operation.

**What gets activated (event types):**
- `EventProviderInvocationStarted` — emitted when a managed Codex or Claude runtime starts
- `EventProviderInvocationFinished` — emitted on managed runtime graceful exit
- `EventApprovalRequested` — emitted when an approval is requested
- `EventApprovalResolved` — emitted when an approval is resolved
- `EventToolCallStarted` / `EventToolCallFinished` — already in contract
- `EventStreamObserved` — thinking/streaming observation
- `EventDegraded` — degradation indicator (disk full, write failure, drop)

**What stays unchanged:**
- `ManagedCodexService` and `ManagedClaudeService` lifecycle
- `OwnedPTYRuntime` session lifecycle
- `ApprovalAuthority` approval ingestion, storage, delivery, resolution
- `TerminalTransport` generation-gated input, resize, output
- `Recorder` PTY byte stream (sole reader, no dual-feed)
- `Transcript` service API and consumer behavior
- Device trust, pairing, permission, bearer, WS ticket authority
- All existing REST and WebSocket handlers

## 2. Producer connection (bounded, non-blocking, registered only)

### 2a. Producer registration

Define a `ProducerCapability` interface in `internal/timeline/writer/`:

```go
type ProducerCapability string

type ProducerAuth interface {
    IsRegistered(producer ProducerCapability) bool
}
```

The `Writer` accepts a `ProducerAuth` at construction. `Append` rejects envelopes
from unregistered producers before validation — fail-closed.

Registration occurs at the composition root (`cmd/devremote/app.go`) only.
No `term/` or `cmd/` import of `timeline/writer` beyond the composition boundary.

### 2b. Non-blocking I/O

`Append` must NOT perform synchronous I/O under the caller's lock or goroutine.
A hung filesystem (NFS/disk full/frozen volume) must not block the managed runtime.

**Required architecture:**

```text
Producer goroutine (managed runtime)
  → Writer.Submit(envelope)       // non-blocking, validates+pushes to channel
  → bounded chan (capacity 256)   // ring buffer replacement for submission
  → single I/O worker goroutine   // marshal + write + sync to shadow file
  → on write success: push to ReadRecent ring buffer (128 cap)
  → on write failure: increment Stats.Dropped, log, continue
```

`Submit` returns immediately after pushing to the bounded channel. If the channel
is full, the envelope is dropped and `Stats.Dropped` increments — no blocking,
no backpressure to the producer.

**Test requirement:** inject a blocking `write(2)` sink (never-returning Write).
Prove the runtime continues (session create, input, approval all work) and
`Shutdown` completes without waiting for the hung I/O worker.

### 2c. I/O worker lifecycle

The I/O worker goroutine starts with the Writer and shuts down on `Close()`.
`Close` signals the worker, drains remaining items (best-effort, timeout 5s),
then closes the file. Items not drained before timeout are dropped and counted.

## 3. Fail-open guarantee

Timeline failure is degradation, never daemon crash:

- `Submit` returns `bool` — false on validation/rejection/channel-full. Caller logs, continues.
- I/O worker failure (disk full, permission denied) → drop, increment counter, continue.
- Kill -9 restart → channel and ring buffer empty, new submissions succeed.
- No production path may call `log.Fatal`, `panic`, or `os.Exit` from Timeline code.
- Timeline initialization failure must not block daemon startup.
- `Shutdown` with hung I/O must complete within deadline (5s); remaining items dropped.

## 4. Drop visibility and degradation endpoint

### 4a. Writer.Stats()

The Writer exposes `Stats{Appended uint64, Dropped uint64, Failures uint64}`.
`Appended` counts successfully written+synchronized envelopes. `Dropped` counts
every submission that was lost (channel full, marshal failure, write failure,
sync failure). `Failures` counts I/O errors only.

### 4b. Degradation GET endpoint

A new authenticated endpoint `GET /api/cockpit/degradation` (device bearer
`sessions:read`) returns:

```json
{
  "timeline": {
    "enabled": true,
    "shadowPath": "/path/to/shadow.jsonl",
    "appended": 12345,
    "dropped": 7,
    "failures": 3,
    "degraded": true
  }
}
```

`degraded=true` when `dropped > 0` or the shadow writer is nil despite being
enabled. Cockpit polls this on its own schedule; the endpoint never blocks.

**Test requirement:** after inducing disk-full, the degradation endpoint returns
`degraded:true` with correct `dropped` and `failures` counts.

## 5. Producer authorization

The `Writer` constructor accepts a `ProducerAuth` interface. Only registered
producers may submit:

| Producer | Capability | Registration |
|----------|-----------|-------------|
| `ManagedCodexService` | `"codex_app_server"` | `cmd/devremote/app.go` |
| `ManagedClaudeService` | `"claude_headless"` | `cmd/devremote/app.go` |

`Submit` checks `auth.IsRegistered(ProducerCapability(envelope.SessionID prefix))`
before validation. Unregistered producers are rejected with `Stats.Dropped++`.

**Negative test:** submit an envelope with `Provider: "generic"` → rejected.
Submit from an unregistered producer → rejected. Registered producer → accepted.

### 5a. Dependency direction

- `internal/timeline/writer/` defines `ProducerAuth` interface — no term/cmd imports.
- `cmd/devremote/app.go` implements registration (creates auth, passes to Writer).
- `internal/term/managed_codex.go` and `managed_claude.go` call `Submit` via interface
  — they do NOT import `timeline/writer` directly. The writer is injected through
  the composition root.
- Zero circular dependencies.

## 6. Secret prevention

Every envelope submitted through the staging path MUST use a redacted, digest,
or opaque payload variant. The existing `contract.Payload` validation enforces
this (exactly-one variant required). Additionally:

- `RedactedPayload.Summary` must pass `containsSecretMarker` check (reject "bearer " and "sk-")
- `OpaquePayload.Reference` must pass same check
- No raw `AgentEvent.Text`, `ToolName`, `ApprovalID`, or `RawRef` may accompany a payload variant

**Sentinel-secret test:** submit an envelope with `RedactedPayload{Summary: "Authorization: Bearer sk-abc"}` → rejected. Submit with `RedactedPayload{Summary: "safe summary"}` → accepted.

These checks are already implemented in `contract.go` (lines 84-86, 307-319) and
validated in `writer.go Append` (line 90: `envelope.Validate()`).

## 7. Acceptance tests

1. **Non-blocking submission:** blocking I/O sink → runtime continues, shutdown completes
2. **Drop visibility:** disk-full → `Stats.Dropped` increments, degradation endpoint exposes
3. **Producer auth:** unregistered producer rejected, registered accepted
4. **Secret prevention:** sentinel secrets rejected by envelope validation
5. **No authority regression:** all existing tests pass, no production import changes
6. **Kill -9 restart:** channel+ring empty, new submissions succeed
7. **Graceful shutdown:** hung I/O worker → shutdown completes in <5s
8. **Default-off:** without `--enable-timeline-shadow`, zero Timeline codepaths execute

## 8. Implementation files

**May change:**
- `internal/timeline/writer/writer.go` — add `Submit`, I/O worker, `ProducerAuth`, `Stats`
- `internal/timeline/writer/writer_test.go` — staging acceptance tests
- `cmd/devremote/app.go` — composition wiring (optional, guarded by flag)

**Must NOT change:**
- Any `internal/term/` file (no term→timeline import)
- Any `internal/transcript/` file
- Any `internal/devicetrust/` file
- Any `mobile/` file
- `internal/agent/` (fixture-only per CT-P0)

## 9. Staging gate

- [ ] All existing tests pass
- [ ] 8 acceptance tests pass
- [ ] No production import regressions
- [ ] Staging daemon runs 7 days with `--enable-timeline-shadow`
- [ ] Cockpit shows degradation when drops occur
- [ ] Zero daemon crashes from Timeline code
- [ ] Separate evidence commit records staging results

## 10. Stop conditions

Stop and reject if the change:
- Makes Timeline a daemon startup/session/shutdown prerequisite
- Blocks the managed runtime on I/O (synchronous write under producer lock)
- Adds a callback, observer, or blocking channel from producer goroutines
- Introduces a term→timeline or cmd→timeline import beyond the composition root
- Fails any existing test
- Changes `--enable-timeline-shadow` default
- Exposes raw secrets to the shadow file
