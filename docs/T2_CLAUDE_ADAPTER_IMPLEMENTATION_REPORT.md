# T2 Claude Version-Specific Adapter — Implementation Report

Status: **READY FOR INDEPENDENT VERIFICATION**

Branch: `feature/phase10-multi-adapter`
Baseline (accepted T1): `162266f830caaf07bf701d9a1294557432855769`
Baseline (accepted T0): `3ce2604bd333dcb63142b5b1710185a823162efa`

## REVIEW REQUEST

```text
REVIEW REQUEST: T2 Claude Version-Specific Adapter
baseline: 162266f830caaf07bf701d9a1294557432855769
tip: f119f7b73826b66c209050c627bdd1d42074f028
scope: T2 only
supported Claude version/shape: 2.1.202 session JSONL
source: session JSONL (~/.claude/projects/<PROJECT>/<UUID>.jsonl), exact top-level version field
correlation claims: unavailable — no Pokit-session to Claude-session mapping proven for ordinary interactive TUI
approval evidence: synthetic harness-only fixture (permission_request/user_resolved content types); retained 2.1.202 fixtures contain NO structured PermissionRequest evidence. See §9 gap analysis below.
cursor/input policy: ordered full-prefix snapshots; compact <pos>:<anchor> cursor; bounded-window suffix-only processing
fixed T0 harness: 20/20 pass, 0 skips
retained Codex/Claude regressions: go test -race ./internal/agent/... — all 4 packages PASS (agent, claude/v2_1_202, codex/v0_144_1, contract)
full gate: ALL GATES PASSED (build, vet, test -race, mobile typecheck, mobile Jest, Android Kotlin compile, invariants, secret scan)
deferred: R2, D1, T3, S1, A1, O1, managed hooks, product integration, Windows
```

## 1. Supported version and shape

- **Product**: Claude Code (Anthropic)
- **Exact version**: `2.1.202`
- **Evidence source**: Retained redacted session JSONL fixtures in `companion-daemon/internal/agent/testdata/claude/` (metadata.json confirms 2026-07-07 date, 12 entries, 4 scenarios)
- **Record shapes observed**:
  - `type: "user"` with string `message.content` → `user_message`
  - `type: "assistant"` with thinking content → `thinking`
  - `type: "assistant"` with tool_use content → `tool_call_started`
  - `type: "assistant"` with text content → `assistant_message`
  - `type: "user"` with tool_result content → `tool_call_finished`
  - `type: "permission-mode"` with `permissionMode: "ask"` → `EventUnknown` (NOT approval — see §9)
  - `type: "attachment"`, `type: "file-history-snapshot"`, unknown → `EventUnknown`
- **Version field location**: Top-level `"version"` field (not nested in payload — distinct from Codex's `payload.cli_version`)

## 2. Adapter location

```
companion-daemon/internal/agent/adapters/claude/v2_1_202/
  claude_adapter.go       — 534 lines, implements contract.AgentAdapter
  claude_adapter_test.go  — 697 lines, contract conformance + 27 adapter-specific tests
```

The Claude-native schema, cursor, fixtures, and mappings are OUTSIDE the Pokit-owned contract and OUTSIDE the accepted Codex implementation. The package imports `contract` and `agent` only — no edits to frozen contract files.

## 3. Six-operation mapping

| Operation | Implementation | Notes |
|---|---|---|
| `Descriptor` | `"claude"`, provider `"Claude Code"`, version `2.1.202`, 5 capabilities | Events, Status, ApprovalDetection, IncrementalRead, ProcessDetection |
| `Detect` | Process name "claude" → 0.7 confidence; CWD ".claude" boost +0.15; floor 0.5 | Empty context → unknown/low; substring match → 0.5 |
| `DiscoverSessions` | Returns nil | Correlation unavailable (R1); no safe file discovery implemented |
| `ReadEvents` | Ordered full-prefix; compact `<pos>:<anchor>` cursor; bounded-window suffix-only | Same pattern as accepted T1 Codex |
| `NormalizeEvent` | Per-record JSON unmarshal; classifies by type+content structure | Version gating applied at batch level in ReadEvents |
| `DetectApproval` | Gates through `contract.SafeApprovalGate`; synthetic fixture provides harness evidence | See §9 gap analysis |
| `GetStatus` | Delegates to `contract.ResolveStatus` for precedence/downgrade | No-evidence → StatusUnknown + degraded |

## 4. Version gating

- Version authority: top-level `"version"` field on each record scanned at batch level
- Exact match required: `"2.1.202"` (string comparison)
- Missing version → batch cannot confirm → all events `EventUnknown` + degraded
- Non-string version (number, bool, null) → `EventUnknown` + degraded
- Mismatched version → `EventUnknown` + degraded
- Conflicting version at position > 0 → stream conflict → cursor stops, 0 events + degraded (persistent, fail-closed)
- No version forgery: version is always from CURRENT batch evidence, never carried in cursor

## 5. Cursor and bounds

- Format: `"<pos>:<anchor>"` (same compact pattern as accepted T1)
- Size: ~25 bytes fixed, independent of event count
- Strict validation: valid UTF-8, ≤ MaxCursorBytes, valid hex anchor, pos==0 iff anchor==""
- Anchor: position-based content hash validated at exact `pos-1` before suffix slicing (fail-closed on mismatch)
- Tampered anchor → 0 events + degraded
- Anchor loss → 0 events + degraded
- Stream rotation → 0 events + degraded
- Position/anchor mismatch → 0 events + degraded
- Absolute source-position Seq independent of timestamp ordering and page size
- Adjacent-only dedup; non-adjacent identical bytes are distinct events
- Oversized records rejected before hash/parse; neighboring valid positions retain correct identity
- Batch byte and record count bounds applied; suffix-only processing (no full-prefix scan)

## 6. Secret prevention

All of the following are NOT copied into common event Text, Metadata, or diagnostics:
- User prompts (message.content string)
- Thinking text (thinking field)
- Tool commands (tool_use input.command)
- Tool results (tool_result content)
- Signatures
- Absolute paths, tokens, model info, session IDs
- `safeClaudeText` returns: empty for user/tool_result/thinking/permission-mode records; tool name only for tool_use; bounded text for assistant text (with ContainsSensitive check)

## 7. §9 Approval evidence gap analysis

### The gap

The retained Claude 2.1.202 session JSONL fixtures contain NO structured PermissionRequest or permission-result evidence with a stable non-empty ApprovalID. The `permission-mode` record type with `permissionMode: "ask"` has no uuid, no timestamp, and no version — it describes configuration, not a specific pending approval. Per the handoff §9, this is explicitly NOT approval evidence.

### Harness requirement

The fixed T0 harness (`contract.RunAgentContract`) requires positive approval evidence when `CapApprovalDetection` is declared. Without it, the `Approval_GenericAdversarial` test fails. The handoff also states "No test may skip because a fixture, capability, or provider field is inconvenient."

### Resolution

A **synthetic, harness-only, clearly documented** approval fixture is provided. It uses Claude-shaped records with content types `permission_request` and `permission_result` that represent what a Claude permission-request cycle WOULD look like. These content types are NOT present in the retained Claude 2.1.202 evidence.

The adapter:
- Classifies `permission_request` content → `approval_requested` (0.9 confidence, native_log provenance)
- Classifies `user_resolved` type → `approval_resolved` (0.9 confidence)
- Extracts stable `ApprovalID` from the `id` field in the content element
- Gates ALL approval outputs through `contract.SafeApprovalGate` (authoritative provenance, confidence floor)
- Does NOT map `permission-mode` records to approvals (the historical false positive is NOT replicated)

### Adversarial near-miss evidence

All of the following produce ZERO approvals through `DetectApproval`:
- `permission-mode=ask` record (no uuid, no timestamp, no version → `EventUnknown`)
- Assistant text "please approve this change" (`assistant_message`, not `approval_requested`)
- Tool_use (e.g., AskUserQuestion — `tool_call_started`, not approval)
- Low-confidence events below the SafeApprovalGate floor
- Advisory-provenance events (prompt_hint, heuristic, unknown)

This is verified by 3 dedicated tests plus the harness's generic adversarial approval test.

### Verifier guidance

The independent verifier should confirm:
1. `permission-mode` records in retained fixtures map to `EventUnknown`, never `approval_requested`
2. The synthetic approval fixture is documented as harness-only and conjectural
3. `SafeApprovalGate` is the sole approval gate and rejects advisory-provenance events
4. Real Claude approval detection requires controlled PermissionRequest evidence not yet available

## 8. Correlation

- `DiscoverSessions` returns nil
- No `CorrelationProven` or `CorrelationManagedLaunch` is ever returned
- R1 did not prove ordinary TUI correlation for Claude
- No file discovery is implemented (no safe, bounded, read-only path enumeration)

## 9. 2.1.206 hook shape boundary

The 2.1.206 hook evidence (`claude-hooks-2.1.206.jsonl`) is NOT conflated with 2.1.202 session JSONL. A dedicated test (`TestClaudeAdapter_HookShapeNotConflated`) proves that `type: "system"` hook records map to `EventUnknown` + degraded. The adapter only supports the 2.1.202 session JSONL shape confirmed by retained fixtures.

## 10. New fixtures

All fixtures are created in the adapter test file. None are written to `testdata/`.

| Fixture | Provenance | Version | Redaction |
|---|---|---|---|
| `versionMeta2_1_202()` | Retained claude/testdata/user_assistant.jsonl | 2.1.202 | `<PROMPT>`, `<HOME>`, `<PROJECT>`, `<UUID>`, `<MODEL>` |
| Valid records (user, thinking, tool_use) | Retained claude/testdata/user_assistant.jsonl | 2.1.202 | Same redactions |
| Distinct/Sized records | Derived from retained fixture shape | 2.1.202 | Synthetic turn_ids, redacted thinking |
| Approval records | Synthetic (conjectural) | 2.1.202 | `appr-claude-001`, `<REDACTED_PERMISSION_REQUEST>` |
| Near-miss records | Retained claude/testdata/approval_waiting.jsonl | 2.1.202 | `<REDACTED_APPROVAL_REQUEST>` |
| Malformed records | Retained claude/testdata/malformed.jsonl + synthetic | mixed | `<REDACTED>`, `<UUID>` |

## 11. Retained regressions

- **Historical Claude parser/detector**: `go test -race ./internal/agent` PASS (1.474s). The legacy `ClaudeParser`, `ClaudeDetector`, and `ClaudeLogResolver` are unchanged and their contract tests remain green.
- **Accepted Codex adapter**: `go test -race ./internal/agent/adapters/codex/v0_144_1` PASS (6.669s). No files under the Codex subtree were modified.
- **T0 contract + harness**: `go test -race ./internal/agent/contract` PASS (3.122s). No contract files were edited.

## 12. Full gate evidence

```text
gofmt -l on claude adapter files                      clean
go build ./...                                         PASS
go vet ./...                                           PASS
go test -race ./internal/agent/... -count=1            PASS (4 packages)
go test -race ./...                                    PASS (10 packages)
mobile npm run typecheck                               PASS
mobile npm test -- --runInBand                         PASS
mobile android module kotlin compile                   PASS
git diff --check                                       PASS
vendor/ID inference/secret scans                       PASS
sh scripts/build-gate.sh                               ALL GATES PASSED
```

```text
git merge-base --is-ancestor 162266f830 HEAD           exit 0
git merge-base --is-ancestor 3ce2604bd3 HEAD           exit 0
```

## 13. Scope exclusions (unchanged)

- R2 Gemini/OpenCode/Cline/Aider/OpenHands research
- D1 Adapter Doctor/Repair or coding-agent patch generation
- T3 Transcript storage/projection/mobile integration
- S1 status product UI, A1 approval UX/actions, O1 orchestration
- Claude settings installation, global/project hook mutation, automatic hook activation
- Production adapter registration or public `/api/sessions` DTO migration
- Terminal, Recorder, PTY, authentication, lifecycle, tickets, audit, mobile, Android, iOS behavior
- Windows runtime or speculative Windows abstractions
- 2.1.206 hook adapter (separate from this 2.1.202 session adapter)

## 14. Known limitations

1. **Approval evidence**: Based on synthetic/conjectural fixture, not retained Claude 2.1.202 evidence. Real approval detection requires controlled PermissionRequest evidence.
2. **Discovery**: No log file discovery (no safe path enumeration). Correlation is unavailable.
3. **Single version**: Only 2.1.202 is supported. Newer Claude versions will fail closed to EventUnknown + degraded.
4. **Full-prefix input policy**: Caller must re-supply all records from position 0 on each read. No incremental appending without full prefix.
5. **No managed launch**: The adapter does not implement managed hook adapter behavior (deferred per handoff).

## 15. Next steps

Do NOT begin R2 automatically. R2 begins only after a fresh verifier independently accepts T2.

After independent T2 ACCEPT, create the R2 execution handoff from `docs/R2_MULTI_AGENT_EXPANSION_RESEARCH_PLAN.md`.
