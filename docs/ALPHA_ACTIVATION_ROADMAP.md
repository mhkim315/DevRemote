# POKIT Base Alpha Activation Roadmap

**Status:** AUTHORITATIVE POST-9.0 EXECUTION PLAN — STEP 9.0 ACCEPTED at `62a50f0a8`; STEP 9.1 ACCEPTED at `dc376f9b7`; STEP 9.2 ACCEPTED at `c82fef47f`; STEP 9.3 ACCEPTED at `f7033b86c` (EVID: `STEP9_3_EVIDENCE.md`); STEP 9.4 NOT STARTED

**Prerequisite:** Step 9.0 authority reconciliation received independent
closeout at `62a50f0a8`. Step 9.1 Operational Timeline staging received
independent ACCEPT at `dc376f9b7`. Step 9.2 Transcript/Activity projection
convergence received independent ACCEPT at `c82fef47f`. Step 9.3 N1
exact-event notification-to-action received independent ACCEPT at
`f7033b86c`. Step 9.4 remains not started — a separate, reviewed
implementation contract is required before Step 9.4 may proceed.

The product boundary is:

> **Managed native-session control first, structured operational views second,
> Terminal as fallback, manual orchestration optional.**

The current state is:

> **Post-PB foundation complete, alpha activation pending.**

Steps 4–8 are accepted foundations. Foundation acceptance is not proof that a
feature is composed, enabled, healthy, or product-live.

## 1. Base Alpha boundary

The first dogfoodable alpha must provide complete value with coordination,
validation, workspace leases, and orchestration disabled. It includes only:

- provider-native managed Codex and Claude sessions through `pokit run`;
- exact provider/runtime/session/generation identity;
- the existing Transcript service and API authority;
- minimal Operational Canonical Timeline activation for Activity and N1;
- distinct Activity and Transcript projections converging on shared canonical
  identity;
- N1 exact-event notification-to-action;
- secure accountless onboarding;
- approval, deny, exact-generation acknowledged input, interrupt, stop, kill,
  reconnect, permission revalidation, and stale-generation rejection;
- always-accessible Terminal fallback under the accepted `TerminalTransport`
  and sole-reader `Recorder` boundaries;
- reproducible daemon/APK source and artifact identity; and
- a bounded real-device alpha safety gate.

The following accepted foundations are **not** Base Alpha prerequisites:

- manual coordination or cross-provider handoff;
- manual or frozen validation activation;
- cooperative workspace lease activation;
- Cockpit activation;
- automatic coordinator or validator dispatch;
- automatic revision, provider switching, or result adoption;
- background or parallel external agents; and
- Navigator, Guard, scorer, or provider-native subagent control.

Manual coordination and validation form a later Manual Alpha wave. Automatic
orchestration is unavailable throughout alpha and is not POKIT's mandatory
final architecture.

## 2. Product surfaces

| Surface | Alpha responsibility | Authority boundary |
| --- | --- | --- |
| **Activity** | Condensed operational events and intervention points | Projection only; never runtime, lifecycle, approval, input, or permission authority |
| **Transcript** | Detailed chronological conversation and structured execution history | Existing Transcript service/API remains authoritative during migration |
| **Terminal** | Raw PTY detail, diagnosis, recovery, and emergency operation | `TerminalTransport` remains exact-generation transport; `Recorder` remains sole PTY reader |
| **Cockpit** | Optional cross-session/workspace aggregation | Read-only optional projection; not the first entry point or a Base Alpha prerequisite |
| **N1** | Notification entry into the exact relevant Activity event | Push payload is a locator, never authority |

Activity and Transcript remain distinct views. They converge gradually on
shared event identity, session/runtime/generation, provenance, ordering/cursor,
request/result and approval relationships, reconnect continuity, and explicit
gap/degraded representation. They are not immediately merged into one feed.
The default per-session view remains a dogfood decision. Terminal remains
permanently accessible as a secondary/fallback surface.

The safest initial hierarchy is one per-session screen with explicit Activity
and Transcript views and a persistent Terminal escape hatch. N1 opens the exact
Activity event when available, then permits drill-down to Transcript or
Terminal. Cockpit remains a separate optional aggregation.

## 3. Capability model

There is no global `orchestration=true` switch. No capability may implicitly
activate another store, authority, lease, route, producer, or workflow.

| Capability | Base Alpha | Manual Alpha | Assisted | Automatic during alpha |
| --- | --- | --- | --- | --- |
| Managed session control | Mandatory | Mandatory | Mandatory | Unavailable |
| Existing Transcript | Mandatory | Mandatory | Mandatory | Unavailable |
| `operational_timeline` | Mandatory support; fail-open | Enabled | Enabled | Unavailable |
| `n1_notifications` | Mandatory product support; OS permission remains user-controlled | Enabled | Enabled | Unavailable |
| Terminal fallback | Mandatory | Mandatory | Mandatory | Unavailable |
| `cockpit` | Optional | Optional | Optional | Unavailable |
| `manual_coordination` | Disabled | Explicit user action | May suggest; user confirms | Unavailable |
| `manual_validation` | Disabled | Explicit user action | May suggest; user confirms | Unavailable |
| `workspace_lease` | Disabled | Enabled only for a workflow that requires it | Required when relevant | Unavailable |

Where relevant, capability negotiation reports one of:

- `supported_and_healthy`;
- `supported_but_degraded`;
- `temporarily_unavailable`;
- `disabled_by_configuration`; or
- `unauthorized`.

A boolean may represent a genuinely binary leaf feature, but it must not hide
degradation, permission, or configuration state for Timeline, N1, Cockpit,
coordination, validation, or workspace behavior.

## 4. Operational Canonical Timeline activation

The intended production behavior is:

> **Default-on after staging evidence, always fail-open and
> non-authoritative.**

This plan does not change the current default. Step 9.1 must prove the staging
conditions before a separate reviewed change may alter it.

The activation contract is:

1. Connect only bounded, accepted provider-native operational producers needed
   by Activity and N1.
2. Keep runtime, lifecycle, approval, input, permission, Transcript, and PTY
   authorities unchanged.
3. Never make Timeline availability a daemon, session, reconnect, approval, or
   shutdown prerequisite.
4. Submit evidence outside authority locks through the accepted bounded
   mailbox/backpressure boundary.
5. Treat validation, append, filesystem, sync, drop, restart, and close failure
   as Timeline degradation, never native-runtime failure.
6. Expose drops, gaps, collisions, unknown versions, degraded producers, and
   unavailable projections explicitly.
7. Never allow replay to deliver an instruction, resolve an approval, send
   input, revive a session, or drive a lifecycle transition.
8. Preserve existing Transcript and Terminal as safe fallback paths.

A healthy Base Alpha candidate must prove Activity and N1 function correctly.
If Timeline later degrades, the managed runtime continues, the UI identifies
the degradation, and the user receives a safe Transcript, Terminal, session
list, or preserved-evidence fallback. The UI must never fabricate an event to
conceal a gap.

## 5. Transcript and Activity migration boundary

The existing Transcript service and API remain authoritative:

1. Preserve the existing Transcript authority and consumer behavior.
2. Produce Timeline shadow events only from accepted provider-native structured
   evidence.
3. Compare Timeline-derived Transcript and Activity projections offline or in
   shadow.
4. Verify generation reset, reconnect, ordering, duplicates, missing events,
   gaps, degradation, and request/result and approval relationships.
5. Switch one consumer or endpoint at a time under a bounded acceptance packet.
6. Preserve immediate rollback and fallback to Transcript/Terminal.
7. Remove an obsolete duplicate read path only after production evidence proves
   zero required consumers and the replacement receives independent ACCEPT.

Equivalence is semantic and authority-based, not byte-for-byte UI equality.
Every intentional lossy or merged projection requires an explicit mapping.
There is no PASS with unexplained missing, extra, reordered, duplicated,
misbound, unknown-version, collision, gap, or degraded input.

`Recorder` remains the sole PTY reader. Raw PTY bytes are neither dual-fed into
nor reinterpreted as Canonical Timeline events.

## 6. N1 exact-event notification-to-action

Each N1 notification locator includes:

- canonical event ID;
- managed session ID;
- runtime identity;
- exact generation; and
- event kind.

The push payload is never authoritative. On tap, mobile re-queries current
server authority, generation, resolution state, and effective permissions.

| Closed outcome | Required response and fallback |
| --- | --- |
| `actionable` | Open the exact Activity event and show only currently authorized contextual actions |
| `already_resolved` | Show result and resolution time; remove actions; permit Activity/Transcript inspection |
| `stale_generation` | Identify the older generation; forbid action; offer historical evidence and current session |
| `session_unavailable` | Explain termination/deletion/unavailability; offer preserved evidence, Cockpit/session list, or safe history |
| `insufficient_permission` | Show the server-issued read-only reason; forbid action; permit authorized inspection |
| `canonical_event_unavailable` | Show bounded notification identity/summary; never guess a replacement; fall back safely |
| `event_degraded_or_gap` | Expose the gap; re-query authority; permit only actions proven current and authorized there |

A stale, resolved, degraded, missing, unauthorized, or mismatched notification
must never replay an action. ACK loss or an unavailable projection is not
permission to infer success.

## 7. Privacy and evidence boundary

Canonical Timeline and its projections must not retain:

- raw PTY byte streams;
- hidden reasoning or chain-of-thought;
- speculative reasoning inferred from terminal text;
- bearer tokens, pairing secrets, private keys, or complete approval secrets;
- unredacted environment values, cwd, tool arguments, or tool results;
- unbounded provider-native payloads; or
- duplicate permanent copies of the full Transcript.

Permitted evidence is limited to redacted and bounded provider-native
operational facts, identities, outcomes, typed references, digests,
provenance, redaction-policy identity, and immutable artifact references.
Prefer a digest/reference when durable content is unnecessary. Existing
Transcript content does not grant Timeline permission to retain another copy.

## 8. Secure accountless onboarding

The user-facing first-session path is approximately:

1. Install and run `pokit setup`.
2. Connect the phone through QR pairing and local computer approval.
3. Run `pokit run codex` or `pokit run claude`.
4. Open the managed session from mobile.

`pokit setup` is a product-flow requirement, not authorization in this plan to
implement a particular installer. Its reviewed implementation must diagnose
provider installation/login, daemon readiness, operational endpoint, and local
permissions without weakening pairing, host identity, device keys, bearer
authentication, owner/member permissions, approval authority, or generation
binding.

First onboarding must not require Cockpit, workspace lease, validator setup,
role catalogs, orchestration mode selection, automatic provider selection, or
manual permission-array editing. Server-issued role and effective permission
remain authoritative and visible.

## 9. Execution waves after Step 9.0 ACCEPT

### Step 9.1 — Operational Timeline staging

Limit scope to minimal producer composition, failure isolation, bounded
mailbox/backpressure, explicit drop/gap/degraded evidence, restart and
filesystem-failure behavior, and capability-state reporting. There is no
consumer-wide cutover.

ACCEPT requires fail-open authority isolation, no startup requirement, bounded
overhead evidence, explicit degradation, full daemon/mobile gates, rollback,
and independent verification.

### Step 9.2 — Canonical projection convergence

Preserve Transcript authority; add shadow/offline comparison, Activity
projection, semantic mapping/equivalence evidence, generation/reconnect tests,
and at most one bounded consumer or endpoint switch per accepted packet.
Fallback and rollback are mandatory. PTY/Recorder authority is out of scope.

### Step 9.3 — N1 exact-event notification-to-action

Add exact event/session/runtime/generation identity, authority re-query on tap,
the closed outcome model, contextual authorized actions, and safe fallback.
This is user-visible notification routing, not Navigator or autonomous policy.

### Step 9.4 — Secure accountless onboarding

Provide the bounded setup-to-first-session flow, provider and daemon readiness
checks, QR/local approval, identity restoration, permission visibility, and
first managed-session discovery. Do not add account registration or weaken
accepted trust boundaries.

### Step 9.5 — Base Alpha candidate

Freeze a reproducible production candidate, build matched daemon/APK artifacts,
run automated safety gates, execute the bounded SM-S926N onboarding-to-N1
matrix, publish exact known issues and release notes, obtain independent
acceptance, and then begin dogfood.

### Later Manual Alpha — optional coordination and validation

Only after Base Alpha dogfood begins, separately activate explicit
user-triggered coordination, frozen clean-snapshot validation, workspace
snapshot/lease when required, optional Cockpit expansion, and explicit session
or provider handoff. Accepted Steps 4–8 foundations are reused; none becomes
authority merely because it is activated. Automatic policy remains unavailable.

## 10. Base Alpha acceptance

### Hard safety and release gates

- reproducible daemon/APK source candidate, SHA-256, VCS revision, and clean
  provenance;
- pairing, host identity, device-key restoration, bearer restoration, and
  effective permission correctness;
- zero wrong-session or wrong-generation intervention;
- approval, deny, input ACK, interrupt, stop, and reconnect bound to the exact
  generation;
- reconnect identity and permission revalidation;
- no stale or resolved N1 action replay;
- Timeline failure does not block runtime or any authority;
- explicit drop, gap, collision, degraded, and unavailable state;
- no raw PTY, reasoning, chain-of-thought, or secret leakage;
- no Transcript, Terminal, lifecycle, input, approval, pairing, or permission
  regression;
- bounded SM-S926N setup-to-N1 product matrix;
- build, vet, race, TypeScript, Jest, formatting, and diff checks; and
- exact artifact provenance, known issues, and release notes.

### Dogfood metrics

Until a baseline exists, these are observations rather than invented release
thresholds:

- setup-to-first-managed-session time and failure step;
- first-attempt pairing and cold-start identity restoration;
- notification delivery latency and exact-event navigation success;
- stale/resolved notification comprehension;
- approval, input, and interrupt response latency;
- reconnect success and manual recovery frequency;
- Activity comprehension and Transcript/Terminal drill-down frequency/reason;
- false blocked/completed state and Timeline gap/drop incidence;
- successful mobile interventions and unattended blocked-time change;
- weekly repeat usage; and
- actual requests for manual handoff or independent validation.

Dogfood evidence, not architectural momentum, determines whether Manual Alpha
or later assisted capability deserves activation.

## 11. CT-P2 boundary and stop conditions

Do not automatically resume the historical full CT-P2 plan. Steps 9.1 and 9.2
authorize only bounded CT work explicitly accepted for minimal production
producer composition, shadow comparison, Activity projection, one controlled
consumer/endpoint cutover at a time, degradation reporting, fallback, and
rollback.

Stop a wave if it:

- weakens accepted provider-native Codex/Claude runtime behavior;
- alters `ManagedRuntimeCatalog`, lifecycle, `OwnedPTYRuntime`,
  `TerminalTransport`, `Recorder`, approval, input, device trust, permission,
  Transcript, pairing, or reconnect authority outside its bounded contract;
- makes Timeline required for startup or native runtime progress;
- hides a gap or invents a canonical event;
- adds raw PTY or prohibited content to Timeline;
- enables coordination, validation, lease, Cockpit, or orchestration
  implicitly;
- removes fallback before production evidence and independent acceptance; or
- begins the next wave without an exact accepted implementation/evidence
  identity and a clean worktree.

## 12. Documentation reconciliation map

| Required correction | Authoritative location |
| --- | --- |
| Base Alpha excludes coordination/validation | Sections 1 and 3 |
| Product surfaces and Terminal fallback | Section 2 |
| Independent capability states | Section 3 |
| Default-on-after-evidence, fail-open Timeline | Section 4 |
| Bounded Transcript/Activity convergence | Section 5 |
| N1 identity, authority re-query, closed outcomes | Section 6 |
| Timeline privacy/redaction boundary | Section 7 |
| Secure accountless first-session gate | Section 8 |
| Steps 9.1–9.5 and later Manual Alpha | Section 9 |
| Hard gates versus dogfood metrics | Section 10 |
| Bounded CT-P2 relationship and stop conditions | Section 11 |

Historical PA/PB evidence, Step 4–8 evidence, adapter/provider expansion plans,
and earlier product rationale remain unchanged because they preserve accepted
identity, implementation evidence, or historical decision context. They do not
override this roadmap.
