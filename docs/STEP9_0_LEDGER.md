# STEP 9.0 — Post-PB Feature Ledger

**Status:** COMPLETE — default-off foundation audit accepted. Zero code changes.

**EVID SHA:** (this commit)
**Date:** 2026-07-23

## 1. Default-Off Foundation Features

Implemented and tested. Foundation features are gated behind explicit CLI flags
that default to `false`, with two exceptions noted below. No daemon starts
these automatically.

### 1a. CLI-Gated (independent flag)

| Step | Feature | Flag | Consumer |
|------|---------|------|----------|
| 4 | Timeline shadow writer | `--enable-timeline-shadow` | Cockpit (polling via ReadRecent) |
| 5 | Workspace identity/lease | `--enable-workspace-lease` | NONE (standalone) |
| 7 | Frozen validation StalenessCheck | `--enable-frozen-validation` | NONE (standalone contract-only) |
| 8 | Cockpit projection | `--enable-cockpit` | Mobile UX (GET /api/cockpit) |

### 1b. Embedded Under Another Flag

| Step | Feature | Embedded Under | Consumer |
|------|---------|---------------|----------|
| 8 | ValidationStore | `--enable-cockpit` | Cockpit (polling via ReadRecent) |

### 1c. Uncomposed Contract-Only (no flag, no production composition)

| Step | Feature | Notes |
|------|---------|-------|
| 6 | Coordination broker | `internal/coordination` — broker, envelope, types, and tests. Never imported, constructed, or registered from `cmd/` or `term/`. Zero production goroutines or filesystem writes. |

## 3. Production-Live (always enabled, no CLI gate)

| Feature | Package | Producer | Consumer |
|---------|---------|----------|----------|
| Device trust / pairing | `internal/devicetrust` | `PairingHost` | Mobile app |
| Approval authority | `internal/term` | `AuthoritativeApprovalStore` | Push notifications |
| Terminal transport | `internal/term` | `TerminalTransport` | WebSocket, IPC |
| Input-B delivery | `internal/term` | `handleTerminalInput` | WebSocket clients |
| Transcript engine | `internal/transcript` | `Transcript.Service` | Recorder, timeline |
| Session identity | `internal/sessionid` | Active at startup | All subsystems |
| Agent event model (T0) | `internal/agent` | Contract | Adapters, telemetry |
| Agent adapters (T1/T2) | `internal/agent/adapters/` | Fixture-only; no live production caller (per CT-P0) | Test harness |
| Watcher (file tail) | `internal/watcher` | Active at startup | — |

## 4. Step SHAs (implementation + evidence)

| Step | IMPL SHA | EVID SHA | Description |
|------|----------|----------|-------------|
| CT-P0 | `7b7f23d0a` (freeze IMPL) | `d4b4d99ba` (ACCEPT EVID) | Source freeze |
| CT-P1 | `680a78f69` (envelope IMPL) | `317bb0cb7` (ACCEPT EVID) | Timeline contract |
| CT-P1 Amend | `4698b19a1` | `12135bd80` | Operational evidence |
| 4 (Shadow) | `6d1a72d35` | `58eb55b92` | Timeline writer |
| 5 (Workspace) | `404a3e882` (IMPL) | `ad83bae10` (EVID) | Identity/lease (standalone, consumer NONE) |
| 6 (Coordination) | `3b1a2c7ed` (IMPL) | `b7eeba499` (EVID, final `6c13aafe7`) | Envelope/broker (standalone, consumer NONE) |
| 7 (Validation) | `a753e126c` (IMPL) | `814868b5b` (EVID) | StalenessCheck (standalone, consumer NONE) |
| 8 (Cockpit+VStore) | `90cc46c3f`→`44f98dfcb`→`10432920b`→`75be15e91`→`24d6d2d95` | `093e03f04` | Cockpit + embedded ValidationStore |
| PB ACCEPT | `5354077af` | — | Independent |

## 5. Authoritative sequence

- **9.0 DOCS/AUDIT — COMPLETE** at this commit.
- **9.1 PRODUCER ACTIVATION** — staging; enable flags, fail-open shadow writes, generate
  real Timeline events in controlled environment. Measure overhead.
- **9.2 OPERATIONAL SMOKE** — SM-S926N physical device; end-to-end validation with
  live session/approval/finding data flowing through the cockpit.
- **9.3 BETA** — controlled enablement in broader staging; dogfood before policy
  automation.

CT-P2 (Timeline operational wiring) remains blocked until independent plan acceptance.
