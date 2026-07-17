# A1.2 C2D Catalog P1 — Strict Classifier Contract

Status: **PRE-IMPLEMENTATION — P1 ONLY — NOT ACTIONABLE**

Parent: `docs/A1_2_C2D_D_PLANNER_CATALOG_AMENDMENT.md`
Handoff: `docs/NEXT_EXECUTOR_A1_2_C2D_CATALOG_HANDOFF.md`

This note freezes the exact strict-decode and classification contract before any
P1 implementation. It does not change production code, storage, or DTOs.

## 1. Single frozen catalog entry

| Field | Frozen value |
|---|---|
| `CatalogActionID` | `"claude.bash.approval_probe.v1"` |
| Provider/adapter | `"claude_headless"` |
| Provider version | `"2.1.209"` |
| Tool name | `"Bash"` |
| Required command | `"echo pokitclaudeapprovalprobe"` |
| Optional `description` | valid UTF-8 string, ≤ 512 bytes, no control code points; advisory only |
| Other `tool_input` keys | **rejected** |
| Public summary | `"Run Claude approval verification probe"` |

The catalog is compiled as a single Go source constant slice. The classifier
iterates the catalog entries; with one entry this is a single comparison.

## 2. Nested strict object decoder

`strictDecodeToolInput(raw []byte) (command, description string, ok bool)`

Uses `encoding/json` token-walking (`dec.Token()`), NOT `json.Unmarshal` into a
map. Rules applied in order:

1. `raw` must be non-empty and ≤ `maxToolInputBytes` (32768).
2. First token must be `{` (JSON object delimiter).
3. Each key token must be a JSON string; unknown keys → reject.
4. `"command"`: must appear **exactly once**. Value must be a JSON string.
   Must be non-empty, valid UTF-8. No byte bound beyond `maxToolInputBytes`
   (the entire object is bounded; a command approaching the object bound is
   already excluded by the catalog comparison).
5. `"description"`: may appear **at most once** (zero or one). When present,
   value must be a JSON string, valid UTF-8, ≤ 512 bytes, containing no
   control code points (U+0000–U+001F, U+007F–U+009F). Empty string is
   permitted (it is advisory and ignored).
6. Duplicate `"command"` key → reject. Duplicate `"description"` key → reject.
7. Wrong JSON type for any recognized key → reject.
8. Trailing content after the closing `}` → reject.
9. Missing `"command"` → reject.

The decoder returns only `(command, description, true)` on success. The raw
bytes never escape the decoder.

## 3. Catalog classifier

`classifyCatalogAction(toolInput []byte, provider, version, toolName string) (catalogActionID string, inputDigest string, ok bool)`

1. `strictDecodeToolInput(toolInput)` → extracts command and description.
2. `canonicalJSON(toolInput)` → SHA-256 → `inputDigest`. (The strict decode
   already ensured the object is well-formed and has no duplicate keys, so
   `canonicalJSON` is deterministic.)
3. Compare `{provider, version, toolName, command}` against every catalog
   entry. Match → return `(entry.CatalogActionID, inputDigest, true)`.
4. No match → return `("", "", false)`.

The classifier returns only `{CatalogActionID, InputDigest}`. It never returns
the command, description, summary label, or partially decoded material.

## 4. Interaction with existing code

The classifier is a pure function. In P1 it is called only from tests. P2 will
wire it into the Claude provider boundary (the hook bridge / observation path)
at the point where `tool_input` is already in hand.

- `canonicalJSON` and `sha256Hex` are the existing functions in
  `managed_claude.go`.
- `maxToolInputBytes` is the existing constant in `claude_hook_bridge.go`.
- No existing file is modified in P1.

## 5. Test inventory (mandatory)

| # | Test | Category |
|---|---|---|
| 1 | Exact command-only object → match | golden |
| 2 | Exact command + bounded description → match | golden |
| 3 | Deterministic repeat (same input, same result) | golden |
| 4 | Command mutation by one byte → no match | negative |
| 5 | Missing `command` key → no match | negative |
| 6 | `command` as number → no match | wrong-type |
| 7 | `command` as null → no match | wrong-type |
| 8 | `command` as array → no match | wrong-type |
| 9 | `command` as object → no match | wrong-type |
| 10 | Duplicate `command` keys → no match | duplicate-key |
| 11 | Duplicate `description` keys → no match | duplicate-key |
| 12 | Unknown nested field → no match | unknown-field |
| 13 | Non-object input (string) → no match | non-object |
| 14 | Non-object input (array) → no match | non-object |
| 15 | Trailing JSON content → no match | trailing |
| 16 | Invalid UTF-8 in command → no match | utf-8 |
| 17 | Over-length description (513 bytes) → no match | over-bound |
| 18 | Description exactly 512 bytes → match | boundary |
| 19 | Control character in description → no match | control |
| 20 | Provider mismatch → no match | provider |
| 21 | Version mismatch → no match | version |
| 22 | Tool name mismatch → no match | tool |
| 23 | Empty `tool_input` object → no match | edge |
| 24 | Empty command string → no match | edge |
| 25 | Known-bad last-key-wins control: `json.Unmarshal` into map silently takes last value; strict decoder rejects | duplicate-key control |
| 26 | Description with path/token sentinels → match succeeds, sentinels absent from output | privacy |
| 27 | Output does not contain command/description | privacy |
| 28 | Exact byte bound: `maxToolInputBytes`-byte valid input → match | boundary |
| 29 | One byte over `maxToolInputBytes` → no match | boundary |

## 6. Explicit non-goals

- No wiring into runtime observation, Store, coordinator, or DTO (P2).
- No production actionability or option enablement (C3D).
- No multi-entry catalog (one entry only).
- No non-Bash tool classification.
- No modification of accepted delivery/lifecycle/witness code.
