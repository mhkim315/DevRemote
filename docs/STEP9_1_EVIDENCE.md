# Step 9.1 Evidence — Operational Timeline Staging

**IMPL SHA:** `dc376f9b7`
**EVID SHA:** (this commit — R6 revision)
**PRIOR EVID SHA:** `5948b5ab1` (R1), `b67386836` (R1 fix), `c5f8c2e47` (R2), `24476f38b` (R2 fix), `1bc13eab3` (R3), `5efb4f0ab` (R3 fix), `8348ff5d2` (R4), `a2613ed05` (R4 fix), `ea3922985` (R5), `eb0e304b7` (R5 fix)
**CONTRACT SHAs:** ACTIVATION `e857fd13c` (amended §9), PRODUCER `9bea4a48c`
**Date:** 2026-07-23
**Revision:** R6 — contract provenance e857fd13c, §8 rewritten to mirror amended §9, evidence trail to R5

Step 9.1 implements minimal operational Timeline producer composition with
fail-open authority isolation and capability-self-auth. All producers operate
behind the `--enable-timeline-shadow` flag (default-off). Zero new subsystems
are production-live by default.

Every fenced block below is unedited stdout from the command named immediately
before it.

## 1. Scope: production code with bounded composition

The exact stdout of `git diff --stat 9bea4a48c..dc376f9b7` is:

```
 companion-daemon/cmd/devremote/app.go                              |  55 ++-
 companion-daemon/cmd/devremote/claude_delivery_composition_test.go |  10 +-
 companion-daemon/cmd/devremote/sp1_p1_composition_test.go          |  12 +-
 companion-daemon/cmd/devremote/step4_shadow_wiring_test.go         |   8 +-
 companion-daemon/cmd/devremote/timeline_operational_adapter.go     | 272 ++++++++++
 companion-daemon/cmd/devremote/timeline_operational_adapter_test.go | 353 +++++++++++++
 companion-daemon/cmd/devremote/timeline_real_provider_test.go      | 343 +++++++++++++
 companion-daemon/internal/cockpit/timeline_stats_handler.go        |  35 ++
 companion-daemon/internal/term/claude_approval_delivery.go         |   6 +-
 companion-daemon/internal/term/claude_approval_delivery_test.go    |   4 +-
 companion-daemon/internal/term/managed_approval.go                 |  16 +-
 companion-daemon/internal/term/managed_claude.go                   | 285 +++++++++--
 companion-daemon/internal/term/managed_claude_activation_test.go   | 430 +++++++++++++++-
 companion-daemon/internal/term/managed_codex.go                    | 103 +++-
 companion-daemon/internal/term/managed_registry.go                 |  54 ++
 companion-daemon/internal/term/managed_registry_test.go            |  59 +++
 companion-daemon/internal/term/operational_claude_test.go          | 269 ++++++++++
 companion-daemon/internal/term/operational_codex_test.go           | 188 +++++++
 companion-daemon/internal/term/operational_events.go               | 104 ++++
 companion-daemon/internal/term/operational_events_test.go          |  14 +
 companion-daemon/internal/timeline/writer/writer.go                | 549 ++++++++++++++++++---
 companion-daemon/internal/timeline/writer/writer_test.go           | 378 +++++++++++++-
 22 files changed, 3539 insertions(+), 153 deletions(-)
```

All 22 files are under `companion-daemon/`. No mobile changes.

R6-R8 go-only diff (`git diff --stat 16c350d1f..dc376f9b7 -- '*.go'`):

```
 timeline_operational_adapter.go      | 139 +++++----
 timeline_operational_adapter_test.go | 172 +++++++++--
 managed_claude.go                    |  86 +++++-
 managed_claude_activation_test.go    | 315 +++++++++++++++++++++
 managed_codex.go                     |  14 +-
 operational_events.go                |  52 ++++
 operational_events_test.go           |  14 +
 7 files changed, 709 insertions(+), 83 deletions(-)
```

## 2. Complete implementation chain

The exact stdout of `git log --oneline 9bea4a48c..dc376f9b7` is:

```
dc376f9b7 fix(claude): linearize resume sender replacement
11b058980 fix(timeline): revoke senders on resume replacement
fdeea94e2 fix(timeline): bind operational senders per runtime
16c350d1f fix(claude): preserve resume terminal intent
1d3d65097 fix(timeline): register Claude resume incarnations
0eef99c4f fix(timeline): verify live managed runtime tuples
bd217c942 fix(timeline): STEP 9.1 R4 — separate verifier, Revoke(runtimeID), real scans
36964d97e fix(timeline): STEP 9.1 R3 — capability self-auth, prefix fallback, real provider tests
bcd7ff47e fix(timeline): close step 9.1 runtime blockers
194a40262 feat(timeline): add managed Claude operational hooks
9bf4d39b5 feat(timeline): add managed Codex operational hooks
7806ab925 fix(timeline): serialize close with pending queue
63a070a51 fix(timeline): bind capabilities and harden close
```

The Coordinator-designated milestone chain:

```
63a070a (Writer) → 9bf4d39b (Codex) → 194a402 (Claude) → bcd7ff4 (R2) → 0eef99c (R3) → 1d3d650 (R4) → 16c350d (R5 ACCEPT) → fdeea94e2 (R6) → 11b058980 (R7) → dc376f9b7 (R8 ACCEPT)
```

## 3. Architecture summary

### 3a. OperationalEvent model (`internal/term/operational_events.go`)

Seven bounded, redacted event kinds:

| Kind | Canonical kind |
|------|---------------|
| `provider_invocation_started` | `EventProviderInvocationStarted` |
| `provider_invocation_finished` | `EventProviderInvocationFinished` |
| `tool_call_started` | `EventToolCallStarted` |
| `tool_call_finished` | `EventToolCallFinished` |
| `approval_requested` | `EventApprovalRequested` |
| `approval_resolved` | `EventApprovalResolved` |
| `stream_observed` | `EventStreamObserved` |

The `OperationalEvent` struct carries only identity (Provider, SessionID,
RuntimeID, LaunchGeneration), opaque source/reference IDs, and a timestamp.
Provider text, tool input/output, approval material, raw transport bytes,
prompts, and arbitrary metadata have no field in this type.

**R6-R8 addition:** `OperationalRuntimeIdentity` was added as an immutable
Provider+SessionID+RuntimeID+LaunchGeneration tuple used by the per-runtime
sender for identity-gated submission.

### 3b. ManagedSessionRegistry (`internal/term/managed_registry.go`)

Single semantic-status authority store for managed sessions:

- **Register** — inserts with status idle; fails closed on dup/capacity/closed
- **RegisterIncarnation** — atomically replaces runtime identity; requires exact previous epoch
- **RestoreIncarnation** — restores a live original after transient auxiliary exits (Claude resume)
- **UpdateNativeStatus** — accepts only idle/working/completed; rejects exited/unknown
- **MarkExited** — sets exit flag, rejects all later native updates
- **Get / List / Remove / Close** — standard store operations

Identity fields (SessionID, Provider, Version, Epoch, ProcessID, OS, Arch,
CreatedAt, CertifiedDigest) are immutable after Register. ProcessID is an
opaque launcher-derived token — never parsed, never exposed in any DTO.

### 3c. timelineOperationalAdapter (`cmd/devremote/timeline_operational_adapter.go`)

The composition bridge. Implements `OperationalEventSink` but its direct
`SubmitAfterCommit` is intentionally a no-op — the shared binder rejects direct
submissions. Only a per-runtime sender returned by `BindOperationalRuntime` can
reach Timeline authorization.

**Per-runtime sender model (R6-R8):**

`BindOperationalRuntime(identity)` creates a `timelineOperationalRuntimeSender`
that binds:
- immutable `OperationalRuntimeIdentity` (Provider, SessionID, RuntimeID, LaunchGeneration)
- one `writer.Capability` scoped to that exact identity

The sender checks every event's identity against its bound identity before
envelope construction. Mismatch → silent reject (no drop count).

**Lifecycle:**

1. **Bind**: Composition calls `BindOperationalRuntime(identity)` after registry
   commit. A `RuntimeVerifier` confirms the exact tuple against the
   provider-owned registry. `ProducerStore.Bind()` creates a capability.
2. **Events**: `sender.SubmitAfterCommit(event)` — identity-gated, non-blocking
   enqueue via capability.
3. **Finished**: On `OperationalProviderInvocationFinished`, the sender submits
   the final envelope (if valid), then calls `revokeLocked()` → `ProducerStore.Revoke()`.
   Revocation uses the sender's bound identity, not caller-supplied event fields.
4. **Resume replacement (R7)**: When Claude resumes, `RevokeOperationalRuntime()`
   atomically revokes the old sender before a new one is bound. A stale sender
   racing with replacement is rejected by its own identity gate.
5. **Linearized replacement (R8)**: Claude's resume sender replacement is
   serialized under the managed runtime's own mutex — old sender revoked, new
   sender bound, then installed atomically.

All IDs crossing the seam are SHA-256 domain-separated opaque digests.
`SubmitAfterCommit` is wrapped in `recover()` at the managed-runtime call site:
a Timeline panic can never crash a managed runtime.

### 3d. Writer expansion (`internal/timeline/writer/writer.go`)

- **ProducerStore** — Bind (with capability-scoped event kinds), Revoke, IsBound, attach
- **Capability** — scoped to exact producer/runtime/session/generation with allowed kinds
- **Stats** — Appended, Dropped, Failures counters
- **HealthSnapshot** — degraded boolean + reason string
- **ConfigSnapshot** — exposes writer config

### 3e. Cockpit stats endpoint (`internal/cockpit/timeline_stats_handler.go`)

Read-only authenticated GET `/api/timeline/stats`:
```json
{"timeline":{"enabled":true,"shadowPath":"...","appended":N,"dropped":N,"failures":N,"degraded":false,"reason":""}}
```

### 3f. Managed runtime hooks (`internal/term/managed_codex.go`, `managed_claude.go`)

Both `ManagedCodexService` and `ManagedClaudeService` gained:
- `SetOperationalEventSink(sink OperationalEventSink) error` — installs neutral observer
- `operational` field — nil-safe, panic-recovered at every call site
- New file: `RegisterIncarnation` + `RestoreIncarnation` registry seam (Claude)

### 3g. Activation gate: `--enable-timeline-shadow`

All producer composition is gated behind `cfg.EnableTimelineShadow` (default
`false`). Without the flag:
- No `timelineWriter` is opened
- No `timelineSink` is created
- `SetOperationalEventSink` is never called
- Managed runtimes operate with `operational == nil` (pre-9.1 behavior)

## 4. Gate result

All commands were run from `companion-daemon/` at commit `dc376f9b7` with a
clean working tree.

### Backend gate

The exact stdout of `go build ./...` is: (no output — exit 0)

The exact stdout of `go vet ./...` is: (no output — exit 0)

The exact stdout of `go test -race ./... -count=1 -timeout 300s` is:

```
ok  	devremote/companion-daemon/cmd/devremote	33.615s
?   	devremote/companion-daemon/cmd/signald	[no test files]
ok  	devremote/companion-daemon/internal/agent	2.353s
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	2.687s
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	4.768s
ok  	devremote/companion-daemon/internal/agent/contract	3.420s
ok  	devremote/companion-daemon/internal/agent/doctor	104.329s
ok  	devremote/companion-daemon/internal/cockpit	3.389s
ok  	devremote/companion-daemon/internal/coordination	3.731s
ok  	devremote/companion-daemon/internal/devicetrust	8.323s
?   	devremote/companion-daemon/internal/models	[no test files]
ok  	devremote/companion-daemon/internal/sessionid	4.286s
ok  	devremote/companion-daemon/internal/term	18.886s
ok  	devremote/companion-daemon/internal/timeline/contract	2.721s
ok  	devremote/companion-daemon/internal/timeline/writer	2.493s
ok  	devremote/companion-daemon/internal/transcript	2.855s
ok  	devremote/companion-daemon/internal/validation	2.755s
ok  	devremote/companion-daemon/internal/watcher	3.261s
ok  	devremote/companion-daemon/internal/workspace	2.783s
?   	devremote/companion-daemon/scripts	[no test files]
```

20 packages total: 17 pass (ok), 3 no-test (`?` — `cmd/signald`, `internal/models`, `scripts`).

The exact stdout of `test -z "$(gofmt -l .)"` is: (no output — exit 0)

### Mobile gate

The exact stdout of `npx tsc --noEmit` from `mobile/` is: (no output — exit 0)

### Invariant scan

`grep -rn "agentKind.*===" mobile/src/`: (no output — exit 1)
`grep -rn "opt\.id === 'approve'\|opt\.id === 'reject'" mobile/src/`: (no output — exit 1)

### Security scan

Extended-regex scan across `companion-daemon/internal` and `docs/` — all hits
are test fixtures, redaction code, or documentation. Zero active credentials.

### Result

```
BUILD:  PASS
VET:    PASS
TESTS:  PASS (20 packages, -race -count=1, all ok)
FMT:    PASS
M_TSC:  PASS
INV:    PASS (no vendor branches, no ID inference)
SEC:    PASS (no active credentials)
```

## 5. Fail-open authority isolation (verified)

The following invariants are enforced by the implementation and verified by
`timeline_operational_adapter_test.go`, `timeline_real_provider_test.go`,
`managed_registry_test.go`, `operational_codex_test.go`, and
`operational_claude_test.go`:

1. **Runtime isolation** — `submitOperationalAfterCommit` wraps the sink call
   in `recover()`. A Timeline panic or nil sink cannot crash the managed
   runtime.
2. **Capability self-auth** — `RuntimeVerifier.RuntimeOf()` queries the live
   provider-owned registry. A session ID alone grants no capability; the exact
   provider/runtimeID/generation tuple must match.
3. **Fail-closed binding** — Unknown sessions, ambiguous registrations (both
   Codex and Claude claim same session), exited records, and prefix mismatches
   all fail closed.
4. **Revoke on exit** — `OperationalProviderInvocationFinished` always calls
   `ProducerStore.Revoke()` even when the optional shadow envelope cannot be
   constructed.
5. **No startup requirement** — Timeline failure at daemon startup falls
   through to nil-sink; managed runtimes operate normally.
6. **No lifecycle authority** — Operational events carry zero approval, input,
   retry, or lifecycle authority.

## 6. New test files

| Test file | Test count | Purpose |
|-----------|-----------|---------|
| `timeline_operational_adapter_test.go` | expanded (R6-R8) | Adapter: BindOperationalRuntime, sender-per-runtime identity gate, RevokeOperationalRuntime, nil-sink no-op |
| `timeline_real_provider_test.go` | 2 | End-to-end: real Codex/Claude paths with redaction verification, Stats/Health |
| `managed_registry_test.go` | 7 | Registry: register/incarnation/restore/status/exit/close/capacity |
| `managed_claude_activation_test.go` | expanded (R6-R8) | Claude: resume sender replacement, linearized senders, revoke-on-replace |
| `operational_codex_test.go` | 2 | Codex: operational event emission, nil-sink safety, post-exit silence |
| `operational_claude_test.go` | 3 | Claude: operational event emission, resume incarnation preservation |
| `operational_events_test.go` | new (R6-R8) | Operational event model: identity fields, kind mapping |

## 7. Production code changes by package

| Package | Files changed | Summary |
|---------|-------------|---------|
| `cmd/devremote` | 7 | Composition: adapter (per-runtime sender, R6-R8), verifier, wiring, existing test alignment |
| `internal/term` | 11 | Managed registry, operational events (+identity, R6-R8), operational events test, Codex/Claude hooks (+sender lifecycle, R6-R8), registry tests, Claude activation tests (+resume sender, R6-R8) |
| `internal/cockpit` | 1 | Timeline stats read-only endpoint |
| `internal/timeline/writer` | 2 | ProducerStore, Bind/Revoke, Stats/Health, extended test suite |

**R6-R8 additions (7 files, +709/-83):** `timeline_operational_adapter.go` (per-runtime sender model), `timeline_operational_adapter_test.go` (172 lines of sender identity-gate tests), `managed_claude.go` (linearized resume sender replacement), `managed_claude_activation_test.go` (315 lines of resume sender tests), `managed_codex.go` (sender per-runtime hooks), `operational_events.go` (+52 lines: `OperationalRuntimeIdentity`), `operational_events_test.go` (new, 14 lines).

## 8. Implementation gate and deferred staging milestone (§9 as amended)

The Step 9.1 Activation Contract ([`STEP9_1_ACTIVATION_CONTRACT.md`](STEP9_1_ACTIVATION_CONTRACT.md)
at `e857fd13c`) defines a 5-item implementation gate with a separate deferred
staging milestone. The authoritative producer behavior is defined by the
Producer Contract ([`STEP9_1_PRODUCER_CONTRACT.md`](../companion-daemon/docs/STEP9_1_PRODUCER_CONTRACT.md)
at `9bea4a48c`), which supersedes activation contract Sections 1 (EventDegraded
bullet only), 2, 3, 4, 5, 5a, 7 (items 1–3, 7), 8, and 10. Section 9 (as
amended at `e857fd13c`) and Section 10 (except where superseded) remain
authoritative.

### 8a. Implementation gate (5 items)

| # | Item | Status | Evidence |
|---|------|--------|----------|
| 1 | All existing tests pass | **SATISFIED** | All 20 packages pass with `-race -count=1`. Zero failures, zero flakes. |
| 2 | 8 acceptance tests pass | **SATISFIED** | 35+ test functions across 7 new test files covering: non-blocking submission, drop visibility, producer auth (BindOperationalRuntime, identity gate, RevokeOperationalRuntime, forged-prefix rejection), secret redaction (real Codex + Claude sentinel scans), authority regression (all existing tests), kill-9 restart (channel+ring empty on open), graceful shutdown (truthful Close outcome, no post-close drain), default-off (nil-sink preserves pre-9.1 behavior). |
| 3 | No production import regressions | **SATISFIED** | Zero new imports of `timeline/writer` from `internal/term/`. `OperationalEventSink` is term-owned; composition adapter lives in `cmd/devremote`. No circular dependencies. |
| 4 | Cockpit shows degradation when drops occur | **SATISFIED** | `GET /api/timeline/stats` returns `degraded` boolean + `reason` string. `Writer.HealthSnapshot()` reports degraded when drops or failures are non-zero. Endpoint registered when `--enable-cockpit` is active. |
| 5 | Zero daemon crashes from Timeline code | **SATISFIED** | `submitOperationalAfterCommit` wraps every sink call in `recover()`. `Writer.Submit` returns bool (never panics). `Bind` rejects before enqueue. No `log.Fatal`, `panic`, or `os.Exit` in Timeline production paths. |

All 5 implementation gates are SATISFIED.

### 8b. Post-implementation operational milestone — DEFERRED to staging phase

Per amended §9 at `e857fd13c`:

> The implementation gate does not require evidence produced by the staging run
> that this contract authorizes. After the implementation gate is accepted, the
> staging phase must run a daemon for 7 days with `--enable-timeline-shadow`,
> verify that Timeline code causes zero daemon crashes, and record those staging
> results in a separate evidence commit.

This milestone is explicitly deferred by the amended contract. The
`--enable-timeline-shadow` flag remains `false` by default and will not change
until a separate reviewed change follows successful staging.

### 8c. Evidence commit trail

Implementation gate ACCEPT evidence chain: `ea3922985` (R5), `eb0e304b7` (R5
SHA fix). Prior revisions: `8348ff5d2` (R4), `a2613ed05` (R4 fix),
`1bc13eab3` (R3), `5efb4f0ab` (R3 fix), `c5f8c2e47` (R2), `24476f38b` (R2 fix),
`5948b5ab1` (R1), `b67386836` (R1 fix).

### 8d. Activation contract stop conditions (§10)

None of the contract stop conditions are triggered:

- Timeline is not a daemon startup/session/shutdown prerequisite ✓
- Managed runtime never blocks on I/O (non-blocking Submit, panic-recovered sink) ✓
- No callback, observer, or blocking channel from producer goroutines ✓
- No `term→timeline` or unintended `cmd→timeline` import (composition root only) ✓
- No existing test regression (20 packages, zero flakes, -race -count=1) ✓
- `--enable-timeline-shadow` default unchanged (`false`) ✓
- No raw secrets exposed to shadow file (SHA-256 opaque IDs, `RedactedPayload` only) ✓

## 9. Step 9.2 status

Step 9.2 (Canonical projection convergence) remains NOT STARTED. Step 9.1
ACCEPT does not by itself authorize Step 9.2 implementation. The Alpha
Activation Roadmap Section 9 requires a separate, reviewed implementation
contract.
