# A1.2 C2D-D Planner Amendment — Closed Action Catalog

Status: **PLANNER-APPROVED CONTRACT AMENDMENT — C2D UNBLOCKED FOR P1 ONLY**

This amendment follows the independently confirmed D1 rejection and the honest
`C2D BLOCKED` record at `defeb9de2e73d8a821fc49a28a027529b05a60e3`.
It does not reinterpret either result. It replaces the rejected arbitrary-command
projection with a narrower architecture chosen by the planning/verifying agent.

## 1. Decision

The first actionable Claude slice uses a finite, server-owned action catalog.
Arbitrary Bash commands remain non-actionable. No provider command, prompt,
description, path, payload or digest source material enters a public DTO.

The initial catalog contains exactly one certification action:

| Field | Frozen value |
|---|---|
| Catalog action ID | `claude.bash.approval_probe.v1` |
| Provider/adapter | `claude_headless` |
| Provider version | exact certified `2.1.209` tuple only |
| Tool | exact `Bash` |
| Required command | exact `echo pokitclaudeapprovalprobe` |
| Optional input field | `description`, valid UTF-8 string of at most 512 bytes with no control code points; advisory only |
| Other input fields | rejected |
| POKIT-owned public summary | `Run Claude approval verification probe` |
| Options | existing `allow_once` and `deny` only |

This is a certification slice, not general Claude Bash approval support. Public
claims must say “one certified Claude approval action,” not “arbitrary Claude Bash
commands can be approved remotely.” Adding any catalog entry requires a separate
contract review with exact provider tuple, input schema, label and live evidence.

## 2. Why this satisfies both frozen contracts

The user reviews a stable POKIT action identity with a one-to-one, POKIT-owned
label. The label is not copied from provider input and is not a provider/model
description. The server knows the exact action because it recognizes only one
compiled catalog entry by strict structural decode and exact command comparison.

The public DTO remains unchanged. `SafeApprovalDTO.Summary` still contains only a
POKIT-owned bounded string. The full command remains prohibited from public/mobile
DTOs, logs, snapshots and evidence reports.

The prior phrase “complete execution-relevant action must be displayed” is narrowed
for catalog actions: an immutable catalog action ID plus its unique POKIT-owned
label is the canonical review representation. A digest, free-form description,
truncated command or heuristic match remains insufficient.

## 3. Strict classification boundary

Classification occurs at the Claude provider boundary while the original bounded
`tool_input` is already present. It must use a nested strict object decoder, not
`json.Unmarshal` into a map.

Required rules:

1. the value is one JSON object with no trailing data;
2. `command` occurs exactly once and is a string;
3. `description` may occur at most once and, when present, must be valid UTF-8,
   at most 512 bytes and contain no control code points;
4. the raw `tool_input` must remain within the existing `maxToolInputBytes` bound;
5. every other key, duplicate key, wrong type, invalid UTF-8 or over-bound value
   rejects catalog classification;
6. decoded command bytes equal the catalog command exactly;
7. provider, version, tool name and the accepted launch-certification tuple match
   exactly at production composition;
8. the existing canonical full-input digest is still computed and remains the
   provider-request identity used by defer/resume/witness matching;
9. raw command and description are discarded immediately after classification.

Classification produces only `{CatalogActionID, InputDigest}`. Empty/no match is
the normal fail-closed result and must not alter existing non-actionable behavior.

## 4. Binding and ownership table

| Value | Created by | Stored in | Compared at | Public? | Invalidated by |
|---|---|---|---|---|---|
| `CatalogActionID` | strict provider catalog classifier | bounded private Claude identity and internal approval record | catalog ingestion/composition and static summary lookup | no; only its static label is public | timeout, stop/kill/delete, exit, epoch replacement |
| `InputDigest` | existing canonical full-input digest | accepted Claude coordinator identity | repeated hook and consumed witness | no | existing coordinator cleanup |
| Static summary | compiled POKIT catalog | no caller-controlled storage; selected by exact provider/version/catalog ID | safe DTO projection | yes | disappears with record |
| Raw command/description | provider hook body | nowhere after classification | exact catalog comparison only | never | discarded in the request handler |

`CatalogActionID` is display metadata, not approval authority. It must never make a
record actionable by itself. C3D production composition is the only future owner
allowed to combine a certified tuple, catalog match, exact provider identity and
the already accepted A1 authority machinery into actionable ingestion.

Catalog IDs and static summaries must be unique, and the exact
provider/version/tool/command tuple must be unique within the compiled catalog. A
production-startup/unit invariant rejects duplicates so two actions can never share
the same public label or concrete tuple.

## 5. Minimal additive internal seam

The planner authorizes one bounded additive seam required by the catalog design:

- a private `catalogActionID` in Claude pending/coordinator identity records; and
- an internal `SafeReviewID` on approval ingestion/record storage, validated against
  an exact provider/version/ID-to-static-label mapping.

`SafeReviewID` is never accepted from a mobile request, never copied to a public
DTO, never included in a receipt, and never grants actionability. Unknown IDs or
provider/version mismatches reject the safe review and preserve the static generic
summary. No raw provider value is stored in this seam.

This is an intentional narrow amendment to the earlier “pure projector only” D2
scope. It does not authorize changes to claim, receipt, idempotency, requester,
runtime generation, lifecycle, denial decoding or witness semantics.

## 6. Packet sequence

1. **P1 — strict classifier**: pure nested decoder and one-entry catalog with
   exhaustive focused tests. No store/coordinator/DTO changes.
2. **P2A — private identity wiring**: classify at the real provider hook and carry
   only the catalog ID through pending/coordinator identity. Store/DTO unchanged.
3. **P2B — bounded display metadata plumbing**: carry only the catalog ID into
   internal approval storage and select the static summary. Actionability and
   options remain off in production.
4. **P3 — final controlled C2D composition**: prove catalog-match allow/deny and
   all negative interleavings using accepted delivery/witness machinery while
   production installation remains off.

Each packet is independently reviewed. C3D remains prohibited until final C2D
ACCEPT. C3D alone may install the certified tuple, mobile CTA and live positive
path atomically.

## 7. Rejected alternatives

- arbitrary command display or DTO expansion;
- regex/secret-scanner claims that arbitrary provider text is safe;
- hash-only or model-description review;
- prefix, truncation or redaction;
- caller-supplied catalog IDs;
- inferring catalog membership from command output, timing, terminal text or FIFO;
- treating a catalog match as approval authority without the A1 claim/delivery/
  consumption chain.
