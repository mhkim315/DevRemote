# STEP 9.0 — Post-PB Feature Ledger

**Status:** PENDING INDEPENDENT CLOSEOUT — zero code changes.

**Candidate resolver:** `git log -1 --format=%H -- docs/STEP9_0_LEDGER.md`
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

## 2. Default-Off Managed Runtimes (behind CLI flag)

| Feature | Flag | Package | Producer |
|---------|------|---------|----------|
| Managed Codex runtime | `--enable-managed-codex` | `internal/term` | `ManagedCodexService` |
| Managed Claude runtime | `--enable-managed-claude` | `internal/term` | `ManagedClaudeService` |

## 3. Production-Live (always enabled, no CLI gate)

| Feature | Package | Producer | Consumer |
|---------|---------|----------|----------|
| Device trust / pairing | `internal/devicetrust` | `PairingHost` | Mobile app |
| Approval authority | `internal/term` | `AuthoritativeApprovalStore` | Push notifications |
| Terminal transport | `internal/term` | `TerminalTransport` | WebSocket, IPC |
| Input-B delivery | `internal/term` | `handleTerminalInput` | WebSocket clients |
| Transcript engine | `internal/transcript` | `Transcript.Service` | Recorder |
| Session identity | `internal/sessionid` | Active at startup | All subsystems |
| Agent event model (T0) | `internal/agent` | Contract | Adapters, telemetry |
| Watcher (file tail) | `internal/watcher` | Active at startup | — |

## 4. Fixture / Test-Only (no production caller)

| Feature | Package | Classification |
|---------|---------|---------------|
| Agent adapters (T1 Codex 0.144.1, T2 Claude 2.1.202) | `internal/agent/adapters/` | Fixture-only; no live production caller (per CT-P0 ACCEPT) |
| Agent doctor (secret scanner) | `internal/agent/doctor/` | Test-only; redacted fixtures |

## 5. Step SHAs (implementation + evidence)

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
| 9.1 (Timeline staging) | `dc376f9b7` (IMPL) | `66824e393` (EVID R7) | Operational Timeline staging — fail-open, capability-self-auth, bounded producer composition |
| 9.2 (Projection convergence) | `c82fef47f` (IMPL) | `c8d6eda20` (EVID R3) | Dual-fed equivalence oracle — read-only, default-off, offline projection |
| 9.3 (N1 notifications) | `ce730accc` (IMPL) | `c5cd9722f` (EVID R4) | Exact-event notification-to-action — 7-outcome re-auth, per-device dedup |

## 6. Authoritative sequence

The post-9.0 execution order is defined in [`ALPHA_ACTIVATION_ROADMAP.md`](ALPHA_ACTIVATION_ROADMAP.md).

- **9.0 DOCS/AUDIT — ACCEPTED at `62a50f0a8`.**
- **9.1** — Minimal Timeline staging — **ACCEPTED at `dc376f9b7`.**
- **9.2** — Transcript/Activity projection convergence — **ACCEPTED at `c82fef47f`.**
- **9.3** — N1 exact-event notification-to-action — **ACCEPTED at `ce730accc`.**
- **9.4** — Secure accountless onboarding
- **9.5** — Matched Base Alpha candidate + SM-S926N product gate
- **Beyond** — Manual Alpha coordination, CT-P2, automation (separate authorization)

Historical broad CT-P2 remains blocked. Step 9.4 remains not started until
a separate reviewed implementation contract is accepted.
