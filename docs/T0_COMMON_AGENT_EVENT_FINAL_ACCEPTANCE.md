# T0 Common AgentEvent Contract — Final Acceptance

Status: **ACCEPT**

Accepted implementation tip:

```text
3ce2604bd333dcb63142b5b1710185a823162efa
```

Accepted baseline ancestry:

```text
9cdf2f290299f70d0b6d58e139771bfc336adb57  M3b ACCEPT
```

Branch: `feature/phase10-multi-adapter`

## Frozen contract

Pokit owns the provider-neutral operations:

```text
detect
discoverSessions
readEvents
normalizeEvent
detectApproval
getStatus
```

The accepted contract includes:

- canonical AgentEvent identity, exact Pokit session binding, stable per-session
  `Seq`, timestamp, mechanical source, evidence provenance, confidence, closed
  event vocabulary, and bounded metadata;
- opaque bounded cursor, per-record and per-batch count/byte limits, caller event
  limits, stable deduplication, and explicit degraded truncation;
- discovery correlation tiers that cannot invent managed process ownership;
- approval events bound through non-empty `ApprovalID`, with positive approval
  limited to runtime/provider protocol/provider hook/native-log provenance;
- advisory provenance unable to assert terminal agent status; strong evidence
  precedence remains distinct from daemon process lifecycle;
- typed bounded diagnostics and safe unknown/degraded defaults;
- an importable fixed conformance harness outside version-specific adapter write
  areas, with required fixtures and generic adversarial cases.

The existing production `models.AgentEvent`, `/api/sessions` DTO, Recorder, PTY,
Terminal, mobile, authentication, lifecycle, and approval behavior were not
changed by T0. Their migration/integration belongs to T3 or later phases.

## Independent evidence

The final verification inspected the contract and harness across four remediation
rounds. It confirmed the final code closes:

- provenance and stable ordering loss;
- advisory/unknown approval false positives;
- cursor, record, batch count/byte, metadata, and caller-limit bypasses;
- empty/no-op fixture and detector bypasses;
- cross-session event/approval binding;
- advisory terminal-status authority;
- unbound approval events;
- malformed/unknown panic or fabrication paths.

Reproduced final evidence:

```text
go test -race ./internal/agent/contract     PASS, no skips
go build ./...                              PASS
go vet ./...                                PASS
go test -race ./...                         PASS
mobile TypeScript + Jest 21/289             PASS
clean Expo Android prebuild                 PASS
Android module Kotlin compile               PASS
invariant + secret scans                    PASS
scripts/build-gate.sh                       ALL GATES PASSED
```

The current synthetic `SizedRecord` factory returns equal IDs for equal sizes.
This slightly reduces the processed-count diagnostic strength of that one test,
but each record is individually below `MaxRecordBytes`, the raw input total
exceeds `MaxBatchBytes`, at least one event must be produced, and degraded
truncation is mandatory. It does not leave a contract bypass and may be made
distinct as a non-blocking harness-quality follow-up.

## Next phase

Proceed only to T1 Codex Adapter using:

```text
docs/NEXT_SESSION_T1_CODEX_ADAPTER_HANDOFF.md
```

Do not begin T2 Claude, D1 Adapter Doctor/Repair, T3 Transcript integration, S1,
A1, O1, Windows work, or unrelated mobile/auth changes during T1.
