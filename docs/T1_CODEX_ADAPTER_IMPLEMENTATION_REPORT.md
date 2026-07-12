# T1 — Codex Version-Specific Adapter — Implementation Report

Status: **READY FOR INDEPENDENT VERIFICATION**

Branch: `feature/phase10-multi-adapter`
Baseline (accepted T0): `3ce2604bd333dcb63142b5b1710185a823162efa` —
`git merge-base --is-ancestor 3ce2604bd333dcb63142b5b1710185a823162efa HEAD` exits 0.

T1 implements a single version-specific Codex adapter behind the accepted T0
contract in an isolated subtree. It does not change the contract, the fixed
harness, the existing agent parser/detector, mobile, lifecycle, auth, Terminal,
or Transcript code.

## 1. Supported version and correlation

**Codex CLI 0.144.1** — confirmed by `codex --version` in R1 evidence
(`docs/R1_RUNTIME_SIGNAL_MATRIX.md`, `docs/r1-fixtures/codex-app-server-0.144.1.json`).

**Source**: session JSONL (`~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`).
Observed discriminators: `session_meta`, `event_msg` (task_started /
waiting_for_approval / approval_resolved), `response_item` (user/assistant),
`turn_context`.

**Correlation**: `managed_launch` — Pokit launched the process; no proven
TUI-session↔log binding beyond launch identity. The R1 app-server JSON-RPC
transport exists but thread/session mapping was not correlated to an ordinary
interactive Codex TUI — it is not attached here.

**Limitations**: `history.jsonl` alone is not authoritative for tool/approval.
Unknown or incompatible shapes return `EventUnknown`/degraded, never a
best-guess confident mapping. A future managed app-server launch mode requires
explicit Pokit-session↔Codex-thread correlation.

## 2. Package layout

```
companion-daemon/internal/agent/adapters/codex/v0_144_1/
    codex_adapter.go       — AgentAdapter implementation (167 lines)
    codex_adapter_test.go  — conformance harness + targeted negative tests (250+ lines)
```

This isolates the version-specific adapter from the Pokit-owned contract
(`internal/agent/contract/`) and from future adapters (T2 Claude, etc.).

## 3. Six-operation mapping

| Operation | Implementation | Key safety rule |
|---|---|---|
| `Descriptor` | Returns `AgentAdapterDescriptor` with name=`codex`, provider=`Codex CLI`, `ContractVersion`, `SupportedVersions=["0.144.1"]`, capabilities=`[Events, Status, ApprovalDetection, IncrementalRead, ProcessDetection, LogDetection]` | declares exactly the supported version from R1 evidence |
| `Detect` | Reuses the existing `CodexDetector` logic: process-name confidence 0.6, CWD `.codex` boost +0.15, log boost +0.1; confidence < 0.5 → `Kind="unknown"` | empty/partial evidence guaranteed unknown + low confidence |
| `DiscoverSessions` | Returns one `DiscoveredSession` with `CorrelationManagedLaunch` when process is `codex`; bounded by `EffectiveDiscoveryLimit`; empty/non-codex returns nil | never invents `proven` ownership or cross-links sessions |
| `ReadEvents` | Bounds: `ValidateCursor`, `BoundBatch` (count + bytes), `AcceptRecord` (per-record), `EffectiveReadLimit` (output). Cursor = compact high-water-mark (last Seq). Dedupe by stable ID + Seq filter against watermark. Session binding from `ReadInput.Session.SessionID`. Degrades on any violation. | all T0 caps enforced; oversized cursor/batch/record triggers degraded, not silent acceptance |
| `NormalizeEvent` | Maps `session_meta` → `EventAgentStarted`, `event_msg` payload type → `EventAgentStarted` / `EventApprovalRequested` / `EventApprovalResolved`, `response_item` role → `EventUserMessage` / `EventAssistantMessage`. Unknown types → `EventUnknown` (confidence 0.3). Stable ID via SHA-256 of raw bytes. Seq from RFC 3339 `timestamp`. `ProvenanceNativeLog`, `SourceJSONL`. Metadata bounded (turn_id, session_id, model_provider, cli_version, resolution). Redacts text via `contract.ContainsSensitive`. | provider-native fields stay inside the adapter; user prompt/input content redacted; approval_requested carries non-empty `ApprovalID` from `approval_id` payload field |
| `DetectApproval` | Uses `contract.SafeApprovalGate` — rejects advisory/unknown/prompt-hint/PTY-structural provenance and < 0.5 confidence. Preserves `ApprovalID`, session, agent kind, source, and confidence binding. | advisory signals never surface an approval; near-miss text mentioning "approve" yields 0 results |
| `GetStatus` | Delegates to `contract.ResolveStatus()` | provenance precedence + advisory-terminal downgrade enforced |

## 4. Fixed harness and conformance evidence

The adapter invokes the accepted external harness:

```go
contract.RunAgentContract(t, "codex", ..., codexFixtures())
```

All 20 harness sub-tests pass with zero skips (18 contract tests + 9 adapter-specific tests).

| Required proof | Harness test | Result |
|---|---|---|
| six operations satisfy stable interface | full `RunAgentContract` compilation + run | PASS |
| deterministic normalization | `Read_DeterministicNormalization` + `DTOSnapshotStable` | PASS (identical JSON across fresh reads) |
| empty detection → unknown | `Detect_EmptyIsUnknown` (harness + adapter) | PASS |
| unknown type ⇒ bounded safe | `Read_MalformedSafe` + `UnsupportedVersion_UnknownShape` | PASS |
| malformed/truncated cannot panic/fabricate | `Read_MalformedSafe` | PASS |
| cursor advance, dedupe, bounds, ordering | `Read_HardBoundDegrades`, `Read_CallerLimitHonored`, `Read_DedupStableID`, `Read_CursorBoundedResumeNoReemit`, `Read_StableSeqOrdering` | PASS |
| session correlation never invents ownership | `Discover_BoundedNoInventedOwnership` + adapter discovery tests | PASS |
| approval positive + adversarial near-miss | `Approval_GenericAdversarial` | PASS (6 generic adversarial + adapter near-miss → 0) |
| status precedence + advisory fallback | `Status_PrecedenceAndAdvisoryPolicy` | PASS |
| diagnostics secret-free | `Diagnostics_SecretFree` + `NoSecretLeak` | PASS |
| failure isolation | `FailureIsolation` | PASS |
| session binding | `Read_SessionIDMustMatch` | PASS |
| oversized record/batch/cursor | `Read_BoundsEnforced_OversizedRecord`, `Read_BoundsEnforced_OversizedBatch`, `Read_BoundsEnforced_InvalidCursor` | PASS |
| DTO snapshot stable | `DTOSnapshotStable` | PASS (agent_started + user_message, provenance, session ID, agent kind all present) |

## 5. Regression preservation

Existing agent-layer tests pass unchanged:

```
internal/agent             — CodexParser_Contract, CodexDetector_Contract, Claude, Antigravity, bridge tests
internal/agent/contract    — fixture adapter conformance, DTO snapshot, secret leak
```

No contract, harness, model, or existing parser/detector file was modified.

## 6. Changed files (all additive)

```
companion-daemon/internal/agent/adapters/codex/v0_144_1/codex_adapter.go       (new)
companion-daemon/internal/agent/adapters/codex/v0_144_1/codex_adapter_test.go  (new)
docs/T1_CODEX_ADAPTER_IMPLEMENTATION_REPORT.md                                  (this report)
```

Zero production, mobile, lifecycle, auth, Terminal, or contract files changed.

## 7. Gates

```
gofmt on changed Go files                     → clean
go build ./...                                → OK
go vet ./...                                  → OK
go test -race ./...                           → OK (all 9 packages + new adapter)
go test -race ./internal/agent/... -count=1   → OK (3 packages, 0 skips)
mobile: npm run typecheck + npm test          → PASS (unchanged)
scripts/build-gate.sh                         → ALL GATES PASSED
git diff --check                              → clean
```

## 8. Scope / limitations (deferred, not defects)

- No Claude adapter (T2).
- No Adapter Doctor/Repair, coding-agent patch generation, or activation (D1).
- No public `/api/sessions` migration or mobile Transcript integration (T3).
- No app-server-managed launch — the JSON-RPC transport is documented but not
  correlated to an ordinary interactive Codex TUI.
- No Recorder, PTY, Terminal, lifecycle, auth, ticket, revoke, audit, mobile,
  Android, or iOS changes.
- No Windows support.
- Correlation is `managed_launch`, not `proven`.
- The existing `internal/agent/codex_adapter.go` (old `CodexParser`/`CodexDetector`/
  `CodexLogResolver`) is intentionally preserved unchanged; convergence is T3.

## 9. Verifier prompt

> Independently verify T1 on `feature/phase10-multi-adapter`. Confirm
> `git merge-base --is-ancestor 3ce2604bd333dcb63142b5b1710185a823162efa HEAD`
> exits 0 and no production, contract, harness, or existing agent file changed.
> Read `internal/agent/adapters/codex/v0_144_1/` and confirm the six-operation
> `AgentAdapter`, the exact `0.144.1` version declaration, `managed_launch`
> correlation, and the 20-pass harness invocation. Confirm `DetectApproval` uses
> `contract.SafeApprovalGate` and `GetStatus` delegates to `contract.ResolveStatus`.
> Verify `waiting_for_approval` events carry non-empty `ApprovalID` from the
> `approval_id` payload field, and `approval_requested` without one degrades to
> `EventUnknown`. Run `sh scripts/build-gate.sh` (ALL GATES PASSED) and
> `go test -race ./internal/agent/... -count=1`. Report a BLOCKER only for: a
> contract/harness edit, a changed production file, a version-specific field
> leaking into a common DTO, an approval from non-authoritative provenance, or
> an unsupported version producing a confident typed event.

## 10. Review request

```text
REVIEW REQUEST: T1 Codex Version-Specific Adapter
baseline: 3ce2604bd333dcb63142b5b1710185a823162efa (accepted T0; is-ancestor exits 0)
tip: feature/phase10-multi-adapter HEAD
scope: T1 only
supported Codex version/shape: Codex CLI 0.144.1 (session JSONL: session_meta, event_msg, response_item)
correlation claims: managed_launch (Pokit launched the process; proven unavailable without app-server thread/session mapping)
gate result: gofmt clean; go build/vet/test -race OK; mobile tsc + jest PASS; scripts/build-gate.sh ALL GATES PASSED; git diff --check clean
deferred: T2, D1, T3, app-server managed launch, product integration
```
