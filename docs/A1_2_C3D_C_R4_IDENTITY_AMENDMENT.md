# A1.2 C3D-C R4 — Identity Model Amendment

Status: **PRE-IMPLEMENTATION — AWAITING REVIEW**

Parent: `docs/A1_2_C3D_C_LIVE_PACKET_CONTRACT_NOTE.md`
Rejected midpoint: `314541267d645b6f1f6e275fb7151e40e5cec1b8`
Authority: `docs/A1_2_C3D_ACTIVATION_CONTRACT_NOTE.md` §8 (accepted R1 `98ca6d9`)

## 1. Live-confirmed contradiction

The accepted C3D contract assumed the **same tool_use_id** across the defer
and the resume. This assumption was never tested against real Claude because
every accepted deterministic test uses a fake launcher that passes the same
`tuid` fire both hooks.

Live evidence (runs 5-7 at `3145412`, raw captures in
`c3dc_allow_launch1_raw_diag.jsonl`) proved:

- Real Claude `--resume` generates a **new** `tool_use_id` and a
  **new** `session_id` for the retried tool call.
- The PreToolUse hook body carries the **new identity**.
- The PostToolUse hook body also carries the **new identity**.
- The tool is executed correctly (probe token present, `exitCode:0`).

The three attempted production fixes (`0793bde`, `1f0ad68`, `85e8830`)
**substituted stored original identity values** for the actual hook body
values. This passed the deterministic composition tests (which pass the
same `tuid` as the stored identity) but is a contract violation: it removes
the real-invocation verification that the accepted design requires.

## 2. Amended guarantee (replacing "same tool-use ID across defer/resume")

> The original invocation identity is **not** preserved across the resume.
> Instead, the **claim-owned managed resume process** atomically binds,
> exactly once, the real new invocation identity whose `tool_name` and
> canonical `input_digest` exactly match the approved original action.
> **Only** the provider-native result bearing that new bound identity
> (PostToolUse for allow, `permission_denials` entry for deny) is admitted
> as the consumption witness. Any tool invocation that does not match the
> original action is fail-closed deferred. Any witness that does not bear
> the bound identity is fail-closed rejected. If the resume process cannot
> produce a distinguishable single invocation (e.g., the model retries and
> produces multiple tool calls with the same `tool_name`+`input`), the
> claim fails closed.

## 3. Split identity model

### 3A. OriginalApprovalIdentity (already exists, unchanged)

Stored in `claudePrivateIdentity` at `joinDeferred` time from the initial
deferred observation:

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
| `claimToken` | The claim token from `ReserveEntry` |
| `resumeNonce` | The resume nonce from `ReserveEntry` |
| `sessionID` (Claude) | **Real** resume PreToolUse `session_id` |
| `toolUseID` | **Real** resume PreToolUse `tool_use_id` |
| `toolName` | **Real** resume PreToolUse `tool_name` (must equal original) |
| `inputDigest` | **Real** resume PreToolUse `tool_input` canonical digest (must equal original) |
| `launchGen` | Resume process epoch |
| `registeredAt` | `clockNow()` |

Registration precondition: `toolName == original.toolName && inputDigest == original.inputDigest` — validated by the bridge **before** calling `ClaimWrite`.

Registration is atomic one-time: the first `ClaimWrite` that passes the bridge-side validation creates the `ResumeAttemptIdentity` on the entry. A second `ClaimWrite` for the same claim with a different identity fails `outcomeMismatch`. Replay with the same identity succeeds (idempotent within the entry timeout).

### 3C. Witness validation (updated `MarkWitnessed`)

| Current (broken) | Fixed |
|---|---|
| Compares against **original** stored identity | Compares against **ResumeAttemptIdentity** (bound at `ClaimWrite` time) |
| PostToolUse body identity discarded | PostToolUse body identity MUST match bound `ResumeAttemptIdentity` |
| Denial entry identity compared against original | Denial entry identity MUST match bound `ResumeAttemptIdentity` |

## 4. Early-witness race: state machine extension

Live evidence also revealed a race: `ConfirmWrite(true)` transitions the
entry to `stateDecisionWritten`, but `PostToolUse` may arrive before
`ConfirmWrite` returns. The HTTP write to the PreToolUse hook is
buffered — `ConfirmWrite` runs before the handler returns — but the
coordinator mutex is released between `ClaimWrite` and `ConfirmWrite`.

New state: `stateWitnessPending` (value `witness_pending`), entered when a
`MarkWitnessed` call finds the entry in `stateWriteClaimed` (write in
flight, not yet confirmed).

State machine:

```
reserved  ──ClaimWrite──▶ write_claimed  ──ConfirmWrite(true)──▶ decision_written
                                  │                                      │
                                  │ MarkWitnessed (early)                │ MarkWitnessed
                                  ▼                                      ▼
                           witness_pending ──ConfirmWrite(true)──▶  witnessed (terminal)
                                  │
                                  │ ConfirmWrite(false)
                                  ▼
                            ambiguous/failure (terminal)
```

Rules:
- `ClaimWrite` at `stateWriteClaimed` with **same** identity → idempotent (replay of buffered hook retry).
- `ClaimWrite` at `stateWriteClaimed` with **different** identity → `outcomeMismatch`.
- `MarkWitnessed` at `stateWriteClaimed` → stores the witness args and transitions to `stateWitnessPending`. Does NOT commit the terminal result yet.
- `ConfirmWrite(true)` at `stateWitnessPending` → commits the stored witness as `TerminalWitnessed` (terminal).
- `ConfirmWrite(false)` at any non-terminal state → terminal with `outcomeAmbiguous`. Wipes any stored early witness.
- `MarkWitnessed` at `stateWitnessPending` with a DIFFERENT identity → `outcomeMismatch` (reject, entry stays witness_pending).
- All other state transitions unchanged.

No coordinator lock is held across I/O. ClaimWrite, MarkWitnessed, and
ConfirmWrite are each mutex-guarded individually.

## 5. Bridge changes (summary)

### `handleResume`
1. Strict decode real body
2. Recompute `inputDigestBody`
3. Validate claim/nonce against context
4. **Compare** `toolNameBody == ctx.toolName && inputDigestBody == ctx.inputDigest`
5. If mismatch → fail-closed defer
6. If match → `ClaimWrite` with **real body values** (sessionIDBody, toolUseIDBody, toolNameBody, inputDigestBody)

### `handlePostTool`
1. Strict decode real body
2. Recompute `inputDigestBody`
3. `MarkWitnessed` with **real body values**

### `routeDenial` (in managed_claude.go pump)
Already uses stream-decoded real denial entry values. No change needed for
identity — the existing `ctx.toolUseID` gating on denial entries is correct
(it gates on the ORIGINAL identity, which is the safety filter for "is this
denial about our tool?"). After the fix, the denial entry's `toolUseID` is
compared against the **ResumeAttemptIdentity** in `MarkWitnessed`, which
closes the loop.

## 6. Required adversarial tests

| # | Test | Assertion |
|---|---|---|
| 1 | Mutated input on resume | 0 responses (fail-closed defer) |
| 2 | Different tool name on resume | 0 responses |
| 3 | Two resume hooks competing | Exactly one identity registered |
| 4 | PostToolUse with different toolUseID | Witness rejected (no commit) |
| 5 | Early PostToolUse (before ConfirmWrite) | Held, one success after ConfirmWrite(true) |
| 6 | Write failure then PostToolUse | No success (ambiguous terminal) |
| 7 | Timeout/stop/replacement | ResumeAttemptIdentity removed |
| 8 | Deny evidence matches new identity | Witness accepted (deny commit) |
| 9 | Deny evidence with different identity | Witness rejected |

Tests 1-2 and 7 already have counterparts in the existing catalog tests
(`TestClaudeDelivery_CatalogMutatedResumeInput` etc.) that were broken by
the rejected substitution fix. Tests 3-6, 8-9 are new coordinator-level
tests.

## 7. Non-goals

- No change to `ReserveIdentity`, `ReserveEntry`, `ConfirmWrite` semantics
  beyond the state machine extension.
- No change to `CancelEntry`, `ClearRuntime`, `Close`.
- No change to the delivery layer (`claude_approval_delivery.go`).
- No new catalog entries or prompt changes beyond the already-accepted
  `claudeCertificationPrompt` alignment.
- No live proof until R4-R2 through R4-R4 are complete and deterministic
  tests pass.

## 8. Stop condition

Stop for independent review of this amendment before implementing R4-R2.
The amendment must be ACCEPTED before any coordinator or bridge code
changes.
