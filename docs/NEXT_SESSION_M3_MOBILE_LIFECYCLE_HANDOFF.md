# Next Session Handoff — M3 Mobile Session Lifecycle UX

Status: implementation begins only after M2.5-4 common authenticated REST/WS
boundary is accepted

Read first:

- `docs/M3_MOBILE_SESSION_LIFECYCLE_UX_PLAN.md`
- `docs/MOBILE_SESSION_LIFECYCLE_AND_TRANSCRIPT_PLAN.md`
- `docs/M2_5_3_DEVICE_CHALLENGE_AUTH_PLAN.md`
- `mobile/src/lib/client.ts`
- `mobile/src/screens/dashboard/DashboardScreen.tsx`
- `mobile/src/components/AgentProfileModal.tsx`
- `mobile/src/screens/FeedScreen.tsx`

## Mission

Replace the old profile-save behavior with safe mobile lifecycle UX for
daemon-owned controlled PTY sessions. Implement M3a first; do not start M3b
until its independent verification passes.

## M3a scope

- use daemon profile discovery and typed create;
- select Shell, Codex, or Claude; optional display name/CWD;
- submit once, await daemon-returned canonical ID/state, refresh, then open;
- preserve form state and show safe errors on failure;
- remove the New-session use of `createOrUpdateSession` and legacy profile
  semantics.

## Never do

- do not send executable paths, shell strings, or client-created IDs;
- do not make an optimistic running card before server success;
- do not use query `DELETE` for lifecycle;
- do not branch on `tmux`, `cmux`, or vendor agent names for lifecycle actions;
- do not add auth headers/signatures/tokens outside the common M2.5-4 client;
- do not change Recorder, PTY, Transcript, or pairing behavior.

## M3b guardrails

Use only `managedLifecycle`, declared capabilities, and lifecycle state:

```text
Back         = detach viewer only
Stop         = managed running process group
Force Kill   = explicit managed stopping fallback
Delete       = terminal managed history only
external     = no managed lifecycle controls
unknown      = View Only safe default
```

## Required report

```text
M3a/M3b status: ACCEPT / SCOPED ACCEPT / NEEDS FOLLOW-UP
Commit: <hash>

API use:
- profiles/create/stop/kill/delete endpoints
- authentication client boundary

State/capability proof:
- managed running/stopping/terminal
- external, observe-only, unknown
- no optimistic phantom session

Execution gates:
- TypeScript
- build
- backend regressions
- component/client boundary tests

Manual gates remaining:
- physical device/LTE stop/detach/confirmation evidence
```
