# A1.2 C3D-C R4 — Identity Model Amendment (R1)

Status: **PRE-IMPLEMENTATION — AWAITING REVIEW**

Parent: `docs/A1_2_C3D_C_LIVE_PACKET_CONTRACT_NOTE.md`
Rejected midpoint: `314541267d645b6f1f6e275fb7151e40e5cec1b8`
Rejected R0: `9cabfe39c5d73b6e4e729910fca82cd40c2838aa` (5 changes required)
Authority: `docs/A1_2_C3D_ACTIVATION_CONTRACT_NOTE.md` §8 (accepted R1 `98ca6d9`)

## 1. Live-confirmed contradiction (unchanged from R0)

The accepted C3D contract assumed the **same tool_use_id** across the defer
and the resume. Live evidence proved real Claude `--resume` generates a
**new** `tool_use_id` and **new** `session_id` for the retried tool call.
The three attempted production fixes (`0793bde`, `1f0ad68`, `85e8830`)
**substituted stored original identity values** — a contract violation.

## 2. Amended guarantee (unchanged from R0)

> The original invocation identity is **not** preserved across the resume.
> Instead, the **claim-owned managed resume process** atomically binds,
> exactly once, the real new invocation identity whose `tool_name` and
> canonical `input_digest` exactly match the approved original action.
> **Only** the provider-native result bearing that new bound identity
> (PostToolUse for allow, `permission_denials` entry for deny) is admitted
> as the consumption witness. Any tool invocation that does not match the
> original action is fail-closed deferred. Any witness that does not bear
> the bound identity is fail-closed rejected.

## 3. Split identity model

### 3A. OriginalApprovalIdentity (already exists, unchanged)

Stored in `claudePrivateIdentity` at `joinDeferred` time:

| Field | Source |
|---|---|
| `sessionID` (Claude) | Initial PreToolUse `session_id` |
| `toolUseID` | Initial PreToolUse `tool_use_id` |
| `toolName` | Initial PreToolUse `tool_name` |
| `inputDigest` | Initial PreToolUse `tool_input` canonical digest |
| `pokitSessionID` | `CreateDetached` return value |
| `runtime` | Initial `RuntimeRef` (original epoch) |

### 3B. ResumeAttemptIdentity (new)

Created at `ClaimWrite` time, atomically under the coordinator lock,
**exactly once per claim**:

| Field | Source |
|---|---|
| `sessionID` (Claude) | **Real** resume PreToolUse `session_id` |
| `toolUseID` | **Real** resume PreToolUse `tool_use_id` |
| `toolName` | **Real** resume PreToolUse `tool_name` |
| `inputDigest` | **Real** resume PreToolUse `tool_input` canonical digest |
| `resumeLaunchGen` | Resume process epoch (from immutable `resumeContext`) |
| `registeredAt` | `clockNow()` |

### 3C. Validation is dual — bridge AND coordinator both verify

**Bridge** (`handleResume`): validates `toolName == ctx.toolName &&
inputDigest == ctx.inputDigest` BEFORE calling `ClaimWrite`. This is the
first line of defense — a tampered hook body never reaches the coordinator.

**Coordinator** (`ClaimWrite`): INDEPENDENTLY compares the passed
`toolName` and `inputDigest` against the stored `OriginalApprovalIdentity`
(from `ReserveEntry`'s identity lookup). The bridge's validation is
observability/convenience; the coordinator's validation is **authority**.
An internal caller that bypasses the bridge cannot forge an identity.

**Coordinator** (`ClaimWrite`): also validates `resumeLaunchGen ==
ctx.resumeLaunchGen` (from the immutable resume context, set by
`ResumeForApproval` before spawn). This binds the attempt to exactly the
claim-owned resume process — a different process incarnation cannot
impersonate the claim.

### 3D. Registration is atomic one-time

The first `ClaimWrite` that passes BOTH bridge and coordinator validation
creates the `ResumeAttemptIdentity` on the entry. Rules:

- First `ClaimWrite` with valid identity → creates `ResumeAttemptIdentity`,
  transitions `stateDecisionReserved → stateWriteClaimed`.
- `ClaimWrite` at `stateWriteClaimed` with the **same** identity fields
  (sessionID, toolUseID, toolName, inputDigest) → **duplicate** detection
  (`outcomeDuplicate`). The provider response is NOT re-written. This is
  idempotent hook replay, not re-delivery.
- `ClaimWrite` at `stateWriteClaimed` with a **different** identity →
  `outcomeMismatch`.
- Re-delivery requires formal provider idempotency evidence — not in scope.

## 4. Deny path also binds to ResumeAttemptIdentity

### Current (broken)

`routeDenial` in `managed_claude.go:704` filters denial entries by
`d.Entries[i].ToolUseID != ctx.toolUseID` where `ctx.toolUseID` is the
**original** identity's tool_use_id. A real resume denial carries a NEW
tool_use_id, so the filter drops it. Deny witness can never reach
`MarkWitnessed`.

### Fixed

After `ClaimWrite` binds the `ResumeAttemptIdentity`, `routeDenial` is
updated to filter denial entries by **both** the original tool_use_id
(safety gate: "is this denial about our tool category?") **and** the bound
`ResumeAttemptIdentity` fields (exact match: "is this the specific resume
invocation we bound?"). The bound identity's `sessionID`, `toolUseID`,
`toolName`, and `inputDigest` must all match the denial entry. If no bound
identity exists yet (pre-ClaimWrite), denial entries are held or rejected
(same fail-closed semantics as early PostToolUse — see §5).

`MarkWitnessed` for deny (`WitnessPermissionDenials`) validates against the
bound `ResumeAttemptIdentity`, identical to the allow path.

## 5. Early-witness race: state machine extension

Live evidence also revealed a race: `MarkWitnessed` (from PostToolUse or
denial) may arrive before `ConfirmWrite(true)`. The HTTP write is buffered
but the coordinator mutex is released between `ClaimWrite` and
`ConfirmWrite`.

### New state: `stateWitnessPending`

```
reserved ──ClaimWrite──▶ write_claimed ──ConfirmWrite(true)──▶ decision_written
                                │                                    │
                                │ MarkWitnessed (early)              │ MarkWitnessed
                                ▼                                    ▼
                         witness_pending ──ConfirmWrite(true)──▶ witnessed (terminal)
                                │
                                │ ConfirmWrite(false)
                                ▼
                          ambiguous (terminal, wipes early witness)
```

Rules:
- `MarkWitnessed` at `stateWriteClaimed` → stores the witness args
  (kind, sessionID, toolUseID, toolName, inputDigest, runtime) in the
  entry and transitions to `stateWitnessPending`. Does NOT send terminal.
- `ConfirmWrite(true)` at `stateWitnessPending` → validates the stored
  early witness against the **ResumeAttemptIdentity** and commits
  `TerminalWitnessed` (terminal).
- `ConfirmWrite(false)` at any non-terminal state → terminal with
  `outcomeAmbiguous`. **Wipes** the stored early witness.
- `MarkWitnessed` at `stateWitnessPending` with a DIFFERENT identity →
  `(false)`. Entry stays `stateWitnessPending`.
- `MarkWitnessed` at `stateWitnessPending` with the SAME identity →
  duplicate detection `(false)` — does not re-commit.
- `MarkWitnessed` at `stateDecisionWritten` → validates against
  `ResumeAttemptIdentity` and commits `TerminalWitnessed` (normal path).
- Provider response write (in `handleResume`) is exactly once per
  successful `ClaimWrite`. Duplicate `ClaimWrite` at `stateWriteClaimed`
  does not write.

### Cleanup of witness_pending (ALL cleanup functions updated)

Every cleanup path MUST handle `stateWitnessPending` as a terminal
non-success:

| Function | `stateWitnessPending` action |
|---|---|
| `CancelEntry` | → `stateTerminal`, send `TerminalCancelled` |
| `ClearRuntime` | → `stateTerminal`, send `TerminalStaleRuntime` |
| `ClearForApproval` | → `stateTerminal`, send `TerminalCancelled` |
| `Close` | → `stateTerminal`, send `TerminalCancelled` |
| Timeout (`clearStaleEntries`) | → `stateTerminal`, send `TerminalAmbiguous` |
| `ConfirmWrite(false)` | → `stateTerminal`, send `TerminalAmbiguous` |

The early witness is wiped — it can never commit after cleanup.

## 6. Immutable resume context changes

`resumeContext` gains one new field:

| Field | Source |
|---|---|
| `resumeLaunchGen` | Resume process epoch from `ResumeForApproval` |

Set by `Deliver` → `ResumeForApproval` before spawn. The bridge passes it
to `ClaimWrite`, which validates it against the stored identity.

## 7. Bridge changes (summary, implementation in R4-R3)

### `handleResume`
1. Strict decode real body
2. Recompute `inputDigestBody`
3. Validate claim/nonce/`resumeLaunchGen` against context
4. **Compare** `toolNameBody == ctx.toolName && inputDigestBody == ctx.inputDigest` (bridge-side validation)
5. If mismatch → fail-closed defer
6. If match → `ClaimWrite(claimToken, resumeNonce, sessionIDBody, toolUseIDBody, toolNameBody, inputDigestBody, ctx.resumeLaunchGen)`
7. `ClaimWrite` INDEPENDENTLY re-validates against stored `OriginalApprovalIdentity` AND binds `ResumeAttemptIdentity` (coordinator-side validation)

### `handlePostTool`
1. Strict decode real body
2. Recompute `inputDigestBody`
3. `MarkWitnessed` with **real body values**
4. `MarkWitnessed` validates against the bound `ResumeAttemptIdentity`

### `routeDenial` (in `managed_claude.go` pump)
1. Filter denial entries by the bound `ResumeAttemptIdentity`'s
   `sessionID`, `toolUseID`, `toolName`, `inputDigest` (exact match).
   If no bound identity exists yet, fail closed.

## 8. Required adversarial tests (expanded from R0)

| # | Test | Assertion |
|---|---|---|
| 1 | Mutated input on resume | 0 responses (fail-closed defer, BOTH bridge and coordinator reject) |
| 2 | Different tool name on resume | 0 responses |
| 3 | Two resume hooks competing | Exactly one identity registered; second is duplicate or mismatch |
| 4 | PostToolUse with different toolUseID (not bound) | Witness rejected (no commit) |
| 5 | Early PostToolUse (before ConfirmWrite) | Stored, one success after ConfirmWrite(true) |
| 6 | Write failure then PostToolUse | No success (ambiguous terminal wipes early witness) |
| 7 | Timeout/stop/replacement | ResumeAttemptIdentity removed, early witness wiped |
| 8 | Deny evidence matches new bound identity | Witness accepted (deny commit) |
| 9 | Deny evidence with different identity | Witness rejected |
| 10 | Wrong resume session ID | `ClaimWrite` rejects (`outcomeMismatch`) |
| 11 | Wrong resume process generation | `ClaimWrite` rejects |
| 12 | Missing tool_input | Bridge rejects (fail-closed defer) |
| 13 | Same resume hook re-invocation (duplicate) | Detected as duplicate; provider write exactly once |
| 14 | witness_pending + stop | Terminal cancelled, early witness wiped |
| 15 | witness_pending + delete | Terminal cancelled, early witness wiped |
| 16 | witness_pending + replacement | Terminal stale, early witness wiped |
| 17 | witness_pending + timeout | Terminal ambiguous, early witness wiped |
| 18 | witness_pending + close | Terminal cancelled, early witness wiped |
| 19 | Ambiguous denial entries (duplicate bound tool_use_id) | Fail-closed cancel |
| 20 | Cross-use: denial with original identity (not bound) | Witness rejected |

Tests 1-2 and 7 have existing catalog-test counterparts that were broken by
the rejected substitution fix (R3). Tests 3-6, 8-20 are new.

## 9. Non-goals (corrected from R0)

- No change to `ReserveIdentity` or `ReserveEntry`.
- No change to `ConfirmWrite` semantics beyond the state machine extension
  (already described in §5).
- No change to the delivery layer (`claude_approval_delivery.go`).
- No new catalog entries or prompt changes beyond the already-accepted
  `claudeCertificationPrompt` alignment.
- **REMOVED** (R0 §7): "No change to CancelEntry, ClearRuntime, Close."
  These MUST be updated to handle `stateWitnessPending` (§5).
- No live proof until R4-R2 through R4-R4 are complete and deterministic
  tests pass.

## 10. Implementation order

1. **R4-R2**: Coordinator changes — `ResumeAttemptIdentity` struct,
   `ClaimWrite` dual validation + one-time bind, `MarkWitnessed` against
   bound identity, early-witness states, all cleanup paths, `routeDenial`
   against bound identity. `resumeContext.resumeLaunchGen` field.
2. **R4-R3**: Bridge changes — `handleResume` compare-then-bind,
   `handlePostTool` real body values.
3. **R4-R4**: Adversarial deterministic tests (all 20).
4. **R4-R5**: Full `-race` gate, focused adversarial tests ×5, then (and
   only then) live allow/deny proof.

## 11. Stop condition

Stop for independent review of this amendment. R4-R2 implementation is
**not** authorized until this amendment is ACCEPTED.
