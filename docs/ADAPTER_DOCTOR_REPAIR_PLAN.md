# Adapter Doctor/Repair Plan

Status: **PLANNED — do not implement before T1, T2, and R2 are complete**

Position in the execution sequence:

```text
M3-auth-1B
→ M3-auth-2B
→ M3b lifecycle UX
→ T0 common AgentEvent contract
→ T1 Codex adapter
→ T2 Claude adapter
→ R2 multi-agent expansion research
→ D1 Adapter Doctor/Repair
→ T3 Transcript integration
→ S1 status
→ A1 approval
→ O1 orchestrator
```

Windows remains deferred until after the Mac/Android closed beta. D1 does not
introduce ConPTY, CNG/TPM, DPAPI, Named Pipes, Windows Services, NTFS ACL work,
or speculative Windows provider abstractions.

This sequence supersedes the earlier planning sequence that placed D1 directly
after T2. The earlier documents remain historical evidence; they do not
authorize D1 before the research-only R2 gate. R2 itself is defined in
`docs/R2_MULTI_AGENT_EXPANSION_RESEARCH_PLAN.md`.

## Purpose

Codex CLI and Claude Code are independently versioned products. Their JSONL
locations, hook entry points, field names, and event schemas may change without
Pokit changing. D1 provides a user-controlled repair workflow for ordinary
version drift:

```text
adapter compatibility check fails
→ collect bounded/redacted evidence
→ local coding agent inspects the version-specific implementation and fixtures
→ coding agent proposes a narrow adapter patch
→ fixed compatibility and regression suite runs
→ user reviews diff, provenance, and test results
→ user explicitly approves or rejects activation
```

D1 is not a self-modifying core runtime. It is a constrained patch-generation
and review surface around version-specific adapters.

## Pokit-owned stable contract

Pokit owns the following contract and its tests. The repair agent may not change
its meaning, signature, safety defaults, or downstream DTOs:

```text
detect
discoverSessions
readEvents
normalizeEvent
detectApproval
getStatus
```

Contract intent:

| Operation | Stable responsibility |
| --- | --- |
| `detect` | determine whether a supported agent/version is present, with provenance and confidence |
| `discoverSessions` | enumerate correlatable agent sessions without inventing terminal ownership |
| `readEvents` | read bounded, incremental, version-native records using an explicit cursor |
| `normalizeEvent` | map one native record to the common AgentEvent contract or a safe unknown result |
| `detectApproval` | emit approval only from accepted high-confidence evidence; ambiguous input is not approval |
| `getStatus` | derive common status from accepted normalized evidence and explicit precedence rules |

The stable contract, common AgentEvent schema, permission model, approval
semantics, mobile behavior, Recorder ownership, and Terminal transport are not
repair targets.

## Allowed repair scope

The coding agent may modify only the selected version-specific adapter
implementation and its version-specific fixtures/mappings. Examples:

- Codex JSONL path resolver for a newly observed Codex version;
- Claude hook resolver for a moved hook location;
- renamed native fields mapped into an existing common field;
- an updated event discriminator table;
- a version capability declaration;
- new redacted fixtures for the changed version.

It may not modify:

- the common `AgentEvent` or approval contract;
- stable adapter interfaces;
- Recorder, PTY, lifecycle, auth, ticket, audit, or mobile authorization code;
- unrelated adapters or older-version fixtures;
- build/security gates;
- filesystem/process sandbox policy;
- activation or user-approval logic.

Any proposed change outside the allowlisted adapter subtree is rejected before
tests run.

## Detection and evidence

Compatibility detection compares observed evidence with the adapter's declared
version/shape support. A drift report is bounded and redacted and may contain:

- agent product and reported version;
- adapter version range;
- missing/changed path candidates without file contents;
- unknown event discriminator and bounded field-name/type shape;
- hook discovery result;
- cursor/read failure category;
- fixture/test mismatch identifiers.

Do not include prompts, source code, terminal input, tokens, signatures, private
keys, full home paths, or unrestricted raw JSONL. Evidence collection must be
read-only and must not change Claude/Codex configuration.

## Repair sandbox

The local coding agent runs with a dedicated workspace and explicit allowlist:

- read: selected adapter source, stable contract documentation, redacted drift
  evidence, relevant public source/docs already acquired by the user;
- write: a temporary patch workspace containing only the selected adapter and
  new version fixtures;
- process: fixed formatter/compiler/test commands only;
- network: disabled by default; any documentation/source retrieval is a separate
  user-approved research action;
- secrets: unavailable;
- host agent logs: unavailable except the prepared redacted fixture/evidence;
- activation path: unavailable.

No arbitrary shell, home-directory scan, package installation, daemon restart,
or agent configuration edit is granted to the repair agent.

## Mandatory safety rules

1. **No automatic unreviewed activation.** A generated patch remains inert until
   the user explicitly approves it.
2. **Fixed tests must pass.** The repair agent cannot edit, skip, weaken, or
   replace the compatibility/security suite.
3. **Older versions remain covered.** Every accepted older-version fixture and
   regression must continue to pass.
4. **Unknown events fail safely.** Unknown records become bounded unknown/degraded
   evidence; they never crash Terminal or fabricate semantic events.
5. **Prevent approval false positives.** New or ambiguous fields cannot map to
   `approval_requested` without an accepted positive fixture and negative
   near-miss tests.
6. **Restrict filesystem and process access.** Only declared paths and commands
   are available during inspection and patch generation.
7. **Show evidence before approval.** The UI/CLI presents the exact diff,
   changed-file allowlist, test commands, complete pass/fail summary, adapter and
   agent versions, and remaining unknowns.
8. **Activation is reversible.** Preserve the prior accepted adapter and allow
   immediate rollback without modifying stored common events.
9. **No cross-adapter collateral changes.** Repairing Codex cannot change Claude,
   and repairing Claude cannot change Codex.
10. **Terminal remains available.** Adapter detection/repair failure degrades
    metadata only and never blocks the controlled PTY or raw Live Terminal.

## Fixed compatibility and regression suite

The suite is Pokit-owned and includes at least:

- stable contract conformance for all six operations;
- current-version positive fixtures;
- every retained older-version fixture;
- malformed/truncated/unknown records;
- unknown-field forward compatibility;
- incremental cursor and duplicate suppression;
- session-correlation positive and negative cases;
- status transition precedence;
- approval positive fixtures and adversarial near-miss negatives;
- parser panic/error isolation and degraded fallback;
- bounded reads and filesystem allowlist enforcement;
- no raw secret/path/prompt leakage in diagnostics;
- unchanged common DTO snapshots;
- unchanged non-target adapter results.

Tests run outside the patch agent's writable test area. A patch cannot update an
expected result merely to make a failure green; new fixtures require a separate
reviewed addition with provenance.

## Review and activation

Before activation, show:

```text
agent product/version
current adapter/version range
detected incompatibility and provenance
files changed (must match allowlist)
unified diff
new fixtures and redaction manifest
fixed-suite result
older-version regression result
approval false-positive result
remaining unsupported/unknown evidence
rollback target
```

The user chooses `Approve and activate` or `Reject`. There is no default approval,
timeout approval, background activation, or activation based only on a green
test result.

## Limits

D1 is intended for ordinary version drift such as:

- changed JSONL paths;
- renamed fields;
- moved hooks;
- modified but still observable event schemas.

It does **not** guarantee recovery when required information has been removed,
encrypted, made inaccessible, or no longer correlates to a Pokit session. In
those cases the adapter remains degraded/unsupported and Terminal continues
without fabricated status or approval signals.

## Planning acceptance

This stage is ready for implementation planning only after T1 and T2 receive
independent ACCEPT decisions and R2 establishes:

- accepted Codex and Claude fixtures across known versions;
- the immutable six-operation contract;
- a fixed external contract harness;
- explicit adapter source/write allowlists;
- approval false-positive negative fixtures;
- version/capability declarations and safe unknown behavior.
- real multi-provider version-drift scenarios and a fit-gap assessment against
  the frozen T0 contract.

No D1 production implementation is authorized by this document.
