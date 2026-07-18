# A1.2 C3D-C R4 — Identity Model Amendment (R2, verifier-frozen)

Status: **ACCEPTED FOR R4-R2 THROUGH R4-R4 DETERMINISTIC IMPLEMENTATION**

Parent: `docs/A1_2_C3D_C_LIVE_PACKET_CONTRACT_NOTE.md`
Rejected midpoint: `314541267d645b6f1f6e275fb7151e40e5cec1b8`
Rejected R0: `9cabfe39c5d73b6e4e729910fca82cd40c2838aa` (5 changes required)
Reviewed R1: `91ce0dbf034718b8c54b94ab3433f81cb3783d42` (4 final changes applied here)
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

**Coordinator-owned process pre-binding**: `ResumeForApproval` allocates the
resume epoch, then calls a coordinator transition equivalent to
`BindResumeProcess(claimToken, resumeNonce, resumeLaunchGen)` **before** the
process is spawned or its hook bridge is published. The transition validates
the existing reserved entry and stores the expected generation exactly once.
It rejects a missing entry, wrong nonce, duplicate/different generation, stale
runtime, or non-reserved state. Spawn/publication failure cancels the entry.

**Coordinator** (`ClaimWrite`): validates the generation received from the
immutable resume context against the independently stored expected generation
created by `BindResumeProcess`. Comparing two values derived from the same
caller context is prohibited. This binds the attempt to exactly the
claim-owned resume process — another process incarnation cannot impersonate
the claim even if it reaches an internal callable boundary.

### 3D. Registration is atomic one-time

The first `ClaimWrite` that passes BOTH bridge and coordinator validation
creates the `ResumeAttemptIdentity` on the entry. The new Claude `sessionID`
has no provider-issued parent-ID join available before this hook: it is
therefore accepted only as the first observed session ID from the
claim-specific bridge whose token, nonce, and coordinator-owned resume process
generation have all matched. It is not inferred from timing, command text, or
FIFO ordering. Rules:

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

After `ClaimWrite` binds the `ResumeAttemptIdentity`, `routeDenial` filters
denial entries **only** by the bound attempt's `sessionID`, `toolUseID`,
`toolName`, and `inputDigest`. The original and resume tool-use IDs are known
to differ; requiring both is impossible and is prohibited. Tool category is
already enforced by the bound `toolName` and input digest.

The pump must not obtain a mutable/read-only attempt snapshot and later call a
second authority operation. It passes the strictly decoded, bounded denial
event to a coordinator operation equivalent to `MarkDenialWitness`; under one
coordinator lock that operation requires exactly one entry matching the bound
attempt, validates state/kind/runtime/deadline, and performs the same early- or
normal-witness transition as `MarkWitnessed`. Zero or multiple matches are
non-success, with multiple matches cancelling the claim as ambiguous.

If no bound attempt exists yet (pre-`ClaimWrite`), a denial cannot prove
consumption of this decision. It is not held for later association and does
not become a witness. The entry is cancelled/fails closed according to its
current pre-write state.

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
- Before any early witness is stored, `MarkWitnessed` validates the expected
  witness kind for the selected decision, every field of the bound
  `ResumeAttemptIdentity`, the original authoritative `RuntimeRef`, and the
  witness deadline. A mismatched or expired witness returns false and leaves
  `stateWriteClaimed` unchanged; it cannot occupy the pending slot.
- `MarkWitnessed` at `stateWriteClaimed` with a fully valid witness → stores
  the bounded witness args (kind, sessionID, toolUseID, toolName, inputDigest,
  runtime) and transitions to `stateWitnessPending`. Does NOT send terminal.
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

`resumeContext` gains one new field, but it is not the authority owner:

| Field | Source |
|---|---|
| `resumeLaunchGen` | Resume process epoch from `ResumeForApproval` |

`ResumeForApproval` allocates the epoch, stores it first through the
coordinator-owned `BindResumeProcess` transition, then places the same value in
the immutable context before spawn. The bridge passes it to `ClaimWrite`,
which compares it against the coordinator's independently stored expected
generation.

## 7. Bridge changes (summary, implementation in R4-R3)

### `handleResume`
1. Strict decode real body
2. Recompute `inputDigestBody`
3. Validate claim/nonce against context; pass the immutable
   `resumeLaunchGen` to the coordinator for comparison with its pre-bound value
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
1. Strictly decode a bounded denial event and pass it to the coordinator.
2. Under one coordinator lock, select exactly one entry matching the bound
   `ResumeAttemptIdentity`'s `sessionID`, `toolUseID`, `toolName`, and
   `inputDigest`, then perform the witness transition. Never gate on the
   original tool-use ID and never use lookup-then-Mark TOCTOU. If no bound
   identity exists yet, cancel/fail closed without retaining the event.

## 8. Required adversarial tests (expanded from R0)

| # | Test | Assertion |
|---|---|---|
| 1 | Mutated input on resume | 0 responses (fail-closed defer, BOTH bridge and coordinator reject) |
| 2 | Different tool name on resume | 0 responses |
| 3 | Two resume hooks competing | Exactly one identity registered; second is duplicate or mismatch |
| 4 | PostToolUse with different toolUseID (not bound) | Witness rejected (no commit) |
| 5 | Valid early PostToolUse (before ConfirmWrite) | Stored, one success after ConfirmWrite(true) |
| 6 | Write failure then PostToolUse | No success (ambiguous terminal wipes early witness) |
| 7 | Timeout/stop/replacement | ResumeAttemptIdentity removed, early witness wiped |
| 8 | Deny evidence matches new bound identity | Witness accepted (deny commit) |
| 9 | Deny evidence with different identity | Witness rejected |
| 10 | Different resume session ID after first bind | Second `ClaimWrite` rejects (`outcomeMismatch`); the first ID is accepted only through the claim-specific token/nonce/pre-bound process generation |
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
| 21 | Wrong early witness before ConfirmWrite | Rejected without entering `stateWitnessPending`; later correct witness may still succeed |
| 22 | Missing pre-bound resume generation | Spawn/publication and `ClaimWrite` are unavailable; zero provider response |

Tests 1-2 and 7 have existing catalog-test counterparts that were broken by
the rejected substitution fix (R3). Tests 3-6 and 8-22 are new or require
explicit strengthening against this frozen model.

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
   coordinator-owned `BindResumeProcess`, `ClaimWrite` dual validation +
   one-time bind, `MarkWitnessed` against bound identity, early-witness states,
   coordinator-owned bounded denial matching, all cleanup paths, and
   `resumeContext.resumeLaunchGen` field.
2. **R4-R3**: Bridge changes — `handleResume` compare-then-bind,
   `handlePostTool` real body values; `routeDenial` selects only the bound
   attempt identity.
3. **R4-R4**: Adversarial deterministic tests (all 22), focused adversarial
   tests ×5, and the full deterministic `-race` gate. Freeze HEAD and stop for
   independent review.
4. **R4-R5** (only after R4-R4 ACCEPT): live allow/deny proof, then the final
   repository gate and evidence report on a frozen exact HEAD.

## 11. Current unsafe ancestry and implementation authorization

The rejected substitution changes `1f0ad68` and `85e8830` are committed
ancestors of this branch; they were not stashed and are not accepted
production behavior. Until R4-R3 replaces them, the Claude actionable/live
path MUST NOT be run or represented as supported. R4-R2 and R4-R3 may be
developed and tested deterministically, but must land as one reviewable
authority repair before any live turn.

This R2 amendment authorizes R4-R2 through R4-R4 deterministic
implementation. Freeze HEAD and stop for independent review after all 22
adversarial tests and the full deterministic `-race` gate pass. R4-R5 live
proof remains prohibited until that review ACCEPT.
