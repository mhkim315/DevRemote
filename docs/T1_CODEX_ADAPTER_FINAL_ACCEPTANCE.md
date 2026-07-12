# T1 Codex Version-Specific Adapter — Final Acceptance

Status: **ACCEPT**

Accepted implementation tip:

```text
162266f830caaf07bf701d9a1294557432855769
```

Accepted contract baseline:

```text
3ce2604bd333dcb63142b5b1710185a823162efa  T0 Common AgentEvent Contract ACCEPT
```

Branch: `feature/phase10-multi-adapter`

## Accepted support boundary

- provider: Codex CLI;
- exact version/shape: `0.144.1` session JSONL;
- source: ordered full-prefix `~/.codex/sessions/.../rollout-*.jsonl`
  records supplied to the adapter;
- correlation: unavailable for ordinary independently launched TUI sessions;
- discovery: no invented `proven` or `managed_launch` result;
- app-server: retained as R1 evidence only, without speculative TUI-thread
  correlation;
- production integration: not part of T1.

The accepted descriptor advertises events, status, approval detection,
incremental read, and process detection. It does not advertise unimplemented log
discovery or managed-launch correlation.

## Accepted contract behavior

The version-specific adapter implements the frozen T0 operations:

```text
Descriptor
Detect
DiscoverSessions
ReadEvents
NormalizeEvent
DetectApproval
GetStatus
```

Independent verification confirmed:

- exact `0.144.1` version gating and persistent fail-closed behavior for missing,
  malformed, conflicting, or later `session_meta` records;
- exact Pokit session binding without fabricated provider-session ownership;
- compact `<position>:<anchor>` cursor with strict syntax, position/anchor
  validation, deterministic encoding, and bounded size;
- source-position event IDs and absolute-position `Seq` values stable across
  one-shot and paginated reads;
- page-size-independent `(ID, Seq, Type)` results;
- bounded adjacent replay suppression and explicit position semantics for
  non-adjacent equal bytes;
- suffix-only record-count and byte bounds without full-prefix parsing or
  unbounded historical ID storage;
- oversized records rejected before hashing or JSON parsing, while neighboring
  valid positions retain correct identity, ordering, and cursor behavior;
- bounded metadata and diagnostics with prompt, command, path, and credential
  leakage prevented;
- approval request/resolution bound by a stable non-empty `ApprovalID` and T0
  authoritative provenance rules;
- generic approval near-misses and advisory provenance produce no approval;
- status uses T0 precedence and advisory-terminal downgrade and never becomes
  daemon process lifecycle authority;
- malformed, unknown, truncated, replayed, and failing-adapter paths return safe
  unknown/degraded results without panic or Terminal impact.

## Remediation evidence

The final result was independently reviewed through focused remediation of:

- missing exact version enforcement;
- invented session correlation;
- timestamp collisions and unstable ordering;
- discarded degradation reasons;
- unbound approval resolution;
- hash-watermark event loss;
- unbounded seen-ID cursors;
- cursor tamper, rotation, and version-authority forgery;
- cross-page duplicate and page-size inconsistencies;
- conflicting-version recovery;
- full-prefix resource-bound bypasses;
- absolute-position Seq drift;
- mixed oversized-record position misalignment;
- hashing rejected oversized payloads;
- final `gofmt` gate drift.

The accepted design does not change the frozen T0 contract or fixed harness.

## Independently reproduced gates

```text
gofmt -l changed adapter files                         clean
go build ./...                                         PASS
go vet ./...                                           PASS
go test -race ./internal/agent/... -count=1            PASS
go test -race ./...                                    PASS
mobile TypeScript typecheck                            PASS
mobile Jest                                            PASS
Android module Kotlin compile                          PASS
git diff --check                                       PASS
vendor/ID inference/secret scans                       PASS
sh scripts/build-gate.sh                               ALL GATES PASSED
```

Local HEAD and `origin/feature/phase10-multi-adapter` both resolved to the
accepted tip and the final worktree was clean.

## Deferred and unchanged

- T2 Claude implementation;
- R2 multi-agent expansion research;
- D1 Adapter Doctor/Repair;
- T3 Transcript integration;
- S1 status product surface, A1 approval product surface, and O1 orchestrator;
- app-server managed launch and production adapter registration;
- Terminal, Recorder, PTY, authentication, lifecycle, mobile DTO, and public
  wire changes;
- Windows runtime and Windows-specific abstractions.

## Next phase

Proceed only to T2 using:

```text
docs/NEXT_SESSION_T2_CLAUDE_ADAPTER_HANDOFF.md
```

R2 remains research-only and cannot begin until T2 also receives an independent
ACCEPT decision.
