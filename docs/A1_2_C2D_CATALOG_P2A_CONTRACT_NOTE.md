# A1.2 C2D Catalog P2A — Private Identity Wiring Contract

Status: **PRE-IMPLEMENTATION — P2A ONLY — P2B/P3/C3D PROHIBITED**

Parent: `docs/NEXT_EXECUTOR_A1_2_C2D_CATALOG_P2A_HANDOFF.md`
Accepted P1-R1: `f29e1b6dc776bc8f4799809b59accd7ceb755072`

This note freezes the exact field additions and data flow for P2A before any
implementation. P2A wires the accepted P1 classifier into the real hook path
but stops at private Claude identity. No Store, DTO, delivery, or
actionability change.

## 1. Added field: `catalogActionID`

| Attribute | Value |
|---|---|
| Created at | `handleHook` in `claude_hook_bridge.go`, immediately after the existing canonical `inputDigest` computation |
| Source | `classifyCatalogAction(fields["tool_input"], "claude_headless", rt.authorityVersion, toolName)` — the accepted P1 classifier |
| Stored in | `claudePendingObservation.catalogActionID` (runtime-private, under `turnMu`) |
| Copied to | `claudePrivateIdentity.catalogActionID` in `ReserveIdentity` (coordinator-private, under coordinator mutex) |
| Compared at | Coordinator `ReserveIdentity`: non-empty ID is validated against the compiled catalog entry's tool name + provider + version against the identity's RuntimeRef |
| Invalidated by | Existing lifecycle: stop, kill, delete, exit, epoch replacement, timeout (no new invalidation paths) |
| Byte bound | Inherits from catalog entry ID: ≤ 256 bytes, only printable non-space ASCII (validated by `validCoordinatorToken` in coordinator) |
| Public? | No. The ID never enters a public DTO, log line, or mobile projection. P2B will map it to a Pokit-owned static summary server-side. |

## 2. Modified signatures

### `observePreToolUse`

```
// Before:
func (rt *claudeManagedRuntime) observePreToolUse(toolUseID, toolName, claudeSessionID, inputDigest string)

// After:
func (rt *claudeManagedRuntime) observePreToolUse(toolUseID, toolName, claudeSessionID, inputDigest, catalogActionID string)
```

### `ReserveIdentity`

```
// Before:
func (c *claudeResumeCoordinator) ReserveIdentity(approvalID, sessionID, toolUseID, toolName, inputDigest, pokitSessionID string, rt RuntimeRef) bool

// After:
func (c *claudeResumeCoordinator) ReserveIdentity(approvalID, sessionID, toolUseID, toolName, inputDigest, catalogActionID, pokitSessionID string, rt RuntimeRef) bool
```

Validation added: if `catalogActionID` is non-empty, `validCoordinatorToken(id, 256)` must pass and `lookupCatalogEntry(catalogActionID)` must find a matching entry whose `ToolName` equals `toolName`, `Provider` equals `rt.Adapter`, and `Version` equals `rt.Version`.

## 3. Data flow

```text
handleHook (claude_hook_bridge.go)
  │
  ├─► existing strict top-level decode
  ├─► existing canonicalJSON(tool_input) → inputDigest
  ├─► NEW: classifyCatalogAction(tool_input, "claude_headless", version, toolName)
  │     → (catalogActionID_or_empty, classifierDigest, matched)
  │     → accept only if matched && classifierDigest == inputDigest
  │
  └─► observePreToolUse(..., inputDigest, catalogActionID)

observePreToolUse (managed_claude.go)
  │
  └─► claudePendingObservation{toolUseID, toolName, sessionID, inputDigest, catalogActionID, observedAt}

joinDeferred (managed_claude.go)
  │
  ├─► existing four-field identity match
  ├─► reads catalogActionID from pending observation
  └─► ReserveIdentity(..., catalogActionID, ...)
        │
        ├─► empty catalogActionID → valid, existing non-catalog path
        └─► non-empty → validated against catalog entry + identity fields
```

## 4. Call-site mechanical updates

All existing callers of `observePreToolUse` and `ReserveIdentity` must pass
`""` (empty string) for `catalogActionID` unless the test explicitly exercises a
catalog match. No second reserve API or post-reservation attach operation is
created.

## 5. Files touched

| File | Change |
|---|---|
| `claude_hook_bridge.go` | Call classifier in `handleHook`; pass `catalogActionID` to `observePreToolUse` |
| `managed_claude.go` | Add field to `claudePendingObservation`; update `observePreToolUse` sig; pass to `ReserveIdentity` in `joinDeferred` |
| `claude_resume_coordinator.go` | Add field to `claudePrivateIdentity`; update `ReserveIdentity` sig + validation; update `LookupIdentity` copy |
| `claude_catalog_classifier.go` | Add `lookupCatalogEntry` for coordinator validation |
| `*_test.go` files | Mechanical empty-string updates + catalog-match tests |

## 6. Explicit non-goals

- No Store `ApprovalIngest` / `approvalRecord` change.
- No `SafeApprovalDTO` / public DTO change.
- No delivery, denial decoder, witness routing, or lifecycle semantic change.
- No mobile, `cmd/devremote`, or composition-root wiring.
- No actionability, options, or production capacity activation.
- No exported test APIs, callbacks, channels, sleeps, or live Claude turns.
