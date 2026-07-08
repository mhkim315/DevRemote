# POKIT Preview v1

Release: preview-v1
Branch: preview/v1
Date: 2026-07-08

## Status

Platform (A5–A10): COMPLETE
Product (P1a, P2): COMPLETE
Execution (E6, E7): COMPLETE

## Frozen contracts

### Agent Event types (13)
agent_started, user_message, assistant_message, thinking,
tool_call_started, tool_call_finished, approval_requested,
approval_resolved, waiting_input, completed, failed, interrupted,
unknown

### Agent Status (10)
unknown, idle, thinking, working, waiting_approval, waiting_input,
completed, failed, interrupted, degraded

### Interaction contract
- InteractionOption: id, label, kind, payload, input (InputSchema)
- InputSchema: required, placeholder, multiline, placement
- Placement: as_payload | after_payload | metadata_only
- Semantics from Kind, never from ID

### Capabilities
observe, control, approve, input

### API endpoints
- GET/POST/DELETE /api/sessions
- POST /api/sessions/{id}/approvals/{approvalId}
- GET /debug/diag
- GET/POST /debug/cmd
- GET /term/ws, GET /term/

### Mobile
- Dashboard sections: Needs Attention, Running, Recently Completed, Degraded/View Only
- Feed categories: Message, Thinking, Tool, Interaction, Error, Completed, Unknown
- No vendor-specific branching

## What's included

- Multi-agent observability (Claude, Codex, Antigravity)
- Common event contract with graceful unknown fallback
- Capability-aware interaction UX
- Redacted diagnostics endpoint
- Local-first daemon
- Mobile dashboard + feed
- No-login local test mode (E6)
- Install/packaging scripts (E7)
- Build gate (8 checks)

## Known gaps

- Push notification not tested on physical device
- Real-device UX not validated
- First-time onboarding requires manual setup
- No automated UI tests
- 10 moderate npm audit vulnerabilities
- daemon must be started separately

## Preview success criteria

- [ ] install succeeds
- [ ] connect succeeds
- [ ] dashboard works
- [ ] feed works
- [ ] interactions work
- [ ] diagnostics work
- [ ] package/install flow works

## Post-Preview

Any feature request should be recorded as Preview feedback.
No new abstractions or speculative features before the next
development cycle driven by real Preview usage.
