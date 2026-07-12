# R2 Multi-agent Expansion Research — Final Report

Status: **COMPLETE — research only**

Branch: `feature/phase10-multi-adapter`
Research date: 2026-07-12
Accepted baselines: T0 (`3ce2604bd`), T1 (`162266f83`), T2 (`ef4a162c7`)

## 1. Multi-agent Source Matrix

### Researched projects

| Project | Version/Date | Strongest Signal | Local Observability | Strategy |
|---|---|---|---|---|
| **Gemini CLI** | 2026-04 (hooks v1) | Hooks (JSON stdin/stdout) + transcript JSON | ~/.gemini/tmp/ session files | Hook adapter |
| **OpenCode** | 2026-05 (SSE + plugins) | HTTP REST + SSE event stream | `opencode serve` local server | Server/protocol adapter |
| **Cline** | v3.36+ (hooks) | PreToolUse/PostToolUse/TaskStart hooks + AgentRuntimeEvents | `.clinerules/hooks/` + SDK events | Hook/plugin adapter |
| **Goose** | 2026-03 (ACP) | ACP JSON-RPC 2.0 (session/new, streaming notifications) | `goose serve` local binary | ACP adapter |
| **OpenHands** | 2026-02 (hooks V1) | PreToolUse/PostToolUse/SessionStart hooks + EventStream | `.openhands/hooks.json` + server | Hook/server adapter |
| **Aider** | 2025-2026 | `.aider.chat.history.md` (markdown) + notifications | PTY + history file parsing | PTY fallback only |
| **Claude Code** (T2) | 2.1.202 | Session JSONL | `~/.claude/projects/` | Accepted T2 |
| **Codex CLI** (T1) | 0.144.1 | Session JSONL | `~/.codex/sessions/` | Accepted T1 |

### Evidence classification

| Project | Structured events | Session correlation | Approval evidence | Status authority | Version drift risk |
|---|---|---|---|---|---|
| **Gemini CLI** | Yes (hooks + transcript) | session_id in hooks, project-hash in path | BeforeTool decision: allow/deny/block | SessionStart/End | MEDIUM — active development, hook schema evolving |
| **OpenCode** | Yes (SSE stream) | session_id from POST /session | permission.asked + reply endpoint | session.status (idle/busy) | MEDIUM — API versioned, plugin system maturing |
| **Cline** | Yes (hooks + SDK events) | taskId in every hook payload | PreToolUse can cancel with errorMessage | TaskStart/Resume/Cancel + Notification | MEDIUM — IDE-integrated; CLI/SDK paths differ |
| **Goose** | Yes (ACP JSON-RPC) | session/new returns session ID | RequestPermission notification | Agent state via streaming | LOW-MEDIUM — ACP is a public spec |
| **OpenHands** | Yes (hooks + EventStream) | conversation_id in API | PreToolUse + UserPromptSubmit hooks | SessionStart/End + EventStream state | MEDIUM — SDK under active restructure |
| **Aider** | No (markdown files only) | Process lifetime (no session ID) | None structured | Notification command only | HIGH — purely file-based, no protocol |
| **Claude Code** | Yes (session JSONL) | sessionId in records | NOT from permission-mode (§9) | From event sequence | LOW — accepted T2 |
| **Codex CLI** | Yes (session JSONL) | session_id in meta | approval_id in event_msg | From event sequence | LOW — accepted T1 |

### Sources

| Project | Primary source | Evidence type |
|---|---|---|
| Gemini CLI | [geminicli.com/docs/hooks/reference](https://geminicli.com/docs/hooks/reference/), [github.com/google-gemini/gemini-cli](https://github.com/google-gemini/gemini-cli) | documented + public source |
| OpenCode | [dev.opencode.ai/docs/plugins](https://dev.opencode.ai/docs/plugins/), [github.com/anomalyco/opencode](https://github.com/anomalyco/opencode) | documented + public source |
| Cline | [docs.cline.bot/sdk/events](https://docs.cline.bot/sdk/events), [cline.bot/blog/cline-v3-36-hooks](https://cline.bot/blog/cline-v3-36-hooks) | documented |
| Goose | [block.github.io/goose](https://block.github.io/goose/), [github.com/block/goose](https://github.com/block/goose) | documented + public source |
| OpenHands | [docs.openhands.dev/sdk/guides/hooks](https://docs.openhands.dev/sdk/guides/hooks), [github.com/OpenHands/OpenHands](https://github.com/OpenHands/OpenHands) | documented + public source |
| Aider | [aider.chat/docs](https://aider.chat/docs/), [github.com/Aider-AI/aider](https://github.com/Aider-AI/aider) | documented |

## 2. R1-to-R2 Research Traceability

### Previously verified (R1) → confirmed or extended in R2

| R1 finding | R2 status | Change |
|---|---|---|
| Claude Code has hooks + stream-json | T2 accepted with session JSONL (not hooks) | Refined: session JSONL is the stable surface; hooks remain separate |
| Codex CLI has app-server JSON-RPC | T1 accepted with session JSONL | Confirmed; app-server path remains deferred |
| OpenCode has HTTP/SSE + plugin events | Richer: full SSE event catalog, session lifecycle, permission.asked, plugin hook system | Extended with concrete API details |
| Cline has task/tool hooks | Richer: 6 hook types + Notification hook + AgentRuntimeEvent SDK | Extended with SDK and event architecture |
| Aider has notifications only | Confirmed: no structured event protocol emerged | No change — remains PTY fallback |
| Goose has ACP + local REST/SSE | Richer: ACP JSON-RPC 2.0 spec, session/new-load-prompt-cancel, streaming notifications, per-session isolation | Extended with full ACP details |

### Newly verified (R2)

| Finding | Evidence |
|---|---|
| Gemini CLI hooks: SessionStart/End, BeforeTool/AfterTool, BeforeAgent/AfterAgent, BeforeModel/AfterModel, Notification, PreCompress | [Hooks reference](https://geminicli.com/docs/hooks/reference/), [Issue #9070](https://github.com/google-gemini/gemini-cli/issues/9070) |
| Gemini CLI transcript: `~/.gemini/tmp/<project-hash>/chats/session-<date>-<shortid>.json` | [Magia issue #295](https://github.com/magiash/magia/issues/295), public source |
| OpenHands hooks: PreToolUse, PostToolUse, UserPromptSubmit, Stop, SessionStart, SessionEnd | [docs.openhands.dev/sdk/guides/hooks](https://docs.openhands.dev/sdk/guides/hooks), [Issue #11943](https://github.com/OpenHands/OpenHands/issues/11943) |
| OpenHands EventStream: pub-sub with AGENT/USER/ENVIRONMENT sources, JSON-persisted with auto-increment IDs | Public source (`openhands/events/stream.py`) |
| Goose ACP: session/new → session ID, session/prompt → streaming notifications (AgentMessageChunk, AgentThoughtChunk, ToolCall, ToolCallUpdate, RequestPermission) | [ACP DeepWiki](https://deepwiki.com/block/goose/3.4-agent-communication-protocol-(acp)), PRs #6392, #7115, #7984 |

### Inference (not observed)

| Claim | Basis | Confidence |
|---|---|---|
| Gemini CLI transcript JSON is stable enough for version-gated adapter | Active development with hook schema evolving; path contains project-hash (stable), format may change | Medium |
| OpenCode SSE stream can be consumed without owning the server | Protocol is REST+SSE; server is launched by `opencode serve`; client connects to known URL | High |
| Cline CLI/SDK path is separable from IDE extension | Public SDK packages (`@clinebot/agents`, `@clinebot/shared`) exist independently | Medium-High |
| Goose ACP is the future stable interface | Team consolidation toward single binary + single protocol; `goosed` being deprecated | High |

### Unresolved assumptions

1. Whether Gemini CLI hooks can fire during ordinary interactive TUI sessions (not just managed/headless launches) — same correlation gap as Claude/Codex
2. Whether OpenCode SSE events carry stable per-session identifiers that survive server restart
3. Whether Cline's hook system works identically in CLI-only mode vs IDE-embedded mode
4. Whether OpenHands EventStream IDs are stable across conversation export/reload

### Traceability map

```
Observation → Design principle → Affected stage → T0/T1/T2/D1/T3/S1/A1

Gemini hooks carry session_id + cwd + timestamp
→ adapter-local session binding, managed-launch required for correlation
→ T3.x Gemini hook adapter
→ T0 correlation = managed_launch

OpenCode SSE stream has session.status + permission.asked
→ authoritative status from provider protocol, approval from permission.asked
→ T3.x OpenCode server adapter
→ T0 provenance = provider_protocol

Goose ACP has RequestPermission + ToolCall state machine
→ authoritative approval from structured RequestPermission
→ T3.x Goose ACP adapter
→ T0 provenance = provider_protocol, approval = authoritative

Aider has only markdown history + notifications
→ PTY structural signal only, no native event protocol
→ T1 generic byte-stream Transcript (already covers this)
→ T0 provenance = pty_structural, no approval

Cline hooks + SDK events cover full tool lifecycle
→ adapter-local mapping to T0 events
→ T3.x Cline plugin adapter
→ T0 provenance = provider_hook

OpenHands hooks + EventStream cover session + tool lifecycle
→ adapter-local mapping to T0 events
→ T3.x OpenHands hook adapter
→ T0 provenance = provider_hook

Gemini transcript JSON at ~/.gemini/tmp/
→ D1 repair scenario: path change, schema migration
→ D1 repair

OpenCode server API versioning
→ D1 repair scenario: endpoint changes, new event types
→ D1 repair

Goose ACP being a public spec
→ D1 repair scenario: spec version bump, method deprecation
→ D1 repair

No provider adds genuinely new semantic categories beyond T0 closed vocabulary
→ T0 contract is sufficient; no revision needed
→ T0 remains frozen
```

## 3. Frozen T0 Fit-Gap Report

### Fully representable by T0

| Concept | Gemini CLI | OpenCode | Cline | Goose | OpenHands | Aider |
|---|---|---|---|---|---|---|
| user_message | transcript/hooks | chat.message | UserPromptSubmit | session/prompt | UserPromptSubmit | .chat.history.md parsing |
| assistant_message | transcript | message.part.delta | assistant-text-delta | AgentMessageChunk | EventStream Observation | .chat.history.md parsing |
| thinking | transcript (THOUGHT_CHUNK) | message.part.delta (reasoning) | — | AgentThoughtChunk | EventStream reasoning | — |
| tool_call_started | BeforeTool hook | message.part.updated | tool-started | ToolCall | PreToolUse hook | .chat.history.md parsing |
| tool_call_finished | AfterTool hook | message.part.updated | tool-finished | ToolCallUpdate | PostToolUse hook | .chat.history.md parsing |
| agent_started | SessionStart hook | session.created | TaskStart | session/new | SessionStart hook | process launch |
| completed | SessionEnd hook | session.status (idle after busy) | run-finished | StopReason (end_turn) | SessionEnd hook | exit event |
| failed | — | session.error | run-failed | Error StopReason | EventStream error | exit event |
| interrupted | — | session/abort | TaskCancel | session/cancel | interrupt/Stop hook | process signal |
| unknown | default | default | default | default | default | default |

### Representable through bounded metadata

| Concept | How |
|---|---|
| Gemini CLI `project-hash` in transcript path | metadata: `gemini_project_hash` |
| OpenCode `session.diff` | metadata: `opencode_diff_count` (bounded) |
| Cline `workspaceRoots` | metadata: `cline_workspace` (redacted path) |
| Goose model/provider config | metadata: `goose_model`, `goose_mode` |
| OpenHands `conversation_id` | metadata: `openhands_conversation_id` |
| All: tool name | metadata: `tool_name` (T0 already supports this) |

### Safely handled inside version-specific adapter

| Concept | Adapter-local handling |
|---|---|
| Gemini CLI hook decision (allow/deny/block) | Adapter maps `decision: "block"` → advisory degradation; not approval authority |
| OpenCode `session.compacted` | Adapter records compaction as unknown event with metadata; cursor advances |
| Cline `contextModification` in hook response | Adapter-local; not copied to common event |
| Goose `session/set_config_option` | Adapter-local state; not surfaced as event |
| OpenHands `HookRegistry` priority ordering | Adapter-local hook dispatch; not visible to T0 |
| Aider notification command | Adapter-local; enriches status at best |

### Genuine cross-provider gaps requiring separate review

**None identified.** All semantic categories observed across the 6 researched agents map into the existing T0 closed vocabulary. No provider exposes a fundamentally new event category that cannot be represented as:

1. An existing T0 event type, OR
2. A bounded metadata entry on an existing type, OR
3. An adapter-local concern invisible to T0, OR
4. An unknown/degraded event

### T0 revision recommendation

**No T0 contract revision is needed at this time.** The frozen T0 AgentEvent vocabulary (13 event types), provenance tiers, confidence levels, correlation states, and approval authority rules accommodate all 6 researched agents plus the 2 accepted adapters without modification.

Evidence: the fit-gap analysis above covers 8 agents (Codex, Claude, Gemini CLI, OpenCode, Cline, Goose, OpenHands, Aider) and found zero concepts that require a new T0 event type, provenance tier, or correlation state.

If a future agent exposes a genuinely novel category (e.g., a structured "agent delegated to sub-agent" event with transfer-of-control semantics), that should first be handled as adapter-local metadata, then compared across at least 3 providers before proposing a T0 revision.

## 4. Redacted Fixture Inventory

No local fixtures were captured during R2. All evidence is from public documentation and public source code, not from installed binaries. This matches the handoff requirement: "Create redacted fixtures only when they are reproducible from public documentation or controlled local experiments."

### Synthetic fixture sketches (for future adapter implementation)

| Agent | Fixture type | Shape | Version |
|---|---|---|---|
| Gemini CLI | Hook stdin JSON | `{"session_id":"...","transcript_path":"...","cwd":"...","hook_event_name":"BeforeTool","timestamp":"..."}` | hooks v1 (2026-04) |
| OpenCode | SSE event | `event: session.status\ndata: {"session_id":"...","status":"busy"}` | 2026-05 |
| Cline | Hook stdin JSON | `{"hookName":"PreToolUse","taskId":"...","toolName":"...","toolInput":{...}}` | v3.36+ |
| Goose | ACP notification | `{"jsonrpc":"2.0","method":"notifications/tool_call","params":{"id":"...","name":"...","status":"pending"}}` | ACP 2026-03 |
| OpenHands | Hook event | `{"event":"pre_tool_use","tool_name":"...","conversation_id":"..."}` | V1 (2026-02) |
| Aider | History file | `# USER\n<prompt>\n\n# ASSISTANT\n<response>` | 2025-2026 |

Redaction method: all fixtures would use `<REDACTED>` for prompts, `<HOME>` for paths, `<UUID>` for IDs, `<MODEL>` for model names — same pattern as accepted T1/T2 fixtures.

## 5. D1 Adapter Doctor Repair-scenario Expansion

Based on version-drift patterns observed across researched agents:

### New repair scenarios identified

| Scenario | Agent | Trigger | Repair action |
|---|---|---|---|
| Hook schema field rename | Gemini CLI | `hook_event_name` → `event_name` in future version | Update adapter field mapping, keep version gate |
| SSE event type addition | OpenCode | New event type added to stream | Map to EventUnknown + metadata, no adapter failure |
| Hook directory relocation | Cline | `.clinerules/hooks/` → `~/.config/cline/hooks/` | Update path discovery, validate with version check |
| ACP method deprecation | Goose | `session/set_mode` deprecated for `session/set_config_option` | Update adapter to use new method, fall back to legacy |
| Transcript path format change | Gemini CLI | `session-<date>-<shortid>.json` → new naming | Update glob pattern, version-gate the change |
| Server port/endpoint change | OpenCode | REST endpoint paths change between versions | Version-specific endpoint registry in adapter |
| Config file format migration | OpenHands | `.openhands/hooks.json` schema changes | Schema version detection, graceful degradation |
| History file encoding change | Aider | `.aider.chat.history.md` format additions | Robust parsing with unknown-section fallthrough |

### Repair scenario compatibility with D1 plan

All identified scenarios fit within the existing D1 plan's repair sandbox:
- Read-only source inspection ✓
- Version-gated adapter code patches ✓
- Fixture updates with provenance preservation ✓
- No public DTO or contract changes ✓

## 6. Risks and Unsupported-signal List

### Risks

| Risk | Severity | Affected agents | Mitigation |
|---|---|---|---|
| Active development instability | MEDIUM | Gemini CLI, OpenCode, OpenHands | Version-gate strictly; fail-closed on unknown shapes |
| IDE/CLI path divergence | MEDIUM | Cline | Target CLI/SDK path only; document IDE limitation |
| Protocol deprecation | LOW | Goose (`goosed` → ACP) | Target ACP only; it's the declared future |
| No structured events | HIGH | Aider | PTY fallback only; accept limited semantic coverage |
| Hook correlation gap | MEDIUM | All hook-based agents | Managed launch required (same as Claude/Codex) |
| Permission/approval ambiguity | MEDIUM | All agents with PreToolUse hooks | Hook cancellation ≠ approval; distinguish carefully |
| Multi-session concurrency | LOW | Goose, OpenCode | Per-session isolation already implemented upstream |

### Unsupported signals (by agent)

| Signal | Why unsupported |
|---|---|
| Gemini CLI `PreCompress` hook | Adapter-local optimization; not a semantic event |
| OpenCode `session.diff` | Code change tracking; belongs in Transcript (T3), not AgentEvent |
| Cline `contextModification` | Injects text into next LLM call; adapter-local, not an event |
| Goose `UsageUpdate` | Token accounting; metadata only |
| OpenHands EventStream `Memory` subscriber | Internal agent memory; not a surfaced event |
| Aider analytics events (PostHog) | Opt-in telemetry; not an event protocol |
| All: PTY screen text as approval/status | Explicitly rejected by T0 authority rules (§9, S1) |

## 7. Third Production Adapter Recommendation

### Recommended: **Goose** (via ACP)

**Rationale (weighted):**

| Factor | Goose | Gemini CLI | OpenCode | Cline | OpenHands |
|---|---|---|---|---|---|
| Structured event quality | ★★★★★ ACP JSON-RPC 2.0 | ★★★★ hooks JSON | ★★★★★ SSE stream | ★★★★ hooks + SDK | ★★★★ hooks + EventStream |
| Session correlation | ★★★★★ session/new returns ID | ★★★★ session_id in hooks | ★★★★★ POST /session | ★★★★ taskId in hooks | ★★★★ conversation_id |
| Cursor/replay support | ★★★★ session/load replays history | ★★★ transcript JSON file | ★★★ SSE stream (no built-in cursor) | ★★★ task state (no cursor) | ★★★ EventStream IDs |
| Approval reliability | ★★★★★ RequestPermission structured | ★★★★ BeforeTool decision | ★★★★★ permission.asked + reply | ★★★★ PreToolUse cancel | ★★★★ PreToolUse + UserPromptSubmit |
| Status reliability | ★★★★ Agent state via streaming | ★★★★ SessionStart/End | ★★★★★ session.status | ★★★★ TaskStart/Complete | ★★★★ SessionStart/End |
| Public contract stability | ★★★★★ ACP is a public, versioned spec | ★★★ hooks under active dev | ★★★★ API versioned | ★★★ hooks v3.36+ | ★★★ SDK under restructure |
| Implementation complexity | ★★★★ Single binary, one protocol | ★★★ stdio hooks + file parsing | ★★★ SSE client + REST | ★★★ hook scripts + SDK | ★★★ hook scripts + server |
| Maintenance risk | ★★★★★ ACP spec stable, deprecation path clear | ★★★ active Google dev, rapid changes | ★★★★ community-driven, MIT | ★★★ IDE-integrated, CLI path young | ★★★ SDK restructuring |
| User demand | ★★★★ Block/Square backing, community growth | ★★★★★ Google brand, rapid adoption | ★★★★ Charmbracelet ecosystem | ★★★★ VS Code ecosystem | ★★★ Docker-native users |

### Runner-up: Gemini CLI

Gemini CLI has the strongest brand recognition and fastest adoption growth. Its hooks system is comprehensive. However, it is under very active Google development with frequent schema changes, making version-gating more fragile. It is the recommended **fourth** adapter after Goose.

### Priority order with evidence

1. **Goose** (ACP adapter) — Public stable spec, full event coverage, session correlation, structured approval, clear migration path
2. **Gemini CLI** (hook + transcript adapter) — Comprehensive hooks, transcript files, strong adoption; higher drift risk
3. **OpenCode** (SSE server adapter) — Rich SSE stream, session lifecycle, permission events; community-driven stability
4. **Cline** (hook/plugin adapter) — Full tool lifecycle hooks, SDK events; CLI/SDK path needs maturation
5. **OpenHands** (hook/server adapter) — Comprehensive hooks + EventStream; SDK restructuring underway
6. **Aider** — PTY fallback only; no structured event protocol; covered by existing T1 Transcript

### Recommendation is planning evidence only

Implementing any third adapter requires a separately authorized handoff with acceptance scope, fixture evidence, and independent verification — matching the T1/T2 pattern.

## 8. Contract-change Policy Compliance

Per the R2 handoff: no T0 change is recommended. The analysis covered 8 agents (2 accepted + 6 researched) and found zero concepts requiring a new T0 event type, provenance tier, correlation state, or approval rule.

The contract-change escalation path was followed for every identified provider-specific concept:

```
Provider-specific concept → adapter-local mapping → metadata → unknown/degraded → cross-provider comparison → NO revision needed
```

Examples:
- Gemini `PreCompress` → adapter-local (not an event)
- OpenCode `session.diff` → metadata (diff count) or Transcript (T3)
- Cline `contextModification` → adapter-local (hook response, not surfaced)
- Goose `UsageUpdate` → metadata (token counts)
- OpenHands `HookRegistry` → adapter-local (hook dispatch order)

## Scope exclusions confirmed

R2 did not:
- Implement any production adapter
- Modify T0, T1, T2 production code or the fixed conformance harness
- Start D1, T3, S1, A1, O1, or Windows work
- Integrate research output into mobile or daemon production paths
- Invent unavailable approval or status schemas
- Classify UI text or prompt-like strings as authoritative evidence
- Capture or commit binary fixtures or non-public data

## Gate checks

```text
Research documents only — no code changes
gofmt: N/A (no Go files changed)
go build ./...: PASS (unchanged)
go test -race ./...: PASS (unchanged)
git diff --check: PASS
secret scan: PASS
T1 ancestry (162266f83): PASS
T0 ancestry (3ce2604bd): PASS
Worktree: clean
```

## REVIEW REQUEST

```text
REVIEW REQUEST: R2 Multi-agent Expansion Research
baseline T2: ef4a162c7f9a5644fd52d89501e97f4e62301dfa
scope: R2 research only
researched: Gemini CLI (hooks v1), OpenCode (SSE 2026-05), Cline (v3.36+),
            Goose (ACP 2026-03), OpenHands (hooks V1 2026-02), Aider (2025-2026)
T0 fit-gap: zero cross-provider gaps requiring revision
third-adapter recommendation: Goose (ACP)
contract-change proposal: NONE
deferred: D1, T3, S1, A1, O1, all adapter implementation, Windows
```
