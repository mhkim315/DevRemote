# T0 — Common AgentEvent Contract — Implementation Report

Status: **READY FOR INDEPENDENT VERIFICATION**

Branch: `feature/phase10-multi-adapter`
Baseline (accepted M3b): `9cdf2f290299f70d0b6d58e139771bfc336adb57` —
`git merge-base --is-ancestor 9cdf2f290 HEAD` exits 0.

T0 freezes a small, OS-neutral contract that T1 Codex and T2 Claude implement
independently, without changing mobile, lifecycle, authentication, Terminal,
approval, or Transcript behavior. It provides the common types, the six-operation
adapter interface, validation/safe-default behavior, and a fixed external
conformance harness. It does **not** implement Codex or Claude parsing.

## 1. Design decision (additive; no parallel model)

The audit found the daemon already carries TWO event models:

- `internal/agent.AgentEvent` — a clean provider-neutral draft (identity, status,
  event vocabulary, approval, parser/detector interfaces + contract harnesses),
  exercised only by `internal/agent`'s own tests; and
- `internal/models.AgentEvent` — the LEGACY production wire event serialized in
  `SessionTelemetry.Events` (the public `/api/sessions` DTO).

T0 makes the FIRST the frozen contract and **reuses** it (no fork): a new package
`internal/agent/contract/` re-exports those canonical types via aliases (single
source of truth) and ADDS the frozen T0 surface. The legacy wire event and the
term-layer parsers are intentionally left UNCHANGED — converging them onto this
contract would materially change the public DTO and existing agent behavior,
which per the handoff (§8) must not be done silently. That convergence is the
**migration map** below, scheduled for T3, not T0.

### Migration map (current → T0 contract)

| Current | T0 disposition |
|---|---|
| `agent.AgentEvent` / `AgentEventType` / `AgentEventSource` / `AgentStatus` / `AgentIdentity` / `AgentApproval` | **Frozen** — re-exported by `contract` as the canonical model (`contract.go` aliases). |
| `agent.AgentParser` / `ParseResult` (ParseBatch+cursor) | Retained; the T0 six-op `ReadEvents` is the stable superset (bounded, opaque cursor). T1/T2 implement the six-op `AgentAdapter`. |
| `agent.AgentDetector` / `LogResolver` / `DetectionEvidence` | Retained; `Detect`/`DiscoverSessions` in the six-op interface subsume them. |
| `agent.RunParserContract` / `RunDetectorContract` (test-only harnesses in `agent`) | Kept for the existing draft; T0 adds the FIXED `contract.RunAgentContract` (importable, adapter-owned-outside) for the six-op contract. |
| `models.AgentEvent` (legacy wire event in `SessionTelemetry.Events`) | **Unchanged in T0.** T3 maps it onto `contract.AgentEvent` behind a versioned/compat read path; public DTO untouched now. |
| `term.AgentLogParser` / `term.AgentDetector` / `term.evaluateState` (production telemetry path) | **Unchanged in T0.** T3 migration; today they keep producing `models.AgentEvent`. |

No production file was modified. `GET /api/sessions` bytes are identical.

## 2. Frozen contract surface (`internal/agent/contract/`)

### Six operations (`contract.AgentAdapter`, `contract.go`)

| Operation | Signature (abridged) | Safety rule |
|---|---|---|
| `Detect` | `(ctx, SessionContext) (AgentIdentity, error)` | confidence < 0.5 ⇒ Kind `unknown`; no confident false positive |
| `DiscoverSessions` | `(ctx, DiscoveryInput) ([]DiscoveredSession, error)` | never invents terminal ownership / cross-links; unproven ⇒ `CorrelationUnavailable`; bounded by `MaxDiscoveredSessions` |
| `ReadEvents` | `(ctx, ReadInput) (ReadResult, error)` | bounded (`MaxEventsPerRead`), opaque `Cursor`, dedupe, stable order, truncation ⇒ degraded |
| `NormalizeEvent` | `(ctx, RawRecord) (AgentEvent, DegradedInfo)` | no provider-native leak; unknown/malformed ⇒ `EventUnknown`/degraded; never panics/fabricates |
| `DetectApproval` | `(ctx, []AgentEvent) ([]AgentApproval, error)` | default NO approval; positive only from unambiguous `approval_requested` ≥ floor |
| `GetStatus` | `(ctx, StatusInput) (StatusResult, error)` | provenance precedence; never derives/asserts process lifecycle |
| `Descriptor` | `() AgentAdapterDescriptor` | name/provider/`ContractVersion`/supported versions/capabilities (for Adapter Doctor) |

### New evidence tiers (from R1 signal matrix)

- `Provenance` (runtime > provider_protocol > provider_hook > native_log >
  pty_structural > heuristic > prompt_hint > unknown) + `ProvenanceRank`/`Stronger`/
  `Advisory`; advisory tiers can never establish authority.
- `ConfidenceLevel` (authoritative/high/medium/low) via `ConfidenceLevelFor` (clamped).
- `Correlation` (proven/managed_launch/unavailable).

### Validation & safe defaults (`validate.go`, `cursor.go`)

`ValidateEvent`, `SafeEvent` (coerce to unknown), `DedupeEvents`, `BoundEvents`,
`ResolveStatus` (precedence), `SafeApprovalGate` (`ApprovalConfidenceFloor=0.5`),
`DegradedInfo`/`Degrade` (bounded, sanitized), `SanitizeDiagnostic`/`ContainsSensitive`
(strip common credential prefixes and absolute home paths, length-bounded),
read/discovery bound helpers.

### Fixed conformance harness (`harness.go` — importable, non-test)

`RunAgentContract(t, name, factory, ConformanceFixtures)` in the `contract`
package (outside any adapter's write area). Proves: descriptor, detect invariants,
discovery-never-invents-ownership, deterministic normalization, validate+closed
vocabulary, malformed-safe, read bounds, dedupe+cursor resume, ordering,
normalize-no-leak/no-panic, approval positive + adversarial near-miss, status
precedence + fallback, secret-free diagnostics.

## 3. Changed files (all additive)

```
companion-daemon/internal/agent/contract/contract.go          (types + six-op interface)
companion-daemon/internal/agent/contract/cursor.go            (opaque cursor + bounds)
companion-daemon/internal/agent/contract/validate.go          (validators + safe defaults + redaction)
companion-daemon/internal/agent/contract/harness.go           (FIXED conformance harness)
companion-daemon/internal/agent/contract/validate_test.go     (pure-function unit tests)
companion-daemon/internal/agent/contract/fixture_adapter_test.go (synthetic adapter + harness run + isolation/bounds/snapshot/secret tests)
docs/T0_COMMON_AGENT_EVENT_IMPLEMENTATION_REPORT.md            (this report)
docs/ROADMAP_AFTER_E10B.md                                    (T0 tick)
```

No existing production file changed. No mobile change. No Recorder/PTY/Terminal/
auth change.

## 4. Evidence matrix (fixed harness + unit tests)

| Required proof (handoff §6) | Evidence |
|---|---|
| six operations satisfy the stable interface | `fixtureAdapter` implements `AgentAdapter`; `RunAgentContract` compiles+runs it |
| deterministic normalization | `Read_DeterministicNormalization` (two fresh adapters ⇒ identical JSON); `DTOSnapshotStable` |
| unknown type/fields ⇒ bounded safe | `Read_MalformedSafe`, `SafeEvent`/`ValidateEvent` unit tests |
| malformed/truncated cannot panic/fabricate | `Read_MalformedSafe` + `guard` recover; `Normalize_NoLeakNoPanic` |
| cursor advance, dedupe, bounds, ordering | `Read_DedupAndCursor`, `Read_Bounds`, `ReadBoundTruncatesAndDegrades`, `Read_Ordering` |
| session-correlation +/- | `Discover_NeverInventsOwnership` (proven vs unavailable) |
| approval positive + adversarial near-miss | `Approval_PositiveAndNearMiss` (message mentioning "approve" ⇒ 0), `SafeApprovalGate` |
| status precedence + unknown/degraded fallback | `Status_PrecedenceAndFallback`, `ResolveStatus` unit tests |
| diagnostics secret-free | `Diagnostics_SecretFree`, `NoSecretLeak`, `DiagnosticSanitization` |
| failing adapter cannot affect another / Live Terminal | `FailureIsolation` |
| common DTO snapshots stable | `DTOSnapshotStable` (exact JSON golden) |

## 5. Gates (run here)

```
gofmt -l internal/agent/contract           → clean
go build ./...                             → OK
go vet ./...                               → OK
go test -race ./...                        → OK (incl. internal/agent/contract)
mobile: npm run typecheck && npm test      → PASS (21 suites / 289 tests, unchanged)
scripts/build-gate.sh                      → ALL GATES PASSED (incl. secret scan)
git diff --check                           → clean
```

Clean-checkout note: the mobile gate needs the documented Expo Android prebuild
when `mobile/android` is only partially tracked (see M3B acceptance §). No
physical-device gate is claimed.

## 6. Scope / limitations (deferred, not defects)

- No Codex (T1) / Claude (T2) parsing; no reverse-engineered provider fixtures —
  T0 fixtures are synthetic.
- Legacy `models.AgentEvent` wire event and term-layer parsers unchanged;
  convergence is T3 (migration map §1).
- No Adapter Doctor/Repair (D1), Transcript (T3), status UI (S1), approval UI
  (A1), orchestration (O1), Windows, or iOS/Android auth work.
- `contract` reuses `agent`'s model; a future step may relocate the model into
  `contract` and alias back, but that is not required for the frozen contract.

## 7. Verifier prompt

> Independently verify T0 on `feature/phase10-multi-adapter`. Confirm
> `git merge-base --is-ancestor 9cdf2f290 HEAD` exits 0 and no production file
> (term/telemetry/app.go/mobile/Recorder) changed — `GET /api/sessions` bytes are
> unchanged. Read `internal/agent/contract/` and confirm the six-operation
> `AgentAdapter`, opaque bounded `Cursor`, provenance/confidence/correlation tiers,
> validators/safe-defaults, and the FIXED `RunAgentContract` harness live outside
> any adapter's write area. Confirm the harness proves malformed-no-panic,
> bounds/dedupe/cursor, discovery-never-invents-ownership, approval near-miss
> negatives, status precedence, secret-free diagnostics, failure isolation, and a
> stable DTO snapshot. Run `sh scripts/build-gate.sh` (ALL GATES PASSED) and
> `go test -race ./...`. Report a BLOCKER only for: a parallel model with no
> migration map, a production-DTO/behavior change, an operation that can panic or
> fabricate a confident event on malformed input, an approval emitted from
> low-confidence/near-miss evidence, agent status becoming lifecycle authority, or
> the harness being editable from an adapter's own package.

## 8. Review request

```text
REVIEW REQUEST: T0 Common AgentEvent Contract
baseline: 9cdf2f290299f70d0b6d58e139771bfc336adb57 (accepted M3b; is-ancestor exits 0)
tip: feature/phase10-multi-adapter HEAD — the single commit that adds internal/agent/contract/ and this report (git rev-parse HEAD after push)
scope: T0 only (frozen contract + six-op interface + validators + fixed harness; no T1/T2 parsing)
gate result: gofmt clean; go build/vet/test -race OK; mobile tsc + jest 21/289 PASS; scripts/build-gate.sh ALL GATES PASSED; git diff --check clean
known deferred items: legacy models.AgentEvent + term parsers convergence (T3); model relocation into contract (optional follow-up); T1 Codex; T2 Claude; D1; physical-device gate
```
