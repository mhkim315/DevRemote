# A1.2 C2D-D1 — Projection Contract Decision

Status: **REJECTED HISTORICAL PROPOSAL — DO NOT IMPLEMENT**

Independent review rejected this proposal; the fail-closed record is
`docs/A1_2_C2D_D1_R1_BLOCKED.md`. The later planner-owned replacement is
`docs/A1_2_C2D_D_PLANNER_CATALOG_AMENDMENT.md`. The original text remains below
only as audit history.

Parent: `docs/NEXT_EXECUTOR_A1_2_C2D_D_STAGED_HANDOFF.md` §3
Preceding contract: `docs/A1_2_C2D_PACKET_CONTRACT_NOTE.md` §7.3

This is the mandatory D1 contract amendment required before any D2
implementation. It decides whether a closed, useful Bash subset can be
represented completely inside the frozen public DTO bounds without revealing
prohibited material. If it cannot, C2D is BLOCKED.

## 1. Decision

**A useful closed Bash subset EXISTS and is certified below.** D2 may proceed
with a pure safe-review projector implementing this contract. The subset is
syntactic, closed under the defined grammar, and every accepted command fits the
frozen public DTO bounds without loss, secret/path disclosure, or ambiguous
normalization.

## 2. Required table

### 2.1 Exact stored certified source field

| Item | Value |
|---|---|
| Source field | `command` member of the JSON object in `PreToolUse.tool_input` |
| Extraction point | `claudeHookBridge.handleHook` / `handleResume`, after strict allowlist decode of `tool_input` |
| Storage location | Private in-memory field retained alongside `inputDigest` in the C2D-B immutable identity record; never enters the provider-neutral Store or a log |
| Storage lifetime | Created at PreToolUse observation; invalidated on epoch replacement, stop, delete, exit, or timeout; never persisted across daemon restart |
| Why not the full `tool_input` | The full JSON object may contain fields beyond `command` (e.g. `description`, future additions) whose content is either advisory/non-execution, or uncertified. Only `command` carries the execution-relevant action. Storing the full input would also retain material that must never enter any DTO. |
| Why not `description` | Per §7.3 of the packet contract: "A provider/model-supplied `description` is advisory and can misrepresent the command; it is never review authority." |

### 2.2 Canonical byte representation and UTF-8 byte limit

| Item | Value |
|---|---|
| Canonical representation | The exact UTF-8 bytes of the JSON string value of the `command` field, after JSON unescaping (per RFC 8259 §7). No normalization, case-folding, whitespace trimming, or shell-word splitting is applied. |
| Storage byte limit | 2048 UTF-8 bytes. Derived from `maxToolInputBytes` (32 KiB) minus JSON envelope overhead; a 2048-byte command is well within the 32 KiB canonical input bound while covering every plausible safe command. Commands exceeding this limit are rejected at observation time with no stored review material. |
| DTO display limit | 194 UTF-8 bytes (the `safeSummaryMaxLen` of 200 minus the 6-byte `"Bash: "` prefix). A stored command that fits the storage bound but exceeds the DTO display bound produces no safe review (non-actionable). |
| Character encoding | Valid UTF-8 only. Invalid UTF-8, isolated surrogates, and overlong encodings are rejected at extraction. |
| Control characters | Code points U+0000–U+001F and U+007F–U+009F are prohibited. The command must be printable. |

### 2.3 Accepted Bash shape

The command string must match the closed grammar below in its entirety.

```
SAFE_BASH_COMMAND = SAFE_TOKEN (SP SAFE_TOKEN)*

SAFE_TOKEN = ALPHA_START (ALPHANUM / '.' / '_' / '-')*

SP         = %x20            ; exactly one ASCII space
ALPHA_START = %x41-5A / %x61-7A   ; A-Z a-z
ALPHANUM   = %x30-39 / ALPHA_START ; 0-9 A-Z a-z
```

In prose: one or more space-separated tokens. Each token starts with an ASCII
letter, followed by zero or more alphanumeric characters, dots, underscores, or
hyphens. No leading/trailing whitespace, no consecutive spaces, no empty
command. The grammar admits exactly the regular language
`^[a-zA-Z][a-zA-Z0-9_.-]*(\x20[a-zA-Z][a-zA-Z0-9_.-]*)*$`.

**Rationale for each element:**

- **Letter-first tokens**: Rejects flags (`-la`, `--verbose`), which are the
  most common carrier of paths (`--output=/tmp/out`), shell expansions
  (`--eval=$(id)`), and option-shaped secrets (e.g. `--token=<secret>`).
- **No `/`, `~`, `\`**: Rejects absolute paths (`/bin/rm`), home-directory
  references (`~/secret`), and Windows path separators.
- **No shell metacharacters** (`$`, `` ` ``, `(`, `)`, `;`, `|`, `&`, `<`,
  `>`, `!`, `\`, `'`, `"`, `*`, `?`, `[`, `]`, `{`, `}`): Rejects command
  substitution, pipelines, redirects, backgrounding, globs, variable expansion,
  quoting (which can hide metacharacters), and multi-command forms.
- **No `=`**: Rejects environment variable assignments (`FOO=bar cmd`) and
  inline option values.
- **No `#`**: Rejects comment-based truncation (`cmd # harmless` where `cmd` is
  dangerous).
- **No `%`, `^`, `@`, `:`**: Rejects URL schemes (`http://`), percent
  encodings, and scp-style paths (`user@host:path`).
- **Space as sole separator**: Rejects tab-separated and newline-injected
  commands. A single space is the only accepted token delimiter.

**Accepted examples (non-exhaustive):**

| Command | Tokens | Fit in DTO? |
|---|---|---|
| `echo ok` | `echo`, `ok` | 7 ≤ 194 ✓ |
| `echo c1d-probe-ok` | `echo`, `c1d-probe-ok` | 18 ≤ 194 ✓ |
| `git status` | `git`, `status` | 10 ≤ 194 ✓ |
| `make build` | `make`, `build` | 10 ≤ 194 ✓ |
| `npm run test` | `npm`, `run`, `test` | 13 ≤ 194 ✓ |
| `python3 script.py` | `python3`, `script.py` | 19 ≤ 194 ✓ |
| `docker compose up` | `docker`, `compose`, `up` | 17 ≤ 194 ✓ |

### 2.4 Every rejected Bash shape

| Category | Examples rejected | Reason |
|---|---|---|
| Empty command | `""` | No execution-relevant action |
| Leading/trailing space | `" echo"`, `"echo "` | Ambiguous; not in grammar |
| Consecutive spaces | `"echo  ok"` | Ambiguous; not in grammar |
| Flag (leading `-`) | `"ls -la"`, `"rm -rf"` | Token does not start with letter |
| Long-flag (leading `--`) | `"cmd --verbose"` | Token does not start with letter |
| Absolute path | `"cat /etc/hosts"`, `"/bin/sh"` | `/` not in grammar |
| Home-directory path | `"cat ~/secret"` | `~` not in grammar |
| Relative traversal | `"cat ../../etc"` | `/` not in grammar; also `.` alone fails ALPHA_START |
| Dot-start token | `".hidden.sh"`, `".."` | Does not start with letter |
| Shell variable | `"echo $HOME"`, `"echo ${PATH}"` | `$`, `{`, `}` not in grammar |
| Command substitution | `` "echo `id`" ``, `"echo $(whoami)"` | Backtick, `$`, `(`, `)` not in grammar |
| Pipeline | `"cat f \| grep x"` | `\|` not in grammar |
| Redirect | `"echo ok > /tmp/out"` | `>` and `/` not in grammar |
| Background | `"sleep 10 &"` | `&` not in grammar |
| Semicolon chain | `"echo a; echo b"` | `;` not in grammar |
| Quoted string | `"echo 'hello world'"`, `'echo "hello"'` | `'`, `"` not in grammar |
| Glob/wildcard | `"ls *.go"`, `"cat file?"` | `*`, `?` not in grammar |
| Env assignment | `"FOO=bar cmd"` | `=` not in grammar |
| URL/colon | `"curl http://example.com"` | `:`, `/` not in grammar |
| Comment | `"cmd # harmless"` | `#` not in grammar |
| Non-ASCII/emoji | `"echo café"` | `é` not in grammar (outside ASCII letter range) |
| Control character | `"echo\x00ok"` | U+0000–U+001F prohibited |
| Invalid UTF-8 | `"echo \xFF\xFE"` | Not valid UTF-8 |
| Newline embedded | `"echo a\necho b"` | `\n` (U+000A) is a control character |
| Single non-letter token | `"426"` | Does not start with letter |
| Over-length | 2049-byte command string | Exceeds storage bound |

A command outside this grammar produces **no safe review**; the approval remains
non-actionable. The grammar is total: any command not matching it is rejected
for review purposes.

### 2.5 Fields allowed in the public projection

The public projection is the existing `SafeApprovalDTO`. No new fields are
added. The safe review uses the `Summary` field, whose current static value
(`"Agent requested an approval"`) is replaced for actionable records that carry
a certified safe review.

| DTO field | Value when safe review available | Value when no safe review |
|---|---|---|
| `id` | `ApprovalID` (unchanged) | unchanged |
| `sessionId` | `SessionID` (unchanged) | unchanged |
| `summary` | `"Bash: <exact command text>"` | `"Agent requested an approval"` (current static value) |
| `state` | `projectPublicStatus(rec.state)` (unchanged) | unchanged |
| `actionable` | `true` | `false` (or `true` only if separately certified) |
| `options` | `[allow_once, deny]` projected via existing `SafeOptionDTO` | empty (or separately certified) |
| `createdAt` | unchanged | unchanged |
| `expiresAt` | unchanged | unchanged |

The `summary` format is `"<ToolName>: <command>"` where:
- `<ToolName>` is the certified tool name (`Bash` in the first slice);
- `<command>` is the exact, unmodified UTF-8 command string as extracted from
  the `tool_input.command` JSON field;
- The separator is exactly `": "` (colon, space);
- The total including prefix is bounded by `safeSummaryMaxLen` (200 bytes).

No other `tool_input` fields (`description`, future additions) appear in the
projection. No digest, truncated form, redacted form, or model-supplied summary
is substituted.

### 2.6 Relation between projected action and digests

```
PreToolUse.tool_input (JSON object, e.g. {"command":"echo ok","description":"test"})
  │
  ├─► canonicalJSON → SHA-256 → inputDigest
  │     │
  │     └─► contributes to CanonicalAction (InputType + InputPlacement + NormalizedInput)
  │            │
  │            └─► SHA-256 → ActionDigest (binding field B7)
  │
  └─► .command field (UTF-8 string "echo ok")
        │
        ├─► matches safe grammar? ──YES──► public projection summary: "Bash: echo ok"
        │
        └─► matches safe grammar? ──NO───► no safe review; non-actionable
```

**Invariants:**

1. **Completeness**: The projected command is byte-identical to the `command`
   field value extracted from the `tool_input` JSON that produced `inputDigest`.
   No character is dropped, added, or modified.

2. **One-way**: Given a projected command, an attacker cannot reconstruct
   `inputDigest` (the full `tool_input` may contain additional fields, and the
   canonicalization includes JSON structure bytes).

3. **Binding**: Two meaningfully different commands produce two different
   projected summaries. There is no collision in the projection. (Two identical
   commands with different `description` values produce the same summary but
   different digests — the description is not execution-relevant, and the
   digest difference prevents cross-approval substitution at the A1 binding
   layer.)

4. **Tamper evidence**: If any byte of `tool_input` (including the `command`
   field) changes, `inputDigest` changes, `ActionDigest` changes, and the A1
   binding match fails. The projection is display-only; authority stays with
   the digest chain.

### 2.7 Failure behavior

Every failure is total for the safe review: the command produces no projection.
The underlying approval record may still exist (non-actionable) per the
existing C1D path. No partial review, redacted review, or truncated review is
ever produced.

| Failure category | Detection | Behavior |
|---|---|---|
| **Secret-shaped value** | Token matches known secret patterns (recognizable API key / token prefixes) after grammar pass | No safe review. The grammar already rejects `-` as token-start, so hyphenated-prefix tokens are structurally excluded. An additional pattern check on the extracted command is defense-in-depth and must fail closed. |
| **Absolute path** | `/` anywhere in command | Rejected by grammar (`/` ∉ SAFE_TOKEN) |
| **Traversal path** | `..` as a token | Rejected by grammar (`.` is not ALPHA_START) |
| **Home-directory ref** | `~` anywhere in command | Rejected by grammar |
| **Control character** | Code point < U+0020 (except SP at U+0020) or in U+007F–U+009F | Rejected at extraction; invalid UTF-8 or control bytes → no stored command, no review |
| **Invalid UTF-8** | `utf8.ValidString(command) == false` | Rejected at extraction; no stored command, no review |
| **Shell metacharacter** | `$`, `` ` ``, `(`, `)`, `;`, `\|`, `&`, `<`, `>`, `!`, `\`, `'`, `"`, `*`, `?`, `[`, `]`, `{`, `}` | Rejected by grammar |
| **Multi-command form** | `;`, `\|`, `&&`, `\|\|`, newline | Rejected by grammar (these characters are not in SAFE_TOKEN; `&` and `\|` explicitly excluded) |
| **Env expansion** | `$NAME`, `${NAME}` | Rejected by grammar |
| **Quote-wrapped** | `'...'`, `"..."` | Rejected by grammar |
| **Over-bound (storage)** | `len(command) > 2048` | No stored command; no review |
| **Over-bound (DTO)** | `len("Bash: " + command) > 200` | No safe review (command stored but non-actionable for display) |
| **Empty command** | `command == ""` | No stored command; no review |
| **Missing `command` field** | `tool_input` has no `"command"` key, or the key's value is not a JSON string | No stored command; no review |
| **Wrong JSON type** | `tool_input.command` is a number, boolean, array, object, or null | No stored command; no review |
| **Duplicate fields** | `tool_input` has duplicate `"command"` keys (rejected by the strict decoder before extraction) | Entire `tool_input` rejected; no observation stored |
| **Ambiguous canonicalization** | The `command` field is a valid UTF-8 JSON string but its Go `string` representation differs from the bytes inside the canonical JSON (e.g., Unicode escape ` ` vs literal space) | JSON unescaping is deterministic per RFC 8259; the extracted Go `string` is the canonical form. No ambiguity. |
| **Canonicalization error** | `canonicalJSON(tool_input)` fails (invalid JSON, unsupported value) | Entire observation rejected at the hook bridge |
| **Unknown `tool_input` fields** | `tool_input` contains keys other than `command` and `description` | Not a failure for safe review. The additional fields contribute to `inputDigest` but are not projected. If a future field is execution-relevant, it must be added to the certified set via a new contract amendment. |

## 3. Fields excluded from the projection

These fields from `tool_input` are explicitly NOT projected. Their presence
does not invalidate a safe review of the `command` field, but they contribute
to `inputDigest` and are covered by the digest chain.

| Field | Reason excluded |
|---|---|
| `description` | Provider/model-supplied; advisory; can misrepresent the command (contract §7.3) |
| Any unknown future field | Not certified; execution relevance unknown; adding it to the projection would require a new contract amendment |

If a future certified `tool_input` field is execution-relevant, it must either
(a) be added to the safe review via a new D1-style contract amendment, or
(b) render the approval non-actionable until certified.

## 4. Interaction with ActionDigest and input digest

The `CanonicalAction` that produces `ActionDigest` (binding field B7) includes:

```
SchemaVersion ‖ OptionID ‖ Kind ‖ InputType ‖ InputPlacement ‖ NormalizedInput ‖ DeliverySchemaVersion ‖ DeliveryPayloadDigest
```

Where `InputType` for Claude Bash is `"tool_input.command"` and
`NormalizedInput` is the `inputDigest` (SHA-256 of `canonicalJSON(tool_input)`).

The safe review projection (`"Bash: echo ok"`) is NOT an input to `ActionDigest`.
It is a display-only derivative of the same source bytes. This separation means:

- **The digest binds execution**: `ActionDigest` covers the full `tool_input`
  canonicalization chain. Any input modification changes the digest and breaks
  the binding.
- **The projection informs the user**: The safe review shows what the digest
  commits to, in a human-readable form that fits the DTO bounds.
- **No authority path from projection to execution**: The mobile client sends
  an `OptionID`, not the projected text. The delivery boundary uses the stored
  decision and digest chain, never the displayed summary.

## 5. Usefulness assessment

The safe grammar accepts commands that are commonly used in development
workflows and in the C0D certification prompt itself:

| Use case | Example command | Accepted? |
|---|---|---|
| C0D certification probe | `echo c1d-probe-ok` | Yes |
| Version check | `node version` | Yes |
| Build step | `make build` | Yes |
| Test runner | `go test` | Yes (but `go test ./...` is rejected — `./...` has `/` and `.`) |
| Package manager | `npm install` | Yes (but `npm install --save` is rejected — `--save` starts with `-`) |
| Git inspection | `git status` | Yes |
| Docker | `docker compose up` | Yes |
| Python script | `python3 deploy.py` | Yes |

**Limitations accepted**: Commands with flags (`-la`), paths (`./...`), or
shell features are not reviewable. This is the correct fail-closed behavior:
the user sees no CTA rather than an ambiguous or redacted command. The subset
is deliberately narrow so that every displayed command is complete and
unambiguous.

**Counterexample proof** (truncation would hide meaning): The two commands
`echo safe operation proceeding` and `echo safe operation proceeding; rm -rf /`
share the prefix `echo safe operation proceeding`. Truncation to a 30-byte
prefix would show the same text for both. Under the safe grammar, the first
(48 bytes, all safe tokens) would produce a review; the second is rejected
outright (`;` and `/` are not in the grammar). The user never sees a truncated
or ambiguous command — they either see the full, exact command or nothing.

## 6. Explicit non-goals

1. **General shell redaction**: The grammar does not attempt to redact or
   sanitize arbitrary shell text. Commands outside the grammar are simply not
   projected.
2. **Semantic analysis**: The projector does not parse shell semantics,
   interpret commands, or assess safety/risk.
3. **Multi-tool review**: Only `Bash` is certified in this slice. Every other
   tool remains non-actionable until separately certified.
4. **Mobile UI changes**: D1 defines the DTO contract; mobile implementation
   is C3D work.
5. **Production actionability**: The projector is a pure function. C3D alone
   enables actionability.

## 7. Pre-D2 verification checklist

Before D2 implementation begins, confirm:

- [ ] The safe grammar is closed (no ambiguous edge cases).
- [ ] The 2048-byte storage bound and 194-byte DTO bound are compatible with
  every accepted example.
- [ ] The counterexample proves truncation would hide meaning.
- [ ] Every failure category has a defined behavior and no path produces a
  partial/redacted review.
- [ ] The `command` field extraction point (hook bridge) is the single source
  of truth and is already strict-decoded.
- [ ] No DTO field is broadened; the `Summary` field is the only carrier.
- [ ] The relation to `ActionDigest` and `inputDigest` is one-way (projection
  is display-only, digest chain is authority).
