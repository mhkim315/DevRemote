# Base Alpha Activation Roadmap

**Status:** AUTHORITATIVE POST-9.0 EXECUTION ORDER

**Prerequisite:** Step 9.0 independent closeout COMPLETE at `921804fef` (or later accepted SHA).

## 1. Bounded Activation Sequence

Each wave requires a separate reviewed packet and independent ACCEPT before the next.

| Wave | Scope | Gate |
|------|-------|------|
| 9.1 | Minimal Timeline staging — enable `--enable-timeline-shadow` in controlled environment; fail-open writes of RuntimeStarted/Stopped, ApprovalRequested/Resolved, InputDelivered, TaskCompleted/Failed events. Bounded queue. Privacy/redaction enforced. | Independent ACCEPT |
| 9.2 | Transcript/Activity projection convergence — compare Timeline-derived projections against existing Transcript read model. Equivalence report. | Independent ACCEPT |
| 9.3 | N1 exact-event notification-to-action — single daemon event → mobile push → user action roundtrip. No batch, no queue aggregation. | Independent ACCEPT |
| 9.4 | Secure accountless onboarding — device identity without cloud account. QR pairing V1 hardening. Keystore recovery. | Independent ACCEPT |
| 9.5 | Matched Base Alpha candidate + SM-S926N product gate — freeze candidate, rebuild matched daemon/APK, full device smoke matrix. | Independent PB-style ACCEPT |

## 2. Deferred

- Manual Alpha coordination/validation (optional, separate authorization)
- Broad CT-P2 and automation (separate authorization)
- Container/VM isolation
- Automatic provider/model selection
- Forked recovery worktrees

## 3. Authority Boundary

- Each wave is independently accepted before the next begins.
- No wave may modify the frozen PB candidate `059bef181` or its matched artifacts.
- Foundation Steps 1-8 contracts remain frozen unless a wave-specific amendment is independently accepted.
- CT-P2 remains blocked; Base Alpha is not CT-P2.

## 4. Stop Conditions

- Stop if any wave weakens accepted lifecycle, generation, transport, device trust, approval, input, pairing, or artifact-provenance contracts.
- Stop if Timeline is used as authority.
- Stop if shadow writes block primary operations.
- Stop if a wave modifies frozen foundation contracts without independent amendment acceptance.
