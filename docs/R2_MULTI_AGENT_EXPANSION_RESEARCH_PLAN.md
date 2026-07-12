# R2 Multi-agent Expansion Research Plan

Status: **PLANNED — research only**

Entry gate: **both T1 Codex and T2 Claude must have independent ACCEPT
decisions.** Finishing either implementation, passing its local tests, or
writing its implementation report is not sufficient.

Execution position:

```text
T1 Codex adapter
→ T2 Claude adapter
→ R2 Multi-agent expansion research
→ D1 Adapter Doctor/Repair
→ T3 Transcript integration
→ S1 Status
→ A1 Approval
→ O1 Orchestrator
```

This document changes future planning only. It does not authorize R2 during T1
or T2 and does not change either adapter's active handoff or acceptance history.
The future authoritative T2 handoff must state that R2 follows independent T2
ACCEPT, but must not instruct the T2 executor to perform R2.

## Purpose

R2 verifies that the frozen T0 contract, the accepted T1/T2 adapter model, the
D1 repair model, and the future Transcript design are not overfitted only to
Codex and Claude. It studies public documentation and legally inspectable
implementations or controlled local experiments. Useful candidates include:

- Gemini CLI;
- OpenCode;
- Cline;
- Aider;
- OpenHands;
- other actively maintained local coding agents with inspectable session or
  event behavior.

R2 must not assume equivalent data exists across products. Signals that are
private, encrypted, inaccessible, unstable, UI-only, or inferred must be
recorded explicitly rather than reconstructed or presented as observed facts.

R2 does **not** implement a production adapter.

## Required analysis per project

### Event sources

Record available provider protocols, hooks, stream JSON/JSONL, local databases,
transcript/history files, process evidence, and PTY/screen fallback. Distinguish
native structured evidence from advisory text or UI observation.

### Session discovery and correlation

Record native session IDs, process/CWD binding, managed-launch evidence, and the
risk of cross-session attribution. Do not claim ownership from discovery alone.

### Ordering and replay

Record native sequence, timestamp, cursor or byte offset, reconnect/resume
behavior, and duplicate-event behavior. Identify whether the source supports an
append-position cursor or only best-effort observation.

### Semantic coverage

Compare support for user messages, assistant messages, partial output, tool
calls/results, status transitions, approval requests/results, completion,
failure, and unknown events.

### Provenance and authority

Classify signals as authoritative, advisory, spoofable, or heuristic. State
whether each source can safely establish approval or terminal agent status under
the frozen T0 authority rules.

### Version-drift risks

Record observed or documented path changes, renamed fields, schema changes,
hook changes, storage migrations, and removed or inaccessible data. Separate
confirmed drift from hypothetical risk.

### T0 fit

Classify each concept as:

1. fully representable by T0;
2. representable through bounded metadata or an unknown event;
3. safely handled inside a version-specific adapter; or
4. a possible cross-provider gap requiring a later, separate contract review.

## Required outputs

R2 produces:

1. a source matrix comparing all researched agents;
2. a traceability map from external observations to T0, T1/T2, D1, T3, S1,
   and A1;
3. legally usable, redacted fixtures where public behavior can be reproduced;
4. a fit-gap report against the frozen T0 contract;
5. additional D1 repair scenarios derived from real version-drift patterns;
6. a recommendation for the best third production-adapter candidate.

The recommendation is planning evidence only. Implementing the third adapter
requires a later, separately authorized handoff and acceptance scope.

## Contract-change policy

R2 findings never modify T0 automatically. Apply this order:

```text
new provider-specific concept
→ attempt adapter-local mapping
→ attempt bounded metadata or unknown-event representation
→ compare against Codex, Claude, and at least one additional provider
→ propose a separately reviewed contract revision only if genuinely cross-provider
```

Do not add a common field merely because one project exposes it. R2 may propose
a change, with evidence and compatibility impact, but cannot edit the frozen
contract or fixed conformance harness.

## Fixture and research safety

Every fixture or capture must:

- contain no secrets, tokens, private prompts, private repository paths, or
  personal data;
- be reproducible from public documentation or a controlled local experiment;
- record provider, version, source, and collection provenance;
- distinguish direct observation from inference;
- avoid restricted or non-redistributable code and data;
- preserve unknown or unavailable fields as unknown rather than fabricate them.

Public research does not authorize network scraping, account access, provider
configuration changes, or unrestricted home-directory inspection. Any such
action requires separate user authorization and must remain within applicable
licenses and terms.

## Scope exclusions

R2 must not:

- implement Gemini, OpenCode, Cline, Aider, OpenHands, or another production
  adapter;
- modify T0, T1, T2, D1 production code, Transcript, Status, Approval, or
  Orchestrator behavior;
- change Terminal, Recorder, authentication, lifecycle, mobile DTOs, or public
  wire contracts;
- treat PTY text as authoritative approval or status;
- begin Windows runtime work or introduce speculative Windows abstractions.

Windows support remains deferred. R2 evaluates provider-neutral wire and event
contracts only; ConPTY, CNG/TPM, DPAPI, Named Pipes, Windows Services, NTFS ACLs,
and Windows-specific host providers remain out of scope.

## R2 completion condition

R2 is complete only when all required outputs are reviewed and their evidence
clearly separates observation, inference, unavailable signals, and unresolved
gaps. Completion authorizes planning D1; it does not authorize an automatic T0
revision, third adapter, repair activation, or product integration.
