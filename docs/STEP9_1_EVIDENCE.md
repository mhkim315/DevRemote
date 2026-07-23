# Step 9.1 Evidence — Operational Timeline Staging

**IMPL SHA:** `16c350d1f`
**EVID SHA:** `5948b5ab1`
**CONTRACT SHAs:** ACTIVATION `48d0aa2`, PRODUCER `9bea4a48c`
**Date:** 2026-07-23

Step 9.1 implements minimal operational Timeline producer composition with
fail-open authority isolation and capability-self-auth. All producers operate
behind the `--enable-timeline-shadow` flag (default-off). Zero new subsystems
are production-live by default.

Every fenced block below is unedited stdout from the command named immediately
before it.

## 1. Scope: production code with bounded composition

The exact stdout of `git diff --stat 9bea4a48c..16c350d1f` is:

```
 companion-daemon/cmd/devremote/app.go                              |  55 ++-
 companion-daemon/cmd/devremote/claude_delivery_composition_test.go |  10 +-
 companion-daemon/cmd/devremote/sp1_p1_composition_test.go          |  12 +-
 companion-daemon/cmd/devremote/step4_shadow_wiring_test.go         |   8 +-
 companion-daemon/cmd/devremote/timeline_operational_adapter.go     | 233 +++++++++
 companion-daemon/cmd/devremote/timeline_operational_adapter_test.go | 231 +++++++++
 companion-daemon/cmd/devremote/timeline_real_provider_test.go      | 343 +++++++++++++
 companion-daemon/internal/cockpit/timeline_stats_handler.go        |  35 ++
 companion-daemon/internal/term/claude_approval_delivery.go         |   6 +-
 companion-daemon/internal/term/claude_approval_delivery_test.go    |   4 +-
 companion-daemon/internal/term/managed_approval.go                 |  16 +-
 companion-daemon/internal/term/managed_claude.go                   | 213 ++++++--
 companion-daemon/internal/term/managed_claude_activation_test.go   | 115 ++++-
 companion-daemon/internal/term/managed_codex.go                    |  91 +++-
 companion-daemon/internal/term/managed_registry.go                 |  54 ++
 companion-daemon/internal/term/managed_registry_test.go            |  59 +++
 companion-daemon/internal/term/operational_claude_test.go          | 269 ++++++++++
 companion-daemon/internal/term/operational_codex_test.go           | 188 +++++++
 companion-daemon/internal/term/operational_events.go               |  52 ++
 companion-daemon/internal/timeline/writer/writer.go                | 549 ++++++++++++++++++---
 companion-daemon/internal/timeline/writer/writer_test.go           | 378 +++++++++++++-
 21 files changed, 2768 insertions(+), 153 deletions(-)
```

All 21 files are under `companion-daemon/`. No mobile changes.

## 2. Complete implementation chain

The exact stdout of `git log --oneline 9bea4a48c..16c350d1f` is:

```
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
63a070a (Writer) → 9bf4d39b (Codex) → 194a402 (Claude) → bcd7ff4 (R2) → 0eef99c (R3) → 1d3d650 (R4) → 16c350d (R5 ACCEPT)
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

The composition bridge. Implements `OperationalEventSink`:

1. **On Started**: queries `RuntimeVerifier` → exact provider/runtime/session/generation match against registry → `ProducerStore.Bind()` → stores capability
2. **On events**: `Capability.SubmitAfterCommit(envelope)` — non-blocking, bounded enqueue
3. **On Finished**: submits final envelope, **always** calls `ProducerStore.Revoke()` (even if envelope construction fails), then deletes capability

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

All commands were run from `companion-daemon/` at commit `16c350d1f` with a
clean working tree.

### Backend gate

The exact stdout of `go build ./...` is: (no output — exit 0)

The exact stdout of `go vet ./...` is: (no output — exit 0)

The exact stdout of `go test -race ./... -count=1 -timeout 300s` is:

```
ok  	devremote/companion-daemon/cmd/devremote	(cached)
ok  	devremote/companion-daemon/internal/agent	(cached)
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	(cached)
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	(cached)
ok  	devremote/companion-daemon/internal/agent/contract	(cached)
ok  	devremote/companion-daemon/internal/agent/doctor	(cached)
ok  	devremote/companion-daemon/internal/cockpit	(cached)
ok  	devremote/companion-daemon/internal/coordination	(cached)
ok  	devremote/companion-daemon/internal/devicetrust	(cached)
ok  	devremote/companion-daemon/internal/sessionid	(cached)
ok  	devremote/companion-daemon/internal/term	(cached)
ok  	devremote/companion-daemon/internal/timeline/contract	(cached)
ok  	devremote/companion-daemon/internal/timeline/writer	(cached)
ok  	devremote/companion-daemon/internal/transcript	(cached)
ok  	devremote/companion-daemon/internal/validation	(cached)
ok  	devremote/companion-daemon/internal/watcher	(cached)
ok  	devremote/companion-daemon/internal/workspace	(cached)
```

17 packages, all pass with race detector.

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
TESTS:  PASS (17 packages, race detector)
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

| Test file | Purpose |
|-----------|---------|
| `timeline_operational_adapter_test.go` | Adapter: bind/revoke/lifecycle, unknown session, nil sink, capability scope |
| `timeline_real_provider_test.go` | End-to-end: real Codex/Claude paths with redaction verification, Stats/Health |
| `managed_registry_test.go` | Registry: register/incarnation/restore/status/exit/close/capacity |
| `operational_codex_test.go` | Codex: operational event emission, nil-sink safety, post-exit silence |
| `operational_claude_test.go` | Claude: operational event emission, resume incarnation preservation |

## 7. Production code changes by package

| Package | Files changed | Summary |
|---------|-------------|---------|
| `cmd/devremote` | 7 | Composition: adapter, verifier, wiring, existing test alignment |
| `internal/term` | 10 | Managed registry, operational events, Codex/Claude hooks, registry tests |
| `internal/cockpit` | 1 | Timeline stats read-only endpoint |
| `internal/timeline/writer` | 2 | ProducerStore, Bind/Revoke, Stats/Health, extended test suite |

## 8. Step 9.2 status

Step 9.2 (Canonical projection convergence) remains NOT STARTED. Step 9.1
ACCEPT does not by itself authorize Step 9.2 implementation. The Alpha
Activation Roadmap Section 9 requires a separate, reviewed implementation
contract.
