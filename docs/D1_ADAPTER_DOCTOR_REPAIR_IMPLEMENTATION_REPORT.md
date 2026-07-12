# REVIEW REQUEST: D1 Adapter Doctor/Repair

Status: **IMPLEMENTED — ready for independent verification**

Branch: `feature/phase10-multi-adapter`

## Baseline ancestry

- T0 Common AgentEvent: `3ce2604bd333dcb63142b5b1710185a823162efa` (ACCEPT)
- T1 Codex adapter: `162266f830caaf07bf701d9a1294557432855769` (ACCEPT)
- T2 Claude adapter: `ef4a162c7` (implemented, verified)
- R2 Multi-agent research: `26722c5f1` (complete)

## Implementation SHA

`886be4cc4` (subject to rebase on push)

## Files changed

```
companion-daemon/internal/agent/doctor/
├── doctor.go          # Core types, Doctor (stateless), unchanged from v1
├── compat.go          # Pure compatibility check (unchanged)
├── sandbox.go         # REWRITTEN: PatchFile parser, HardenedPath, containment
├── evidence.go        # FIXED: real records, AcceptRecord bounds
├── suite.go           # REWRITTEN: FixedSuite owns commands, skip=fail
├── approval.go        # NEW: ReviewBundle, ApprovalStore, digest-bound state machine
├── orchestrator.go    # NEW: Orchestrator, RepairRunner interface, full workflow
├── doctor_test.go     # REWRITTEN: 42 tests covering all blockers
```

## Blocker-by-blocker evidence

### BLOCKER 1 — Production Orchestrator
- `orchestrator.go`: Orchestrator with explicit state machine (idle→detected→...→awaiting_approval)
- `RepairRunner` interface with `StubRepairRunner` for testing
- Doctor owns: provider allowlists, immutable baselines, allowed subtrees, workspace root, process list, network policy, timeout
- E2E test: `TestOrchestrator_CompleteSuccessfulWorkflow` (all states exercised)

### BLOCKER 2 — Real Sandbox
- `sandbox.go`: `PatchFile` parser for unified diffs (`diff --git`, `---`, `+++`, mode changes, binary markers)
- `HardenedPath`: rejects absolute, parent-relative, symlink escape paths
- Rejects: binary patches, oversized patches (>1MiB), too many files (>32), executable mode changes
- Immutable security boundaries checked BEFORE allowlist (deny list → accepted adapters → T0 contract → allowlist)
- 10 sandbox tests: symlink escape, parent-relative, binary, oversized, cross-adapter, accepted-adapter, T0-contract, delete, deny list

### BLOCKER 3 — Digest-Bound Approval
- `approval.go`: `ReviewBundle` with RequestID, BaselineSHA, EvidenceDigest, PatchDigest, ChangedFile manifest, SuiteCommandManifest, SuiteResultDigest, 24h expiry
- `ApprovalStore` with replay protection (request ID used once)
- `Approve` validates digest match; `Reject` transitions to rejected; `Activate` fail-closed (ErrActivationUnsupported)
- `ActivateWithResult` for caller-managed activation; `Rollback` active→rolled_back
- `approved_but_activation_unsupported` fail-closed state when safe activation unavailable
- 9 approval tests: submit/approve, digest mismatch, replay, expired, no-activation-without-approval, reject, rollback, activation-failure, unsupported-activation

### BLOCKER 4 — Non-Bypassable Fixed Suite
- `suite.go`: `FixedSuite.Commands()` owns T0 contract, T1 Codex, T2 Claude, race, build, vet commands
- Caller cannot inject pkgPath — `SuiteRunner.Run(candidatePkgPath)` only appends candidate, doesn't replace
- `runCommand`: skip→failure, zero tests→failure, decode error→failure, cmd.Wait error→failure, timeout→failure, MinTests not met→failure
- `TestFixedSuite_HasRequiredCommands`: verifies all required labels present

### BLOCKER 5 — Evidence Fix
- `evidence.go`: `CollectDrift` passes `records` to collection methods (was nil)
- `contract.AcceptRecord` check before `json.Unmarshal` in all collection loops
- `maxEvidenceRecords` (5000) and `maxEvidenceBytes` (8MiB) bounds enforced
- `TestCollectDrift_UsesRealRecords`: proves real samples reach the collector
- `TestCollectDrift_AcceptRecordBounds`: proves oversized records are skipped

### BLOCKER 6 — REVIEW REQUEST
- This document. Literal heading present. ✅

## Test results

```
42 tests pass (internal/agent/doctor)
All 12 packages pass (full go test -race ./...)
go build ./... : OK
go vet ./...   : OK
git diff --check : OK
Secret scan    : OK (no new leaks)
```

## Remaining unknowns

- `SuiteRunner.Run` invokes real `go test` subprocess; in CI environments this requires Go toolchain.
- `Activate` returns `ErrActivationUnsupported` (fail-closed) — full filesystem activation requires OS-level staging/atomic-switch which is gated on the caller's environment.
- `RepairRunner` is an interface; production implementation (invoking a real coding agent) is a separate integration step.
- `HardenedPath` does not call `os.Lstat` on each component (filesystem traversal) — symlink detection is pattern-based in D1 scope.

## No changes to

- T0 contract (`internal/agent/contract/`)
- T1 Codex adapter (`internal/agent/adapters/codex/v0_144_1/`)
- T2 Claude adapter (`internal/agent/adapters/claude/v2_1_202/`)
- Terminal, Recorder, auth, lifecycle, mobile, Windows
- `internal/agent/models.go`, `bridge.go`, `detector.go`, `parser.go`

---

REVIEW REQUEST: D1 Adapter Doctor/Repair
Completion SHA: (see push output below)
