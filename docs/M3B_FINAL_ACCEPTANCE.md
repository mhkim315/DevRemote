# M3b Final Acceptance

Status: **ACCEPT**

Accepted implementation commit:

```text
9cdf2f290299f70d0b6d58e139771bfc336adb57
```

Branch: `feature/phase10-multi-adapter`

## Accepted product contract

- `/api/sessions` exposes daemon-authoritative managed `lifecycleState`, separate
  from agent activity/status.
- terminal Catalog rows remain listable after runtime exit until explicit Delete,
  preserving Activity/Transcript access.
- Stop, Force Kill, and Delete History use the canonical authenticated lifecycle
  routes and declared permissions.
- Stop failure refreshes authoritative state, making Force Kill reachable when the
  daemon remains in `stopping`.
- lifecycle action responses are bound to the requested session and action and
  validated against an action-specific state set.
- FeedScreen lifecycle actions are capability- and state-driven. Unknown or
  observe-only sessions fail closed; external sessions retain input only when the
  adapter explicitly declares it.
- the production controller serializes actions and uses a monotonic epoch so late
  responses after session change or unmount cannot update state, navigate, refresh,
  or release a newer request's pending ownership.
- Back/unmount is viewer detach only. It never becomes Stop, Kill, or Delete.

## Independent verification

The final review inspected the production daemon list/catalog merge, mobile
FeedScreen/controller wiring, client DTO parser, and the added tests rather than
relying on the implementation report.

Reproduced evidence:

```text
mobile TypeScript                         PASS
mobile Jest                              21 suites / 289 tests PASS
Go build/vet/test -race                  PASS
clean Expo Android prebuild              PASS
Android module release Kotlin compile    PASS
invariant and secret scans               PASS
full scripts/build-gate.sh                ALL GATES PASSED
```

The first clean checkout gate invocation encountered the repository's partial
tracked `mobile/android` tree without `gradlew`. Running the documented clean Expo
prebuild produced the complete generated project; the full gate then passed. This
is not an M3b product defect, but future clean-checkout automation should either
prebuild explicitly or avoid treating a partial generated directory as complete.

## Deferred M-track

Physical Android/iOS and LTE product smoke remains a release gate. It is not
claimed by this acceptance and does not weaken the automated contract above.

## Next phase

Proceed only to T0, using:

```text
docs/NEXT_SESSION_T0_COMMON_AGENT_EVENT_HANDOFF.md
```

Do not begin T1 Codex, T2 Claude, D1 Adapter Doctor/Repair, T3 Transcript, S1,
A1, O1, Windows, or unrelated mobile/auth work during T0.
