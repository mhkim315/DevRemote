# STEP 9.3 — N1 Exact-Event Notification-to-Action Contract

**Status:** IMPLEMENTATION CONTRACT — PENDING IMPLEMENTATION

**Branch:** `feature/canonical-timeline-foundation`
**PREREQUISITE:** Step 9.2 ACCEPTED at `c82fef47f`

## 1. Scope and authority

Step 9.3 adds a minimal, default-off N1 notification layer that delivers
exact-event locators to the mobile OS notification channel. N1 is a
non-authoritative push payload — tapping the notification re-queries
server authority. N1 never delivers content, commands, secrets, or
approval payloads directly.

**What is NEW:**
- `internal/notification/` — notification builder, dedup store, delivery pipeline
- `--enable-n1-notifications` — default-off CLI flag

**What stays UNCHANGED:**
- Existing push notification infrastructure (OS channel, token store)
- Mobile UI (N1 is a locator payload only — no new screens)
- All authority services (runtime, lifecycle, approval, input, terminal)
- Cockpit, Transcript, Timeline writer — read-only
- `Recorder` (sole PTY reader)

## 2. Notification payload

A delivered notification contains ONLY a locator, never content:

```json
{
  "eventId": "<canonical event ID>",
  "sessionId": "<managed session ID>",
  "runtimeId": "<runtime identity>",
  "generation": 7,
  "kind": "tool_call_finished",
  "timestamp": "<occurredAt>",
  "n1Token": "<opaque dedup token>"
}
```

The locator identifies the EXACT event. The mobile app uses the locator
to re-query the cockpit GET endpoint for authoritative state. No user
content, command text, approval payload, or secret appears in the payload.

**Deep link integrity:** `RuntimeID + LaunchGeneration + SessionID` must
match the event's envelope identity. No fabricated links. No session-only
or session+generation-only shortcuts.

## 3. Closed taxonomy

Only these event kinds trigger N1 notifications:

| Event Kind | Notification Label |
|-----------|-------------------|
| `provider_invocation_finished` | "Agent completed" / "Agent failed" |
| `approval_requested` | "Approval requested" |
| `approval_resolved` | "Approval resolved" |
| `tool_call_finished` | "Tool completed" |
| `stream_observed` | suppressed (no notification) |
| Unknown / other | fail-closed (no notification) |

Any event kind outside this set is silently ignored. Unknown event kinds
are never delivered.

## 4. Exactly-once delivery

Each notification is delivered at most once per event. The dedup store
holds a bounded window of recently delivered `(eventId, n1Token)` pairs.

**Dedup window:** 4096 entries, LRU eviction on overflow. Restart
(daemon crash/reconnect) clears the window — a delivery after restart
is a new notification, not a duplicate.

**Stale block:** if the event's approval or input has already been
processed (state is terminal), the notification is suppressed. A
notification must never re-notify an already-handled action.

**Retry/idempotency:** after daemon restart or network recovery, the
dedup window is empty. A re-delivered event is a new locator, not a
duplicate. The mobile app's tap → re-query path is the idempotency
guarantee.

## 5. Generation gate

Session replacement (new generation launch) blocks notifications from
the old generation. The notification builder checks:

- `event.LaunchGeneration == currentGeneration(sessionID)` from the
  managed runtime catalog
- If generation is stale (old generation), notification is suppressed
- Reconnect after restart → new generation → old events suppressed

**Multi-device:** last-write-wins for the notification's session-scoped
state. If two devices produce notifications for the same session and
generation, the later timestamp wins. Explicit merge is deferred.

## 6. Privacy

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

## 7. Re-authorization

Tapping a notification does NOT trust the payload. The mobile app:

1. Receives the locator from the OS notification
2. Opens the app with the locator as a deep-link parameter
3. Queries `GET /api/cockpit` with the session ID and event ID
4. Server returns authoritative state (the notification payload is
   verified against the authoritative Timeline projection)
5. If the event is not found or has been superseded, the app displays
   a fallback (session list, terminal) — never fabricated state

The notification is a signal, not authority.

## 8. Implementation files

**May create:**
- `internal/notification/dedup.go` — bounded LRU dedup store
- `internal/notification/builder.go` — locator construction + generation gate
- `internal/notification/delivery.go` — push channel adapter
- `internal/notification/notification_test.go` — 10 acceptance tests
- `cmd/devremote/app.go` — `--enable-n1-notifications` flag + composition wiring

**Must NOT change:**
- `mobile/` — any mobile code
- `internal/term/` — any terminal/runtime/approval code
- `internal/transcript/` — Transcript service
- Existing REST/WS handlers

## 9. Acceptance tests

1. **Closed taxonomy:** unknown event kind → suppressed (no notification)
2. **Exactly-once:** same event ID delivered once; second attempt suppressed
3. **Stale block:** already-processed approval → suppressed
4. **Generation gate:** old-generation event → suppressed
5. **Deep link:** locator carries correct RuntimeID+Generation+SessionID
6. **Privacy:** notification payload contains zero secrets, commands, or content
7. **Dedup window:** bounded 4096-entry store; overflow evicts oldest
8. **Restart:** dedup window empty after restart → re-delivery is a new notification
9. **Re-authorization:** tap → cockpit GET → authoritative state, not payload
10. **Default-off:** without `--enable-n1-notifications`, zero notification code executes

## 10. Gate

- [ ] All existing tests pass
- [ ] 10 new acceptance tests pass
- [ ] Default-off flag: zero runtime effect when disabled
- [ ] No mobile UI or push infrastructure changes
- [ ] Separate evidence commit records test results

## 11. Stop conditions

Stop and reject if the change:
- Delivers user content, commands, secrets, or approval payloads in the notification
- Sends a notification without verifying generation identity
- Re-delivers a suppressed/stale event
- Requires mobile app changes
- Changes any existing authority (runtime, approval, input, terminal)
- Makes notification delivery a daemon startup or session prerequisite
