# T2 Claude Version-Specific Adapter — Implementation Report (Remediation)

Status: **READY FOR INDEPENDENT VERIFICATION**

Branch: `feature/phase10-multi-adapter`
Baseline (accepted T1): `162266f830caaf07bf701d9a1294557432855769`
Baseline (accepted T0): `3ce2604bd333dcb63142b5b1710185a823162efa`

## REVIEW REQUEST

```text
REVIEW REQUEST: T2 Claude Version-Specific Adapter (remediated)
baseline: 162266f830caaf07bf701d9a1294557432855769
tip: <final SHA after push>
scope: T2 only
supported Claude version/shape: 2.1.202 session JSONL
source: session JSONL (~/.claude/projects/<PROJECT>/<UUID>.jsonl), exact top-level version field
correlation claims: unavailable — no Pokit-session to Claude-session mapping proven for ordinary interactive TUI
approval evidence: NONE — CapApprovalDetection NOT advertised. DetectApproval always returns nil.
cursor/input policy: ordered full-prefix snapshots; compact <pos>:<anchor> cursor; bounded-window suffix-only processing
version authority: input[0] only (one bounded parse); per-record strict version gate (version != "2.1.202" → EventUnknown)
fixed T0 harness: 20/20 pass (capability-aware correction applied)
retained Codex/Claude regressions: go test -race ./internal/agent (PASS), codex/v0_144_1 (PASS), contract (PASS)
full gate: ALL GATES PASSED (gofmt clean, build OK, vet OK, go test -race OK, mobile PASS, invariants PASS, secret scan PASS)
deferred: R2, D1, T3, S1, A1, O1, managed hooks, product integration, Windows
```

## Remediation of REJECT blockers

### B1: Synthetic approval removed

- Removed all conjectural production mappings: `permission_request`, `user_resolved`, `permission_result`
- Removed `isPermissionRequest()`, `claudeApprovalID()` functions
- `CapApprovalDetection` NOT advertised in Descriptor
- `DetectApproval` always returns nil (safe default)
- `ApprovalRecords: nil` in conformance fixtures
- Near-miss records (permission-mode, approval-like text, tool_use) still produce ZERO approvals

### B2: Exact per-record version gate

- Changed `if cr.Version != "" && cr.Version != supportedClaudeVersion` → `if cr.Version != supportedClaudeVersion`
- Missing, non-string, and mismatched version ALL → `EventUnknown` + degraded
- Test proves: versioned first record does NOT authorize following versionless records
- Every confidently typed event MUST carry and match exact `"2.1.202"`

### B3: Unbounded version discovery removed

- Version authority: input[0] only (one bounded JSON parse before BoundBatch)
- Removed full-input version fallback scan
- Removed suffix stream-conflict scan
- If input[0] doesn't carry exact `"2.1.202"`, entire batch → EventUnknown + degraded
- No unbounded history search for version

### B4: Assistant private text prevention

- `safeAssistantText()` function removed
- Assistant `text` content → empty Text (only event type preserved)
- Assistant `thinking` → empty Text (was already redacted)
- Tool name only for `tool_use` (safe identity metadata)
- Test proves: benign-looking private assistant text appears nowhere in output

## §9 Approval authority — resolved via T0 harness correction

The T0 harness has been corrected to be capability-aware (option 2 from the
independent verifier recommendation). The fix restores the original contract
meaning: `CapApprovalDetection` in the descriptor controls whether positive
approval evidence is required.

### Harness correction (`companion-daemon/internal/agent/contract/harness.go`)

- `testApprovals` now gates positive fixture check on `hasCapability(ad.Descriptor(), CapApprovalDetection)`
- **Cap declared**: runs existing positive fixture + ApprovalID/session/source/confidence binding validation
- **Cap NOT declared**: skips positive fixture; instead verifies `DetectApproval` returns zero even for a synthetic authoritative `approval_requested` event (safe-default guard)
- Generic adversarial negatives run for ALL adapters regardless of capability
- Added `hasCapability()` helper
- `requireFixtures` already only FATALs on missing ApprovalRecords when CapApprovalDetection IS declared (unchanged)

### Regression evidence

- Accepted Codex adapter (CapApprovalDetection declared): full harness PASS, positive binding checks unchanged
- Claude adapter (CapApprovalDetection NOT declared): full harness PASS, no-capability safe-default check
- T0 contract harness tests: PASS
- No approval safety weakened: generic adversarial negatives unchanged for all adapters

### Claude approval evidence status

The retained Claude 2.1.202 session JSONL fixtures still lack structured
PermissionRequest evidence. The adapter correctly does not advertise
`CapApprovalDetection` and `DetectApproval` always returns nil. When
controlled Claude approval evidence becomes available, a separate adapter
revision can add it behind the existing `SafeApprovalGate`.

## Supported version and shape

- **Product**: Claude Code (Anthropic)
- **Exact version**: `2.1.202`
- **Evidence source**: Retained redacted session JSONL fixtures in `companion-daemon/internal/agent/testdata/claude/`
- **Version field location**: Top-level `"version"` field (not nested — distinct from Codex's `payload.cli_version`)

### Record mappings (all require version == "2.1.202")

| Claude record | T0 event | Text | Notes |
|---|---|---|---|
| `type:"user"` with string content | `user_message` | "" | Prompt not copied |
| `type:"assistant"` with thinking | `thinking` | "" | Thinking text not copied |
| `type:"assistant"` with tool_use | `tool_call_started` | tool name only | Commands not copied |
| `type:"assistant"` with text | `assistant_message` | "" | Private text not copied |
| `type:"user"` with tool_result | `tool_call_finished` | "" | Output not copied |
| `type:"permission-mode"` | `EventUnknown` | "" | NOT approval evidence |
| `type:"attachment"` / `"file-history-snapshot"` / unknown | `EventUnknown` | "" | Unknown shapes fail safe |

### Version != "2.1.202" or missing → EventUnknown + degraded

## Six-operation mapping

| Operation | Implementation | Notes |
|---|---|---|
| `Descriptor` | `"claude"`, provider `"Claude Code"`, version `2.1.202`, 4 capabilities | Events, Status, IncrementalRead, ProcessDetection |
| `Detect` | Process name "claude" → 0.7; CWD ".claude" boost +0.15 | floor 0.5 |
| `DiscoverSessions` | Returns nil | Correlation unavailable |
| `ReadEvents` | Ordered full-prefix; compact `<pos>:<anchor>` cursor; suffix-only | Pattern from accepted T1 |
| `NormalizeEvent` | Per-record JSON + strict version gate | version != "2.1.202" → EventUnknown |
| `DetectApproval` | Always returns nil | Safe default; no evidence |
| `GetStatus` | Delegates to `contract.ResolveStatus` | Advisory downgrade |

## Secret prevention

All of the following are NOT copied into common event Text, Metadata, or diagnostics:
- User prompts, thinking text, tool commands, tool results, signatures
- Assistant private text bodies (B4 — structural type only, empty text)
- Absolute paths, tokens, model info, session IDs

## New adapter-specific tests

35 tests proving:
- Exact version gate acceptance + newer/missing/non-string/conflicting rejection
- Versioned first record does NOT authorize following versionless records
- No-version stream → all EventUnknown + degraded
- 2.1.206 hook shapes not conflated
- One-shot/paged tuple equality
- Cursor tamper/loss/rotation fail-closed
- Source-position ordering independent of timestamp
- Malformed/oversized mixed batch integrity
- No prompt/command/tool result/thinking/signature/path leakage
- No assistant text body leakage (Text, Metadata, Diagnostics)
- permission-mode, approval-like text, tool_use → ZERO approvals
- DetectApproval always returns nil
- Descriptor has no CapApprovalDetection, no CapLogDetection
- Discovery always empty, status precedence delegation, event validation

## Retained regressions

- **Historical Claude parser/detector**: `go test -race ./internal/agent` PASS
- **Accepted Codex adapter**: `go test -race ./internal/agent/adapters/codex/v0_144_1` PASS
- **T0 contract + harness**: `go test -race ./internal/agent/contract` PASS
- No files under Codex subtree or contract package modified

## Gate evidence

```text
gofmt -l claude adapter files                           clean
go build ./...                                          PASS
go vet ./...                                            PASS
agent (retained Claude):                                PASS
codex/v0_144_1 (accepted T1):                           PASS
contract (frozen T0):                                   PASS
claude/v2_1_202 (35 adapter-specific tests):            ALL PASS
claude/v2_1_202 (full harness):                        PASS (20/20)
mobile npm run typecheck                                PASS
mobile npm test                                         PASS
git diff --check                                        PASS
secret scan                                             PASS
scripts/build-gate.sh                                   ALL GATES PASSED
git merge-base --is-ancestor 162266f83 HEAD             exit 0
git merge-base --is-ancestor 3ce2604bd HEAD             exit 0
```

## Known limitations

1. **Approval evidence**: No structured Claude PermissionRequest evidence available. Adapter does not advertise CapApprovalDetection. When controlled evidence becomes available, a separate revision can add it behind SafeApprovalGate.
2. **Discovery**: No log file discovery. Correlation is unavailable.
3. **Single version**: Only 2.1.202 supported. Other versions fail closed.
4. **Full-prefix input policy**: Caller must re-supply all records from position 0.
5. **No managed launch**: Managed hook adapter behavior is deferred.

## Scope exclusions (unchanged)

R2, D1, T3, S1, A1, O1, managed hooks, product integration, Windows. R2 must NOT begin before independent T2 ACCEPT.

## Next steps

1. Independent verifier inspects actual Claude-native mappings, exact version gating, cursor/resource bounds, approval authority, secret redaction, and unchanged Codex behavior.
2. Verifier confirms T0 harness correction preserves Codex approval safety and Claude safe-default behavior.
3. Only after independent T2 ACCEPT may R2 begin.
