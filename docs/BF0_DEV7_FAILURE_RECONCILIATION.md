# BF-0 — DEV7 Failure Reconciliation

**Status:** READ-ONLY TRIAGE — NO PRODUCTION CHANGES
**Parent:** DS-DEV7 physical test report (`d91152d3c`)
**Date:** 2026-07-25

> **ART6 CANDIDATE REJECTED. DS-AUD8 PAUSED.** This document records the
> evidence chain, classifies every DEV7 failure, identifies shared root causes,
> and proposes bounded write scopes for remediation waves BF-1A, BF-1B,
> BF-2A, and BF-2B. No production changes are made in this wave.

---

## 1. Authority Chain

### 1.1 Frozen Identities

| Artifact | SHA |
|----------|-----|
| **Source (STAB5 freeze)** | `3ded6f6ff183bafae7b42303590d8bf984230c8c` |
| **Daemon (DEV7 report)** | `b6c0a36e0ea1d92909a98aa72d1b21590bcb0713015b7c7d354a2760bfc0cb65` |
| **APK (DEV7 report)** | `a875538d0b45743c6b44b2b964398efb422de52b1f9a421c36e224d654b77f45` |

### 1.2 ARTIFACT MISMATCH — INVALIDATES ART6

| Source | Daemon SHA-256 |
|--------|---------------|
| `docs/DS_ART6_MANIFEST.md` (frozen at `50cc05cb9`) | `0c9d0418bcc1b771f39a6ba701f4e47513137739a58b2d8c64aee0a3e1685dc1` |
| `docs/DS_DEV7_PHYSICAL_TEST_REPORT.md` (at `d91152d3c`) | `b6c0a36e0ea1d92909a98aa72d1b21590bcb0713015b7c7d354a2760bfc0cb65` |

**The DEV7 physical test used a daemon binary (`b6c0a36e`) that does NOT match
the ART6 frozen manifest (`0c9d0418`).** The ART6 candidate is invalid. The
evidence chain is broken — there is no proof that the tested daemon was built
from the frozen source `3ded6f6f`.

### 1.3 Stale On-Disk Manifest

`pokit-alpha-device-artifacts/MANIFEST.json` still records the R0-era
`1bdf6b632` identity. It was never updated for either ART6 or DEV7.

### 1.4 Jest Non-Determinism Persists

The ART6 manifest reports `550/552 (2 flaky in pairingStore)` despite
DS-STAB5 (`3ded6f6f`) claiming a fix via global `async-storage` mock.
Either:
- The STAB5 fix did not survive the ART6 build environment, OR
- The ART6 manifest was written before the STAB5 fix was verified

The pairingStore order-dependence (R0 §2.5) may still be present in the
frozen source. See bug classification §2.10.

### 1.5 Missing APK Identity in ART6 Manifest

The ART6 manifest (`DS_ART6_MANIFEST.md`) records the daemon SHA-256 and the
mobile build metadata (Expo SDK 56, TypeScript 5.8), but does NOT record the
APK SHA-256. The DEV7 report is the first document to record an APK hash
for this candidate. The ART6 freeze was incomplete.

---

## 2. Bug Classification

### Classification Key

| Code | Meaning |
|------|---------|
| **P** | Product bug — defect in production code logic |
| **T** | Test error — test infrastructure or environment issue |
| **A** | Artifact mismatch — identity/documentation discrepancy |
| **E** | Environment error — OS, shell, or runtime environment issue |
| **UX** | Deferred UX — cosmetic or presentation issue, not safety-critical |
| **S** | Safety violation — security, authorization, or data integrity issue |

### 2.1 B7 — IPC Socket Disappears on Terminal Close

**Classification:** E (Environment error)

**Description:** Starting the daemon from a terminal and then closing the
terminal sends SIGHUP to the daemon process, killing it and removing the
IPC socket. Workaround: `nohup` or LaunchAgent.

**Production call path:**
```
Terminal close → SIGHUP → daemon process killed
  → IPCServer.Close() (ipc.go:101) → listener.Close()
  → socket file becomes stale (not removed by Close())
  → On restart: StartIPCServer (ipc.go:46) detects stale socket, removes, re-binds
```

| File | Line | Call |
|------|------|------|
| `internal/term/ipc.go` | 24-34 | `IPCServer` struct — owns `listener`, `closeOnce` |
| `internal/term/ipc.go` | 101-106 | `Close()` — closes listener but does NOT remove socket file |
| `internal/term/ipc.go` | 46-54 | `StartIPCServer` — stale socket detection and removal |
| `cmd/devremote/main.go` | — | Signal handling — no explicit SIGHUP handler |

**Note:** This is NOT a code defect. The daemon correctly cleans up on
shutdown. The issue is that a terminal-close SIGHUP kills the daemon
unexpectedly. The LaunchAgent deployment (which the project already has)
is the intended production configuration. The `nohup` workaround is
acceptable for development.

**Affected modes:** All (daemon-level)

### 2.2 B8 — Dual Input Paths (xterm.js + POKIT TextInput)

**Classification:** P (Product bug — Known #8 from R0)

**Description:** Two separate input paths converge on `doSend()`:
1. xterm.js keyboard → `pokitSendInput` (WebView JS bridge)
2. POKIT TextInput + Send button → `submitLine` → `doSend`

Both paths write to the same WebSocket. The overlapping surfaces can cause
confusion about which input is active.

**Production call path:**

| File | Line | Call |
|------|------|------|
| `screens/FeedScreen.tsx` | 517-522 | `doSend()` — sends via `window.pokitSendInput` |
| `screens/FeedScreen.tsx` | 563-582 | `submitLine()` — splits text+Enter into two `doSend` calls |
| `screens/FeedScreen.tsx` | 592-596 | `sendMacro()` — sends macro chars via `doSend` |
| `screens/FeedScreen.tsx` | 626-632 | `handlePasteRequest()` — sends pasted text via `doSend` |
| `screens/FeedScreen.tsx` | 519 | `window.pokitSendInput(data, operationId, part)` |
| `internal/term/pty.go` | 269-295 | `handleWSWithPrincipal` — WebSocket input handler |
| `internal/term/terminal_transport.go` | 273-304 | `WriteInput` — one-writer enforcement + PTY write |

**Note:** R0 §2.4 already classified this as stale-diagnosis (not a
WebRTC/WebSocket split — both paths already use WebSocket). The remaining
defect is the UI presenting two input surfaces. They are NOT independent
paths to the PTY — both write through the same `TerminalTransport.WriteInput`
which enforces one-writer policy. The UX issue is that users don't know
which input surface is "active."

**Affected modes:** `controlled_pty:*` (terminal sessions only; managed
sessions use `ManagedSessionView` with a separate prompt box)

### 2.3 UX1 — Input Field ~1cm Gap Above Keyboard

**Classification:** UX (Deferred UX)

**Description:** Manual `kbHeight` state + `paddingBottom` calculation
produces a visible gap between the input field and the keyboard on iOS.

**Production call path:**

| File | Line | Call |
|------|------|------|
| `screens/FeedScreen.tsx` | 140 | `const [kbHeight, setKbHeight] = useState(0)` |
| `screens/FeedScreen.tsx` | 372-380 | `keyboardDidShow`/`keyboardDidHide` listener → `setKbHeight` |
| `screens/FeedScreen.tsx` | 1194 | `paddingBottom: Math.max(kbHeight, Platform.OS === 'ios' ? 20 : 6)` |

**Root cause:** No `KeyboardAvoidingView` is used. The manual padding
calculation overshoots on iOS (static 20pt floor + keyboard height).
Floating/split keyboards on iOS 17+ report different heights.

**Affected modes:** All mobile terminal sessions

### 2.4 UX2 — Status Bar Wastes Vertical Space

**Classification:** UX (Deferred UX)

**Description:** `SafeAreaView` wraps the entire layout (adding ~44-47pt
status bar inset), then the header adds `paddingVertical: 10`. Stacked
padding wastes vertical space in the terminal view.

**Production call path:**

| File | Line | Call |
|------|------|------|
| `screens/FeedScreen.tsx` | 896 | `<SafeAreaView style={styles.container}>` |
| `screens/FeedScreen.tsx` | 1254-1257 | `header: { paddingVertical: 10 }` |

**Affected modes:** All mobile terminal sessions

### 2.5 UX3 — "Delivered/Sent to socket" User-Unfriendly

**Classification:** UX (Deferred UX)

**Description:** Protocol-level delivery status strings ("Sent to socket",
"Delivered to terminal") are exposed directly to the user.

**Production call path:**

| File | Line | Call |
|------|------|------|
| `screens/FeedScreen.tsx` | 54 | `SendStatus` type definition |
| `screens/FeedScreen.tsx` | 277 | `const [sendStatus, setSendStatus] = useState<SendStatus>('idle')` |
| `screens/FeedScreen.tsx` | 1211-1214 | Status text rendering with raw protocol strings |

**Affected modes:** All mobile terminal sessions

### 2.6 UX4 — Transcript Forced Line-Wrap

**Classification:** UX (Deferred UX)

**Description:** Monospace transcript text wraps at container width instead
of preserving terminal-width formatting with horizontal scroll.

**Production call path:**

| File | Line | Call |
|------|------|------|
| `components/TranscriptRenderer.tsx` | 16-51 | `E8g2Transcript` — `FlatList` without `horizontal` |
| `components/TranscriptRenderer.tsx` | 37-39 | `<Text style={styles.transcriptOutputText}>` |
| `components/TranscriptRenderer.tsx` | 64 | `transcriptOutputText: { fontSize: 12, fontFamily: 'monospace' }` — missing `whiteSpace`, `overflow` |
| `screens/FeedScreen.tsx` | 1369-1376 | Unused `transcriptOutputText` style with "E8i: preserve terminal-width formatting" comment |

**Note:** The intent is documented at FeedScreen line 1369 but the style is
never applied — `E8g2Transcript` uses its own styles.

**Affected modes:** All sessions with Transcript tab

### 2.7 UX5 — Transcript Frozen (Codex Static, Claude Empty)

**Classification:** P (Product bug — Known #5 from R0, partially-wired)

**Description:** Codex sessions show static/frozen transcript content.
Claude sessions show empty transcript despite having a running agent.
Two separate polling mechanisms serve different session types, and the
transcript endpoint may not return data for managed sessions.

**Production call path:**

| File | Line | Call |
|------|------|------|
| `screens/FeedScreen.tsx` | 417-484 | `fetchTranscript()` — polls `/api/sessions/{id}/transcript` every 3s |
| `screens/FeedScreen.tsx` | 114-116 | Codex routing: `transcriptOnly` → `<ManagedSessionView>` |
| `components/ManagedSessionView.tsx` | 39-63 | Codex polling: `getManagedEvents()` → `/api/managed-sessions/{id}/events` every 1.5s |
| `lib/client.ts` | 562-570 | `getTranscript()` → `/api/sessions/{id}/transcript` |
| `lib/client.ts` | 373-380 | `getManagedEvents()` → `/api/managed-sessions/{id}/events` |
| `lib/client.ts` | 509-560 | `validateTranscriptResponse()` — strict validation, returns null on mismatch |
| `lib/managedSession.ts` | 116-137 | `decodeManagedEventsResponse()` — strict fail-closed, returns null on violation |

**Root cause analysis:**

- **Codex (ManagedSessionView):** Uses `ManagedSessionController` polling
  `/api/managed-sessions/{id}/events`. This endpoint dispatches through
  `ManagedCodexService` only. The event store may not be receiving new events
  after the initial certification turn.
- **Claude (LegacyFeedScreen):** Uses `getTranscript()` polling
  `/api/sessions/{id}/transcript`. The transcript service may not have
  Claude events projected into it (Claude uses a separate normalizer path
  through `claudeJSONLNormalizer`). The transcript response may be
  `healthy_empty` because the projection hasn't occurred yet.
- **Contract validation:** Both paths have strict fail-closed validation.
  Unknown fields or contract version mismatches cause the response to be
  silently discarded (`null`), which presents as "frozen" or "empty."

**Affected modes:** `codex_app_server:*` (static), `claude_headless:*` (empty)

### 2.8 UX6 — QR y/N Prompt Not Visible

**Classification:** UX (Deferred UX — future: 6-digit code)

**Description:** The QR pairing approval prompt (`"Approve this device? (y/N)"`)
exists only in the CLI `pokit pair` subcommand. When the daemon runs
standalone via LaunchAgent, there is no visible approval prompt. The mobile
QR bridge auto-validates without operator confirmation.

**Production call path:**

| File | Line | Call |
|------|------|------|
| `cmd/devremote/pair.go` | 120 | `fmt.Printf("\nApprove this device? (y/N): ")` — CLI-only prompt |
| `cmd/devremote/pair.go` | 39 | `net.Dial("unix", "/tmp/pokit.sock")` — local socket pair client |
| `cmd/devremote/app.go` | 508-510 | `term.SetQRPairBridge(...)` — daemon-side bridge, no prompt |
| `cmd/devremote/qr_pair.go` | 107 | `Verify()` — auto-validates mobile echoed metadata |

**Note:** DEV7 report acknowledges this is future work ("future: 6-digit").
Not a regression.

**Affected modes:** QR pairing flow (daemon mode)

### 2.9 B9 — Send Button: Bash OK, Agents Broken (submitLine Framing)

**Classification:** P (Product bug)

**Description:** The mobile `submitLine` function splits text+Enter into two
separate WebSocket messages with a 40ms delay. For bash sessions, this works
(the shell receives text, then Enter). For agent/Claude sessions, the
two-frame split may not reach the provider process correctly because:

1. Claude sessions that route through `LegacyFeedScreen` use the terminal
   WebView + `submitLine` for input
2. The PTY stdin receives text and Enter as separate writes
3. If the Claude process is in a TUI mode, the 40ms delay between text and
   Enter may cause the TUI to interpret them as separate inputs rather than
   one command

**Production call path:**

| File | Line | Call |
|------|------|------|
| `screens/FeedScreen.tsx` | 563-582 | `submitLine()` — text → `doSend(text, opId, 'text')`, 40ms → `doSend('\r', opId, 'enter')` |
| `screens/FeedScreen.tsx` | 517-522 | `doSend()` → `window.pokitSendInput` |
| `internal/term/pty.go` | 269-295 | WebSocket → `handleTerminalInput` → `inputTransport.WriteInput` |
| `internal/term/terminal_transport.go` | 273-304 | `WriteInput` → PTY write (text and \r arrive as separate writes) |

**Root cause hypothesis:** For `controlled_pty:*` (bash), the PTY receives
text, then Enter, which the shell interprets correctly. For
`claude_headless:*` (dual-surface), the Claude TUI may have different input
buffering behavior. The 40ms delay between text and Enter may cause the
TUI's input parser to see them as separate keystrokes rather than one
command line.

**Note:** This is a regression from the two-frame ACK protocol (PA3 Input-B)
which was designed for bash sessions and may not be appropriate for Claude
TUI sessions where the Enter key is part of the terminal byte stream, not
a separate delivery concern.

**Affected modes:** `claude_headless:*` (dual-surface terminal input)

### 2.10 B10 — Claude App Create: EISDIR Settings Error

**Classification:** P (Product bug — Safety: blocks Claude creation)

**Description:** Claude app create fails with EISDIR (Is a directory) when
the `--settings` flag receives a directory path instead of a file path.

**Production call path:**

| File | Line | Call |
|------|------|------|
| `internal/term/claude_interactive_host.go` | 95 | `hookDir, err := os.MkdirTemp(...)` — creates directory |
| `internal/term/claude_interactive_host.go` | 109 | `writeInteractiveHookSettings(hookDir, ...)` — writes `settings.json` INSIDE the directory |
| `internal/term/claude_interactive_host.go` | 126 | `"--settings", hookDir` — **BUG: passes directory, not `hookDir/settings.json`** |
| `internal/term/claude_interactive_host.go` | 457 | `os.WriteFile(filepath.Join(hookDir, "settings.json"), data, 0600)` — file written at correct path |

**Root cause:** Line 126 passes `hookDir` (a directory path) to
`--settings`, but `--settings` expects a JSON FILE path. The correct path
is `filepath.Join(hookDir, "settings.json")`. Claude's `--settings` flag
attempts to `open(2)` the directory as a file, which returns EISDIR.

**Fix:** Change line 126 from `"--settings", hookDir` to
`"--settings", filepath.Join(hookDir, "settings.json")`.

**Severity:** This is a **P0 blocker** for Claude interactive sessions.
Every `pokit run claude` or mobile Claude session creation fails at launch.

**Affected modes:** `claude_headless:*` (all Claude creation paths)

### 2.11 QW12b — nohup Cloudflared Dedup Partial

**Classification:** P (Product bug — Known #12 from R0, partially fixed)

**Description:** The daemon's tunnel dedup check (`a.tunnel == nil`) only
prevents duplicate tunnel starts within a single `App.Run()` lifecycle.
An externally spawned `cloudflared tunnel run devremote` (via nohup,
separate terminal, or second LaunchAgent) is NOT detected.

**Production call path:**

| File | Line | Call |
|------|------|------|
| `cmd/devremote/app.go` | 972-976 | `if a.tunnel == nil { a.tunnel = a.startTunnel() }` — in-memory nil check only |
| `cmd/devremote/app.go` | 1372-1406 | `startTunnelProd()` — `exec.Command("cloudflared", "tunnel", "run", "devremote")` |
| `cmd/devremote/app.go` | 1387 | Direct spawn — no PID file, no `pgrep`, no lockfile, no port check |

**Note:** Comment on line 973 says "BUG-012: only start a tunnel if one
isn't already running." Acknowledged as known issue since R0. QW12 (R0)
fixed dedup within the daemon process; external dedup was deferred.

**Affected modes:** All (tunnel-level, not session-level)

---

## 3. Dependency and Shared Root Cause Analysis

### 3.1 Direct Dependency Graph

```
B10 (EISDIR)
  ├─ BLOCKS: All Claude creation
  └─ Fix enables: Claude interactive mode testing

B9 (submitLine framing)
  ├─ DEPENDS ON: Input-B two-frame ACK protocol design
  └─ Shared with: B8 (dual input surfaces both use submitLine/doSend)

UX5 (frozen transcript)
  ├─ Codex: ManagedSessionView event polling may not receive new events
  ├─ Claude: Transcript service may not have Claude events projected
  └─ DEPENDS ON: Event store population (R4 Codex, R5 Claude)

B7 (IPC socket)
  └─ INDEPENDENT: Environment-level (SIGHUP), not a code defect

UX1-4, UX6 (cosmetic)
  └─ INDEPENDENT: UI polish, no safety impact

QW12b (cloudflared dedup)
  └─ INDEPENDENT: Known deferred issue, no safety impact
```

### 3.2 Shared Root Causes

| Root Cause | Bugs |
|------------|------|
| **Input-B two-frame split inappropriate for non-bash sessions** | B8, B9 |
| **Codex event store not populating after initial turn** | UX5 (Codex) |
| **Claude transcript projection not wired to Transcript service** | UX5 (Claude) |
| **Launch path: `--settings` receives directory, not file** | B10 |
| **No SIGHUP handling for terminal-launched daemon** | B7 |
| **ART6 manifest/production identity mismatch** | A1 (§1.2) |

---

## 4. Proposed Bounded Write Scopes

### 4.1 BF-1A — Critical Product Bug Fixes

**Write scope:**
- `internal/term/claude_interactive_host.go:126` — Fix `--settings` path from directory to file (`filepath.Join(hookDir, "settings.json")`)
- Test: add EISDIR regression test in `internal/term/` or `dscl1/`

**Bugs addressed:** B10 (EISDIR)

**Classification:** P0 product bug fix

**Dependencies:** None (independent fix)

**Estimated scope:** 1 line + test

### 4.2 BF-1B — Transcript Projection Wiring

**Write scope:**
- `internal/transcript/service.go` — Ensure Claude events reach Transcript service
- `internal/term/claude_interactive_host.go` — Wire normalizer output to Transcript
- Mobile: `FeedScreen.tsx` — Update transcript polling to handle partial/degraded state correctly
- Mobile: `ManagedSessionView.tsx` — Fix Codex event polling refresh logic

**Bugs addressed:** UX5 (Claude empty + Codex static)

**Classification:** P1 product bug fix (two separate issues, same domain)

**Dependencies:** BF-1A must complete first (Claude creation must work to test transcript)

**Estimated scope:** ~50 lines across 4 files

### 4.3 BF-2A — Input Framing and Surface Unification

**Write scope:**
- `screens/FeedScreen.tsx` — Add session-type-aware framing in `submitLine` (single-frame for Claude/Codex, two-frame for bash)
- `screens/FeedScreen.tsx` — Relabel or hide duplicate input surface when xterm keyboard is active

**Bugs addressed:** B8 (dual input surfaces), B9 (submitLine framing for agents)

**Classification:** P2 product behavior change

**Dependencies:** BF-1A (Claude must work to test input framing)

**Estimated scope:** ~30 lines in FeedScreen.tsx

### 4.4 BF-2B — UX Polish and Transcript Rendering

**Write scope:**
- `components/TranscriptRenderer.tsx` — Add horizontal scroll for long lines
- `screens/FeedScreen.tsx` — Replace protocol-level delivery strings with user-friendly labels
- `screens/FeedScreen.tsx` — Fix keyboard gap using `KeyboardAvoidingView`

**Bugs addressed:** UX1 (keyboard gap), UX3 (delivery strings), UX4 (line-wrap)

**Classification:** UX polish (non-blocking)

**Dependencies:** None (cosmetic only)

**Estimated scope:** ~40 lines across 2 files

### 4.5 Deferred to Future Wave

| Bug | Reason |
|-----|--------|
| B7 (IPC socket) | LaunchAgent is the intended production configuration; `nohup` is acceptable for development |
| UX2 (status bar) | Non-critical space optimization |
| UX6 (QR prompt) | Planned 6-digit code feature |
| QW12b (cloudflared dedup) | Known deferred since R0; PID file / `pgrep` solution requires separate design |
| A1 (artifact mismatch) | Resolved by BF-ART rebuild after BF-1A/BF-1B fixes |

---

## 5. Safety and Security Impact

| Bug | Safety Impact |
|-----|---------------|
| B7 | **None** — daemon shutdown is clean; socket is recreated on restart |
| B8 | **None** — both paths converge on `TerminalTransport.WriteInput` with one-writer enforcement |
| B9 | **Low** — input may be rejected/misinterpreted but cannot reach wrong session |
| B10 | **None** — fail-closed: Claude session never starts, no partial state |
| UX1-4 | **None** — cosmetic |
| UX5 | **None** — missing evidence, not wrong evidence. Transcript is labeled partial/degraded |
| UX6 | **None** — QR bridge validates mobile-echoed metadata; no unauthenticated pairing path |
| QW12b | **Low** — extra tunnel processes waste resources but don't create security holes |

**Zero safety violations found.** No device trust bypass, no cross-session
mutation, no unauthenticated approval path, no secret leakage.

---

## 6. BF-1A / BF-1B Parallel Execution Assessment

| Check | BF-1A | BF-1B |
|-------|-------|-------|
| Files touched | `claude_interactive_host.go` | `service.go`, `claude_interactive_host.go`, `FeedScreen.tsx`, `ManagedSessionView.tsx` |
| Overlap with BF-1B? | Yes — `claude_interactive_host.go:126` (same file, different line) | Yes |
| Safer to serialize? | **Yes** — BF-1A must complete first. BF-1B tests require working Claude creation. | Must wait for BF-1A. |

**Decision: BF-1A and BF-1B must run SERIALLY.** They share `claude_interactive_host.go`
and BF-1B's tests depend on BF-1A's fix.

BF-2A and BF-2B can run in parallel (disjoint write scopes: FeedScreen.tsx vs
TranscriptRenderer.tsx). But both must wait for BF-1A and BF-1B to complete.

---

## 7. ART6 Invalidation and Next Candidate

### 7.1 Why ART6 Is Invalid

1. **Daemon hash mismatch:** `DS_ART6_MANIFEST.md` records `0c9d0418`; DEV7
   tested `b6c0a36e`. Different binaries. Different evidence.
2. **Missing APK hash in ART6 manifest:** ART6 manifest does not record APK
   SHA-256. The DEV7 report is the first to record it.
3. **Stale on-disk manifest:** `pokit-alpha-device-artifacts/MANIFEST.json`
   has R0-era values. Never updated.
4. **Non-deterministic test gate:** Jest 550/552 despite STAB5 claiming
   552/552. The gate is not deterministic at the frozen SHA.

### 7.2 Next Candidate (BF-ART)

After BF-1A and BF-1B are ACCEPT:
1. Freeze a new source SHA.
2. Build daemon and APK from THAT exact source.
3. Record BOTH daemon and APK SHA-256 in the manifest.
4. Update `pokit-alpha-device-artifacts/MANIFEST.json`.
5. Prove `vcs.modified=false`.
6. Run full gate: build, vet, race, tsc, Jest (must be 552/552 deterministic).
7. Re-run DS-DEV7 physical matrix.
8. Only then may DS-AUD8 resume.

---

## 8. Decision Log

### D1 — BF-1A is the first executable packet
EISDIR blocks all Claude creation. Nothing else can be tested until it's fixed.

### D2 — BF-1A and BF-1B are serial
Shared file (`claude_interactive_host.go`) and BF-1B tests depend on BF-1A.

### D3 — BF-2A and BF-2B are parallel
Disjoint write scopes. No shared files.

### D4 — ART6 is invalid
Daemon hash mismatch between manifest and DEV7 report. New candidate required.

### D5 — DS-AUD8 remains paused
Until a new artifact candidate passes DS-DEV7 with deterministic gates.

---

**Triage complete:** 2026-07-25
**Next executable packet:** BF-1A — Fix Claude EISDIR (1 line + test)
