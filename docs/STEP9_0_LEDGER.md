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
| 6 | Coordination broker | (standalone `internal/coordination`) | `false` | None | None (broker-only) | IMPLEMENTED |
| 7 | Frozen validation store | (embedded in cockpit) | `false` | None | Cockpit (polling) | IMPLEMENTED |
| 8 | Cockpit projection | `--enable-cockpit` | `false` | None | Mobile UX (GET) | IMPLEMENTED |

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
| 6 (Coordination) | `3b1a2c7ed` | `b7eeba499` | Envelope/broker (comment fixes `0dac78ffa`→`6c13aafe7`) |
| 7 (Validation) | `a753e126c` | `814868b5b` | Validation store |
| 8 (Cockpit) | `f69d9eb1a` | `093e03f04` | Mobile projection |
| PB ACCEPT | `5354077af` | — | Independent |

## 5. Next (9.1-9.3)

- 9.1: Dogfood readiness review. Audit feature-flag off-by-default behavior in production daemon.
- 9.2: Mobile cockpit alpha release. Operational smoke with real session/approval/finding data.
- 9.3: Beta expansion. Enable flags in controlled staging environment, measure overhead.

CT-P2 (Timeline operational wiring) remains blocked until independent plan acceptance.
