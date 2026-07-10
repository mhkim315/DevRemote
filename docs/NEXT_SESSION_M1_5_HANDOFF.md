# Next Session Handoff — M1.5 Session Ownership and Host Contract

Status: execution handoff
Branch: `feature/phase10-multi-adapter`
Accepted baseline: `a99070015`

Read first:

- `docs/SESSION_OWNERSHIP_AND_LOCAL_HOST_CONTRACT.md`
- `docs/M0_M1_A990700_ACCEPTANCE.md`
- `docs/MOBILE_SESSION_LIFECYCLE_AND_TRANSCRIPT_PLAN.md`

## Mission

Implement only the M1.5 ownership/capability boundary needed before M2.

Do not implement Stop, Kill, Delete, mobile lifecycle UI, new terminal adapters,
or Transcript work.

## Core correction to the existing capability model

The existing `control` capability is insufficient to prove lifecycle ownership.
tmux can accept input/control but its process lifecycle is externally owned.

Introduce an explicit capability or equivalent contract:

```text
managedLifecycle
```

Required rule:

```text
control=true does not imply managedLifecycle=true
input=true does not imply managedLifecycle=true
```

## Required mapping

| Adapter | Product ownership | Required lifecycle capability |
| --- | --- | --- |
| controlled_pty | Pokit-managed | managedLifecycle=true |
| tmux | external attachable | managedLifecycle=false |
| cmux | external observer | managedLifecycle=false |
| localpty | internal/experimental | managedLifecycle=false for product MVP |
| unknown | safe external default | managedLifecycle=false |

Do not implement this with mobile/backend adapter-name branching. Use an
optional provider/capability declaration with a safe false default. Only
controlled_pty should explicitly opt in for M1.5.

## Backend scope

- add `managedLifecycle` to the adapter capability contract;
- make controlled_pty explicitly declare it;
- keep tmux control/input/liveTerminal capabilities unchanged but exclude
  managedLifecycle;
- keep cmux and unknown safe defaults unchanged;
- expose the new capability through the existing `/api/sessions`
  `adapterCapabilities` boundary;
- correct comments/documentation that currently equate generic `control` with
  lifecycle ownership.

Do not add Stop/Kill routes yet.

## Mobile scope

No lifecycle buttons are implemented in M1.5. Mobile types/helpers may recognize
the additive capability, but no adapter-name switch or speculative UI is needed.

M2/M3 will use this capability to gate lifecycle actions.

## Local terminal host contract

The detailed host matrix is already documented in
`docs/SESSION_OWNERSHIP_AND_LOCAL_HOST_CONTRACT.md`.

M1.5 may update documentation/tests, but must not create Terminal.app, VS Code,
iTerm2, Ghostty, Warp, or tmux-pane adapters. They are hosts for `pokit run`, not
runtime sources.

## Required product-boundary tests

- controlled_pty includes `managedLifecycle`;
- tmux excludes it while preserving input/control/liveTerminal;
- cmux excludes it and remains best-effort observer;
- localpty excludes it for current product scope;
- unknown/no-provider excludes it by safe default;
- `/api/sessions` exposes it for controlled_pty and not for external adapters;
- existing capability order/schema remains additive/backward compatible;
- no vendor-specific mobile behavior branch is added;
- full build gate passes.

## Explicit non-goals

- Stop/Kill/Delete implementation;
- adapter lifecycle termination changes;
- mobile action buttons;
- attaching to arbitrary existing terminal application tabs;
- terminal-specific plugins/APIs;
- geometry arbitration beyond the documented single-host MVP;
- multi-writer controller lease;
- Transcript changes.

## Acceptance report

Return:

```text
M1.5 status: ACCEPT / SCOPED ACCEPT / NEEDS FOLLOW-UP
Commit: <hash>

Capability matrix:
- controlled_pty: ...
- tmux: ...
- cmux: ...
- localpty: ...
- unknown: ...

API boundary tests:
- ...

Build gate:
- ...
```

Stop after committing/pushing M1.5. M2 begins only after verifier acceptance.
