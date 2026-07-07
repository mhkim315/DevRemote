# Agent Adapter Phase A0 Review

Date: 2026-07-07

Executor commit: `21addd65f`

Verifier decision: **REJECT**

## Scope verification

The commit is documentation-only:

- added `docs/AGENT_ADAPTER_PHASE_A0_VERIFICATION.md`
- no production code changes
- no fixture changes
- no mobile changes

That part is within Phase A0 scope. However, the document does not satisfy the
handoff requirement because it records contradictory phase ordering and leaves its
own discovered gaps unapplied.

## Findings

### P1 — Phase A7-A10 order is rewritten incorrectly

File: `docs/AGENT_ADAPTER_PHASE_A0_VERIFICATION.md`

The verification table states:

```text
A7 | Approval UX
A8 | Redaction + diagnostics
A9 | Antigravity vertical slice
A10 | 운영 문서 + handoff
```

This contradicts both approved handoff documents:

```text
A7 Third agent / Antigravity slice
A8 Approval UX
A9 Agent UX
A10 Diagnostics / doctor
```

Why this blocks Phase A0 acceptance:

- Phase A0 is supposed to lock scope and execution order.
- The new verification document is now part of the repository handoff trail.
- A future execution agent could follow the wrong A7-A10 sequence.
- It also invents "Redaction + diagnostics" as A8, even though redaction is an A1
  acceptance requirement and diagnostics is A10 productization.

Required fix:

- Correct the table to match `docs/AGENT_ADAPTER_LAYER_PLAN.md` and
  `docs/NEXT_SESSION_AGENT_ADAPTER_HANDOFF.md`.
- Do not introduce alternate phase names in the verification document.

### P1 — Discovered gaps were not applied to the actual plan/handoff

File: `docs/AGENT_ADAPTER_PHASE_A0_VERIFICATION.md`

The document says two gaps were found:

1. Antigravity `transcript.jsonl` structure must be checked in A1.
2. Codex uses `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`.

But the commit only adds a verification note. It does not update
`docs/AGENT_ADAPTER_LAYER_PLAN.md` or `docs/NEXT_SESSION_AGENT_ADAPTER_HANDOFF.md`.

Why this blocks Phase A0 acceptance:

- Phase A0's job is to confirm and refine the plan.
- If a gap is found, the plan/handoff should carry the resulting instruction.
- Leaving the correction only in a side review document makes the executor handoff
  ambiguous.

Required fix:

- Update Phase A1 in `docs/AGENT_ADAPTER_LAYER_PLAN.md` to include:
  - `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`
  - `~/.gemini/antigravity/brain/*/transcript.jsonl`
  - the requirement to inspect at least one Antigravity transcript structure before
    Phase A2.
- Update `docs/NEXT_SESSION_AGENT_ADAPTER_HANDOFF.md` with the same A1 path hints.

### P2 — Codex handoff gap references a pattern that is not in the current handoff

File: `docs/AGENT_ADAPTER_PHASE_A0_VERIFICATION.md`

The document says the handoff contains `~/.codex/projects/*/`. The current
`docs/NEXT_SESSION_AGENT_ADAPTER_HANDOFF.md` does not contain that pattern.

Why this matters:

- The finding direction is useful, but the evidence should reference the actual
  document state.
- Phase A0 is a planning baseline; inaccurate review notes reduce trust in the
  handoff trail.

Required fix:

- Reword the Codex finding as an additive Phase A1 path hint rather than as a
  correction to a nonexistent handoff line.

## Automated verification

Executed:

```sh
git diff --check HEAD~1..HEAD
```

Result: passed.

No Go/mobile tests were required because the executor commit is documentation-only.

## Required executor actions

1. Fix the A7-A10 table in `docs/AGENT_ADAPTER_PHASE_A0_VERIFICATION.md`.
2. Update `docs/AGENT_ADAPTER_LAYER_PLAN.md` Phase A1 with the discovered Codex and
   Antigravity path hints.
3. Update `docs/NEXT_SESSION_AGENT_ADAPTER_HANDOFF.md` Phase A1 with the same path
   hints.
4. Keep Phase A0 documentation-only. Do not collect or commit raw fixture content yet.

## Next phase permission

**BLOCKED**

Phase A1 should not start until the Phase A0 handoff documents reflect the discovered
path/structure information and the phase sequence is consistent across all Agent
Adapter docs.
