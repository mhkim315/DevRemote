# STEP 9.0 — Post-PB Feature Ledger

**Status:** DOCUMENTATION-ONLY — zero code changes. Default-off foundation audit complete.

**EVID SHA:** (this commit)
**Date:** 2026-07-23

## 1. Default-Off Foundation Features (implemented, no production producer)

These features are coded and tested. Every one is gated behind a CLI flag
that defaults to `false`. No production code path enables them; no daemon
starts them automatically.

| Step | Feature | Flag | Default | Producer | Consumer | Status |
|------|---------|------|---------|----------|----------|--------|
| 4 | Timeline shadow writer | `--enable-timeline-shadow` | `false` | None | Cockpit (polling) | IMPLEMENTED |
| 5 | Workspace identity/lease | `--enable-workspace-lease` | `false` | None | Coordination (identity) | IMPLEMENTED |
| 6 | Coordination broker | (embedded in workspace) | `false` | None | None (broker-only) | IMPLEMENTED |
| 7 | Frozen validation store | (embedded in cockpit) | `false` | None | Cockpit (polling) | IMPLEMENTED |
| 8 | Cockpit projection | `--enable-cockpit` | `false` | None | Mobile UX (GET) | IMPLEMENTED |

**Verification:** `go test -race ./...` passes for every package. No production
codepath starts any of these without the corresponding CLI flag set.

## 2. Production-Live Features (default-on, active producers/consumers)

These features are always enabled in production. They have no CLI gate and
form the core operational surface.

| Feature | Package | Producer | Consumer |
|---------|---------|----------|----------|
| Managed Codex runtime | `internal/term` | `ManagedCodexService` | REST API, IPC |
| Managed Claude runtime | `internal/term` | `ManagedClaudeService` | Approval system |
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
| CT-P1 Amend | `12135bd80` | `12135bd80` (self) | Operational evidence |
| 4 (Shadow) | `6d1a72d35` | `58eb55b92` | Timeline writer |
| 5 (Workspace) | `404a3e88` | `ad83bae10` | Identity/lease |
| 6 (Coordination) | `6c13aafe7` | `6c13aafe7` (self) | Envelope/broker |
| 7 (Validation) | `e1cbe7211`→`093e03f04` | `814868b5b` | Validation store |
| 8 (Cockpit) | `75be15e91`→`093e03f04` | `093e03f04` | Mobile projection |
| PB ACCEPT | `5354077af...` | — | Independent |

## 5. Next (9.1-9.3)

- 9.1: Dogfood readiness review. Audit feature-flag off-by-default behavior in production daemon.
- 9.2: Mobile cockpit alpha release. Operational smoke with real session/approval/finding data.
- 9.3: Beta expansion. Enable flags in controlled staging environment, measure overhead.

CT-P2 (Timeline operational wiring) remains blocked until independent plan acceptance.
