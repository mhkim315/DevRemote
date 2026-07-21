# PB Legacy Removal Execution Plan

**Status:** EXECUTED THROUGH AUTOMATED CLOSEOUT — pre-device QR/Input remediation planned; physical device gate not started; PB ACCEPT SHA remains UNSET.

**Branch:** `feature/phase10-multi-adapter`

## 1. Frozen identities

| Identity | Exact SHA | Purpose |
|---|---|---|
| `PA4_PRODUCTION_SHA` | `4aaf3b76a5a4bfab64b041b3be19eaa32d2dd0e2` | Frozen managed-isolation production implementation |
| `PA4_ACCEPT_SHA` | `74560edd88ef5b53c3b3d8d215f3007efbf468a2` | Independent PA4 acceptance record |
| `PB_PREREQUISITE_SHA` | `74560edd88ef5b53c3b3d8d215f3007efbf468a2` | Contract authority required before PB |
| `PB_START_BASELINE_SHA` | `abe4df1d6485a8eceafde30e7dfc8da06b8c7f06` | Operational rollback preserving the frozen PA4 and roadmap documents |
| `CURRENT_NON_DEVICE_CHECKPOINT_SHA` | `f052e7f8a60ae0ece8b7a5535045f59c55b8e3c4` | Automated PB checkpoint before QR/Input remediation |
| Future `PB_DEVICE_CANDIDATE_SHA` | **UNSET** | Exact production/mobile source for the device daemon and APK |
| Future `PB_AUTOMATED_EVIDENCE_SHA` | **UNSET** | Documentation-only head after PB.6/PB.7 regeneration |
| Future `PB_ACCEPT_SHA` | **UNSET** | Assigned only by the final independent PB verifier |

`PB_PREREQUISITE_SHA` and `PB_START_BASELINE_SHA` have different meanings.
PB must prove both are ancestors. Reverting to the prerequisite alone would
discard the later authority freeze; operational rollback therefore uses the
start baseline.

## 2. Scope and invariant

PB removes legacy authority without redesigning the managed product. It retains:

- `ManagedRuntimeCatalog`, managed Codex/Claude runtimes, and provider-native
  semantic status and approval;
- `OwnedPTYRuntime`, captured generation cleanup, compare-and-terminate, and
  stale-generation rejection;
- `Recorder` as the sole PTY reader and exact-generation
  `TerminalTransport` for input, resize, replay, WS, and IPC;
- a narrow platform-neutral PTY launch/session boundary for Unix PTY and
  future Windows ConPTY.

PB must not pull Canonical Timeline, a common provider interface,
Grok/ACP, Navigator/Guard, pairing redesign, or unrelated terminal UX fixes
forward.

## 3. PB.0 consumer inventory

The executor must regenerate this inventory at the start of every affected
wave. File names below identify current ownership; they are not permission to
delete a file before its consumers are zero.

### 3.1 Legacy mux ownership

- Interfaces and registry:
  `internal/mux/adapter.go`, `adapter_capability.go`, `session.go`,
  `registry.go`, `id_parser.go`, `tracker.go`,
  `transcript_capture.go`.
- Replaced local PTY:
  `internal/mux/localpty_adapter.go` and its tests.
- Managed PTY still sharing mux primitives:
  `internal/mux/controlled_pty_adapter.go`,
  `internal/term/create.go`, `owned_pty_runtime.go`, and application
  registration in `cmd/devremote/app.go`.
- tmux:
  `internal/mux/tmux_adapter.go`, tests, registration, commands, scripts,
  packaging, and capability projections.
- cmux:
  `internal/mux/cmux_adapter.go`, `cmux_delta.go`, parser/diagnostic tests,
  test data, registration, and capability projections.

Before PB.1 deletes localpty, PB.0 must prove whether it shares a spawn or
session primitive with controlled PTY. A shared primitive is first moved behind
the retained platform-neutral boundary; the legacy adapter is then deleted.

### 3.2 Link, discovery, and observer ownership

- Route/store/application composition:
  `cmd/devremote/app.go` and link/attach handlers and stores reached from it.
- Resolver and external observation:
  `internal/term/tracker.go`, `resolver.go`, `gemini_resolver.go`,
  `telemetry_service.go`, `telemetry.go`, and their tests.
- Process discovery:
  `internal/agent/detector.go` and consumers that project legacy sessions.
- Provider-native paths:
  managed Codex/Claude runtimes and parsers are retained. Merely parsing
  provider-owned JSONL is not legacy discovery.

Gemini resolver is removed only after an exact production-consumer search
proves zero accepted consumers. It is not retained merely as an “accepted
adapter.”

### 3.3 Recorder and terminal compatibility

- Global ID lookup and compatibility:
  `internal/term/recorder.go`, `pty.go`, `ipc.go`, and lifecycle/create
  callers.
- cmux-only compatibility:
  snapshot/delta capture mode, `snapshotEndMarker`, `deltaMarker`,
  snapshot drain/filter, and degraded snapshot projection.
- Retained behavior:
  byte-stream capture, Transcript projection, atomic bootstrap/live fan-out,
  generation-bound transport, resize, and input.

### 3.4 Mobile, scripts, and packaging

Current mobile consumers requiring PB.6 review include:

- `mobile/src/components/AgentCard.tsx` and `NewSessionModal.tsx`;
- `mobile/src/lib/client.ts`, `lifecycle.ts`, and `agentActivity.ts`;
- dashboard/feed screens and corresponding tests.

Scripts and packaging searches include `scripts/`,
`companion-daemon/scripts/`, `cmd/devremote/main.go`, launch-agent files,
and platform manifests. Historical evidence may retain legacy words only when
clearly marked superseded; it cannot claim current product authority.

## 4. Execution waves

Every code wave uses an implementation commit followed by an evidence-only
commit and independent read-only acceptance. A rejected wave is repaired before
the next wave begins. Compatibility stubs, skipped tests, early returns,
nil-filled fixtures, or log-instead-of-assert substitutions are forbidden.

### PB.1 — replaced localpty paths

1. Prove or separate any spawn/session primitive shared with controlled PTY.
2. Delete localpty registration, creation, session/transport code,
   configuration, tests, scripts, and current product claims.
3. Prove zero production/mobile/package consumers.
4. Prove managed shell create, replace, stop, and cleanup remain unchanged.

### PB.2a — link and arbitrary attach

1. Remove manual-link and arbitrary-attach handlers, stores, commands, mobile
   calls, and tests in ownership order.
2. Removed HTTP routes must return **404 Not Found**. A 404/410 range is not an
   acceptable assertion.
3. Prove no removed route mutates catalog, transcript, approval, or lifecycle.

### PB.2b — discovery, resolver, and external observer

1. Remove process discovery and legacy telemetry projection consumers.
2. Remove `ResolveAgentLog` and external raw-log observation.
3. Remove Gemini resolver only after zero accepted production consumers.
4. Preserve managed Codex/Claude provider-native evidence and assert that no
   external observation can create managed status or approval.

### PB.3 — tmux

Delete the tmux adapter, registration, commands, configuration, packaging,
capability projection, and tests after zero-consumer proof. Preserve the
generic `adapter:id` session-ID grammar and provider-neutral validation.

### PB.4 — cmux and recorder snapshot compatibility

Delete cmux and its delta/tree tooling, then remove only cmux-specific recorder
sentinel, snapshot drain/filter, capture-mode, and degraded projection paths.
Do not redesign Recorder. Positive byte-stream, ANSI clear-screen,
Transcript, replay, and subscriber tests must remain fail-closed.

### PB.5a — managed PTY spawn seam independence

Move controlled-PTY creation and termination off `mux.Registry` onto a narrow
platform-neutral launcher/session boundary. It may expose spawn, resize, write,
wait, signal, and close capabilities needed by managed PTY ownership; it must
not expose registry, discovery, adapter selection, or legacy list/get APIs.
Independently verify lock-free external I/O and captured-generation cleanup.

### PB.5b — Registry, Adapter, and global identity deletion

After PB.5a consumer count is zero, delete `mux.Registry`, legacy
`Adapter`/`Session` abstractions, context registry plumbing, global
ID-addressed Recorder lookup, legacy handler dependencies, and dead factories.
Prove same-ID replacement cannot make old cleanup act on the new generation.

### PB.6 — daemon-wire and mobile cleanup

After daemon wire absence is proven, remove obsolete adapter unions,
capabilities, DTO fields, feature gates, API fallbacks, and UI assumptions.
Retain managed Codex/Claude, controlled PTY, Transcript/replay, trimmed
telemetry, pairing, and device authentication.

### PB.7 — automated closeout and device gate

The original automated closeout reached the current non-device checkpoint, but
the physical gate has not started. Before device execution, complete the QR,
Input-A, and Input-B packets and their independent acceptances under
[`PRE_DEVICE_QR_INPUT_REMEDIATION_PLAN.md`](PRE_DEVICE_QR_INPUT_REMEDIATION_PLAN.md).
Then regenerate all affected PB.6/PB.7 evidence, freeze the exact production
candidate, and build both daemon and APK from that same candidate. Only then set
status to `AWAITING_DEVICE_GATE`, run the Android matrix in Section 5, publish
exact evidence, and request independent PB verification.

## 5. Frozen PA4 physical-device comparison matrix

Baseline device: **Samsung SM-S926N (Galaxy S24 Ultra), Android 16**.
Daemon mode: production configuration, without `--insecure-local-only`.

| Surface | Frozen PA4 state | PB.7 rule |
|---|---|---|
| Production QR pairing | PASS | Any failure is a PB blocker |
| Device identity save | PASS during the accepted pairing flow | Save regression blocks; cold-start restore is separately classified below |
| Stored identity cold-start restore | Not independently isolated in PA4 evidence | PB.7 must demonstrate successful restore; failure blocks acceptance |
| Authenticated REST | PASS | Any failure is a PB blocker |
| Authenticated WS | PASS | Any failure is a PB blocker |
| Managed session discovery | PASS | Any failure is a PB blocker |
| Codex/Claude launch, lifecycle, approval | PASS | Any failure is a PB blocker |
| Transcript/output/replay | PASS on accepted live path | New loss or changed failure point blocks |
| Managed shell empty terminal | Known intermittent/pre-existing debt | Same behavior may remain deferred; worse or changed behavior blocks |
| Terminal input authorization/WebView keyboard | FAIL/known pre-existing debt at PA4 | Pre-device Input-A/Input-B acceptance supersedes this waiver: owner input must use exact-generation ACK; member remains explicitly read-only |
| Terminal restart/recovery | Known pre-existing debt, not fully bounded | Reproduce and record; changed or broader failure blocks |
| Generation isolation and cleanup | PASS in automated/live-supported paths | Any regression is a PB blocker |

The baseline-relative exception now applies only to managed-shell empty-terminal
and terminal restart/recovery debt. Terminal-input authorization is removed from
the waiver by the authoritative pre-device remediation plan. No exception
waives pairing, authentication, managed discovery, lifecycle, approval,
output/replay, generation isolation, or cleanup.

HTTPS pairing through the public tunnel is not a PB fix. Existing security
authority defines pairing as LAN-only and excluded from tunnel ingress. A
remote-pairing design requires a separate post-PB threat model and contract.
Android physical-device comparison is mandatory for PB acceptance. iOS
physical-device execution remains a post-PB M-track; TypeScript and iOS
integration fixtures remain mandatory automated gates in PB.6/PB.7.

## 6. Gates

Run root-relative commands and record exact output in each evidence document:

```sh
git status --short
git rev-parse HEAD
git rev-parse @{upstream}
git merge-base --is-ancestor 74560edd88ef5b53c3b3d8d215f3007efbf468a2 HEAD
git merge-base --is-ancestor abe4df1d6485a8eceafde30e7dfc8da06b8c7f06 HEAD
git diff --check

cd companion-daemon
go build ./...
go vet ./...
go test -race ./... -count=1

cd ../mobile
npx tsc --noEmit
npm test -- --runInBand
```

Add focused repeated race tests for every touched lifecycle, recorder,
transport, or registry boundary. Run Android build/lint gates whenever mobile,
wire, or packaging surfaces change. Static gates must cover production, tests,
mobile, scripts, packaging, and current-authority documentation.

## 7. Stop conditions and post-PB order

Stop the current wave when:

- a managed path regains Registry, discovery, screen, or external-log authority;
- a retained PTY/Transcript/generation invariant changes outside the wave;
- a deletion still has a production, test, mobile, script, or package consumer;
- a test is weakened to make deletion pass;
- either frozen prerequisite SHA is missing from ancestry;
- PB.7 lacks the Android device comparison.
- the pre-device QR, Input-A, or Input-B packet lacks independent acceptance;
- regenerated PB.6/PB.7 evidence does not name a distinct production candidate
  and evidence head, or the daemon/APK source identities do not equal the
  production candidate.

The QR renderer/security and Input-A/Input-B packets must complete before the
physical PB gate under the linked authoritative plan. After independent PB
ACCEPT, proceed in this order:

1. remaining empty-terminal/restart debt remediation under its own reviewed
   contract;
2. additional pairing hardening, including any remote-pairing threat model,
   under a separate reviewed contract;
3. Canonical Timeline;
4. Codex/Claude common provider contract;
5. Grok/ACP conformance;
6. Navigator/Guard;
7. durable orchestration and product hardening.
