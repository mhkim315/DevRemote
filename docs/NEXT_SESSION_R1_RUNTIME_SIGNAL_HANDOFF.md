# Next Session Handoff — R1 Runtime Signal Discovery

Status: future research handoff; do not start before M3 acceptance unless the
orchestrator explicitly authorizes independent research

Read first:

- `docs/R1_RUNTIME_SIGNAL_DISCOVERY_PLAN.md`
- `docs/MOBILE_SESSION_LIFECYCLE_AND_TRANSCRIPT_PLAN.md`
- `docs/ROADMAP_AFTER_E10B.md`

## Mission

Complete the one-to-two-day R1 evidence phase. Determine which Claude Code,
Codex, and generic PTY signals can safely inform Runtime Status or Transcript
without changing production behavior.

## Execution rules

- research and documentation only;
- use official documentation/source for claimed provider contracts;
- use isolated temporary configuration for experiments;
- do not edit the user's actual `~/.claude`, `~/.codex`, AGENTS.md, or CLAUDE.md;
- do not install persistent hooks/plugins;
- do not modify Recorder, ActivityBuffer, Transcript, mobile, or auth code;
- do not commit raw prompts, source, commands, paths, tokens, or native logs;
- classify every claim as documented, observed, or inferred;
- stop when the time box expires.

## Required work

1. Build the Claude signal matrix for hooks and session correlation.
2. Build the Codex signal matrix for app-server, JSONL, notify, and interactive
   session correlation.
3. Record generic PTY structural signals and the semantic states they cannot
   prove.
4. Test instruction-emitted markers only to quantify unreliability/spoofing;
   never recommend them as authoritative.
5. Produce adopt/optional/defer/reject decisions and T0/S1 constraints.

## Required output

Add:

- `docs/R1_RUNTIME_SIGNAL_MATRIX.md`;
- a redacted fixture/evidence manifest;
- an ADR or decision section containing source precedence and fallback;
- an R1 completion report with remaining unknowns.

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

Generic PTY:
- reliable structural signals
- semantic limits

Decisions:
- adopt
- optional enrichment
- defer/reject
- T0/S1 contract impact
```
