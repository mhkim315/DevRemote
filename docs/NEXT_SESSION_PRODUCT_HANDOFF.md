# Next Session Product Handoff

> Architecture answers "Can we build it?"
> Product answers "Why would people love using it?"

## Current state

Architecture Phase (A0~A10): COMPLETE.
Product Phase P1a: ACCEPTED (3fa4ad3c8).

Current baseline:

- P1a Live Dashboard Core has four sections: Needs Attention, Running, Recently Completed, Degraded / View Only.
- A9 interaction card is compatible and rendered within the dashboard.
- Observe-only sessions show VIEW ONLY badge.
- No vendor-specific mobile branches exist.

## Next target

P2 — Core Feed Taxonomy.

Do not implement P1b, P3, P4, P5, or P6.

## Why P2 now

P1a sorted the dashboard. P2 makes individual sessions readable.

When a user taps a session, they should see a story, not raw logs.

P1b (push routing) comes after P2 because routing to an unreadable feed is confusing.

## Mandatory reading

Before implementation, read:

- `docs/PRODUCT_PHASE_PLAN.md` (P2 section)
- `docs/AGENT_PHASE_A7_ACCEPTANCE.md`
- `docs/AGENT_PHASE_A10_ALPHA_CHECKLIST.md`

## P2 scope

### Goal

Transform the existing event list into a readable activity story using event type → bubble category mapping.

### Event → Category mapping

Implement this taxonomy in EventBubble rendering:

| Existing event type | Category | Visual |
|---------------------|----------|--------|
| user_message | Message | 💬 user-aligned bubble |
| assistant_message | Message | 💬 agent-aligned bubble |
| thinking | Thinking | 💭 dimmed, italic |
| tool_call_started | Tool | ⚙ bubble, show tool name |
| tool_call_finished | Tool | ⚙ dimmed result (collapsed by default) |
| approval_requested | Interaction | ⚠ attention border, red |
| approval_resolved | Interaction | ⚠ dimmed, resolved state |
| failed | Error | ❌ red bubble |
| error | Error | ❌ red bubble |
| completed | Completed | ✅ green bubble |
| agent_started | Message | 💬 system message |
| unknown | Unknown | ❓ dimmed fallback |
| *future types* | Unknown | ❓ dimmed fallback |

### Acceptance

- Each known event type renders in its correct category.
- Unknown/future event types fall back to a dimmed generic bubble (never crash).
- Interaction events (approval_requested) are visually prominent with attention-colored border.
- Tool events display the tool name.
- Error events use red styling.
- Thinking events are visually distinct from regular messages (dimmed).
- No vendor-specific branch in event rendering (`if agentKind === ...` forbidden).
- No backend parser changes. Use existing `/api/sessions.Events` data as-is.
- Existing A9 interaction card in FeedScreen is preserved.
- Mobile typecheck run if `node_modules` available; otherwise report `not-run`.

### Explicitly avoid

Do not implement:

- diff viewer
- file viewer
- commit summary
- deterministic aggregation / summary
- new parser or backend event model
- LLM summarization
- push notification routing
- deep link work
- installer / distribution
- Agent Cockpit
- new dashboard sections

Do not introduce:

```ts
if (agentKind === 'claude') { ... }
if (agentKind === 'codex') { ... }
switch (agentKind) { ... }
```

Bubble category is determined by `event.type`, never by `agentKind`.

## Non-negotiable invariants

- A7: unknown event fallback preserved.
- A9: interaction events do not imply control capability.
- A9: interaction card remains compatible.
- No vendor-specific rendering branch.
- No backend contract change.

## Suggested execution order

1. Read the current `EventBubble.tsx` and `FeedScreen.tsx`.
2. Identify the current event type → render logic.
3. Map existing types into the P2 taxonomy.
4. Add missing categories (Error, Completed, Interaction attention).
5. Preserve existing A9 interaction card behavior.
6. Ensure unknown/future types render as dimmed fallback.
7. Run `go test ./...` in companion-daemon (no backend changes expected).
8. Run mobile typecheck if available.
9. Commit as P2 implementation slice.

## Reviewer checklist

Reviewer will check:

- each event type renders in the correct category;
- unknown/future event types don't crash;
- interaction events are visually prominent;
- no vendor-specific branch exists;
- no backend parser change;
- A9 interaction card preserved;
- mobile typecheck result reported.

## Completion statement format

```text
P2 implementation complete.

Commit: <sha>

Changed:
- ...

Validation:
- go test ./...: ...
- mobile tsc: ...
- vendor branch grep: ...

Known gaps:
- ...

Not done:
- P1b push routing
- P3 build gate
- P4 device smoke
- P5 installer
- P6 alpha packaging
- Post-MVP: cockpit, summary, review
```
