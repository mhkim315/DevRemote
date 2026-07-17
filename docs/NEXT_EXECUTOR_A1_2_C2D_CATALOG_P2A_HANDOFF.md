# Next Executor Handoff — A1.2 C2D Catalog P2A Private Identity Wiring

Status: **P1 ACCEPTED — START P2A ONLY — P2B/P3/C3D PROHIBITED**

P2A wires the accepted pure classifier into the real Claude hook path and carries
only its bounded catalog ID through Claude-private identity. It does not touch the
A1 Store, public DTO, delivery, mobile, or production actionability.

## 1. Canonical start

- Checkout: `/Users/mhk/Documents/codex/DevRemote`
- Branch: `feature/phase10-multi-adapter`
- Planner contract: `42fe23b340d3fe786fc57b9c6b9ac5a937705294`
- Accepted P1-R1: `f29e1b6dc776bc8f4799809b59accd7ceb755072`

Fetch and fast-forward. Require local/remote equality, both ancestors and a clean
worktree. Read the planner amendment, P1 contract/code, this handoff, and the
accepted C2D-C coordinator/bridge code before editing.

## 2. Frozen data flow

Implement exactly this flow:

```text
POST /hook with bounded tool_input
  → existing strict top-level hook decode
  → existing canonical full-input digest
  → accepted P1 nested catalog classifier
  → observePreToolUse(..., inputDigest, catalogActionID-or-empty)
  → pending observation
  → exact tool_deferred digest/identity join
  → ReserveIdentity(..., catalogActionID-or-empty, RuntimeRef)
  → private identity copy/lookup
```

Catalog failure is not hook failure. A valid non-catalog Claude request follows the
existing non-actionable observation path with an empty catalog ID. A catalog ID is
accepted only when classifier digest equals the existing canonical digest and the
provider/version/tool tuple matches.

The coordinator validates a non-empty catalog ID against the compiled catalog and
the identity RuntimeRef/tool name. Empty remains valid for existing non-catalog
observations. Store admission in P2A remains exactly as before and receives no safe
review metadata.

## 3. Authorized files and changes

Production changes are limited to:

- `claude_hook_bridge.go`: invoke the accepted classifier without replacing the
  existing canonical digest path;
- `managed_claude.go`: add bounded `catalogActionID` to pending observation and
  pass it only after exact deferred join;
- `claude_resume_coordinator.go`: add the optional ID to private identity,
  validation, defensive copy and cleanup lifecycle;
- `claude_catalog_classifier.go`: an internal exact entry lookup may be added for
  coordinator validation, but no public-summary lookup yet;
- directly corresponding `_test.go` fixtures/call sites.

Do not modify `approval_store_gen.go`, `approval_dto.go`, `approval_execution.go`,
Claude delivery/witness/denial semantics, `cmd/devremote`, mobile, or composition
root. Do not add exported test APIs, callbacks, channels, sleeps or live turns.

If changing `ReserveIdentity` requires call-site updates, update them mechanically
to pass empty catalog ID unless the test explicitly exercises a catalog match. Do
not create a second reserve API or a post-reservation attach operation; identity
creation remains one atomic coordinator transition.

## 4. Required production-path tests

Use the real hook HTTP handler and existing deferred join path, not direct classifier
calls alone.

1. exact catalog hook → pending carries the ID → matching deferred result → private
   identity lookup carries the same ID;
2. exact catalog hook with advisory description follows the same path;
3. non-catalog command, unknown/duplicate nested field, wrong provider version or
   tool mismatch yields empty catalog ID and never becomes actionable;
4. classifier digest and existing bridge digest must be equal for a catalog match;
5. mutated deferred input cannot join or preserve catalog metadata;
6. duplicate hook cannot replace an existing pending catalog ID;
7. capacity rejection, Store admission failure, stop, kill, delete, exit and epoch
   replacement leave no catalog-bearing private identity;
8. private identity lookup is a defensive copy;
9. raw command and description sentinels appear in no retained runtime/coordinator
   field, DTO, log or failure output;
10. production actionability remains false, options remain empty and generic summary
    remains unchanged because P2B is not installed.

Include a known-bad test/control demonstrating that classifying again after the raw
hook body is discarded cannot reconstruct the catalog identity from digest alone;
therefore classification must occur at the provider boundary exactly once.

Concurrency tests use existing deterministic barriers. Inspect the state between
classification, pending insertion, identity reservation and cleanup so later
success cannot hide stale metadata. Run focused race repeatedly.

## 5. Gates and stop

Before implementation, write a short contract note with the added field's creation,
storage, comparison, invalidation and test locations. Then run:

- format and `git diff --check`;
- focused bridge/runtime/coordinator tests and race repetitions;
- frozen C2D-C allow/deny/lifecycle regression subset;
- build/vet for affected packages;
- changed-file secret scan.

Do not run paid/live Claude turns or the full mobile/Android gate. Freeze the
implementation SHA, commit, push and stop with:

```text
REVIEW REQUEST: A1.2 C2D Catalog P2A Private Identity Wiring — <implementation SHA>
```

P2B remains unauthorized until independent P2A ACCEPT.
