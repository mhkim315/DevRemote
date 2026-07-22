# STEP6 T1 Evidence — Coordination Protocol Fixes

**IMPL SHA:** `3b1a2c7ed`
**EVID SHA:** (this commit)
**Branch:** `feature/canonical-timeline-foundation`
**Date:** 2026-07-23

## Blocker 1 — Behavioral Bounds Tests

| Test | Coverage | Status |
|------|----------|--------|
| `TestHandoff_OversizedRefsRejected` | HandoffReference, EvidenceReference, ReplyToID, CausationID > 512 | ✅ PASS |
| `TestHandoff_OversizedListCountsRejected` | ChangedFiles, TestCommands, TestResults, UnresolvedFindings > 64 | ✅ PASS |
| `TestHandoff_OversizedPerItemSizeRejected` | Per-item > 512 bytes for all 4 lists | ✅ PASS |
| `TestEnvelope_ClosedVariantsAccepted` | Question, Finding, RevisionRequest accepted | ✅ PASS |
| `TestEnvelope_UnknownTypeRejected` | Unknown type → ErrInvalid | ✅ PASS |
| `TestEnvelope_RefBoundsAtNMinusOneNAndNPlusOne` | HandoffReference, EvidenceReference N-1/N/N+1 | ✅ PASS |

## Blocker 2 — Authorization Enforcement

`CapabilityChecker` interface + `Broker.SetCapabilityChecker(auth)`. `Enqueue`
checks `auth.HasCapability(target, RequiredTargetCapability)` before accepting.
Nil CapabilityChecker → fail-closed (all envelopes rejected, must be explicitly
configured before use).

| Test | Scenario | Status |
|------|----------|--------|
| `TestBroker_AuthorizationEnforced` | Authorized target → accepted | ✅ PASS |
| | Target lacks capability → ErrInvalid | ✅ PASS |
| | Target not in allow map → ErrInvalid | ✅ PASS |
| | Broker without auth hook → fail-closed (ErrInvalid) | ✅ PASS |

## Blocker 3 — Handoff Source Bindings

Added to `Handoff` struct: `SourceProvider`, `SourceRuntimeID`, `SourceSessionID`,
`SourceGeneration`, `DiffDigest`. Validated in `Handoff.Validate()`.

| Test | Coverage | Status |
|------|----------|--------|
| `TestHandoff_SourceBindingsRequired` | All 5 fields required (empty/negative → ErrInvalid) | ✅ PASS |

## Gate Results

```
go build ./...       PASS
go vet ./...         PASS
gofmt -l .           CLEAN
go test -race ./...  ALL PASS (18 packages)
```
