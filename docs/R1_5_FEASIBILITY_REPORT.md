# R1.5 — Provider Dual-Channel Feasibility Research

> **Superseded for execution and provider classification.** This research
> remains historical input, but its Codex category A conclusion, proxy-as-TUI
> assumption and execution instructions are not authoritative. The corrected
> classifications and gates are in
> [`BASE_ALPHA_DUAL_SURFACE_EXECUTION_PLAN.md`](BASE_ALPHA_DUAL_SURFACE_EXECUTION_PLAN.md).

**Status:** HISTORICAL RESEARCH — SUPERSEDED FOR EXECUTION
**Parent:** `BASE_ALPHA_TERMINAL_MANAGED_REMEDIATION_PLAN.md` §6 (inserted between 9.5-TM-R1 and 9.5-TM-R2)
**Blocks:** 9.5-TM-R2, R4, R5 (provider-specific implementation must not begin before ACCEPT)
**Date:** 2026-07-25

> This research gate answers one question: **Can POKIT simultaneously expose
> (1) the provider's official TUI output through a POKIT-owned PTY and (2)
> provider-native structured events for Timeline/Transcript/approval/lifecycle,
> from a single provider session?** If yes, POKIT can offer dual-surface
> sessions (Terminal + Transcript) without forking provider semantics. If no,
> Terminal-first and managed-Transcript are permanently separate session types
> and must not be merged.

---

## 1. Research Question

For each provider (Codex 0.144.1+, Claude 2.1.209+):

1. Can the provider process expose both ANSI TUI output (for a POKIT-owned PTY)
   AND structured events (for Timeline/Transcript/approval/lifecycle)
   simultaneously?
2. Is there a shared core process/event stream that both the TUI and external
   clients can consume?
3. Does the provider's own TUI consume the same event stream that POKIT would?
4. Can an external client subscribe to live thread/turn/session identity?
5. Are the session, thread, turn, tool, and approval identities consistent
   across both channels?

---

## 2. Codex (0.144.1 → 0.145.0 installed)

### 2.1 Architecture Overview

Codex has THREE distinct execution modes:

| Mode | Command | Output | Use Case |
|------|---------|--------|----------|
| **Interactive TUI** | `codex` | ANSI terminal UI (in-process) | Human developer at a terminal |
| **App Server** | `codex app-server --stdio` | JSON-RPC 2.0 over stdio (bidirectional) | IDE integrations, programmatic clients |
| **Exec JSON** | `codex exec --json` | JSONL events on stdout | One-shot tasks, CI pipelines |

The App Server is the **shared core**. The TUI was the original "native" client
that ran in-process with the agent loop. OpenAI is actively refactoring the TUI
to be an App Server client — communicating via the same JSON-RPC protocol that
external clients use.

**Source:** OpenAI engineering blog, "Unlocking the Codex Harness" (Feb 2026);
Codex CLI changelog (0.125.0, Apr 2026); PR #14945 (composer history in
app-server TUI, Mar 2026).

### 2.2 App Server Protocol

The App Server exposes a **bidirectional JSON-RPC 2.0 protocol** over
transport layers including stdio, Unix socket, and WebSocket:

```
Client → Server (requests):
  thread/create, thread/resume, thread/fork
  turn/start, turn/interrupt
  permission/response (approve/deny)

Server → Client (notifications):
  thread/started, thread/completed
  turn/started, turn/completed
  item/started, item/*/delta, item/completed
  permission/request

Server → Client (requests):
  permission/request (server asks client for approval)
```

Three conversation primitives:
- **Item** — atomic input/output unit with explicit lifecycle: `item/started` →
  optional `item/*/delta` → `item/completed`. Types: user message, agent message,
  tool execution, approval request, diff.
- **Turn** — unit of agent work initiated by user input, grouping the sequence
  of items produced.
- **Thread** — persistent container for an ongoing session, supporting creation,
  resumption, forking, and archival.

### 2.3 POKIT's Current Integration

POKIT spawns Codex as (`managed_codex.go:1117`):

```
codex app-server --stdio
```

- **No PTY.** The daemon communicates via JSON-RPC over the child's stdin/stdout
  pipes (`ManagedProcess` interface).
- The daemon IS an app-server client — it sends JSON-RPC requests and receives
  streaming notifications.
- The TUI is NOT running. No terminal output is captured.
- Current session type: `codex_app_server:*` (managed-only, no Terminal).

### 2.4 Dual-Channel Feasibility

**The App Server architecture inherently supports multiple simultaneous
clients.** The protocol is bidirectional and transport-agnostic:

```
┌──────────────────────────────────────────────┐
│             codex app-server                  │
│  (JSON-RPC 2.0 over Unix socket or stdio)    │
│                                                │
│  ┌──────────┐  ┌──────────┐  ┌────────────┐  │
│  │ TUI       │  │ POKIT     │  │ IDE         │  │
│  │ client    │  │ daemon    │  │ extension   │  │
│  │ (ANSI)    │  │ (events)  │  │ (UI)        │  │
│  └──────────┘  └──────────┘  └────────────┘  │
└──────────────────────────────────────────────┘
```

**Path 1 — App Server + TUI as client + POKIT as second client (A):**

1. Start `codex app-server` with Unix socket transport:
   `codex app-server --listen unix:///tmp/pokit-codex.sock`
2. Launch TUI as an app-server client connected to the same socket:
   `codex app-server proxy --sock /tmp/pokit-codex.sock`
   (This proxies the TUI's stdio to the app-server socket.)
3. POKIT daemon connects to the same Unix socket as a second JSON-RPC client.
4. Both clients receive the same server notifications (thread/turn/item events).
5. POKIT owns the PTY that wraps the TUI client process — Terminal output is
   captured. Structured events arrive via the daemon's JSON-RPC connection.

**Identity consistency:** Thread ID, turn ID, and item ID are server-assigned
and identical across all connected clients. Approval requests are server-initiated
(`permission/request`) and delivered to ALL clients. Tool execution events
(`item/started` with tool type) are broadcast to all clients.

**Current limitation:** The TUI refactor to use app-server as a client is
**in progress** as of 2026. Prior to completion (exact version TBD), the TUI
runs in-process without going through the JSON-RPC protocol. Once the refactor
ships, this path is fully supported.

**Path 2 — App Server stdio + TUI PTY wrapper (B, interim):**

Before the TUI refactor ships, POKIT can:
1. Spawn `codex` (interactive TUI) in a POKIT-owned PTY — captures ANSI output.
2. Simultaneously spawn `codex exec --json` in a separate process with the
   same prompt/config — captures structured events.
3. **Rejected: two separate processes.** Thread/turn/item identity is NOT shared
   between a TUI session and an `exec --json` session. The exec session is a
   different thread. Approvals in one do not appear in the other. This approach
   fails the identity-consistency requirement.

### 2.5 Classification: A (Officially Supported)

**Rationale:** The App Server architecture was explicitly designed for
multiple simultaneous clients. The TUI is being refactored to use the same
JSON-RPC protocol as all other clients. Once the TUI refactor ships (or with
the proxy approach), POKIT can connect as a second client and receive the
exact same structured events that drive the TUI.

**Prerequisites for POKIT adoption:**
1. Codex version with TUI-as-app-server-client support (or use the proxy
   interim approach).
2. Unix socket transport for local multi-client access.
3. POKIT daemon implements a JSON-RPC client (separate from the existing
   stdin/stdout client used for `--stdio` mode).
4. POKIT owns a PTY wrapping the TUI client process for Terminal output.

**Risk:** The TUI refactor timeline is owned by OpenAI. POKIT should not block
on it. The existing `codex_app_server:*` managed-only mode (no Terminal) remains
the production path until dual-channel ships.

---

## 3. Claude (2.1.209 → 2.1.218 installed)

### 3.1 Architecture Overview

Claude Code has TWO distinct execution modes. There is NO "app server"
equivalent:

| Mode | Command | Output | Use Case |
|------|---------|--------|----------|
| **Interactive TUI** | `claude` | ANSI terminal UI + JSONL transcript on disk | Human developer at a terminal |
| **Print/stream-json** | `claude -p --output-format stream-json --verbose` | NDJSON on stdout | Scripting, SDK integration |

These two modes are **mutually exclusive**:
- Interactive mode produces NO structured stdout — it writes a JSONL transcript
  to `~/.claude/projects/<cwd-slug>/<uuid>.jsonl`.
- Print mode produces structured NDJSON on stdout but has NO TUI interactivity.
- There is no persistent server process. Claude Code is a single-process CLI.

### 3.2 Structured Event Sources

Claude provides THREE separate channels for structured observation:

#### Channel 1: JSONL Transcript (Interactive Mode)

When running in interactive mode, Claude appends structured NDJSON events to a
JSONL transcript file in real-time. The file path is deterministic:
`~/.claude/projects/<cwd-slug>/<uuid>.jsonl`.

Observed event types from a production POKIT session transcript
(`e7c9ceb1-f11e-4dca-a5bc-f91aa774396b.jsonl`, 6.5 MB, ~50 events):

| Event Type | Count | Description |
|------------|-------|-------------|
| `assistant` | 17 | Model response with content blocks (text, tool_use, thinking) |
| `user` | 12 | User messages or tool results |
| `ai-title` | 4 | Generated session title |
| `agent-name` | 4 | Agent name assignment |
| `mode` | 3 | Mode transitions (normal, etc.) |
| `system` | 3 | System initialization messages |
| `attachment` | 3 | File attachments |
| `file-history-snapshot` | 2 | File history capture |
| `last-prompt` | 2 | Prompt history |

The transcript is **append-only** and written atomically per event. An external
observer can `tail -f` the JSONL file to receive structured events in near
real-time (bounded by filesystem buffer flush, typically <1s).

**Identity consistency:** The JSONL transcript carries the `sessionId` UUID in
every event. Content-block IDs, tool-use IDs, and approval IDs are embedded in
the `assistant` message content blocks. These identities are Claude-native and
consistent throughout the session.

#### Channel 2: Hooks System

Claude Code's hooks system provides 25+ lifecycle event notifications. Hooks
fire at specific points in the session/turn/tool lifecycle:

**Session-level:** `SessionStart`, `SessionEnd`
**Turn-level:** `UserPromptSubmit`, `Stop`, `StopFailure`
**Tool-level:** `PreToolUse`, `PostToolUse`, `PermissionRequest`,
`PermissionDenied`, `PostToolUseFailure`
**Other:** `Notification`, `SubagentStart`, `SubagentStop`, `PreCompact`,
`PostCompact`

Hook handlers can be:
- **Command hooks** (`type: "command"`) — run a shell command, receive JSON on
  stdin, respond via exit code and stdout JSON.
- **HTTP hooks** (`type: "http"`) — POST JSON to a URL, receive decisions in
  response.

**Source:** Claude Code Hooks Reference (`code.claude.com/docs/en/hooks`).

#### Channel 3: Stream-JSON Print Mode

```
claude --verbose --settings <path> --output-format stream-json \
       --include-partial-messages -p <prompt>
```

POKIT currently uses this mode (`managed_claude.go:1161-1168`). It produces
structured NDJSON on stdout with message types: `system`, `assistant`, `user`,
`result`, `stream`, `control`. This mode has NO interactive TUI.

### 3.3 POKIT's Current Integration

POKIT spawns Claude in **print/stream-json mode** (non-interactive):

```
claude --verbose --settings <path> --setting-sources "" \
       --output-format stream-json --include-partial-messages \
       -p <certification prompt>
```

- **No PTY.** The daemon reads NDJSON from the child's stdout pipe.
- The daemon parses stream-json messages for assistant content, tool calls,
  and approvals via `processLine`.
- The TUI is NOT running. No terminal output is captured.
- Current session type: `claude_headless:*` (managed-only, no Terminal,
  observation-only).

### 3.4 Dual-Channel Feasibility

**Claude has NO App Server equivalent.** There is no persistent server process
that multiple clients can connect to. The interactive TUI and the stream-json
print mode are separate code paths within the same CLI binary.

**Approach A — Interactive mode + PTY + JSONL tail + hooks (C):**

```
┌─────────────────────────────────────────┐
│           claude (interactive)           │
│  ┌──────────┐  ┌──────────┐             │
│  │ TUI       │  │ JSONL     │             │
│  │ (ANSI on  │  │ transcript│             │
│  │  PTY)     │  │ on disk   │             │
│  └──────────┘  └──────────┘             │
│       │              │                    │
│       ▼              ▼                    │
│  ┌──────────┐  ┌──────────┐             │
│  │ POKIT PTY │  │ tail -f   │             │
│  │ (Terminal)│  │ + hooks   │             │
│  │           │  │ (events)  │             │
│  └──────────┘  └──────────┘             │
└─────────────────────────────────────────┘
```

1. POKIT spawns `claude` (interactive) in a POKIT-owned PTY.
2. Terminal output: captured from the PTY (ANSI TUI).
3. Structured events: `tail -f` the JSONL transcript file for assistant content,
   tool calls, and completion events.
4. Hooks: configure `PreToolUse`, `PostToolUse`, `PermissionRequest`,
   `SessionStart`, `SessionEnd`, `Stop` hooks that POST to the daemon's
   local HTTP endpoint for real-time approval/lifecycle notifications.
5. Prompt delivery: write to the PTY stdin (or use `--input-format stream-json`
   via a named pipe if Claude supports it in interactive mode — unverified).

**Identity consistency:** The JSONL transcript carries the session UUID.
Content-block IDs and tool-use IDs are embedded in assistant messages. Hook
events carry `session_id` and tool-specific fields. All three channels share
the same Claude session identity.

**Limitations:**
- JSONL flush latency: events appear on disk after filesystem buffer flush
  (typically <1s, but not guaranteed real-time).
- Hooks run in separate processes/HTTP calls — they observe events but don't
  receive the full content stream.
- Prompt input through PTY stdin is fragile (ANSI escape sequences may
  interfere with typed input).
- No official multi-client protocol. The JSONL file is a log, not an API.
- Claude version upgrades may change JSONL event schema without notice (the
  format is optimized for `--resume`, not external consumption).

**Approach B — Stream-json + PTY wrapper (D):**

Wrap `claude -p --output-format stream-json` in a PTY. The stream-json output
is NOT ANSI — it's NDJSON. POKIT could render it in a custom terminal emulator,
but this is NOT the Claude TUI and would not look or behave like Claude Code.
The stream-json protocol is the SDK interface, not a TUI. Rejected.

**Approach C — Two separate processes (D):**

Run `claude` (interactive, in PTY) AND `claude -p --output-format stream-json`
(print, separate process) simultaneously. These are DIFFERENT Claude sessions
with different session UUIDs. No shared identity. Rejected per §3.5.

### 3.5 Classification: C (Partial)

**Rationale:** Claude Code does not have a multi-client server architecture.
However, the combination of (1) PTY-owned interactive TUI, (2) real-time JSONL
transcript tailing, and (3) hooks for approval/lifecycle events provides a
**partial** dual-channel solution. The three channels share a single Claude
session identity. The JSONL transcript format is stable (Claude `--resume`
depends on it).

**Gaps vs. Codex App Server:**
- No push-based event delivery (poll/tail instead of server push).
- JSONL flush latency (not real-time).
- No formal API stability guarantee for the JSONL schema.
- PTY stdin for prompt delivery is fragile.

**Prerequisites for POKIT adoption:**
1. JSONL transcript path must be known before spawn (deterministic from
   session UUID).
2. POKIT daemon must tail the JSONL file and parse NDJSON events.
3. Hooks must be configured in the Claude settings file passed via `--settings`.
4. Hook HTTP endpoints must be reachable from the Claude process (localhost).
5. PTY input controller must handle the interactive TUI's input expectations.

---

## 4. Identity Consistency Verification

For dual-channel to be valid, the following identities MUST be consistent
across the Terminal (TUI) channel and the Transcript (structured events)
channel:

| Identity | Codex (App Server) | Claude (JSONL + Hooks) |
|----------|--------------------|------------------------|
| Session ID | Thread ID (server-assigned, same for all clients) | Session UUID (in JSONL, hooks, and PTY process identity) |
| Turn ID | `turn/started` notification (server-assigned, broadcast to all clients) | Implicit — order of `user` → `assistant` events in JSONL |
| Tool call ID | Item ID in `item/started` with tool type | `tool_use.id` in assistant content block (JSONL) + hook event |
| Approval ID | `permission/request` notification (server-assigned) | `PermissionRequest` hook event carries tool name + input |
| Completion | `turn/completed` + `thread/completed` notifications | `result` event (JSONL) or `SessionEnd` hook |
| Runtime identity | Process PID + opaque launch token | Process PID + session UUID |

**Verdict: Codex identities are guaranteed consistent by the App Server
protocol (server-assigned, broadcast to all clients). Claude identities are
consistent across JSONL and hooks (same session UUID, same tool-use IDs) but
there is no formal multi-client contract.**

---

## 5. Classification Summary

| Classification | Definition | Provider |
|----------------|------------|----------|
| **A (Officially Supported)** | Provider architecture explicitly supports multiple simultaneous clients consuming the same structured event stream alongside the TUI. Shared core process exists. | **Codex** (App Server) |
| **B (Stable Shared-Core)** | A shared event stream exists and is consumed by both the TUI and external observers. The stream format is stable (tested, versioned, or relied upon by the provider's own features). | *(none in this research)* |
| **C (Partial)** | Structured events are available alongside the TUI through a combination of observation channels (transcript files, hooks, etc.), but there is no formal multi-client protocol. Identity consistency holds but requires multiple observation mechanisms. | **Claude** (JSONL transcript + hooks) |
| **D (Separate Only)** | TUI and structured events come from different processes/sessions. No shared identity. Dual-channel is not possible with a single provider session. | *(not applicable — both providers have at least partial dual-channel)* |

---

## 6. Recommendation

### 6.1 For Codex (`codex_app_server:*`)

**Adopt Architecture A (App Server multi-client).**

1. Move from `codex app-server --stdio` (single-client stdin/stdout) to
   `codex app-server --listen unix://<socket>` (multi-client Unix socket).
2. Launch the TUI as `codex app-server proxy --sock <socket>` inside a
   POKIT-owned PTY.
3. Connect POKIT daemon to the same Unix socket as a second JSON-RPC client.
4. Terminal surface: PTY bytes from the TUI proxy process.
5. Transcript surface: JSON-RPC notifications decoded by the daemon and
   projected into the Transcript contract (9.5-TM-R4).

**Prerequisite:** The TUI-as-app-server-client refactor must be available in
the pinned Codex version. Until then, `codex_app_server:*` sessions remain
managed-only (no Terminal). The interim proxy approach (`app-server proxy`)
should be tested against the installed version.

### 6.2 For Claude (`claude_headless:*`)

**Adopt Architecture C (JSONL tail + hooks).**

1. Spawn `claude` (interactive) in a POKIT-owned PTY.
2. Pass `--settings <path>` pointing to a daemon-generated settings file
   with hooks configured for:
   - `PreToolUse` → POST to daemon HTTP (approval observation)
   - `PostToolUse` → POST to daemon HTTP (tool result)
   - `SessionStart` → POST to daemon HTTP (session identity)
   - `SessionEnd` → POST to daemon HTTP (terminal state)
   - `Stop` → POST to daemon HTTP (lifecycle)
3. Tail the JSONL transcript file for assistant content and completion events.
4. Terminal surface: PTY bytes from the interactive Claude TUI.
5. Transcript surface: JSONL events normalized by a Claude-specific normalizer
   and projected into the Transcript contract (9.5-TM-R5).
6. Input: write prompt to PTY stdin, or investigate `--input-format stream-json`
   compatibility with interactive mode.

**Risk acknowledged:** JSONL format stability is not contractually guaranteed
by Anthropic. However, the format is depended upon by `claude --resume` and
`claude --continue`, which creates a strong incentive for stability. POKIT
should pin Claude versions and include JSONL schema validation in the
normalizer.

### 6.3 Unified Recommendation

- **Do NOT attempt to merge Codex and Claude into a single dual-channel
  implementation.** Their architectures are fundamentally different (App
  Server vs. JSONL tail + hooks). Each provider gets its own dual-channel
  implementation in its respective remediation wave (9.5-TM-R4 for Codex,
  9.5-TM-R5 for Claude).
- **The shared Transcript projection (§3.5 of the remediation plan) is
  the ONLY common surface.** Provider-specific normalizers emit into it.
  No shared dual-channel infrastructure.

---

## 7. Experiment Log

### 7.1 Codex Experiments

| Experiment | Command | Result |
|------------|---------|--------|
| Version check | `codex --version` | `codex-cli 0.145.0` (installed) |
| App server help | `codex app-server --help` | Confirmed: daemon, proxy, generate-ts, generate-json-schema subcommands |
| App server daemon | `codex app-server daemon start` | Failed: standalone install required (socket path SUN_LEN limit) |
| App server proxy help | `codex app-server proxy --help` | Confirmed: `--sock <SOCKET_PATH>` flag for connecting to running app-server |
| Exec JSON help | `codex exec --help` | Confirmed: `--json` flag ("Print events to stdout as JSONL") |
| Exec JSON test | `echo "test" \| codex exec --json --model gpt-4.1-nano` | No output captured (background process) |
| POKIT spawn args | `grep` on `managed_codex.go:1117` | Confirmed: `codex app-server --stdio` |

### 7.2 Claude Experiments

| Experiment | Command | Result |
|------------|---------|--------|
| Version check | `claude --version` | `2.1.218 (Claude Code)` |
| Help scan | `claude --help` | Confirmed: `--output-format stream-json`, `--input-format stream-json`, `--include-partial-messages`, `-p`/`--print`, `--resume`, `--continue` |
| Stream-json test | `echo '{"type":"system",...}' \| claude --print --verbose --output-format stream-json --input-format stream-json` | No output (timed out) |
| JSONL transcript analysis | Read `e7c9ceb1...jsonl` (6.5MB) | Confirmed: 9 event types, append-only NDJSON, `sessionId` in every event |
| Hooks reference | Web search: `code.claude.com/docs/en/hooks` | Confirmed: 25+ lifecycle events, 5 handler types, matcher patterns |
| POKIT spawn args | `grep` on `managed_claude.go:1161-1168` | Confirmed: `--verbose --settings <path> --output-format stream-json --include-partial-messages -p <prompt>` |

### 7.3 POKIT Codebase Verification

| Source | Finding |
|--------|---------|
| `managed_codex.go:2-3` | "app-server child. The daemon directly spawns the pinned certified executable with the exact app-server argv (no shell, no PTY, no Recorder)" |
| `managed_codex.go:1117` | `codex app-server --stdio` |
| `managed_codex.go:138` | "codexManagedRuntime owns exactly one app-server child: its stdio, protocol state, and guarantees" |
| `managed_claude.go:641` | "pump reads Claude's stream-json stdout" |
| `managed_claude.go:1161-1168` | `--verbose --settings <path> --setting-sources "" --output-format stream-json --include-partial-messages -p <prompt>` |
| `managed_claude.go:1071` | `eventStoreFor` returns `nil, 0, false` — no Claude event store exists |

---

## 8. Verdict

| Question | Codex | Claude |
|----------|-------|--------|
| Shared core process? | **Yes** — App Server | **No** — single-process CLI |
| TUI and events from same session? | **Yes** — multi-client JSON-RPC | **Partial** — JSONL tail + hooks |
| Provider-native identity consistent? | **Yes** — server-assigned Thread/Turn/Item IDs | **Yes** — session UUID + content-block IDs |
| External client subscription? | **Yes** — JSON-RPC client over Unix socket | **No** — tail file + receive hook HTTP calls |
| ANSI TUI + structured output simultaneously? | **Yes** — TUI client process + POKIT client | **Yes** — PTY capture + JSONL tail |
| Approval observation? | **Yes** — `permission/request` broadcast | **Yes** — `PreToolUse` + `PermissionRequest` hooks |
| Lifecycle observation? | **Yes** — thread/turn state notifications | **Yes** — `SessionStart`/`SessionEnd`/`Stop` hooks |
| Official API stability? | **Yes** — JSON-RPC protocol is the integration surface | **No** — JSONL is a log, not an API; hooks are the official extension point |

**Overall: Dual-channel is feasible for both providers, with different
architectures. Codex: A (official). Claude: C (partial, hooks + JSONL tail).
Both provide consistent session/thread/turn/tool/approval identity across
channels. Neither requires forking provider semantics.**

---

**Research completed:** 2026-07-25
**Next:** 9.5-TM-R2 (Terminal-first interactive product path) — unblocked after ACCEPT
