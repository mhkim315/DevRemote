# R1 — Runtime Signal Discovery Plan

Status: planned research gate
Placement: after M3, before T0 Transcript Contract Reset
Duration: two working days hard cap; finish earlier when decisions are complete

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

R1 is evidence-based, not documentation-only. Official documentation and public
source establish the claimed contract; redacted runtime capture establishes
what the installed product actually emits. When runtime capture is impractical,
the matrix must say `not observed` and record the concrete access, platform,
license, installation, or correlation blocker.

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

## Required integration inventory

R1 covers these named products independently:

```text
Claude Code
Codex CLI
OpenCode
Orca
Omnara
Cline
Aider
Goose
Continue
Warp
```

The researcher must first resolve the exact product, publisher, repository,
runtime surface, and tested version. Names that are ambiguous (especially
`Orca`) must not be guessed: record candidate products and classify the target
as unavailable until the intended integration is established.

For every resolved target inspect:

- official documentation;
- publicly available first-party source code where available;
- exact event producer and consumer path;
- hook, plugin, app-server, OSC, JSONL, wrapper, PTY, or other transport;
- whether the signal exists in interactive mode, headless mode, IDE mode, or
  only a separate product surface;
- practical runtime output using an isolated test configuration where access
  and licensing permit;
- correlation to a Pokit `controlled_pty` session;
- version stability, failure behavior, and generic fallback.

Each matrix claim is labeled exactly one of:

```text
documented  official contract/source proves it
observed    a redacted runtime fixture proves it
heuristic   inferred from PTY/text/behavior only
unavailable exact product, source, runtime, or correlation could not be proven
```

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

## R1.3 — Additional integration discovery

Apply the common inventory and evidence contract to:

- OpenCode;
- Orca;
- Omnara;
- Cline;
- Aider;
- Goose;
- Continue;
- Warp.

Do not reduce these to feature-list summaries. Identify the exact notification
or runtime-event emission site in official documentation/source and trace its
transport to a possible Pokit consumer. Where practical, launch the installed
agent under an isolated `controlled_pty` or its documented server/plugin mode
and capture at least session start, one activity, and completion/attention
events. IDE-only and terminal-host products must be classified accurately and
must not inherit PTY-control capability merely because they can display a
terminal.

For each integration recommend one strategy:

```text
native signal adapter
managed hook/plugin
app-server or local protocol adapter
native JSONL/log enricher
controlled wrapper
PTY-only fallback
observe-only integration
defer
reject
```

## R1.4 — Generic PTY discovery

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

## R1.5 — Instruction-emitted marker experiment

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

Every observed fixture manifest also records:

```text
product and publisher
binary/plugin version
OS and launch surface
exact redacted invocation or setup ID
source URL plus commit/path/line or documentation section
capture timestamp
controlled_pty session correlation key and result
fixture SHA-256
redaction performed
```

Only minimal redacted fixtures, manifests, and reproducible research-only probes
may be committed. Probes must live outside production packages and cannot be
called by daemon/mobile runtime. Apply the existing fixture redaction contract
to paths, usernames, repositories, prompts, commands, source, tokens, and
identifiers.

## Required deliverables

R1 completes with evidence and decisions, not product features:

1. `R1_RUNTIME_SIGNAL_MATRIX.md` with a per-agent row and per-signal details.
2. Stable official documentation/source references, preferably commit-pinned
   source path/line references rather than search-result URLs.
3. Redacted observed fixtures plus a manifest identifying captures, hashes,
   correlation results, and material intentionally not committed.
4. Session-correlation feasibility for every integration.
5. Recommended adapter strategy for every integration.
6. A confidence/provenance and precedence model for Runtime Event Merger.
7. An adopt, defer, or reject decision for every integration.
8. Updated T0-T3, Runtime Event Merger, S1 Status, and N1 Notification roadmap
   based on the evidence.
9. A bounded list of unknowns; absence of perfect semantics does not extend R1.

## Acceptance criteria

- no production runtime, Recorder, mobile, or authentication behavior changes;
- no persistent modification of any real agent/IDE/terminal configuration;
- every conclusion is labeled `documented`, `observed`, `heuristic`, or
  `unavailable` using the definitions above;
- all ten named integrations have independent matrix entries;
- every target has an exact product/publisher/repository decision or an
  explicit ambiguity/unavailable result;
- official documentation and public source are cited where available;
- real runtime events are captured where practical; missing captures include a
  concrete reason and are never presented as observed;
- fixtures are redacted, hashed, reproducible, and correlated where possible;
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
- expanding beyond the ten named integrations;
- production parsing of any researched provider event.

## Roadmap effect

After R1:

```text
T0  uses the provenance/source decisions when resetting the event contract
T1  still builds the minimal generic byte-stream Transcript fallback
T2  uses PTY structural evidence for safe TUI degradation
T3  adds only accepted native semantic enrichers and adapter strategies
S1  derives status from authoritative lifecycle/native signals first
N1  uses merged attention/completion events rather than Transcript text alone
```

If no strong provider signal correlates with the interactive session, R1 still
succeeds: Pokit proceeds with T0/T1 and records native integration as deferred.

## Rollback

R1 changes no production behavior. Rejecting a candidate signal requires no
product rollback. Provider-specific experiments must use a temporary
HOME/config or reversible isolated setup and leave the user's normal agent
configuration unchanged. Research-only probes and fixtures can be removed as a
single isolated commit if their retention is not justified.
