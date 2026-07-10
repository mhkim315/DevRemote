# R1 Runtime Signal Matrix

Status: SCOPED ACCEPT — independent research completed before M3 by explicit
orchestrator authorization

Research date: 2026-07-10
Scope: evidence and architecture decisions only; no production integration

## Labels

| Label | Meaning |
|---|---|
| `documented` | First-party documentation or public first-party source proves the contract. |
| `observed` | A redacted local R1 fixture demonstrates installed runtime behavior. |
| `heuristic` | Derived from terminal behavior only; never authoritative. |
| `unavailable` | Product, runtime, or correlation was not proven. |

## Summary matrix

| Integration | Strongest signal | Controlled PTY correlation | Strategy | Decision |
|---|---|---|---|---|
| Claude Code 2.1.206 | hooks + stream-json hook lifecycle | unavailable for ordinary interactive TUI | managed hook adapter | adopt later |
| Codex CLI 0.144.1 | app-server JSON-RPC approvals/events | unavailable for ordinary interactive TUI | managed app-server launch mode | adopt later |
| OpenCode | local HTTP/SSE and plugin events | unavailable | server/protocol adapter | conditional adopt |
| Orca | runtime JSON CLI and agents feed | unavailable | observe/runtime adapter | defer |
| Omnara | its own daemon/session wrapper | unavailable | interop/observe only | defer |
| Cline | task/tool lifecycle hooks and CLI JSON | unavailable | managed hook/plugin | conditional adopt |
| Aider | completion/waiting notification command | unavailable | PTY plus optional notifier | defer native semantics |
| Goose | ACP and local REST/SSE | unavailable | ACP/local server adapter | conditional adopt |
| Continue | tool/permission handshake | unavailable | PTY fallback | defer |
| Warp | Oz cloud agent API | unavailable | separate cloud adapter decision | reject as local PTY adapter |
| Generic PTY | ANSI/CSI structural signals | observed | TUI classifier/fallback | adopt |
| Prompt markers | model-emitted text or OSC | heuristic only | advisory only | reject as authority |

## Claude Code

Sources:

- <https://code.claude.com/docs/en/hooks>
- <https://code.claude.com/docs/en/hooks-guide>

Documented: lifecycle hooks cover user prompt, permission, notification, tool,
Stop, and session events; hook payloads include provider session/cwd/transcript
context; command or local HTTP hooks may consume JSON. Print-mode stream JSON
can include hook lifecycle events.

Observed: installed `claude --version` is `2.1.206`. A temporary settings file
with a `UserPromptSubmit` hook emitted `hook_started` and successful
`hook_response` before the no-credential model call failed. See
`docs/r1-fixtures/claude-hooks-2.1.206.jsonl`.

Correlation: Claude `session_id` was observed, but not mapped to a normal
`pokit run claude` interactive TUI. Hook duplication, user/project/admin policy,
safe/bare mode, and hook failure require a generic fallback.

Decision: adopt an opt-in managed hook adapter only after a controlled launch
proves Pokit-session ↔ Claude-session mapping. It enriches Recorder output; it
never replaces raw PTY capture.

## Codex CLI

Sources:

- <https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md>
- <https://github.com/openai/codex/blob/main/codex-rs/exec/src/lib.rs>

Documented: app-server exposes JSON-RPC thread, turn, item, command, approval,
input, and error events over stdio/Unix socket. WebSocket transport is marked
experimental/unsupported. Schemas are generated per installed version. `exec
--json` is non-interactive JSONL, not proof of an interactive TUI mapping.

Observed: installed `codex --version` is `0.144.1`. A generated schema exposed
command/file/permission approval and user-input requests plus thread/item/turn
notifications. An isolated no-auth app-server completed `initialize`. See
`docs/r1-fixtures/codex-app-server-0.144.1.json`.

Correlation: app-server thread IDs were not correlated to an independently
launched interactive `codex` TUI session.

Decision: adopt a later dedicated app-server launch mode only with explicit
thread/session mapping. Do not attach it speculatively to arbitrary Codex TUI.

## OpenCode

Sources:

- <https://dev.opencode.ai/docs/plugins/>
- <https://dev.opencode.ai/docs/server/>

Documented: plugin events include `permission.asked`, `permission.replied`,
`session.idle`, `session.status`, message updates, and tool execution. The local
server documents OpenAPI, session status, permission response, and SSE events.

Observed: unavailable; binary not installed.

Decision: conditionally adopt a server/SSE or managed-plugin adapter only when
Pokit launches OpenCode and owns its server/session identity.

## Orca

Sources:

- <https://www.onorca.dev/docs/cli/reference>
- <https://www.onorca.dev/docs/activity>

Documented: the identified Orca runtime has JSON CLI commands and an agents
feed for completion, blocked questions, waiting input, and response previews.

Observed: unavailable; product not installed.

Decision: defer. It is a separate runtime/control-plane surface, not evidence
that arbitrary nested terminal agents expose a reusable PTY protocol.

## Omnara

Sources:

- <https://docs.omnara.com/cli>
- <https://github.com/omnara-ai/omnara>

Documented: Omnara manages sessions through its own daemon. Its public legacy
repository says its earlier Claude/Codex wrapper was retired because upstream
wrapper maintenance was unsustainable.

Observed: unavailable; product not installed.

Decision: defer to an explicit interop study. Do not make Omnara cloud/daemon
state authoritative for Pokit.

## Cline

Sources:

- <https://docs.cline.bot/customization/hooks>
- <https://docs.cline.bot/sdk/plugins>
- <https://github.com/cline/cline>

Documented: task start/resume/cancel/complete, tool before/after, user-prompt,
and compaction hooks carry task ID, workspace roots, provider/model, timestamps,
tool result, success, and duration. CLI/SDK/plugin surfaces are separate from
IDE-extension hosting.

Observed: unavailable; product not installed.

Decision: conditionally adopt a managed hook/plugin for Cline CLI/SDK launches.
Treat IDE use as observe-only until an explicit local correlation contract is
proven.

## Aider

Sources:

- <https://aider.chat/docs/usage/notifications.html>
- <https://aider.chat/docs/config/options.html>
- <https://github.com/Aider-AI/aider>

Documented: Aider can invoke a notification command when an LLM response is
ready and it waits for input. It has streaming terminal output and optional
history files, but R1 found no documented versioned tool/approval event API.

Observed: unavailable; product not installed.

Decision: use byte-stream Transcript by default and optional completion
notification as low-detail enrichment. Defer native semantic integration.

## Goose

Sources:

- <https://block.github.io/goose/>
- <https://github.com/aaif-goose/goose/blob/main/CUSTOM_DISTROS.md>

Documented: ACP supports session creation/resume, streaming messages, tool-call
updates, permissions, and cancellation. `goosed` documents local REST/SSE
session and message surfaces.

Observed: unavailable; product not installed.

Decision: conditionally adopt ACP or local-server integration only when Pokit
launches Goose and owns its session ID.

## Continue

Sources:

- <https://docs.continue.dev/ide-extensions/agent/how-it-works>
- <https://github.com/continuedev/continue>

Documented: Agent mode has a tool and user-permission handshake. The product
surface is in transition: current public source emphasizes `cn` CLI and checks,
while the docs also describe a final legacy IDE extension release.

Observed: unavailable; `continue` resolved to the shell builtin, not a Continue
agent binary.

Decision: defer native integration until a specific Continue version and local
event contract are chosen; retain PTY fallback.

## Warp

Sources:

- <https://docs.warp.dev/reference>
- <https://docs.warp.dev/reference/api-and-sdk/quickstart>

Documented: Warp Oz provides a separate cloud-agent HTTP API. Warp desktop is
also a terminal host for externally launched CLI agents.

Observed: unavailable; Warp not installed.

Decision: reject Warp desktop as a Pokit agent adapter; it remains a terminal
host for `pokit run`. Defer a separate Oz cloud-adapter decision because it is
outside the current local-first runtime boundary.

## Generic PTY and instruction markers

Observed synthetic PTY bytes include alternate-screen `CSI ?1049h/l`,
erase-line `CSI 2K`, and carriage return. These are valid structural signals
for line projection versus TUI omission, not logical agent state. See
`docs/r1-fixtures/generic-pty-structural.json`.

`AGENTS.md`/`CLAUDE.md`-requested `POKIT_EVENT` text or OSC can be omitted by a
model and spoofed by user/tool/subprocess output. Reject it as approval,
lifecycle, or input authority; at most retain `source=prompt_hint` with low
confidence.

## T0/S1/N1 contract impact

T0 needs a provider-neutral envelope before any parser:

```text
RuntimeEvent
  eventId, sessionId, kind, observedAt
  source: runtime | provider_hook | provider_protocol | native_log |
          pty_structural | heuristic | prompt_hint
  confidence: authoritative | high | medium | low
  providerKind?, providerEventId?, providerVersion?
  correlation: proven | managed_launch | unavailable
  payload (redacted/minimal)
```

Precedence is daemon lifecycle/versioned protocol > managed hook > native log >
PTY structure > text heuristic/prompt hint. Weak evidence cannot manufacture
approval, input, or lifecycle authority.

Roadmap decision:

```text
T0   RuntimeEvent envelope, provenance, deduplication, version gates,
     and correlation registry
T1   generic byte-stream shell/unknown-agent Transcript
T2   PTY structural TUI safe degradation
T3.1 Claude managed-hook enrichment after correlation E2E
T3.2 Codex app-server or exec-json enrichment after correlation E2E
T3.3 OpenCode/Cline/Goose adapters after controlled-launch E2E
S1   status consumes daemon lifecycle and Tier A/B sources first
N1   notifications consume merged authoritative attention/completion events
```
