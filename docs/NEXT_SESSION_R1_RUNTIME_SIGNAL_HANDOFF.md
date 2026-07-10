# Next Session Handoff — R1 Runtime Signal Discovery

Status: future research handoff; do not start before M3 acceptance unless the
orchestrator explicitly authorizes independent research

Read first:

- `docs/R1_RUNTIME_SIGNAL_DISCOVERY_PLAN.md`
- `docs/MOBILE_SESSION_LIFECYCLE_AND_TRANSCRIPT_PLAN.md`
- `docs/ROADMAP_AFTER_E10B.md`

## Mission

Complete the two-day-hard-cap R1 evidence phase. Determine which Claude Code,
Codex, OpenCode, Orca, Omnara, Cline, Aider, Goose, Continue, Warp, and generic
PTY signals can safely inform Runtime Status, Notifications, or Transcript
without changing production behavior.

## Execution rules

- research, runtime evidence, and architecture decisions only;
- use official documentation/source for claimed provider contracts;
- use isolated temporary configuration for experiments;
- do not edit the user's actual `~/.claude`, `~/.codex`, AGENTS.md, or CLAUDE.md;
- do not install persistent hooks/plugins;
- do not modify Recorder, ActivityBuffer, Transcript, mobile, or auth code;
- do not commit raw prompts, source, commands, paths, tokens, or native logs;
- classify every claim as documented, observed, heuristic, or unavailable;
- use `unavailable` rather than guessing an ambiguous product or inaccessible
  runtime;
- research-only probes may be committed outside production packages, but no
  daemon/mobile/runtime code may import or execute them;
- stop when the time box expires.

## Required work

1. Build the Claude signal matrix for hooks and session correlation.
2. Build the Codex signal matrix for app-server, JSONL, notify, and interactive
   session correlation.
3. Resolve and inspect OpenCode, Orca, Omnara, Cline, Aider, Goose, Continue,
   and Warp using official docs/public source and practical runtime capture.
4. Record exact event producer, transport, version, failure behavior, and
   `controlled_pty` correlation for every integration.
5. Record generic PTY structural signals and the semantic states they cannot
   prove.
6. Test instruction-emitted markers only to quantify unreliability/spoofing;
   never recommend them as authoritative.
7. Produce adopt/defer/reject decisions and update the Transcript, Runtime Event
   Merger, Status, and Notification roadmap.

## Required output

Add:

- `docs/R1_RUNTIME_SIGNAL_MATRIX.md`;
- exact official source references and a redacted fixture/evidence manifest;
- per-agent controlled-session correlation results;
- recommended adapter strategy and adopt/defer/reject decision per integration;
- an ADR or decision section containing source precedence and fallback;
- an R1 completion report with remaining unknowns and updated downstream
  roadmap.

Do not implement accepted integrations. Their implementation belongs to later
provider-enrichment phases after verifier acceptance.

## Acceptance report

Return:

```text
R1 status: ACCEPT / SCOPED ACCEPT / NEEDS FOLLOW-UP
Commit: <hash>

Claude:
- strongest signal
- correlation result
- stability/failure result

Codex:
- strongest signal
- correlation result
- stability/failure result

OpenCode / Orca / Omnara / Cline / Aider / Goose / Continue / Warp:
- exact product/version/source
- signal transport and runtime evidence status
- controlled_pty correlation
- adopt/defer/reject and adapter strategy

Generic PTY:
- reliable structural signals
- semantic limits

Decisions:
- adopt
- optional enrichment
- defer/reject
- per-agent adapter strategy
- T0/Event Merger/S1/N1 contract impact
```
