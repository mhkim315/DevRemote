# STEP 9.0 — Post-PB Feature Ledger

**Status:** DOCUMENTATION-ONLY — zero code changes. Default-off foundation audit complete.

**EVID SHA:** (this commit)
**Date:** 2026-07-23

## 1. Default-Off Foundation Features

Implemented and tested. Every feature is gated behind an explicit CLI flag
that defaults to `false`. No daemon starts these automatically.

### 1a. CLI-Gated (independent flag)

| Step | Feature | Flag | Consumer |
|------|---------|------|----------|
| 4 | Timeline shadow writer | `--enable-timeline-shadow` | Cockpit (polling) |
| 5 | Workspace identity/lease | `--enable-workspace-lease` | NONE (standalone) |
| 7 | Frozen validation StalenessCheck | `--enable-frozen-validation` | NONE (standalone contract-only) |
| 8 | Cockpit projection + ValidationStore | `--enable-cockpit` | Mobile UX (GET) |

### 1b. Embedded Under Another Flag

ValidationStore (submit/ReadAll/ReadRecent/Close) is created by the cockpit
composition path when `--enable-cockpit` is set. It has no standalone CLI flag.

| Step | Feature | Embedded Under | Consumer |
|------|---------|---------------|----------|
| 8 | ValidationStore | `--enable-cockpit` | Cockpit (polling via ReadRecent) |

### 1c. Uncomposed Contract-Only (no flag, no production composition)

| Step | Feature | Notes |
|------|---------|-------|
| 6 | Coordination broker | `internal/coordination` — broker, envelope, types, and tests. Never imported, constructed, or registered from `cmd/` or `term/`. Zero production goroutines or filesystem writes. |

**Verification:** `go test -race ./...` passes for every package. No production
codepath starts any of these without the corresponding CLI flag set.

## 2. Default-Off Production Features (behind CLI flag, implemented but not enabled by default)

These features require an explicit `--enable-*` flag. Without the flag, no
production codepath starts them.

| Feature | Flag | Package | Producer |
|---------|------|---------|----------|
| Managed Codex runtime | `--enable-managed-codex` | `internal/term` | `ManagedCodexService` |
| Managed Claude runtime | `--enable-managed-claude` | `internal/term` | `ManagedClaudeService` |

## 3. Production-Live Features (always enabled, no CLI gate)

These form the core operational surface. They are not flag-gated.

| Feature | Package | Producer | Consumer |
|---------|---------|----------|----------|
| Device trust / pairing | `internal/devicetrust` | `PairingHost` | Mobile app |
| Approval authority | `internal/term` | `AuthoritativeApprovalStore` | Push notifications |
| Terminal transport | `internal/term` | `TerminalTransport` | WebSocket, IPC |
| Input-B delivery | `internal/term` | `handleTerminalInput` | WebSocket clients |
| Transcript engine | `internal/transcript` | `Transcript.Service` | Recorder, timeline |

## 3. Production-Live Foundation (enabled by code structure, not flag-gated)

| Feature | Package | Notes |
|---------|---------|-------|
| Session identity (canonical ID) | `internal/sessionid` | Always active |
| Agent event model (T0) | `internal/agent` | Contract layer |
| Agent adapters (Codex 0.144.1, Claude 2.1.202) | `internal/agent/adapters/` | Telemetry consumers only |
| Agent contract tests | `internal/agent/contract/` | Test harness |
| Doctor (secret scanner) | `internal/agent/doctor/` | Test-only |
| Watcher (file tail) | `internal/watcher` | Always active |

## 4. Step SHAs (implementation + evidence)

| Step | IMPL SHA | EVID SHA | Description |
|------|----------|----------|-------------|
| CT-P0 | `9688cc687` | `9885caf1f` | Source freeze |
| CT-P1 | `317bb0cb7` | `317bb0cb7` (self) | Contract ACCEPT |
| CT-P1 Amend | `4698b19a1` | `12135bd80` | Operational evidence |
| 4 (Shadow) | `6d1a72d35` | `58eb55b92` | Timeline writer |
| 5 (Workspace) | `404a3e882` | `ad83bae10` | Identity/lease |
| 6 (Coordination) | `3b1a2c7ed` (IMPL) | `b7eeba499` (EVID, final: `6c13aafe7`) | Envelope/broker |
| 5 (Workspace) | `404a3e882` (IMPL) | `ad83bae10` (EVID) | Identity/lease (standalone, consumer NONE) |
| 6 (Coordination) | `3b1a2c7ed` (IMPL) | `b7eeba499` (EVID, final `6c13aafe7`) | Envelope/broker (standalone, consumer NONE) |
| 7 (Validation) | `a753e126c` (IMPL) | `814868b5b` (EVID) | StalenessCheck (standalone, consumer NONE) |
| 8 (Cockpit+VStore) | `90cc46c3f`→`44f98dfcb`→`10432920b`→`75be15e91`→`24d6d2d95` | `093e03f04` | Cockpit + embedded ValidationStore |
| PB ACCEPT | `5354077af` | — | Independent |

## 5. Next (9.1-9.3)

- 9.1: Dogfood readiness review. Audit feature-flag off-by-default behavior in production daemon.
- 9.2: Mobile cockpit alpha release. Operational smoke with real session/approval/finding data.
- 9.3: Beta expansion. Enable flags in controlled staging environment, measure overhead.

CT-P2 (Timeline operational wiring) remains blocked until independent plan acceptance.
