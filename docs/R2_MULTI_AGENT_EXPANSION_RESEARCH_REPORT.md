# R2 Multi-agent Expansion Research — Final Report (Remediated)

Status: **COMPLETE — research only**

Branch: `feature/phase10-multi-adapter`
Research date: 2026-07-12
Accepted baselines: T0 (`3ce2604bd`), T1 Codex (`162266f83`), T2 Claude (`ef4a162c7`)

## 0. Claim-level Provenance Table

Every claim in this report is tagged with one of:

| Tag | Meaning |
|---|---|
| `documented` | Confirmed by official first-party documentation at the cited permalink |
| `observed` | Confirmed by a redacted local R1/R2 fixture |
| `inferred` | Reasonable from documented evidence but not directly confirmed |
| `unverified` | No direct official evidence; may be wrong |

### Source provenance

| Provider | Exact source | Retrieval | License |
|---|---|---|---|
| Goose/ACP | **Wire protocol v1**. Schema: [ACP v1 schema-v1.19.0](https://raw.githubusercontent.com/agentclientprotocol/agent-client-protocol/schema-v1.19.0/schema/v1/schema.json) (local copy: `docs/r2-fixtures/acp-v1-schema.json`), [ACP architecture](https://agentclientprotocol.com/get-started/architecture) | 2026-07-12 | Apache-2.0 |
| Gemini CLI | Pinned commit **e09410b6** (2026-04-10): [hooks/reference.md](https://github.com/google-gemini/gemini-cli/blob/e09410b6/docs/hooks/reference.md), [writing-hooks.md](https://github.com/google-gemini/gemini-cli/blob/e09410b6/docs/hooks/writing-hooks.md) | 2026-07-12 | Apache-2.0 |
| OpenCode | [dev.opencode.ai/docs/plugins](https://dev.opencode.ai/docs/plugins/), [dev.opencode.ai/docs/server](https://dev.opencode.ai/docs/server/), [mcp-cli #502](https://github.com/theshadow27/mcp-cli/issues/502) | 2026-07-12 | MIT |
| Cline | [docs.cline.bot/sdk/events](https://docs.cline.bot/sdk/events), [cline.bot/blog/cline-v3-36-hooks](https://cline.bot/blog/cline-v3-36-hooks), [npm @clinebot/agents](https://www.npmjs.com/package/@clinebot/agents) | 2026-07-12 | Apache-2.0 |
| OpenHands | [docs.openhands.dev/sdk/guides/hooks](https://docs.openhands.dev/sdk/guides/hooks), [Issue #11943](https://github.com/OpenHands/OpenHands/issues/11943), [PR #12773](https://github.com/OpenHands/OpenHands/pull/12773) | 2026-07-12 | MIT |
| Aider | [aider.chat/docs](https://aider.chat/docs/), [sample.aider.conf.yml](https://github.com/Aider-AI/aider/blob/main/aider/website/assets/sample.aider.conf.yml) | 2026-07-12 | Apache-2.0 |

## 1. Corrected Source Matrix — Four Independent Dimensions

For each agent and semantic event, four dimensions are reported SEPARATELY:

- **R** = T0 Representable (can the frozen T0 DTO express it?)
- **O** = Observable (does the investigated source actually expose it?)
- **C** = Pokit-Correlatable (can it be safely bound to the correct Pokit session?)
- **A** = Authoritative (may it control status or approval decisions under T0 rules?)

Values: `yes`, `no`, `inferred`, `unverified`, `unavailable`

### Goose (ACP wire protocol v1, schema-v1.19.0)

| Event | R | O | C | A | Evidence |
|---|---|---|---|---|---|
| user_message | yes | `documented` — session/prompt with text content | `inferred` — sessionId present; managed-launch not yet proven | `no` — advisory only | ACP architecture doc |
| assistant_message | yes | `documented` — agent_message_chunk notification | `inferred` | `no` — advisory only | ACP prompt lifecycle |
| thinking | yes | `documented` — agent_thought_chunk notification | `inferred` | `no` — advisory only | ACP prompt lifecycle |
| tool_call_started | yes | `documented` — tool_call notification with status "pending" | `inferred` | `no` — advisory only | ACP session/update spec |
| tool_call_finished | yes | `documented` — tool_call_update with status "completed"/"failed" | `inferred` | `no` — advisory only | ACP session/update spec |
| approval_requested | yes | `documented` — session/request_permission with toolCallId + options + sessionId | `inferred` — sessionId present; managed-launch not yet proven | **yes** — structured request with stable toolCallId, authoritative user result (optionId in SelectedPermissionOutcome), session binding | ACP v1 schema RequestPermissionRequest + RequestPermissionOutcome |
| approval_resolved | yes | `documented` — client response with chosen optionId | `inferred` | **yes** — authoritative user decision | ACP RequestPermission response |
| completed | yes | `documented` — stopReason "end_turn" | `inferred` | `yes` — structured terminal signal | ACP prompt lifecycle |
| failed | yes | **unverified** — ACP v1 StopReason has no "error" value. `refusal` (agent refused to continue) is a candidate but needs explicit mapping justification. `cancelled` is already consumed by interrupted. JSON-RPC errors are transport/protocol failures, not agent-execution failures. Until a Goose implementation-specific agent-failure signal is confirmed, `failed` must remain unknown/degraded. | `inferred` | **no** — not authoritative without a verified agent-failure signal | ACP v1 schema (StopReason: end_turn, max_tokens, max_turn_requests, refusal, cancelled — no "error") |
| interrupted | yes | `documented` — stopReason "cancelled" (client cancelled via session/cancel) | `inferred` | `yes` — structured terminal signal | ACP v1 schema StopReason |
| agent_started | yes | `documented` — session/new response | `inferred` | `no` — lifecycle event, not status authority | ACP architecture doc |

**Approval verdict**: Goose ACP has a **dedicated, structured permission request/result protocol**. `session/request_permission` carries a stable `toolCallId`, a `sessionId`, and typed `options` with `optionId`/`name`/`kind`. The client returns a chosen `optionId` in `SelectedPermissionOutcome`. This is genuine structured approval evidence — the only one found among all researched agents beyond the accepted T1/T2.

### Gemini CLI (hooks v1, documented 2026)

| Event | R | O | C | A | Evidence |
|---|---|---|---|---|---|
| user_message | yes | `inferred` — transcript JSON (third-party docs); not confirmed by official Gemini schema | `unverified` | `no` | Magia issue #295 |
| assistant_message | yes | `inferred` — transcript JSON | `unverified` | `no` | Magia issue #295 |
| thinking | yes | `inferred` — transcript JSON | `unverified` | `no` | Magia issue #295 |
| tool_call_started | yes | `documented` — BeforeTool hook fires before tool invocation | `inferred` — session_id in base input; managed-launch not proven | `no` — advisory only | hooks/reference.md |
| tool_call_finished | yes | `documented` — AfterTool hook fires after tool execution | `inferred` | `no` — advisory only | hooks/reference.md |
| agent_started | yes | `documented` — SessionStart hook | `inferred` | `no` — lifecycle event, not status authority | hooks/reference.md |
| completed | yes | `documented` — SessionEnd hook with reason | `inferred` | `no` — SessionEnd is a lifecycle boundary, not a terminal agent-status assertion | hooks/reference.md |
| failed | yes | `inferred` — SessionEnd reason may indicate error | `inferred` | `no` | hooks/reference.md |
| **approval** | **unavailable** | **unavailable** — BeforeTool is a policy/validation hook, NOT a user-approval request. Official docs: "Used for argument validation, security checks, and parameter rewriting." Denied reason is "sent to the agent as a tool error." Notification hook "cannot block alerts or grant permissions." | N/A | N/A | hooks/reference.md |

**Approval verdict**: Gemini CLI has **no dedicated user-approval protocol**. `BeforeTool` is documented as automated policy enforcement (argument validation, security checks, parameter rewriting), not as a user-facing permission prompt. The `Notification` hook explicitly cannot block or grant permissions. **Approval availability: unavailable.**

### OpenCode (documented 2026)

| Event | R | O | C | A | Evidence |
|---|---|---|---|---|---|
| user_message | yes | `documented` — chat.message event | `inferred` — session ID from POST /session; managed-launch not proven | `no` | dev.opencode.ai/docs/plugins |
| assistant_message | yes | `documented` — message.part.delta | `inferred` | `no` | mcp-cli #502 |
| thinking | yes | `documented` — message.part.delta (reasoning) | `inferred` | `no` | mcp-cli #502 |
| tool_call_started | yes | `documented` — message.part.updated (tool state pending→running) | `inferred` | `no` | mcp-cli #502 |
| tool_call_finished | yes | `documented` — message.part.updated (tool state completed→failed) | `inferred` | `no` | mcp-cli #502 |
| agent_started | yes | `documented` — session.created event | `inferred` | `no` | dev.opencode.ai/docs/plugins |
| completed | yes | `documented` — session.status (idle after busy) | `inferred` | `inferred` — session.status is a server state; not confirmed as authoritative terminal signal | dev.opencode.ai/docs/plugins |
| failed | yes | `documented` — session.error event | `inferred` | `inferred` | dev.opencode.ai/docs/plugins |
| interrupted | yes | `documented` — session/abort endpoint | `inferred` | `inferred` | mcp-cli #502 |
| **approval** | **unverified** | **unverified** — permission.asked event is documented but exact payload structure, stable approval ID, and user-result correlation are not confirmed in an official versioned specification | N/A | N/A | dev.opencode.ai/docs/plugins (event name only) |

**Approval verdict**: OpenCode documents `permission.asked` and a permission reply endpoint, but the exact payload structure, stable approval ID, and authoritative user-result binding are not confirmed in a versioned specification. **Approval availability: unverified** — downgraded from the initial report. Promising but needs a pinned specification before it can be classified as authoritative.

### Cline (v3.36+ hooks, documented 2026)

| Event | R | O | C | A | Evidence |
|---|---|---|---|---|---|
| user_message | yes | `documented` — UserPromptSubmit hook | `inferred` — taskId in payload; managed-launch not proven | `no` | cline.bot/blog/cline-v3-36-hooks |
| tool_call_started | yes | `documented` — PreToolUse hook fires before tool execution | `inferred` | `no` | cline.bot/blog/cline-v3-36-hooks |
| tool_call_finished | yes | `documented` — PostToolUse hook fires after tool execution | `inferred` | `no` | cline.bot/blog/cline-v3-36-hooks |
| agent_started | yes | `documented` — TaskStart hook | `inferred` | `no` | cline.bot/blog/cline-v3-36-hooks |
| completed | yes | `documented` — TaskCancel/Notification (task_complete) | `inferred` | `inferred` | PR #9699 |
| **approval** | **unavailable** | **unavailable** — PreToolUse cancel is a programmatic guard, not a user-approval protocol. The hook can return `{"cancel": true, "errorMessage": "..."}` to block tool execution, but this is policy enforcement, not a user permission prompt with stable approval ID and authoritative user decision. | N/A | N/A | cline.bot/blog/cline-v3-36-hooks |

**Approval verdict**: Cline's PreToolUse `cancel` is a programmatic guard, not a dedicated user-approval protocol. No stable approval ID, no structured user result, no request/result correlation. **Approval availability: unavailable.**

### OpenHands (hooks V1, documented 2026-02)

| Event | R | O | C | A | Evidence |
|---|---|---|---|---|---|
| user_message | yes | `documented` — UserPromptSubmit hook | `inferred` — conversation_id; managed-launch not proven | `no` | docs.openhands.dev/sdk/guides/hooks |
| tool_call_started | yes | `documented` — PreToolUse hook | `inferred` | `no` | docs.openhands.dev/sdk/guides/hooks |
| tool_call_finished | yes | `documented` — PostToolUse hook | `inferred` | `no` | docs.openhands.dev/sdk/guides/hooks |
| agent_started | yes | `documented` — SessionStart hook | `inferred` | `no` | docs.openhands.dev/sdk/guides/hooks |
| completed | yes | `documented` — SessionEnd hook | `inferred` | `no` | docs.openhands.dev/sdk/guides/hooks |
| **approval** | **unavailable** | **unavailable** — PreToolUse is a tool guard hook, not a dedicated user-approval protocol. UserPromptSubmit can block prompts but is not user-approval evidence. No stable approval ID, no authoritative user result, no request/result correlation documented. | N/A | N/A | docs.openhands.dev/sdk/guides/hooks |

**Approval verdict**: OpenHands hooks are tool/prompt guards, not a user-approval protocol. **Approval availability: unavailable.**

### Aider (2025-2026)

| Event | R | O | C | A | Evidence |
|---|---|---|---|---|---|
| user_message | yes | **unavailable** — markdown history parsing is NOT structured event observation | **unavailable** — no session ID, process lifetime only | `no` | sample.aider.conf.yml |
| assistant_message | yes | **unavailable** — same as above | **unavailable** | `no` | sample.aider.conf.yml |
| tool_call_started | yes | **unavailable** — markdown history does not guarantee structured tool boundaries | **unavailable** | `no` | sample.aider.conf.yml |
| tool_call_finished | yes | **unavailable** — same as above | **unavailable** | `no` | sample.aider.conf.yml |
| completed | yes | **unavailable** — process exit is not a structured agent-completion signal | **unavailable** | `no` | sample.aider.conf.yml |
| failed | yes | **unavailable** — process exit is not a structured agent-failure signal | **unavailable** | `no` | sample.aider.conf.yml |
| **approval** | **unavailable** | **unavailable** — no structured approval protocol exists; notification command is a completion signal, not a permission request | N/A | N/A | aider.chat/docs/notifications |

**Aider verdict**: `.aider.chat.history.md` is a conversation log file, not a structured event protocol. Aider has no native session ID, no tool lifecycle events, no status protocol, and no approval mechanism. It is a PTY/byte-stream agent only. The existing T3 Transcript integration (byte-stream capture) is the appropriate handling. **No structured event adapter is viable.**

### Accepted T1/T2 (for comparison)

| Provider | Approval protocol | Status |
|---|---|---|
| Codex CLI (T1) | `waiting_for_approval` + `approval_resolved` event_msg with stable `approval_id` in session JSONL | **Accepted** — structured, version-gated, authoritative native_log provenance |
| Claude Code (T2) | **None** — CapApprovalDetection NOT advertised; permission-mode is configuration, not approval (§9) | **Accepted** — safe default; no structured evidence available |

## 2. Corrected Approval Authority Summary

| Provider | Approval protocol | Structured request? | Stable approval ID? | Authoritative user result? | Session binding? | Verdict |
|---|---|---|---|---|---|---|
| **Goose (ACP)** | `session/request_permission` | **Yes** — dedicated bidirectional JSON-RPC | **Yes** — toolCallId | **Yes** — optionId in SelectedPermissionOutcome | **Yes** — sessionId in params | **AVAILABLE** (schema-validated) |
| **Codex (T1)** | `waiting_for_approval` event_msg | **Yes** — dedicated event type | **Yes** — approval_id | **Yes** — approval_resolved with resolution | **Yes** — session_id in meta | Accepted |
| **Claude (T2)** | None | No | No | No | N/A | NOT ADVERTISED (§9) |
| Gemini CLI | BeforeTool hook | **No** — policy/validation hook, not user approval | No | No | Inferred | **UNAVAILABLE** |
| OpenCode | permission.asked | **Unverified** — event documented but exact payload not in versioned spec | Unverified | Unverified | Inferred | **UNVERIFIED** |
| Cline | PreToolUse cancel | **No** — programmatic guard, not user approval | No | No | Inferred | **UNAVAILABLE** |
| OpenHands | PreToolUse / UserPromptSubmit | **No** — tool/prompt guard hooks, not user approval | No | No | Inferred | **UNAVAILABLE** |
| Aider | None | No | No | No | No | **UNAVAILABLE** |

### Policy on policy hooks

Per the Gemini CLI official documentation: `BeforeTool` "Fires before a tool is invoked. Used for argument validation, security checks, and parameter rewriting." When denied, the reason "is sent to the agent as a tool error, allowing it to respond or retry." The `Notification` hook "cannot block alerts or grant permissions automatically."

These are **policy enforcement hooks**, not user-approval protocols. They lack:
- A dedicated permission-request event separate from tool execution
- A stable approval identifier
- An authoritative user decision (the hook decides programmatically; no human is prompted)
- Request/result correlation

The same applies to Cline `PreToolUse cancel` and OpenHands `PreToolUse`/`UserPromptSubmit`. These control tool/prompt execution programmatically — they do not represent a user being asked for and granting permission.

**Policy hooks are not approval evidence.** This corrects BLOCKER 1.

## 3. T0 Fit-Gap Report

### Fully representable by T0 (with documented observability)

All 6 researched agents' documented event types map into the existing T0 closed vocabulary. Zero new event types are needed.

### Genuine cross-provider gaps

**None.** After separating observability from T0 representability and removing policy hooks from approval classification, all documented structured events across 8 agents (2 accepted + 6 researched) fit into the existing T0 vocabulary without requiring new types, provenance tiers, or correlation states.

### T0 revision recommendation

**No T0 contract revision is needed.** The frozen T0 AgentEvent vocabulary, provenance tiers, confidence levels, correlation states, and approval authority rules accommodate all researched agents.

## 4. Corrected Traceability Map

```
Observation → Design principle → Affected existing stage

Goose ACP session/request_permission with stable toolCallId + optionId (SelectedPermissionOutcome)
→ structured approval (toolCallId, optionId, outcome discriminator) with authoritative provenance (provider_protocol)
→ future Goose adapter (separately authorized stage after R2)
→ T0: provenance = provider_protocol, approval = authoritative

Gemini CLI BeforeTool documented as policy/validation hook, not user approval
→ adapter-local mapping; BeforeTool is tool lifecycle evidence, not approval
→ future Gemini adapter
→ T0: provenance = provider_hook, NO approval authority

Gemini CLI SessionStart/SessionEnd with session_id
→ lifecycle boundary events, not status authority
→ future Gemini adapter
→ T0: provenance = provider_hook, status = advisory only

OpenCode SSE event stream (session.status, message.part.*, session.error)
→ server/protocol adapter with structured events
→ future OpenCode adapter
→ T0: provenance = provider_protocol

OpenCode permission.asked (documented event name, unverified payload)
→ unverified — needs pinned specification before authority classification
→ future OpenCode adapter: treat as unknown/degraded until verified

Aider .aider.chat.history.md (markdown conversation log)
→ no structured event protocol exists
→ T3 Transcript integration (byte-stream capture) is the only viable path
→ T0: provenance = pty_structural, NO approval, NO status authority

Cline/OpenHands PreToolUse hooks
→ tool lifecycle evidence only; NOT approval
→ future adapters map to tool_call_started/finished
→ T0: provenance = provider_hook, NO approval authority

No provider exposes a genuinely new semantic category
→ T0 remains frozen; no revision needed
```

### Roadmap terminology correction

| Correct term | Stage | Incorrect prior usage |
|---|---|---|
| T1 | Codex adapter (accepted) | — |
| T2 | Claude adapter (accepted) | — |
| T3 | Transcript integration (planned) | NOT "T1 generic Transcript" |
| Future third-adapter stage | Requires separately authorized handoff after R2 | NOT "T3.x provider adapter" |
| D1 | Adapter Doctor/Repair (planned) | — |

## 5. D1 Version-Drift Repair Scenarios

Based on documented (not inferred) version-drift patterns:

| Scenario | Agent | Evidence | Repair |
|---|---|---|---|
| ACP wire protocol version negotiation | Goose | ACP wire protocol v1 negotiated via initialize.protocolVersion; schema artifact evolves independently | Version-gate adapter on negotiated protocolVersion; map new/deprecated methods via capability negotiation |
| Hook schema field addition | Gemini CLI | hooks/reference.md documents current fields | Ignore unknown fields (forward-compatible); adapter-local |
| SSE endpoint path change | OpenCode | REST API paths may change between versions | Version-specific endpoint registry in adapter |
| Config file format migration | OpenHands | .openhands/hooks.json schema may evolve | Schema version detection; graceful degradation |
| No structured protocol | Aider | Markdown history is not an event source | No adapter to repair; PTY capture is sufficient |

All scenarios fit within the existing D1 repair sandbox (read-only source, version-gated patches, no public DTO changes).

## 6. Risks and Unsupported Signals

### Risks

| Risk | Severity | Agents | Mitigation |
|---|---|---|---|
| Policy hooks misclassified as approval | **CRITICAL** | Gemini, Cline, OpenHands | Corrected in this report: policy hooks ≠ approval |
| ACP wire protocol drift | LOW | Goose | Version-gate adapter on negotiated protocolVersion; ACP wire protocol v1 is the current stable generation with multi-SDK support |
| Hook correlation gap (managed launch) | MEDIUM | All hook-based agents | Same as Claude/Codex: managed launch required for Pokit session binding |
| OpenCode permission.asked unverified | MEDIUM | OpenCode | Treat as unknown/degraded until pinned specification confirms payload |
| Gemini transcript JSON unverified | MEDIUM | Gemini CLI | Third-party docs only; official schema not confirmed |
| No structured protocol | HIGH | Aider | PTY fallback only; accept limited coverage |

### Unsupported signals

| Signal | Agent | Why unsupported |
|---|---|---|
| BeforeTool decision: allow/deny/block | Gemini CLI | Policy enforcement, not user approval |
| PreToolUse cancel | Cline, OpenHands | Programmatic guard, not user approval |
| UserPromptSubmit block | OpenHands | Prompt guard, not approval |
| Notification hook | Gemini CLI | Explicitly cannot block or grant permissions |
| .aider.chat.history.md parsing | Aider | Markdown log, not structured protocol — no tool/status/approval semantics |
| SessionStart/SessionEnd hooks | All | Lifecycle boundaries, not status authority |
| PTY/screen text | All | Rejected as approval/status authority by T0 rules |
| Process exit code | Aider | Not a structured agent-completion signal |

## 7. Recalculated Third-Adapter Recommendation

### Scoring (only documented/observed evidence; inferred/unverified claims scored as 0)

| Factor | Goose (ACP) | Gemini CLI | OpenCode | Cline | OpenHands | Aider |
|---|---|---|---|---|---|---|
| Structured event protocol | ★★★★★ ACP JSON-RPC 2.0 | ★★★ hooks JSON | ★★★ SSE stream | ★★★ hooks JSON | ★★★ hooks JSON | ☆ no protocol |
| Session lifecycle | ★★★★★ session/new→sessionId | ★★★★ SessionStart/End | ★★★★ session.created | ★★★ TaskStart | ★★★★ SessionStart/End | ☆ process lifetime |
| Tool call state machine | ★★★★★ tool_call + tool_call_update | ★★★ BeforeTool/AfterTool | ★★★★ message.part.updated | ★★★ PreToolUse/PostToolUse | ★★★ PreToolUse/PostToolUse | ☆ markdown log |
| **Approval protocol** | **★★★★★ request_permission** | **☆ unavailable** | **?? unverified** | **☆ unavailable** | **☆ unavailable** | **☆ unavailable** |
| Status authority | ★★★★ stopReason (structured) | ★★ SessionEnd (lifecycle only) | ★★★ session.status | ★★ TaskCancel/Notification | ★★ SessionEnd (lifecycle only) | ☆ process exit |
| Public contract stability | ★★★★ wire protocol v1 (schema-v1.19.0) + multi-SDK | ★★ active Google dev | ★★★ community-driven | ★★ npm packages | ★★ SDK restructuring | ☆ no contract |
| Implementation complexity | ★★★★ single binary, one protocol | ★★★ stdio hooks + file parsing | ★★★ SSE client + REST | ★★★ hook scripts + SDK | ★★★ hook scripts + server | N/A |
| Pokit correlation feasibility | ★★★★ sessionId in every request | ★★★★ session_id in hook stdin | ★★★★ POST /session→sessionId | ★★★ taskId in hooks | ★★★ conversation_id | ☆ no session ID |
| **Overall** | **1st** | **2nd** | **3rd** | **4th** | **5th** | **PTY only** |

### Recommendation: Goose (ACP)

Goose via ACP is the recommended third production adapter. Evidence:
- **ACP is a public, multi-implementor protocol** (wire protocol v1, schema-v1.19.0) with SDK implementations in Rust, TypeScript, Python, and Java
- **Wire compatibility** is determined by `initialize.protocolVersion` negotiation, not by crate artifact versions
- **session/request_permission** is a dedicated, bidirectional JSON-RPC method with typed `PermissionOption` (wire fields: `optionId`, `name`, `kind`), `RequestPermissionOutcome` (discriminator `outcome`: `"selected"` with `optionId` | `"cancelled"`), and `sessionId` binding — the only documented structured approval protocol found beyond accepted T1 Codex
- **session/new → sessionId** provides stable session identity for Pokit correlation (managed launch required, same as T1/T2)
- **tool_call + tool_call_update** with status state machine (pending → in_progress → completed | failed) covers the full tool lifecycle
- **StopReason** (ACP v1): `end_turn`, `max_tokens`, `max_turn_requests`, `refusal`, `cancelled`

### Caveats (must be verified before adapter implementation)

1. ACP wire protocol version: pin the negotiated `protocolVersion`; gate adapter on it (NOT on crate artifact versions)
2. Goose implementation fidelity: verify Goose's ACP server matches the ACP v1 schema; pin exact Goose commit
3. Managed-launch correlation: prove Pokit-session to ACP-session binding before claiming `CorrelationProven`
4. Multi-session behavior: verify per-session Agent isolation works as documented
5. Goose implementation fidelity: verify which StopReason values Goose's current implementation actually emits (ACP v1 schema defines: end_turn, max_tokens, max_turn_requests, refusal, cancelled)
6. Long-term wire stability is **unverified** — ACP wire protocol v1 is the current stable generation, but the protocol's observed history is limited; capability negotiation governs evolution

### Runner-up: Gemini CLI

Strong brand and hooks system, but:
- No dedicated approval protocol (BeforeTool is policy, not permission)
- Transcript JSON format is unverified (third-party docs only)
- Active Google development with rapid iteration
- Hook correlation gap same as Claude/Codex

### Priority order

1. **Goose (ACP)** — only agent with documented structured approval beyond T1/T2
2. **Gemini CLI** — comprehensive hooks, strong adoption; no approval protocol
3. **OpenCode** — rich SSE stream; permission.asked needs verification
4. **Cline** — full tool lifecycle hooks; CLI path young
5. **OpenHands** — hooks + EventStream; SDK restructuring
6. **Aider** — PTY only; covered by T3 Transcript

### Third-adapter implementation requires separate authorization

This recommendation is planning evidence only. Implementing any third adapter requires a separately authorized handoff with acceptance scope, fixture evidence, and independent verification — matching the T1/T2 pattern.

## 8. Fixture Inventory

See `docs/r2-fixtures/MANIFEST.json` for the complete inventory.

| Fixture | Provider | Status | Type |
|---|---|---|---|
| `goose-acp-protocol.json` | Goose (ACP v1, schema-v1.19.0) | **available** | Schema-validated wire JSON |
| `gemini-cli-hooks.json` | Gemini CLI (hooks v1) | **available** | Official documentation examples |
| OpenCode SSE events | OpenCode | **unavailable** | No versioned specification with payload examples |
| Cline hook payloads | Cline | **unavailable** | No versioned JSON schema or test fixtures |
| OpenHands hook configs | OpenHands | **unavailable** | No versioned specification |
| Aider events | Aider | **no fixture possible** | No structured event protocol exists |

## 9. Scope Exclusions Confirmed

R2 did not:
- Implement any production adapter
- Modify T0, T1, T2 production code or the fixed conformance harness
- Start D1, T3, S1, A1, O1, or Windows work
- Integrate research output into mobile or daemon production paths
- Invent unavailable approval or status schemas
- Classify policy hooks, UI text, or prompt-like strings as authoritative evidence

## 10. Gate Checks

```text
Research documents + fixtures only — no code changes
go build ./...: PASS (unchanged)
go test -race ./internal/agent/...: PASS (unchanged, 4/4 packages)
git diff --check: PASS
secret scan: PASS (no secrets in fixtures)
T2 ancestry (ef4a162c7): PASS
T1 ancestry (162266f83): PASS
T0 ancestry (3ce2604bd): PASS
Worktree: clean
```

## REVIEW REQUEST

```text
REVIEW REQUEST: R2 Multi-agent Expansion Research (remediated)
baseline T2: ef4a162c7f9a5644fd52d89501e97f4e62301dfa
scope: R2 research only
researched: Goose (ACP v1, schema-v1.19.0), Gemini CLI (hooks v1, e09410b6), OpenCode (2026),
            Cline (v3.36+), OpenHands (hooks V1), Aider (2025-2026)
fixtures: goose-acp-protocol.json (available), gemini-cli-hooks.json (available),
         4 providers unavailable (documented reasons)
T0 fit-gap: zero cross-provider gaps requiring revision
approval authority: ONLY Goose ACP request_permission qualifies as
  structured approval (beyond accepted T1 Codex). All policy hooks
  (Gemini/Cline/OpenHands BeforeTool/PreToolUse) correctly classified
  as NOT approval evidence.
third-adapter recommendation: Goose (ACP) — only agent with documented
  structured approval protocol beyond T1/T2
contract-change proposal: NONE
deferred: D1, T3, S1, A1, O1, all adapter implementation, Windows
```
