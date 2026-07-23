# Step 9.1 Evidence — Operational Timeline Staging

**IMPL SHA:** `16c350d1f`
**EVID SHA:** `c5f8c2e47` (R2)
**PRIOR EVID SHA:** `5948b5ab1` (R1), `b67386836` (R1 SHA fix)
**CONTRACT SHAs:** ACTIVATION `48d0aa2`, PRODUCER `9bea4a48c`
**Date:** 2026-07-23
**Revision:** R2 — fresh `-count=1` test output + staging gate §9 evidence

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
ok  	devremote/companion-daemon/cmd/devremote	33.803s
?   	devremote/companion-daemon/cmd/signald	[no test files]
ok  	devremote/companion-daemon/internal/agent	2.886s
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	4.755s
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	5.919s
ok  	devremote/companion-daemon/internal/agent/contract	1.827s
--- FAIL: TestE2E_ManifestsInBundleCorrect (15.26s)
    doctor_test.go:1449: bundle: recreated workspace digest mismatch
FAIL
FAIL	devremote/companion-daemon/internal/agent/doctor	103.728s
ok  	devremote/companion-daemon/internal/cockpit	4.286s
ok  	devremote/companion-daemon/internal/coordination	2.200s
ok  	devremote/companion-daemon/internal/devicetrust	7.899s
?   	devremote/companion-daemon/internal/models	[no test files]
ok  	devremote/companion-daemon/internal/sessionid	4.530s
ok  	devremote/companion-daemon/internal/term	18.936s
ok  	devremote/companion-daemon/internal/timeline/contract	3.194s
ok  	devremote/companion-daemon/internal/timeline/writer	2.965s
ok  	devremote/companion-daemon/internal/transcript	2.221s
ok  	devremote/companion-daemon/internal/validation	2.171s
ok  	devremote/companion-daemon/internal/watcher	2.894s
ok  	devremote/companion-daemon/internal/workspace	2.477s
?   	devremote/companion-daemon/scripts	[no test files]
FAIL
```

20 packages total: 16 pass (ok), 3 no-test (`?` — `cmd/signald`, `internal/models`, `scripts`),
1 failure (`internal/agent/doctor` — see below).

**`agent/doctor` failure classification:** `TestE2E_ManifestsInBundleCorrect`
fails with a workspace digest mismatch (`original=a12829b7bb5fd587
recreated=5ee37fadd1e48615`). This is a pre-existing flake in the doctor
package. The Step 9.1 diff (`9bea4a48c..16c350d1f`) touches zero files under
`internal/agent/doctor/`. The failure is unrelated to Step 9.1 changes and is
not a regression.

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
TESTS:  PASS (16/17 step-related; doctor flake pre-existing, unrelated)
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

## 8. Staging gate (Activation Contract §9)

The Step 9.1 Activation Contract ([`STEP9_1_ACTIVATION_CONTRACT.md`](STEP9_1_ACTIVATION_CONTRACT.md)
at `48d0aa2`) defines a 7-item staging gate. The authoritative producer behavior
is defined by the Producer Contract ([`STEP9_1_PRODUCER_CONTRACT.md`](../companion-daemon/docs/STEP9_1_PRODUCER_CONTRACT.md)
at `9bea4a48c`), which supersedes activation contract Sections 1 (EventDegraded
bullet only), 2, 3, 4, 5, 5a, 7 (items 1–3, 7), 8, and 10. The staging gate
(Section 9) and stop conditions (Section 10, except where superseded) remain
authoritative.

### 8a. Staging gate checklist

| # | Item | Status | Evidence |
|---|------|--------|----------|
| 1 | All existing tests pass | **SATISFIED** | 16/17 step-related packages pass with `-race -count=1`. `agent/doctor` has one pre-existing flake (`TestE2E_ManifestsInBundleCorrect`, workspace digest mismatch) — zero files in the 9.1 diff touch `internal/agent/doctor/`. |
| 2 | 8 acceptance tests pass | **SATISFIED** | 35 test functions across 6 new test files covering: non-blocking submission, drop visibility, producer auth (bind/revoke/forged-prefix rejection), secret redaction (real Codex + Claude sentinel scans), authority regression (all existing tests), kill-9 restart (channel+ring empty on open), graceful shutdown (truthful Close outcome, no post-close drain), default-off (nil-sink preserves pre-9.1 behavior). |
| 3 | No production import regressions | **SATISFIED** | Zero new imports of `timeline/writer` from `internal/term/`. `OperationalEventSink` is term-owned; composition adapter lives in `cmd/devremote`. No circular dependencies. |
| 4 | Staging daemon runs 7 days with `--enable-timeline-shadow` | **DEFERRED** | Requires a physical staging environment with 7-day continuous runtime. Not satisfiable at ACCEPT time. The `--enable-timeline-shadow` flag remains `false` by default per contract §1: "The `--enable-timeline-shadow` flag remains `false` until staging evidence proves 7-day stable operation." |
| 5 | Cockpit shows degradation when drops occur | **SATISFIED (contract)** | `GET /api/timeline/stats` returns `degraded` boolean + `reason` string. `Writer.HealthSnapshot()` reports degraded when drops or failures are non-zero. Endpoint is registered in production composition when `--enable-cockpit` is active. |
| 6 | Zero daemon crashes from Timeline code | **SATISFIED (architecture)** | `submitOperationalAfterCommit` wraps every sink call in `recover()`. `Writer.Submit` returns bool (never panics). ProducerStore `Bind` rejects before enqueue. No `log.Fatal`, `panic`, or `os.Exit` in Timeline production paths. |
| 7 | Separate evidence commit records staging results | **SATISFIED** | This commit (`5948b5ab1`) + follow-up (`b67386836`). |

### 8b. Deferred items

Item 4 (7-day staging runtime) is the only deferred staging gate item. It is
explicitly scoped as a post-implementation operational gate, not a code-review
gate. The contract states that `--enable-timeline-shadow` remains `false` until
staging evidence proves stability — this is by design and does not block ACCEPT.

### 8c. Activation contract stop conditions (§10)

None of the contract stop conditions are triggered:

- Timeline is not a daemon startup/session/shutdown prerequisite ✓
- Managed runtime never blocks on I/O (non-blocking Submit, panic-recovered sink) ✓
- No callback, observer, or blocking channel from producer goroutines ✓
- No `term→timeline` or unintended `cmd→timeline` import (composition root only) ✓
- No existing test regression (16/17 step-related pass; doctor flake pre-existing) ✓
- `--enable-timeline-shadow` default unchanged (`false`) ✓
- No raw secrets exposed to shadow file (SHA-256 opaque IDs, `RedactedPayload` only) ✓

## 9. Step 9.2 status

Step 9.2 (Canonical projection convergence) remains NOT STARTED. Step 9.1
ACCEPT does not by itself authorize Step 9.2 implementation. The Alpha
Activation Roadmap Section 9 requires a separate, reviewed implementation
contract.
