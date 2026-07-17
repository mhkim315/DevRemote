# A1.2 C2D-D1-R1 — Projection Contract BLOCKED

Status: **C2D BLOCKED — D2 PROHIBITED — C3D PROHIBITED**

Historical status note: this document correctly closed the rejected arbitrary-
command design. The planning agent subsequently authorized a different, finite
server-owned catalog design in
`docs/A1_2_C2D_D_PLANNER_CATALOG_AMENDMENT.md`. That amendment does not overturn
these findings; it authorizes only its P1 classifier packet.

Parent: `docs/NEXT_EXECUTOR_A1_2_C2D_D_STAGED_HANDOFF.md` §3
Rejected: `docs/A1_2_C2D_D1_PROJECTION_CONTRACT.md` (`5f99980`)

D1 proposed a safe Bash grammar whose accepted commands would be placed
verbatim into `SafeApprovalDTO.Summary`. That design was independently
reviewed and rejected. This document records the five blockers, the
unresolvable contract contradiction, and the formal C2D BLOCKED conclusion.

## 1. Blocker summary

### Blocker 1 — Frozen A1 DTO prohibits raw command text

`SafeApprovalDTO.Summary` is a Pokit-owned static string. The frozen DTO
contract explicitly forbids raw provider text in any public field:

> "They never expose raw provider prompts, terminal output,
> provider/delivery payloads, **full commands**, tokens/secrets, absolute
> paths, arbitrary provider labels/placeholders, internal claim tokens, or
> ActionDigest source material."
>
> — `docs/A1_APPROVAL_SAFETY_PLAN.md` §7 (emphasis added)

And:

> "The external (mobile-facing) approval projection is a strict structural
> allowlist. It NEVER carries a raw provider prompt, terminal output,
> provider/delivery payload, **full command**, token/secret, absolute path,
> arbitrary provider label or placeholder, internal claim token, or
> ActionDigest source material. Display text is Pokit-owned and bounded;
> delivery payloads stay server-side."
>
> — `companion-daemon/internal/term/approval_dto.go:7`

Putting `"Bash: echo ok"` into `Summary` — even through a grammar filter —
is a semantic DTO expansion. The bytes come from the provider; the field
was frozen to carry only Pokit-owned static text ("Agent requested an
approval"). Adding a new JSON field would broaden the DTO structurally;
reusing an existing field to carry provider text broadens it semantically.
Both are prohibited.

### Blocker 2 — Unknown `tool_input` fields not fail-closed

D1 §3 permitted unknown `tool_input` fields (beyond `command` and
`description`) to pass through without invalidating the safe review:

> "If a future field is execution-relevant, it must be added to the
> certified set via a new contract amendment."
>
> — `docs/A1_2_C2D_D1_PROJECTION_CONTRACT.md` §3

But the user sees only the `command` field. If an unknown field changes
execution semantics (e.g. a `timeout` or `dangerouslyDisableSandbox`
field), the projected review no longer describes "the exact action whose
digest is claimed." The closed-world assumption must reject unknown fields
at extraction, not defer certification. An actionable subset whose
displayed action may be incomplete is not a safe review.

Required behavior: exactly `{"command": "<string>"}` and optionally
`{"description": "<string>"}` with no other keys. Unknown keys → no safe
review.

### Blocker 3 — Duplicate-key handling claim is incorrect

D1 §2.7 claimed:

> "Duplicate fields — `tool_input` has duplicate `"command"` keys
> (rejected by the strict decoder before extraction)"

But the strict allowlist decoder (`strictPreToolUseDecode` in
`claude_hook_bridge.go:64`) operates on the **top-level** PreToolUse
fields only (`session_id`, `tool_use_id`, `tool_name`, `tool_input`,
`hook_event_name`, `cwd`, `transcript_path`, `prompt_id`,
`permission_mode`, `effort`).

The `tool_input` value is decoded as a raw `json.RawMessage` and passed to
`canonicalJSON()` (`managed_claude.go` / `claude_hook_bridge.go:251`),
which uses Go's `encoding/json.Unmarshal` — a decoder that silently keeps
the **last** value for duplicate object keys per RFC 8259 §4. Duplicate
`"command"` keys within `tool_input` would NOT be rejected; the last value
would silently win. D1's extraction premise does not match the production
code path.

### Blocker 4 — Secret-free guarantee does not hold

The safe grammar permits plain alphanumeric tokens:

```
SAFE_TOKEN = ALPHA_START (ALPHANUM / '.' / '_' / '-')*
```

A command like `echo supersecrettoken123` passes the grammar and would be
projected verbatim. D1's "additional pattern check on the extracted
command" (§2.7) has no frozen patterns, byte limits, or failure rules — it
is a placeholder, not a privacy boundary.

This is the same reason the frozen DTO prohibits raw commands in the first
place: a syntactic filter cannot distinguish `echo hello` (safe) from
`echo supersecrettoken123` (credential-bearing). Both are structurally
identical. No regex can close this gap without either:
- a complete secret-pattern catalog (unbounded, cannot be closed), or
- rejecting all commands (trivial, non-useful).

### Blocker 5 — Storage requires accepted lifecycle change beyond D2 scope

The current private identity record stores only `inputDigest`:

```go
// managed_claude.go:460-462
rt.coordinator.ReserveIdentity(approvalID, sessionID, toolUseID,
    toolName, inputDigest, rt.sessionID, pokitRT)
```

No raw `command` field is retained. D1 requires storing up to 2048 bytes
of command text alongside the digest. This is a change to the C2D-B
coordinator identity record — accepted at `48ce57d`. But D2 is scoped as
a "pure safe-review projector" packet that "may not change accepted C2D-C
lifecycle or authority code."

The command must be stored at observation time (the hook bridge), flow
through the identity record, and be available to the projector. That path
touches the frozen C2D-B coordinator, which D2 is prohibited from
modifying.

## 2. Unresolvable contract contradiction

The C2D-D handoff requires:

> "The safe review projection must be ... sufficient to distinguish the
> exact action whose digest is claimed."
>
> "A description, model summary, digest, truncated prefix, normalized
> display text, or terminal output is never a substitute for the complete
> execution-relevant action."
>
> — `docs/NEXT_EXECUTOR_A1_2_C2D_D_STAGED_HANDOFF.md` §3

Simultaneously, the frozen A1 safety plan requires:

> "They never expose ... full commands."
>
> — `docs/A1_APPROVAL_SAFETY_PLAN.md` §7

For the Bash tool, the **complete execution-relevant action IS the command
string**. There is no other representation. The command text cannot be
simultaneously shown (per the handoff) and hidden (per the safety plan).

A Pokit-owned label catalog would substitute a description for the
execution-relevant action, which the handoff also prohibits. A digest
without the command is explicitly insufficient. A truncated or redacted
command can hide execution meaning.

**No representation satisfies both frozen contracts simultaneously.** This
is not a design gap that further iteration can close — it is a structural
contradiction between two independently accepted authority documents.

## 3. Formal C2D BLOCKED

Per `NEXT_EXECUTOR_A1_2_C2D_D_STAGED_HANDOFF.md` §3:

> "If no useful closed Bash subset can be represented completely inside
> the frozen public DTO bounds without revealing prohibited material,
> record **C2D BLOCKED** and stop. Do not broaden the DTO, weaken privacy,
> or start D2."

**C2D is BLOCKED.** No safe review projection can satisfy both:
- the handoff's requirement for the complete execution-relevant action, and
- the safety plan's prohibition on exposing full commands.

## 4. Non-blocking correction

D1 §5 claimed `echo safe operation proceeding` is 48 bytes. It is 30 bytes
(`echo` + space + `safe` + space + `operation` + space + `proceeding` =
4+1+4+1+9+1+10 = 30). The counterexample logic is unaffected.

## 5. Path forward

One of the frozen contracts must be amended before C2D can proceed.
Options (none authorized without independent contract acceptance):

### Option A — Amend the safety plan to permit bounded command display

Allow `SafeApprovalDTO.Summary` to carry a bounded, grammar-filtered
command for actionable records where the command passes a verifier-certified
safe grammar. This weakens the "NEVER full commands" guarantee in exchange
for a narrower, auditable path. Requires explicit acceptance of:
- the exact grammar, bounds, and failure behavior;
- the residual risk of plaintext secrets passing the grammar;
- a secret-pattern defense-in-depth check with frozen patterns and fail-closed behavior.

### Option B — Finite action catalog with digest binding

Define a server-owned finite catalog mapping `inputDigest → Pokit-owned
label`. Display the label, not the command. The digest binding proves the
exact action. This accepts that the label is a description, not the
"complete execution-relevant action," and requires amending the handoff's
prohibition on description-as-substitute. The catalog is finite, so unknown
commands have no entry and remain non-actionable.

### Option C — Accept permanent non-actionability for Bash

Leave Bash approvals permanently non-actionable. The user sees "Agent
requested an approval" with no CTA. C2D delivers the decision but shows no
command text. This is the safest option and violates no frozen contract. It
means Claude Bash approvals are never actionable through the mobile
approval path.

## 6. Current state

| Item | Value |
|---|---|
| Rejected D1 SHA | `5f99980bc0b49905fba38d58ad3d0e20f8a8d8ba` |
| This document SHA | `<commit>` |
| C2D status | **BLOCKED** |
| D2 | Prohibited |
| D3 | Prohibited |
| C3D | Prohibited |
| Production actionability | Zero (unchanged) |
| Production code changes | None |

No code was changed. The rejected D1 document remains for audit trail.
D2 must not be started until one of the frozen contracts is independently
amended and the amendment is accepted.
