# Adapter Phase 7 Scope Acceptance

Date: 2026-07-07

Executor commit: `0d0963c36`

Verifier decision: **ACCEPT**

Next implementation permission: **ALLOWED**

## Scope reviewed

- `docs/ADAPTER_PHASE_7_SCOPE.md`
- Phase 7 section of `docs/ADAPTER_EXPANSION_PLAN.md`
- Mobile/backend grep for existing adapter, capability, stale, and diagnostic
  fields

This review accepted Phase 7 scope only. No runtime or mobile implementation
was changed.

## Automated verification

```sh
git diff --check
```

Result: PASS

Read-only inspection:

```sh
rg -n "tmux|cmux|localpty|capabilities|adapter" mobile/src
rg -n "LastError|Stale|ErrAdapterUnavailable|SessionTelemetry|debug/dump|capabilities" companion-daemon/internal companion-daemon/cmd mobile/src
```

## Acceptance findings

### 1. Phase 7 is correctly scoped as operational/UX/diagnostic work

Accepted.

The scope explicitly says Phase 7 is not a new-backend phase. It focuses on
whether the adapter architecture proven in Phase 1-6 is understandable and
safe for users/operators.

The target backends are correctly fixed as:

- `tmux`
- `cmux`
- `localpty`

### 2. Non-goals prevent Phase 7 from drifting into new architecture work

Accepted.

The document explicitly forbids:

- adding a new backend;
- expanding LocalPTY features as the main goal;
- adding common runtime features such as `/term/size` without separate
  approval;
- reintroducing adapter-name-driven UX.

These constraints are important because Phase 7 should productize and verify
the current architecture, not restart adapter expansion.

### 3. Capability-driven mobile UX is a required gate

Accepted.

The scope requires proof that mobile behavior is capability-driven rather than
backend-name-driven. It also calls for unknown-adapter, legacy no-capabilities,
and LocalPTY live-only fixtures.

Read-only grep shows this is a real verification target. For example,
`mobile/src/components/AgentCard.tsx` still contains:

```text
session.adapter && session.adapter !== 'native'
```

That may be a harmless legacy display exception, but Phase 7 must explicitly
classify it. The acceptance condition is not "zero string mentions of adapter
names"; it is "no supported-backend allowlist or behavior branch based on
tmux/cmux/localpty".

### 4. Status taxonomy is aligned with the Phase 7 plan

Accepted.

The scope covers:

- `empty`
- `unavailable`
- `degraded`
- `ended`
- `unsupported`

This matches the Phase 7 requirement that users can distinguish "no sessions"
from "adapter connection failed".

Implementation must prove these states through API/mobile/documentation
evidence rather than only defining terms in prose.

### 5. Operational requirements are in the right shape

Accepted.

The adapter operations table covers the right concerns:

- install/runtime prerequisites;
- common failure causes;
- LaunchAgent/environment differences;
- diagnostic method.

The final Phase 7 implementation should turn this into durable user-facing or
maintainer-facing documentation.

### 6. Diagnostics endpoint/CLI conclusion is accepted as a hypothesis, not final proof

Accepted with condition.

`docs/ADAPTER_PHASE_7_SCOPE.md` currently concludes that `/api/sessions` and
`/debug/dump` are sufficient, so a new diagnostics endpoint/CLI is scoped out.
That is a reasonable starting hypothesis.

However, Phase 7 acceptance still requires evidence that mobile/API can
actually distinguish:

- adapter healthy but empty;
- adapter unavailable;
- stale/degraded data;
- unsupported action;
- ended/missing session.

If the smoke tests show those distinctions cannot be represented clearly, the
diagnostics conclusion must be revised before final Phase 7 acceptance.

## Required gates for the next Phase 7 implementation commit

The next commit should not be accepted unless it proves or documents:

1. no supported-backend allowlist or behavior branch exists in mobile;
2. unknown adapter responses render safely;
3. legacy responses without `capabilities` render safely;
4. LocalPTY live-only sessions render without history/screen affordance
   confusion;
5. empty/unavailable/degraded/ended/unsupported meanings are tied to concrete
   API/mobile behavior;
6. adapter operational requirements are documented for tmux/cmux/localpty;
7. diagnostics endpoint/CLI need is verified with evidence, not only asserted;
8. no new backend is added;
9. no unapproved common runtime feature is added.

## Verdict

Phase 7 scope is **accepted** at `0d0963c36`.

Implementation may begin, but Phase 7 acceptance will depend on concrete
mobile/API/operational evidence, not just the scope document's intended
taxonomy.
