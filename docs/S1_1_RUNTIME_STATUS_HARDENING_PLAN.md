# S1.1 Runtime Status Hardening — Planning Boundary

Status: **R3 REMEDIATION 2 REQUIRED — re-verification REJECT at `faa3917`**

S1.1 is a separate hardening milestone between accepted S1 and A1. S1 was
independently accepted at implementation `b6504bd7d5c634f0c0459ae87503b82d17c1537b`
with canonical marker `70ef5df28dede7b0f3025eeaab7826f76a229fbf`.

Authoritative execution handoff:
`docs/NEXT_SESSION_S1_1_REMEDIATION_2_HANDOFF.md`.

Independent evidence:
`docs/S1_1_RUNTIME_STATUS_HARDENING_REVERIFICATION.md`.

## 1. Purpose

Harden runtime evidence traceability, exact runtime identity, and recovery/replay
behavior without changing the frozen T0/S1 authority model or expanding the
public DTO without a demonstrated consumer.

## 2. Frozen constraints

- T0 `ResolveStatus`, provenance ranking, confidence ceiling, and closed status
  vocabulary remain authoritative.
- Process, CWD, PTY, prompt, Transcript, and screen signals may support bounded
  identity or diagnostics but cannot select agent status.
- `waiting_approval` remains display-only and cannot create or authorize an
  approval action.
- No raw adapter cursor, duplicate evidence-source field, host identity, public
  degraded reason, or new public status is added without a separately reviewed
  downstream requirement.
- S1.1 does not implement A1, O1, O2, Git/worktree operations, automated command
  execution, automatic rework, review bundles, merges, or Windows support.

## 3. S1.1-A — Bounded winning-evidence metadata

Reuse the current internal `AgentActivityRecord` fields: status, provenance,
confidence, observed time, bounded degraded reason, stream generation, and
provider version. Do not duplicate them under new names.

Evaluate only the minimum additional bounded identity needed to explain ordering,
such as winning event ID, winning `Seq`, or an equivalent internal revision.
Keep opaque adapter cursors in the adapter ingestion owner. Do not expose new
public fields unless an identified S1/A1/O1 consumer and privacy/bounds review
justify them.

## 4. S1.1-B — Runtime identity binding

Build on existing canonical session ID, adapter kind, provider/version,
`adapterState.streamGen`, and `LaunchBinding`. Required research/implementation
questions include:

- real monotonic launch generation rather than a constant generation;
- safe launch-binding replacement with incompatible prior evidence invalidated;
- PID plus process-start identity or an equivalent non-reusable process token;
- adapter/provider/version/correlation-generation mismatch handling;
- atomic replacement so no prior-positive authority window remains.

PID alone is never sufficient because it can be reused. Host identity is not part
of the current daemon-local in-memory status key and must not be added without a
cross-host persistence/consumer requirement.

## 5. S1.1-C — Recovery and replay hardening

Cover:

- same-generation replay and cursor regression;
- cursor/event duplication and resume;
- bounded-store eviction under churn;
- correlation loss and recovery;
- adapter/provider/launch-binding replacement;
- daemon recovery boundaries after accepted S1 behavior;
- stale event rejection using the existing generation/Seq/cursor model.

Recovery may produce absent, unknown, unavailable, or degraded status. It must
never confidently display incompatible prior evidence as current.

## 6. Outputs and acceptance boundary

Before implementation, create a focused S1.1 execution handoff from the accepted
S1 SHA. S1.1 acceptance must include an internal evidence/identity matrix, bounded
negative tests, recovery/replay regression evidence, full gates, and confirmation
that the public DTO and frozen T0 contract were unchanged unless separately
approved.

After independent S1.1 acceptance, proceed to A1. Do not combine the S1.1 and A1
review requests.

The first A/B/C implementation (`715c22b`, report head `1547c93`) passed the full
gate but was independently rejected for three narrow correctness gaps: exact
winner binding omitted confidence, adapter identity was stored but not checked,
and replacement publication preceded status invalidation. These are S1.1
correctness requirements, not new features. Complete the focused remediation
handoff before requesting acceptance again.

The first remediation (`ef47b55`, report head `faa3917`) independently closes
the winner-confidence and adapter-binding defects. Its replacement path remains
non-atomic across concurrent registrations because reserve, lookup, invalidate,
and publish are separately locked. Only this R3 transaction remains active; do
not reopen accepted R1/R2 or begin A1.

The ordinary local CLI currently remains a managed-lifecycle path: it sends a
legacy command string and does not register recognized-agent launch authority.
S1.1 acceptance must not describe that path as recognized managed-agent or
orchestration-certified. Redesigning the CLI path is outside the focused
remediation and must be handled as an explicit downstream readiness item rather
than inferred from command/process text.

## 7. Approval boundary carried forward

A1 authority must come from ApprovalStore state bound to exact session ID,
approval ID, authoritative approval provenance, and current generation. S1/S1.1
runtime status is never authorization input.
