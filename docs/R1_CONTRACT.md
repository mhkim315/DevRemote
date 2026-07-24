# R1 — Runtime Mode & Provider Contract Freeze

**Status:** FROZEN CONTRACT — IMPLEMENTATION NOT STARTED
**Parent:** `BASE_ALPHA_TERMINAL_MANAGED_REMEDIATION_PLAN.md` §R1
**Branch:** `feature/canonical-timeline-foundation`
**Date:** 2026-07-25

> This document freezes the R1 contract. No implementation begins before
> independent ACCEPT. It inventories every live caller, defines explicit
> `terminal` and `managed` runtime modes, separates Codex/Claude ownership,
> defines capability states, and decides CLI/mobile mode selection.

---

## 1. Live Caller Inventory

Every caller that invokes session creation, status/read, events, prompt,
lifecycle, Transcript, Terminal, or local IPC is enumerated below. Each entry
records the dispatch method and which provider types it currently handles.

### 1.1 Session Creation

| Caller | File:Line | Path / Operation | Dispatch |
|--------|-----------|------------------|----------|
| HTTP POST (mobile) | `create.go:38` | `POST /api/sessions` `{profileId, name, cwd}` | By `profileId`: `"claude"` (line 68) → `ManagedClaudeService.CreateDetached` (`claude_headless:*`); `"codex"` (line 98) → `ResolveProfile` → `OwnedPTYRuntime.Create` (**`controlled_pty:*` wrapping the `codex` binary — NOT managed Codex**); `"shell"` (line 98) → `OwnedPTYRuntime.Create` (`controlled_pty:*`) |
| IPC JSON create | `ipc.go:166` | 0600 socket `{"operation":"create"}` | By `profileId`: `"codex"` (line 185) → `ManagedCodexService.CreateDetached/Attached` (`codex_app_server:*`); `"claude"` (line 207) → `ManagedClaudeService.CreateDetached` (`claude_headless:*`); other/legacy (line 228) → `createLocalControlled` (`controlled_pty:*`) |
| IPC legacy | `ipc.go:278` | 0600 socket `cmd:...` | **REMOVED** — returns "legacy IPC protocol is not supported" |
| Mobile `createSession()` | `client.ts:302` | `POST /api/sessions` | Sends `{profileId, name, cwd}`, no adapter or mode selection |
| Mobile `createOrUpdateSession()` | `client.ts:391` | `POST /api/sessions` | **LEGACY** — edit/color path only, sends `{id, runner, runnerColor}` |

**Critical finding:** The `"codex"` profile creates DIFFERENT adapter types depending on the caller:
- **HTTP** (`POST /api/sessions`): `controlled_pty:*` wrapping the `codex` binary (PTY mode)
- **IPC** (`pokit run codex`): `codex_app_server:*` via `ManagedCodexService` (managed mode)

This is a hidden mode divergence. The same profile ID produces a fundamentally
different session type depending on whether the caller is HTTP or IPC. Mobile
cannot create managed Codex sessions at all. The `"claude"` profile correctly
creates `claude_headless:*` on both paths.

### 1.2 Session Status / List

| Caller | File:Line | Path | Dispatch |
|--------|-----------|------|----------|
| HTTP GET (mobile) | `pty.go:34` | `GET /api/sessions` | `HandleSessionsAPI` → adapter snapshot + `mergeLifecycleState` (controlled_pty only) + `appendCatalogRows` + `appendClaudeCatalogRows` → merged list |
| Mobile `listSessions()` | `client.ts:255` | `GET /api/sessions` | Unified — receives merged rows from all adapters |
| Mobile `probeDaemon()` | `client.ts:208` | `GET /api/sessions` | Reachability check — same unified endpoint |
| Mobile `getManagedStatus()` | `client.ts:365` | `GET /api/sessions/{id}/native-status` | `HandleManagedNativeStatus` — dispatches via `Catalog.Get(id)` (federated, works for both Codex and Claude) |
| HTTP GET managed list | `managed_api.go:89` | `GET /api/managed-sessions` | `HandleManagedSessions` — catalog `.List()` federated |
| Catalog merge | `telemetry.go:78` | (internal) | `mergeLifecycleState` — controlled_pty only via `OwnedPTYRuntime`; managed provider rows via `appendCatalogRows` + `appendClaudeCatalogRows` |
| Capability gating | `FeedScreen.tsx:125` | (mobile) | `capabilities?.includes('history')`, `adapterCapabilities?.includes('liveTerminal')`, `adapterCapabilities?.includes('bestEffortTranscript')` |

**Finding:** Status/List is the most unified path. The catalog (`ManagedRuntimeCatalog`) federates all three providers correctly (`managed_catalog.go:119-266` dispatches by adapter prefix). `native-status` dispatches correctly for both Codex and Claude via `Catalog.Get()`. `mergeLifecycleState` (`telemetry.go:78`) handles ONLY `controlled_pty` lifecycle merging; managed provider rows come through `appendCatalogRows` (`managed_catalog.go:324`) which correctly federates both `codex_app_server` and `claude_headless` registries. The legacy `appendManagedRows` and `appendClaudeManagedRows` are superseded but still exported.

### 1.3 Events

| Caller | File:Line | Path | Dispatch |
|--------|-----------|------|----------|
| HTTP GET events | `managed_api.go:122` | `GET /api/managed-sessions/{id}/events` | `HandleManagedSessionEvents` → `h.Managed.Registry().Get(id)` + `h.Managed.eventStoreFor(id)` — **CODEX ONLY** |
| Mobile `getManagedEvents()` | `client.ts:373` | `GET /api/managed-sessions/{id}/events` | Uses Codex-only endpoint; Claude sessions 404 |
| Mobile poller | `managedSession.ts:259` | Via `ManagedEventsFetcher` → `getManagedEvents()` | Codex-only event consumption |
| Claude event store | `managed_claude.go:1071` | (internal) | `eventStoreFor` returns `nil, 0, false` — **NOT IMPLEMENTED** |

**Finding:** Events path is Codex-only. Claude sessions have no event store and no event endpoint. The mobile `ManagedSessionView` consumes events through the Codex-only path.

### 1.4 Prompt

| Caller | File:Line | Path | Dispatch |
|--------|-----------|------|----------|
| HTTP POST prompt | `managed_api.go:192` | `POST /api/managed-sessions/{id}/prompt` | `HandleManagedSessionPrompt` → `h.Managed.SubmitPrompt()` — **CODEX ONLY** |
| Mobile `postManagedPrompt()` | `client.ts:385` | `POST /api/managed-sessions/{id}/prompt` | Codex-only endpoint; Claude sessions fail |
| Mobile send button | `ManagedSessionView.tsx:73` | Via `postManagedPrompt()` | Active prompt UI for both Codex and Claude sessions, but Claude backend rejects |
| IPC prompt | (none) | — | No IPC prompt path for managed sessions (only `managed-attach`) |

**Finding:** Prompt path is Codex-only. Claude sessions have no prompt submission capability. The mobile UI shows a prompt box for Claude sessions but the backend will reject it.

### 1.5 Lifecycle (Stop / Kill / Delete)

| Caller | File:Line | Path | Dispatch |
|--------|-----------|------|----------|
| HTTP POST stop | `lifecycle_handlers.go:32` | `POST /api/sessions/{id}/stop` | `HandleSessionStop` → `LifecycleService.Stop` → dispatches by adapter prefix to `OwnedPTYRuntime`, `ManagedCodexService`, or `ManagedClaudeService` |
| HTTP POST kill | `lifecycle_handlers.go:54` | `POST /api/sessions/{id}/kill` | `HandleSessionKill` → `LifecycleService.Kill` — same dispatch |
| HTTP DELETE | `lifecycle_handlers.go:77` | `DELETE /api/sessions/{id}` | `HandleSessionDelete` → `LifecycleService.Delete` — same dispatch |
| Mobile `stopSession()` | `client.ts:332` | `POST /api/sessions/{id}/stop` | **UNIFIED** — uses LifecycleService dispatcher, works for all adapters |
| Mobile `killSession()` | `client.ts:341` | `POST /api/sessions/{id}/kill` | **UNIFIED** — works for all adapters |
| Mobile `deleteSessionHistory()` | `client.ts:352` | `DELETE /api/sessions/{id}` | **UNIFIED** — works for all adapters |
| HTTP POST stop (managed-Codex direct) | `managed_api.go:285` | `POST /api/managed-sessions/{id}/stop` | `HandleManagedSessionStop` → `h.Managed.Stop()` — **CODEX DIRECT** (bypasses LifecycleService) |
| HTTP POST kill (managed-Codex direct) | `managed_api.go:294` | `POST /api/managed-sessions/{id}/kill` | `HandleManagedSessionKill` → `h.Managed.Kill()` — **CODEX DIRECT** |
| HTTP DELETE (managed-Codex direct) | `managed_api.go:303` | `DELETE /api/managed-sessions/{id}` | `HandleManagedSessionDelete` → `h.Managed.Delete()` — **CODEX DIRECT** |
| HTTP POST stop (managed-Claude direct) | `managed_api.go:343` | `POST /api/managed-claude-sessions/{id}/stop` | `HandleManagedClaudeSessionStop` → `h.ManagedClaude.Stop()` — **CLAUDE DIRECT** (bypasses LifecycleService) |
| HTTP POST kill (managed-Claude direct) | `managed_api.go:348` | `POST /api/managed-claude-sessions/{id}/kill` | `HandleManagedClaudeSessionKill` → `h.ManagedClaude.Kill()` — **CLAUDE DIRECT** |
| HTTP DELETE (managed-Claude direct) | `managed_api.go:353` | `DELETE /api/managed-claude-sessions/{id}` | `HandleManagedClaudeSessionDelete` → `h.ManagedClaude.Delete()` — **CLAUDE DIRECT** |
| `LifecycleService.ownerFor` | `lifecycle_service.go:114` | (internal dispatch) | Dispatches `codex_app_server` → Codex owner, `claude_headless` → Claude owner, `controlled_pty` → ownedPTY, unknown → fail closed |

**Finding:** Lifecycle has THREE parallel trees: (1) unified `/api/sessions/{id}/*` via `LifecycleService` — correctly dispatches all adapters, (2) `/api/managed-sessions/{id}/*` — Codex direct, (3) `/api/managed-claude-sessions/{id}/*` — Claude direct. The mobile client uses the unified endpoints (#1). The direct endpoints (#2, #3) bypass `LifecycleService` and its generation derivation. All three trees are live and wired in `app.go`.

### 1.6 Transcript

| Caller | File:Line | Path | Dispatch |
|--------|-----------|------|----------|
| HTTP GET transcript | `app.go:778` | `GET /api/sessions/{id}/transcript` | `transcript.HandleTranscript(transcriptSvc)` — unified, works for all sessions |
| HTTP GET transcript stats | `app.go:779` | `GET /api/sessions/{id}/transcript/stats` | `transcript.HandleTranscriptStats(transcriptSvc)` — unified |
| Mobile `getTranscript()` | `client.ts:562` | `GET /api/sessions/{id}/transcript` | Unified — strict fail-closed decode |
| Transcript input suppression | `arbitration.go:85` | (internal) | `ts.BeginInput()` → byte-stream permanently suppressed after input |

**Finding:** Transcript is the most mature unified path. Works for all session types. The suppression-after-input behavior is by design.

### 1.7 Terminal (WebSocket + HTML)

| Caller | File:Line | Path | Dispatch |
|--------|-----------|------|----------|
| HTTP WS | `pty.go:216` | `GET /term/ws` | `HandleWS` → adapter `StreamOpener` or `OwnedPTYRuntime.Transport` |
| HTTP WS (ticket) | `pty.go:232` | `GET /term/ws` (ticket auth) | `HandleWSTicketAuth` — same transport dispatch |
| HTTP term page | `app.go:768` | `GET /term/` | `HandleHTML` — serves terminal HTML page |
| HTTP term size | `app.go:767` | `GET /term/size` | `HandleTermSize` |
| Mobile WebView | `FeedScreen.tsx:103` | WebView → `/term/` | Interactive PTY sessions only — `controlled_pty:*`, `tmux:*`, `cmux:*` |
| Mobile `terminalWebSocketURL()` | `client.ts:711` | `GET /term/ws` | Used by the terminal HTML page's JS |
| IPC subscriber | `ipc.go:343` | `sub:<sessionID>` | `handleIPCSubscriber` → `controlled_pty` only via `OwnedPTYRuntime.Transport`; other adapters refused |
| Mobile `terminalURL()` | `client.ts:705` | `GET /term/` | Used by WebView source URL |

**Finding:** Terminal WebSocket is correctly gated — `FeedScreen.tsx:97` routes `codex_app_server:*` and `claude_headless:*` to `ManagedSessionView` before reaching the terminal WebView. Managed sessions never get a WebSocket. IPC subscriber is correctly restricted to `controlled_pty`.

### 1.8 Local IPC

| Caller | File:Line | Operation | Dispatch |
|--------|-----------|-----------|----------|
| JSON create | `ipc.go:166` | `{"operation":"create"}` | By `profileId` — see §1.1 |
| JSON managed-attach | `ipc.go:243` | `{"operation":"managed-attach"}` | `handleManagedAttach` → `ManagedCodexService` only |
| Legacy subscriber | `ipc.go:343` | `sub:<sessionID>` | `controlled_pty` only via `OwnedPTYRuntime.Transport` |
| Pair ops | `ipc.go:251-274` | `pair-start/approve/reject` | `handlePairOp` |
| Device admin | `ipc.go:260-268` | `devices-list/revoke/recover-owner` | `handleDevicesList/Revoke/RecoverOwner` |
| Audit list | `ipc.go:270` | `audit-list` | `handleAuditList` |

**Finding:** IPC `managed-attach` is Codex-only. Claude has no local attach/view path.

---

## 2. Runtime Mode Definition

Two explicit modes. Every session creation path MUST select exactly one.
No inference from adapter label, agent kind, or profile ID.

### 2.1 `terminal` Mode

```
Canonical prefix: controlled_pty:*
Primary surface:   Raw Terminal (xterm.js WebView ↔ HandleWS/PTY Transport)
Secondary surface: Explicitly degraded/best-effort Transcript
Input:             WebSocket write (single-writer enforced)
Lifecycle:         OwnedPTYRuntime (Stop/Kill/Delete via LifecycleService)
Capabilities:      liveTerminal, live_stream, history, managedLifecycle, input
                   + bestEffortTranscript (adapter-level)
```

- Terminal output is the authoritative surface.
- Transcript is byte-stream projection — permanently suppressed after first
  input to prevent typed/secrets from re-entering history.
- One PTY read (`Recorder`), multiple observers (mobile WebView + local
  `pokit watch` via IPC subscriber fan-out).
- No managed event stream. No prompt endpoint.

### 2.2 `managed` Mode

```
Canonical prefixes: codex_app_server:*  |  claude_headless:*
Primary surface:    Provider-native structured Transcript
Secondary surface:  Operational status/evidence; NO fake PTY
Input:              Provider-specific prompt contract (NOT a generic text box)
Lifecycle:          ManagedCodexService | ManagedClaudeService
                    (via LifecycleService dispatcher)
Capabilities:       managedLifecycle, history
                    + provider-specific sub-capabilities (see §4)
```

- Structured events projected through provider-specific normalizers into
  the Transcript contract.
- No PTY. No WebSocket terminal. No raw byte stream in Transcript.
- Prompt submission is provider-owned: Codex has a one-active-turn contract;
  Claude has no prompt contract at this baseline.
- The UI must show truthful input capability: enabled (Codex), disabled
  with explanation (Claude observation-only), or unavailable.

### 2.3 Legacy (non-Pokit-managed)

```
Canonical prefixes: tmux:*  |  cmux:*
Primary surface:    Raw Terminal (WebSocket ↔ adapter StreamOpener)
Transcript:         Adapter-provided history (if adapter supports HistoryReader)
Lifecycle:          Adapter-provided (SessionTerminator); NOT managed
Capabilities:       Depends on adapter optional interfaces
```

- Legacy adapter sessions are not lifecycle-managed by POKIT.
- Stop/Kill/Delete return `ErrLifecycleUnsupported` (422).
- These sessions are NOT in scope for R1–R8 remediation.

---

## 3. Provider Contract Boundary

### 3.1 Shared Minimum Contract

Only these fields may be common across Codex and Claude:

```
- provider (string: "codex" | "claude")
- canonical session ID (string: "<prefix>:<local-id>")
- runtime ID (string, opaque, generation-bound)
- launch generation (int64, monotonic, server-assigned)
- configuration epoch (int64)
- lifecycle state (enum: starting | running | stopping | exited | killed | failed)
- lifecycle owner (exact provider service, never inferred)
- server-issued capabilities (string set, see §4)
- availability/degradation state (enum, see §4)
- bounded cursor (uint64, monotonic, server-assigned)
- event identity (session + epoch + seq, globally unique within session)
- source identity/position (provider-native reference, opaque to common layer)
- timestamps (RFC3339 UTC, server-observed)
- redaction policy (closed set of redaction reasons)
- gap/collision representation (explicit gap marker, no silent drop)
- provider-neutral Transcript segment kinds that have proven mappings:
  assistant, working, completed, exited, gap (closed set, R3 expands)
- authorization principal (device ID + device epoch + permission set)
- immutable evidence references (generation-bound, never rewritten)
```

The common layer MUST dispatch to a provider owner. It MUST NOT:
- Call `ManagedCodexService` for a `claude_headless:*` ID.
- Infer provider from UI labels, agent kind, or adapter name strings.
- Claim a capability that the selected provider owner does not implement.

### 3.2 Codex-Owned Semantics

Codex retains exclusive ownership of:

```
- App-server JSON-RPC initialization protocol
- Turn protocol: one-active-turn enforcement, turn identity
- Thread/turn/item identity (Codex-native IDs)
- Codex-native event decoding (JSON-RPC notifications)
- Completion/error outcome classification
- Codex resume/cancel behavior
- Codex prompt contract: ManagedCodexService.SubmitPrompt
```

The common layer MUST NOT:
- Decode Codex JSON-RPC frames or classify Codex errors.
- Invent a "generic" prompt API and claim Codex supports it.
- Infer Codex turn state from Transcript segments.

### 3.3 Claude-Owned Semantics

Claude retains exclusive ownership of:

```
- Stream-json decoding and version pinning
- One-shot, resume, hook, and tool_deferred behavior
- Claude session identity (Claude-native session ID)
- Content-block identity (Claude-native block IDs)
- Approval delivery/consumption witnesses
- Claude-specific termination and resume outcomes
- Claude observation-only mode (no prompt, read-only Transcript)
```

The common layer MUST NOT:
- Decode Claude stream-json frames or classify Claude message types.
- Expose a generic prompt box for Claude sessions until R6 proves a safe
  write contract.
- Claim Claude supports `SubmitPrompt` or one-active-turn semantics.
- Invent Claude tool approval delivery without Claude-specific witnesses.

### 3.4 Provider Dispatch Table

```
                    │  Create      │  Status   │  Events   │  Prompt   │  Lifecycle│  Terminal │  Transcript
────────────────────┼──────────────┼───────────┼───────────┼───────────┼───────────┼───────────┼────────────
controlled_pty:*    │ OwnedPTY     │ Catalog   │ (none)    │ (none)    │ OwnedPTY  │ Transport │ TranscriptSvc
codex_app_server:*  │ ManagedCS    │ Catalog   │ ManagedCS │ ManagedCS │ ManagedCS │ (none)    │ TranscriptSvc
                    │ (IPC ONLY)   │           │           │           │           │           │
claude_headless:*   │ ManagedCl    │ Catalog   │ (NONE)    │ (NONE)    │ ManagedCl │ (none)    │ TranscriptSvc
                    │ (HTTP + IPC) │           │           │           │           │           │
tmux:* / cmux:*     │ Adapter      │ Adapter   │ (none)    │ (none)    │ UNSUPPORT │ Adapter   │ Adapter
```

`(NONE)` = endpoint returns 404 or capability-not-supported. These MUST be
implemented before `DOGFOOD READY` can be restored (R4 for Codex events,
R5 for Claude events, R6 for Claude prompt).

---

## 4. Capability States

Every session carries a closed set of capability states. The server is the
sole authority. The client MUST NOT infer capabilities from adapter labels.

### 4.1 Per-Session Capabilities

| Capability | Meaning | Applicable To |
|------------|---------|---------------|
| `liveTerminal` | Raw PTY output via WebSocket | `controlled_pty:*`, legacy adapters |
| `live_stream` | Real-time event stream available | All |
| `history` | Transcript history readable | All |
| `managedLifecycle` | Stop/Kill/Delete via LifecycleService | `controlled_pty:*`, `codex_app_server:*`, `claude_headless:*` |
| `input` | Terminal input via WebSocket write | `controlled_pty:*` (when principal has `terminal:input` permission) |
| `prompt` | Provider-native prompt submission | `codex_app_server:*` (Claude: UNAVAILABLE until R6) |
| `structuredEvents` | Bounded managed event stream | `codex_app_server:*` (Claude: UNAVAILABLE until R5) |

### 4.2 Transcript Availability States

Per R3 specification, the Transcript surface must distinguish:

| State | Meaning | UI Treatment |
|-------|---------|-------------|
| `healthy` | Transcript is current and populated | Normal rendering |
| `healthy_empty` | Transcript is current but empty (idle agent, no output yet) | "Waiting for output..." — NOT an error |
| `provider_projection_unavailable` | Provider does not support Transcript projection | "Transcript not available for this session type" |
| `temporarily_unavailable` | Transport/poller cannot reach the daemon | "Transcript temporarily unavailable" + retry |
| `gap_or_degraded` | Events were dropped or source is degraded | Explicit gap markers; never silently merged |
| `byte_stream_suppressed_after_input` | PTY Transcript suppressed after input (terminal mode only) | Neutral informational banner, NOT red/error |
| `unauthorized` | Principal lacks read permission | "Transcript not authorized" |
| `session_or_generation_stale` | Session ended or generation replaced | "Session ended" or "New generation active" |

No state may invent an event or present unavailable managed output as a
healthy empty conversation.

### 4.3 Input Capability States

| State | Meaning | UI Treatment |
|-------|---------|-------------|
| `input_owner` | This client is the active input owner | Full input UI enabled |
| `input_readonly` | Another client owns input; this client observes | Input disabled + " observing" indicator |
| `input_unavailable` | Session does not support input | No input UI |
| `prompt_enabled` | Provider-native prompt available | Prompt box enabled |
| `prompt_unavailable` | Provider does not support prompt (e.g. Claude pre-R6) | Prompt box disabled + reason |
| `prompt_turn_active` | A turn is already in progress | Prompt box disabled + "turn running" |
| `delivery_unknown` | Input was sent but ACK not received | Input preserved + "delivery unknown" indicator |

---

## 5. CLI Mode Selection

### 5.1 Explicit Mode Flag

```
pokit run [--mode terminal|managed] [--profile <name>] [-- <args>]
pokit watch <session-id>     # local Terminal observer (terminal mode only)
pokit follow <session-id>    # local Transcript observer (managed mode only)
```

- `--mode terminal` (default for `run`): creates `controlled_pty:*` session,
  attaches local terminal via IPC subscriber.
- `--mode managed`: creates managed session (`codex_app_server:*` or
  `claude_headless:*` depending on `--profile`).
- `--mode` is REQUIRED when `--profile` could be ambiguous (e.g. `codex`
  profile currently routes to `controlled_pty` in HTTP, managed in IPC).
- Without `--mode`, `pokit run` defaults to `terminal` with a compatibility
  warning printed to stderr.

### 5.2 Profile → Mode Mapping (after R1)

| Profile | `--mode terminal` | `--mode managed` |
|---------|-------------------|------------------|
| `shell` (default) | `controlled_pty:*` shell | **REJECTED** — shell is terminal-only |
| `codex` | **REJECTED** — Codex is managed-only | `codex_app_server:*` via `ManagedCodexService` |
| `claude` | **REJECTED** — Claude is managed-only | `claude_headless:*` via `ManagedClaudeService` |
| custom executable | `controlled_pty:*` custom | **REJECTED** — custom executables are terminal-only |

### 5.3 Migration Path

1. R1: `--mode` flag introduced. Without it, `pokit run` prints:
   `pokit: --mode will be required in a future release (defaulting to 'terminal')`
2. R8 (post-remediation): `--mode` becomes required. Missing → error + usage.
3. `pokit run codex` (legacy profile-as-positional) is REMOVED — use
   `pokit run --mode managed --profile codex`.

### 5.4 `watch` vs `--follow`

- `pokit watch <session-id>`: local Terminal observer for `controlled_pty:*`.
  Connects via IPC subscriber (`sub:<session-id>`). Receives raw PTY bytes.
  Read-only by default; input requires explicit `--input` flag.
- `pokit follow <session-id>`: local Transcript observer for managed sessions.
  Polls the same bounded structured event cursor as mobile. Renders
  provider-native Transcript segments. No PTY, no ANSI, no keyboard input.
- Both are supported. They serve different modes and different surfaces.
- `pokit watch` on a managed session → error: "use pokit follow for managed sessions".
- `pokit follow` on a terminal session → error: "use pokit watch for terminal sessions".

---

## 6. Mobile Mode Selection

### 6.1 New Session Screen

The mobile "New Session" surface MUST present two explicit modes:

```
┌─────────────────────────────────┐
│  New Session                     │
│                                  │
│  ○ Terminal                      │
│     Shell, custom commands       │
│     Raw terminal output          │
│                                  │
│  ○ Managed                       │
│     Codex · Claude               │
│     Structured transcript         │
│                                  │
│  [Profile:  ▼ shell/codex/...]  │
│  [Name:     __________________]  │
│  [CWD:      __________________]  │
│                                  │
│  [Create]                        │
└─────────────────────────────────┘
```

- Mode selection determines the adapter prefix and available profiles.
- "Terminal" mode → only `controlled_pty:*` profiles (shell, custom).
- "Managed" mode → only managed profiles (codex, claude).
- The mode is sent to the server as an explicit `mode` field:
  `{"profileId": "...", "mode": "terminal" | "managed", "name": "...", "cwd": "..."}`.
- Server REJECTS mismatches (e.g. `mode=terminal` + `profileId=codex`).

### 6.2 Session List Routing

`FeedScreen.tsx:97` already routes by adapter prefix. After R1:

```
controlled_pty:*    → LegacyFeedScreen (Terminal WebView + Transcript tab)
codex_app_server:*  → ManagedSessionView (Transcript primary, prompt if available)
claude_headless:*   → ManagedSessionView (Transcript primary, prompt disabled pre-R6)
tmux:* / cmux:*     → LegacyFeedScreen (Terminal WebView)
```

The routing MUST NOT infer from `agentKind`, `agentStatus`, or any UI label.
It MUST use the canonical adapter prefix only.

---

## 7. One-Writer Policy for Multi-Client Terminal Input

### 7.1 Problem

`controlled_pty:*` sessions can be observed by multiple clients simultaneously
(mobile WebView + local `pokit watch`). The `Recorder` is the sole PTY reader.
Observers receive output through `Transport.SubscriberFanOut`. But two clients
writing input simultaneously produces interleaved bytes at the PTY.

### 7.2 Policy

**Exactly one client is the input owner at any time.** All other clients are
read-only observers.

- The creating client is the initial input owner.
- Ownership is explicit: `POST /api/sessions/{id}/claim-input` requests
  ownership transfer. The current owner's client receives a "input ownership
  transferred" event.
- Ownership expires after N seconds of inactivity (configurable, default 30s).
  Expiry returns the session to "no owner" state — next write attempt claims
  ownership implicitly (first-write-wins).
- A write from a non-owner client is rejected with 409 + current owner
  identity (device ID + client type).
- Owner disconnect (WebSocket close, app background) releases ownership
  immediately.
- The mobile UI shows a clear "Input: You" or "Input: <other>" indicator.
- `pokit watch --input` requests input ownership explicitly.

### 7.3 Not in Scope (Deferred)

- Automatic writer arbitration / background ownership transfer.
- Writer priority or preemption.
- Multi-writer merge or turn-taking protocol.

---

## 8. Rejected Patterns (REJECT)

These patterns are explicitly rejected and MUST NOT appear in any
implementation:

1. **Adapter choice based on labels or inferred agent kind.**
   Routing by `agentKind === "Claude"` or similar is forbidden. Only
   canonical adapter prefix (`controlled_pty:`, `codex_app_server:`,
   `claude_headless:`) determines behavior.

2. **Managed-to-PTY hidden fallback.**
   A managed session (`codex_app_server:*` or `claude_headless:*`) MUST
   NEVER open a PTY, WebSocket terminal, or raw byte stream. If a client
   requests `/term/ws?session=codex_app_server:...`, the server MUST return
   an error, not a terminal.

3. **PTY-to-managed hidden fallback.**
   A `controlled_pty:*` session MUST NEVER be routed to the managed prompt
   or managed events endpoints. Its Transcript is byte-stream-derived and
   permanently suppressed after input.

4. **Lowest-common-denominator prompt/resume API.**
   There is no `SubmitPrompt` that works for both Codex and Claude. Each
   provider has its own contract. A "generic" prompt that maps to different
   provider protocols with different failure modes is forbidden.

5. **Silent default changes.**
   No existing CLI command or mobile flow may change behavior without an
   explicit mode selection or a documented compatibility transition with
   a deprecation warning period.

6. **Capability claims without provider implementation.**
   The server MUST NOT advertise `prompt` capability for Claude sessions
   until R6 proves a safe write contract. The server MUST NOT advertise
   `structuredEvents` for Claude sessions until R5 implements the Claude
   event store and normalizer.

---

## 9. Implementation Sequence After ACCEPT

1. Add `mode` field to `createSessionRequest` (Go) and `createSession` (TS).
2. Implement server-side mode validation (reject terminal+codex, managed+shell).
3. Add `--mode` flag to CLI.
4. Add mobile New Session mode selector UI.
5. Implement one-writer policy on `TerminalTransport`.
6. Add capability states to SessionTelemetry.
7. Route managed prompt/events by adapter prefix (replace Codex-hardcoded dispatch).
8. Freeze capability vocabulary and add server-side enforcement.

No implementation begins before this contract receives independent ACCEPT.

---

**Contract frozen:** 2026-07-25
**Next:** R2 — Terminal-first interactive product path (after R1 ACCEPT)
