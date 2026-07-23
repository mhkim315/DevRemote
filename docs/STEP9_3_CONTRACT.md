# STEP 9.3 — N1 Exact-Event Notification-to-Action Contract

**Status:** IMPLEMENTATION CONTRACT — ACCEPTED at `e829299c6`

**Branch:** `feature/canonical-timeline-foundation`
**PREREQUISITE:** Step 9.2 ACCEPTED at `c82fef47f`

## 1. Scope and authority

Step 9.3 adds a minimal, default-off N1 notification layer that delivers
exact-event locators to the mobile OS notification channel. N1 is a
non-authoritative push payload — tapping the notification re-queries
a dedicated server endpoint for authoritative resolution. N1 never
delivers content, commands, secrets, or approval payloads directly.

**What is NEW:**
- `internal/notification/` — dedup store, builder, delivery pipeline, cursor
- `GET /api/notification/<eventId>/status` — authoritative re-authorization endpoint
- `--enable-n1-notifications` — default-off CLI flag
- Mobile N1 notification routing: notification-tap handling, exact-event
  Activity deep links, closed-outcome recovery screens, and cold-start auth retry
- Per-device push-token registration changes needed to bind an N1 locator to the
  authenticated mobile device

**Mobile N1 authority boundary:** Mobile may render the exact, locator-only
event returned by the re-authorization endpoint and choose a safe recovery
screen. It must re-query that endpoint before acting, must not infer an event
from session telemetry/Cockpit, and must not receive event content, commands,
secrets, or approval payloads.

**What stays UNCHANGED:**
- All authority services (runtime, lifecycle, approval, input, terminal)
- Cockpit, Transcript, Timeline writer — read-only
- `Recorder` (sole PTY reader)

## 2. Notification payload

A delivered notification contains ONLY a locator, never content:

```json
{
  "eventId": "<canonical event ID>",
  "sessionId": "<managed session ID>",
  "generation": 7,
  "kind": "approval_requested",
  "timestamp": "<occurredAt>",
  "n1Token": "<opaque stable dedup token = SHA256(eventId+generation)>"
}
```

**Token stability:** `n1Token` is `SHA256(eventId + ":" + generation)`.
Stable across daemon restarts — the same event+generation always produces
the same token. The dedup store keys on `(eventId, generation)`, not on
an ephemeral random token.

**ACK timing:** delivery is fire-and-forget to the OS push channel. The
mobile app ACKs via the OS notification API. The daemon does not wait
for ACK and does not retry push delivery.

## 3. Re-authorization endpoint

`GET /api/notification/<eventId>/status?session=<sessionID>&generation=<gen>`
(device bearer `sessions:read`).

Returns current authoritative resolution:

```json
{
  "eventId": "<eventId>",
  "currentGeneration": 7,
  "notificationGeneration": 7,
  "status": "actionable",
  "resolvedBy": null,
  "permissions": ["sessions:read", "terminal:input"],
  "activityLink": "pokit://activity/<sessionId>?event=<eventId>&generation=7",
  "event": {
    "eventId": "<eventId>",
    "sessionId": "<sessionId>",
    "runtimeId": "<runtimeId>",
    "generation": 7,
    "kind": "approval_requested",
    "occurredAt": "<occurredAt>"
  }
}
```

`event` is present only when the endpoint located that exact canonical event.
It is locator/display metadata only; it contains no Timeline payload or user
content. Mobile renders only an event whose `eventId` exactly matches the
notification locator. A missing target after ring wrap must show an explicit
missing-event warning and a safe fallback, never another event from the same
session.

### 3a. Seven closed outcomes

| Outcome | Condition |
|---------|-----------|
| `actionable` | Event exists, generation matches, user has required permissions |
| `already_resolved` | Event's approval or input was already processed |
| `stale_generation` | `notificationGeneration != currentGeneration` |
| `session_unavailable` | Session ID not found in managed runtime catalog |
| `insufficient_permission` | Authenticated device lacks required permission |
| `canonical_event_unavailable` | Event ID not found in Timeline writer ring buffer |
| `event_degraded_or_gap` | Writer is degraded (dropped events) — exact event cannot be confirmed |

Only `actionable` opens the deep-link Activity view. All other outcomes
display a safe fallback (session list, terminal, or explicit "Event no
longer available" message).

### 3b. Activity-event navigation

The `activityLink` field uses the `pokit://` custom scheme for in-app
navigation. The mobile app opens the exact Activity item corresponding
to the notification's event. If the event is no longer in the Activity
projection (ring buffer wrap), the link falls back to the session's
Terminal view.

## 4. Closed taxonomy

Only these event kinds trigger N1 notifications:

| Event Kind | Notification Label | Activity link target |
|-----------|-------------------|---------------------|
| `provider_invocation_finished` | "Agent completed" / "Agent failed" | Session Activity view |
| `approval_requested` | "Approval requested" | Approval action sheet |
| `approval_resolved` | "Approval resolved" | Session Activity view |
| `tool_call_finished` | "Tool completed" | Session Activity view |
| Unknown / other | fail-closed (no notification) | — |

## 5. Exactly-once delivery

Each notification is delivered at most once per `(eventId, generation)`
pair. The dedup store persists a bounded window.

**Dedup key:** `(eventId, generation)` — stable across restarts because
`n1Token = SHA256(eventId + ":" + generation)` is deterministic.

**Dedup window:** 4096 entries, LRU eviction on overflow. After
eviction, a re-delivered event is treated as a new notification
(idempotency gap — the mobile app's re-authorize query is the safety net).

**Stale block:** if `notificationGeneration != currentGeneration`,
the notification is suppressed before push delivery. No stale-generation
notification reaches the device.

**Restart:** after daemon restart, the dedup store is empty. A
re-ingested event (from Timeline ring buffer) produces the same
`n1Token` but is re-delivered because the dedup store was cleared.
The mobile app's re-authorize query handles the duplicate gracefully.

## 6. Generation gate

The notification builder checks generation identity before delivery:

- `event.LaunchGeneration == managedRuntime.CurrentGeneration(sessionID)`
- If generation is stale → `stale_generation` outcome, no push delivery
- Reconnect after restart → new generation → old events suppressed at source

## 7. Multi-device

**Per-device token store:** push tokens are stored per device (DeviceID),
not globally overwritten. Multiple paired devices each receive their own
notification for the same session event.

**Timeline consumption cursor:** each device tracks the last-delivered
event position in the Timeline ring buffer via a per-device `(deviceID,
lastEventID, lastGeneration)` cursor. On daemon restart, delivery resumes
from the oldest event still in the ring buffer (cursor reset). No
cross-device conflict — each device consumes independently.

**No global overwrite:** registering a new push token for device B does
not remove device A's token. Device A and Device B both receive
notifications for events relevant to sessions they are authorized to view.

## 8. Privacy

The notification payload must never contain:

- Source code or tool output text
- Shell commands or command output
- Approval payloads (allow/deny/reject messages)
- API keys, tokens, secrets, bearer tokens
- Session IDs or runtime IDs beyond the locator
- User prompt text

**Lock screen hiding:** on iOS (PrivacySensitive) and Android
(VisibilityPrivate), the notification content shows only the label
("Agent completed", "Approval requested"), never detail.

## 9. Implementation files

**May create:**
- `internal/notification/dedup.go` — bounded LRU dedup store (4096 entries)
- `internal/notification/builder.go` — locator construction + generation gate
- `internal/notification/delivery.go` — push channel adapter + per-device cursor
- `internal/notification/handler.go` — GET /api/notification/<eventId>/status
- `internal/notification/notification_test.go` — 12 acceptance tests
- `cmd/devremote/app.go` — `--enable-n1-notifications` flag + composition wiring
- `mobile/App.tsx` — notification response handling and auth-ready cold-start retry
- `mobile/src/navigation/` — `pokit://` Activity/Terminal/Settings routes
- `mobile/src/screens/GlobalFeedScreen.tsx` — exact status-event rendering and
  missing-target fallback
- `mobile/src/screens/NotificationSettingsScreen.tsx` — permission recovery UI
- `mobile/src/lib/` and `mobile/__tests__/` — status DTO, push registration,
  route, exact-event, and fallback coverage

**Must NOT change:**
- `internal/term/` — any terminal/runtime/approval code
- `internal/transcript/` — Transcript service
- Existing authority REST/WS handlers outside the additive N1 status and
  push-registration integration

## 10. Acceptance tests

1. **Closed taxonomy:** unknown event kind → suppressed
2. **Exactly-once:** same (eventId, generation) delivered once; second suppressed
3. **Stable token:** n1Token = SHA256(eventId:generation), same across restarts
4. **Stale block:** already-processed approval → suppressed before push
5. **Generation gate:** old-generation event → suppressed
6. **Deep link:** locator carries correct generation + session ID
7. **Privacy:** payload contains zero secrets, commands, or content
8. **Dedup window:** 4096 entries; overflow evicts oldest
9. **Re-authorization endpoint:** all 7 outcomes return correct resolution
10. **Multi-device:** per-device tokens, independent cursors, no cross-device overwrite
11. **Restart:** dedup empty, stable tokens re-deliver, mobile re-query handles
12. **Default-off:** without `--enable-n1-notifications`, zero notification code executes

## 11. Gate

- [ ] All existing tests pass
- [ ] 12 new acceptance tests pass
- [ ] Default-off flag: zero runtime effect when disabled
- [ ] Mobile notification routing renders only the exact event returned by
  re-authorization; ring-wrap/missing targets show an explicit fallback
- [ ] Mobile deep links, Settings permission recovery, and per-device push
  registration preserve the locator-only privacy boundary
- [ ] Separate evidence commit records test results

## 12. Stop conditions

Stop and reject if the change:
- Delivers user content, commands, secrets, or approval payloads in the notification
- Sends a notification without verifying generation identity
- Re-delivers a suppressed/stale event
- Overwrites device B's push token when device A registers
- Changes any existing authority (runtime, approval, input, terminal)
- Makes notification delivery a daemon startup or session prerequisite
- Uses an ephemeral random token that changes across restarts
