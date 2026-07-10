# R1 — Runtime Signal Discovery Plan

Status: planned research gate
Placement: after M3, before T0 Transcript Contract Reset
Duration: one to two working days maximum

This R1 is the post-M3 runtime-signal research phase. It is distinct from the
historical and already accepted `R1a Connectivity Baseline`.

## Decision to validate

Pokit must not assume that raw terminal text is the best available source for
states such as:

```text
running
waiting_for_input
waiting_for_approval
running_tool
completed
failed
```

Claude Code, Codex, and future agents may expose stronger lifecycle signals
through official hooks, structured protocols, or native logs. R1 identifies
those sources before T0 fixes the Transcript and Runtime Status contracts.

R1 does not assume those sources are available for every agent. Recorder and
the generic byte-stream path remain the universal fallback.

## Product boundary

R1 separates two problems:

```text
Readable Transcript projection
  ordinary shell output, errors, tests, logs, TUI omission

Runtime semantic state
  approval, input required, tool lifecycle, turn completion, failure
```

Stronger native signals may simplify semantic state and enrich Transcript, but
they do not remove the need for a readable shell/unknown-agent projection.

## Signal confidence model

Every candidate signal is classified by source, confidence, and stability.

| Tier | Source | Intended authority |
|---|---|---|
| A | documented native hook/protocol and daemon process lifecycle | authoritative |
| B | deterministic Pokit-managed integration or shell integration | high-confidence enrichment |
| C | agent-native JSONL/durable log | replayable enrichment, versioned parser |
| D | terminal modes, alternate screen, OSC/BEL, cursor/repaint structure | projection/TUI classification |
| E | text matching, quiet time, output burst frequency | best-effort fallback only |

Weak signals never override stronger contradictory signals. Research output
must recommend at least these provenance fields for future events:

```text
source
confidence
agentKind
sessionId
nativeEventId (when available)
observedAt
schema/protocol version
```

## R1.1 — Claude Code discovery

Investigate official Claude Code surfaces:

- `PermissionRequest`;
- `Notification`;
- `Stop`, `SessionStart`, and `SessionEnd`;
- `PreToolUse`, `PostToolUse`, and failure events;
- command and local HTTP hook JSON payloads;
- `session_id`, `transcript_path`, cwd, and controlled PTY correlation;
- headless/stream-JSON only as a comparison, not an automatic replacement for
  the interactive controlled PTY product.

Questions:

- Can a Pokit-managed hook emit structured events without changing raw PTY
  output?
- Which hook means turn completion versus session termination?
- Does `PermissionRequest` cover the approvals Pokit needs?
- How are hook failure, duplicate delivery, restart, and version changes
  represented?
- Can integration be installed with explicit consent without overwriting user
  hooks or project policy?

Official reference:

- <https://code.claude.com/docs/en/hooks>

## R1.2 — Codex discovery

Investigate official/current Codex surfaces:

- app-server thread/turn/item notifications and approval requests;
- generated TypeScript/JSON schema for the installed Codex version;
- `codex exec --json` JSONL behavior and its non-interactive limitation;
- local rollout/session JSONL and stable session identifiers;
- `notify` completion payload and its narrower event coverage;
- correlation between a normal interactive `codex` controlled PTY session and
  any structured source.

Do not assume an app-server event belongs to the same interactive TUI session
until correlation is demonstrated. Experimental transports or APIs must be
marked as such and isolated behind a provider capability/version boundary.

Official sources:

- <https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md>
- <https://github.com/openai/codex/blob/main/codex-rs/exec/src/lib.rs>

## R1.3 — Generic PTY discovery

Record what a Pokit-owned PTY can observe without agent cooperation:

- process start, natural exit, signal, and exit status;
- foreground process/group where portable and safe;
- raw/canonical mode and echo changes;
- alternate-screen entry/exit;
- cursor movement, erase operations, carriage-return progress, and repaint
  density;
- bracketed-paste, mouse, cursor visibility, synchronized-output modes;
- BEL/OSC notifications when present;
- output burst and quiet periods.

Explicit limitation: PTY ownership does not reliably reveal that a process is
blocked waiting on stdin, nor why it is quiet. Terminal modes and repaint
patterns may classify line output versus TUI output, but cannot authoritatively
mean `thinking`, `waiting_for_input`, approval, or logical completion.

## R1.4 — Instruction-emitted marker experiment

Evaluate `AGENTS.md` / `CLAUDE.md` instructions that ask an agent to emit a
`POKIT_EVENT` line or OSC sequence. The expected default decision is advisory
only because:

- a model may omit or malform the marker;
- user/tool/subprocess output can spoof it;
- completion/crash/compaction can prevent delivery;
- OSC creates terminal-injection and filtering concerns;
- agent instruction precedence and behavior change over time.

No prompt-emitted marker may authorize input, approval, Stop, Kill, or Delete.
If retained at all, it must be classified as `source=prompt_hint` with low
confidence. Prefer deterministic hooks or structured provider protocols.

## Evidence scenarios

Run the same bounded scenarios where supported:

1. session start and ordinary response;
2. simple tool/command start and completion;
3. approval request and approve/decline;
4. question requiring user input;
5. successful turn completion;
6. command or turn failure;
7. user interrupt and natural process exit;
8. reconnect/resume where the provider supports it.

For every scenario capture synchronized, redacted evidence:

```text
raw PTY structural trace (not committed if sensitive)
native hook/protocol/log event
timestamp and session identifiers
expected product state
false-positive / false-negative observation
```

Only minimal redacted fixtures and manifests may be committed. Apply the
existing fixture redaction contract to paths, usernames, repositories, prompts,
commands, source, tokens, and identifiers.

## Required deliverables

R1 completes with documents, not runtime code:

1. `R1_RUNTIME_SIGNAL_MATRIX.md` containing each signal's provider, event,
   official/observed/heuristic status, stability, correlation key, confidence,
   failure behavior, and proposed consumer.
2. A redacted evidence/fixture manifest identifying what was captured and what
   was intentionally not committed.
3. An architecture decision record classifying each source as adopt, optional
   enrichment, defer, or reject.
4. T0/S1 impact notes defining provenance, precedence, deduplication, version
   negotiation, and fallback requirements.
5. A bounded list of unknowns; absence of perfect semantics does not extend R1.

## Acceptance criteria

- no production runtime, Recorder, mobile, or authentication behavior changes;
- no persistent modification of the user's real Claude/Codex configuration;
- every conclusion is labeled documented, directly observed, or inferred;
- Claude and Codex have independent signal matrices;
- generic PTY capabilities and impossibilities are stated explicitly;
- provider event/session correlation is proven or marked unavailable;
- version/experimental stability and failure isolation are recorded;
- prompt marker spoofing and terminal escape injection are evaluated;
- recommendations preserve Recorder as the sole PTY reader;
- missing native integration falls back to generic Transcript safely;
- R1 finishes within the time box with adopt/defer/reject decisions.

## Explicit non-goals

- implementing Claude hooks or a Pokit plugin;
- integrating Codex app-server into production;
- changing `pokit run` launch behavior;
- adding provider-specific mobile branches;
- designing the final Runtime Status UI;
- rewriting Transcript or Activity storage;
- parsing every ANSI/TUI state;
- treating model-emitted text as a security or lifecycle authority;
- researching arbitrary undocumented process memory/private APIs;
- expanding into every available agent.

## Roadmap effect

After R1:

```text
T0  uses the provenance/source decisions when resetting the event contract
T1  still builds the minimal generic byte-stream Transcript fallback
T2  uses PTY structural evidence for safe TUI degradation
T3  adds only accepted native semantic enrichers
S1  derives status from authoritative lifecycle/native signals first
```

If no strong provider signal correlates with the interactive session, R1 still
succeeds: Pokit proceeds with T0/T1 and records native integration as deferred.

## Rollback

R1 is documentation and redacted research evidence only. Rejecting a candidate
signal requires no product rollback. Provider-specific experiments must use a
temporary HOME/config or reversible isolated setup and leave the user's normal
agent configuration unchanged.
