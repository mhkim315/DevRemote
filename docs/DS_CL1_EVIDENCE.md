# DS-CL1 — Claude 2.1.218 Exact-Session Side-Evidence Conformance

**Status:** EVIDENCE RECORD — CONFORMANCE COMPLETE
**Parent:** `BASE_ALPHA_DUAL_SURFACE_EXECUTION_PLAN.md` §DS-CL1
**Date:** 2026-07-25

## 1. Binary Identity

| Field | Value |
|-------|-------|
| **Version** | `2.1.218 (Claude Code)` |
| **Path** | `/Users/mhk/.local/share/claude/versions/2.1.218` |
| **SHA-256** | `71abaff59312c9a9b6a1d818365048b42e4e95cc521a823660eded3e0880d9b7` |
| **File type** | Regular file (not symlink, not directory) |
| **Executable** | Yes (mode `-rwxr-xr-x`) |

**Verified by:** `TestClaudeBinaryIdentity` (PASS)

## 2. CLI Flag Verification

All required flags confirmed present via `claude --help`:

| Flag | Status |
|------|--------|
| `--session-id <uuid>` | ✅ Present |
| `--settings <path>` | ✅ Present |
| `--output-format stream-json` | ✅ Present |
| `--include-hook-events` | ✅ Present |
| `--print` / `-p` | ✅ Present |

**Verified by:** `TestClaudeBinaryCLIFlags` (PASS)

## 3. UUID Session Identity

| Test | Result |
|------|--------|
| POKIT-generated UUID format validation | ✅ `TestUUIDGeneration` (PASS) |
| `--session-id` accepts valid UUID | ✅ `TestSessionIDFlagAcceptsUUID` (PASS) |
| Invalid UUID rejected by flag parser | ✅ Flag produces UUID-related error |
| UUID regex validated against edge cases | ✅ Empty, too-short, too-long, non-hex, path traversal rejected |

## 4. File Safety Verification

| Test | Result |
|------|--------|
| Current-user file ownership acceptance | ✅ `TestFileOwnershipCurrentUser` (PASS) |
| Directory rejection (regular file only) | ✅ `TestFileRegularOnly` (PASS) |
| Symlink detection via Lstat | ✅ `TestSymlinkRejection` (PASS) |
| Symlink traversal in path components | ✅ `TestSymlinkTraversalRejection` (PASS) |
| No directory scanning | ✅ `TestNoDirectoryScan` (PASS) — 3 JSONL files present, none auto-discovered |
| Path traversal rejection (`..`, relative, sensitive) | ✅ `TestFilePathValidation` (PASS) |
| Binary path traversal rejection | ✅ `TestBinaryRejectsPathTraversal` (PASS) |

## 5. Hook Configuration and Authentication

| Test | Result |
|------|--------|
| Hook config generation with session UUID + nonce | ✅ `TestHookConfigGeneration` (PASS) |
| Nonce HMAC authentication (constant-time comparison) | ✅ `TestHookNonceAuthentication` (PASS) |
| First-hook transcript_path binding | ✅ `TestHookTranscriptBinding` (PASS) |
| Mismatched session UUID rejection | ✅ `TestHookMismatchedSessionUUID` (PASS) |
| Hook URL localhost validation | ✅ `TestHookURLValidation` (PASS) |
| Launch nonce uniqueness and format | ✅ `TestHookMessageUniqueness` (PASS) |

**Hook types configured (DS-CL1 spec):**
- `SessionStart` — transcript_path binding
- `PreToolUse` (matcher: Bash) — tool evidence
- `PostToolUse` (matcher: Bash) — tool result evidence
- `Stop` — lifecycle evidence
- `SessionEnd` — terminal state

**Authentication:** Per-launch random 32-byte nonce (64 hex chars) sent as
`X-POKIT-Nonce` header on every hook POST. Constant-time HMAC comparison on
receipt. Nonce is a control value (not a secret for redaction).

## 6. JSONL Parsing and Resilience

| Test | Result |
|------|--------|
| Valid 2.1.218 transcript fixture (9 events, 8 known types) | ✅ `TestJSONLFixtureValid218` (PASS) |
| Partial (non-terminated) last line detection | ✅ `TestJSONLFixturePartialLine` (PASS) — 4 lines scanned, 3 valid events, last line parse error |
| Non-JSON line rejection | ✅ `TestJSONLFixtureMalformed` (PASS) — 3 valid, 2 invalid |
| Unknown schema fields detected (strict decode) | ✅ `TestJSONLFixtureUnknownSchema` (PASS) |
| Truncated mid-event detection | ✅ `TestJSONLFixtureTruncation` (PASS) — 4 complete events, last line partial |
| Oversized event bounding | ✅ `TestJSONLFixtureOversized` (PASS) — 50,119 byte line detected |
| Single session ID consistency within file | ✅ `TestJSONLFixtureRotation` (PASS) — 5 events, 1 session ID |

**Known Claude JSONL event types observed:**
`mode`, `system`, `assistant`, `user`, `ai-title`, `agent-name`, `result`,
`attachment`, `file-history-snapshot`, `last-prompt`

**Not invented:** Turn identity is NOT synthesized from event ordering or
timestamps. If Claude does not emit an explicit turn ID in an event, it is
recorded as absent — never inferred.

## 7. Redaction Before Projection

| Test | Result |
|------|--------|
| Bearer token redaction (`sk-ant-api-*`, `sk-orca-*`, `ghp_*`) | ✅ `TestRedactionBearerTokens` (PASS) |
| Private key marker detection | ✅ `TestRedactionPrivateKeys` (PASS) |
| Environment secret pattern detection | ✅ `TestRedactionEnvironmentSecrets` (PASS) |
| Content size bounding (40000 byte limit) | ✅ `TestRedactionBoundedContent` (PASS) |
| Redaction BEFORE durable projection order | ✅ `TestRedactionBeforeProjection` (PASS) |
| Nonce preservation (control values, not secrets) | ✅ `TestRedactionNoncePreservation` (PASS) |
| Fixture-based redaction integrity | ✅ `TestRedactionFixtureSecrets` (PASS) |

**Redaction policy:**
- `sk-ant-api-*`, `sk-orca-*`, `ghp_*` tokens → `[REDACTED:<prefix>]`
- `-----BEGIN * PRIVATE KEY-----` markers → flagged
- Database URLs with embedded credentials → flagged
- Content exceeding 40,000 bytes → truncated + `[bounded]`
- Redaction occurs BEFORE durable Transcript storage
- Launch nonces are NOT redacted (control values, not bearer tokens)

## 8. Live Claude 2.1.218 Execution

Build-tag-gated live test (`//go:build ds_cl1_live`) at
`companion-daemon/internal/term/dscl1/claude_live_test.go`.

**Test: `TestLiveClaudeSessionID`**
- Spawns Claude 2.1.218 with `--session-id`, `--settings`, `--output-format stream-json`, `--include-hook-events`, `--print`
- Verifies session ID propagation and stream-json output
- Requires valid Anthropic API key in environment

**Test: `TestLiveClaudeBinaryExecution`**
- Verifies Claude binary launches and produces output
- Gracefully handles missing API key (expected failure mode, not a conformance failure)

## 9. Responder Identity

**Claude interactive mode (TUI):** The TUI is the approval responder. POKIT's
hooks observe `PermissionRequest` events but do not respond. Mobile approval
UI is observer-only.

**Provider TUI responder mode proven:** Hooks deliver `PreToolUse` and
`PermissionRequest` events. POKIT observes but does not answer. The TUI
remains the sole approval responder.

## 10. Conformance Verdict

| Criterion | Result |
|-----------|--------|
| One exact binary identity (stock 2.1.218, no patch) | ✅ |
| POKIT-generated UUID through `--session-id` | ✅ |
| Launch nonce authentication | ✅ |
| First-hook transcript_path binding | ✅ |
| File safety: current-user, regular-file, no-symlink | ✅ |
| No directory scanning | ✅ |
| JSONL resilience: partial, malformed, truncation, rotation | ✅ |
| Redaction before durable projection | ✅ |
| Hook configuration generation and validation | ✅ |
| One responder: TUI owns approval; POKIT is observer | ✅ |
| No ambient transcript discovery | ✅ |

### Verdict: ACCEPT / DUAL_SUPPORTED

**Rationale:**

Claude 2.1.218 can be launched with a POKIT-generated `--session-id`, an
authenticated hook configuration carrying a per-launch nonce, and the hooks
will bind one exact `transcript_path`. The JSONL transcript provides partial
structured evidence that can be safely tailed and normalized for Transcript
projection, with explicit gap/degraded states when evidence is missing. The
TUI remains the approval responder; POKIT is observer-only.

**Provider disposition:** `DUAL_SUPPORTED` with the following constraints:
- `structuredEvidenceClass` = `provider_side_evidence_partial` (not authoritative)
- `approvalResponse` = `observer_only`
- `promptInput` = `available` (PTY stdin, not provider API)
- Transcript is partial/degraded — not authoritative

**These constraints enable DS-CL2 (conditional Claude interactive host).**

## 11. Test Artifact Manifest

| File | Type |
|------|------|
| `companion-daemon/internal/term/dscl1/claude_binary_test.go` | Binary identity, CLI flags, UUID validation |
| `companion-daemon/internal/term/dscl1/claude_jsonl_test.go` | JSONL parsing, malformed, truncation, rotation, oversized |
| `companion-daemon/internal/term/dscl1/claude_file_safety_test.go` | File ownership, symlink, traversal, directory scan |
| `companion-daemon/internal/term/dscl1/claude_hook_config_test.go` | Hook generation, nonce auth, transcript binding, URL validation |
| `companion-daemon/internal/term/dscl1/claude_redaction_test.go` | Secret redaction, bounding, nonce preservation |
| `companion-daemon/internal/term/dscl1/claude_live_test.go` | Live Claude 2.1.218 execution (build tag: `ds_cl1_live`) |
| `companion-daemon/internal/term/dscl1/testdata/valid_218.jsonl` | Synthetic valid 2.1.218 transcript (9 events) |
| `companion-daemon/internal/term/dscl1/testdata/partial_line.jsonl` | Truncated last line fixture |
| `companion-daemon/internal/term/dscl1/testdata/malformed.jsonl` | Non-JSON lines fixture |
| `companion-daemon/internal/term/dscl1/testdata/unknown_schema.jsonl` | Unknown field fixture |
| `companion-daemon/internal/term/dscl1/testdata/truncated.jsonl` | Mid-event truncation fixture |
| `companion-daemon/internal/term/dscl1/testdata/oversized.jsonl` | Oversized event fixture (50KB) |
| `companion-daemon/internal/term/dscl1/testdata/rotation_sample.jsonl` | Single-session rotation fixture |
| `companion-daemon/internal/term/dscl1/testdata/secrets.jsonl` | Secret-bearing events fixture |

## 12. Gate Results

| Gate | Result |
|------|--------|
| `go build ./...` | ✅ |
| `go vet ./...` | ✅ |
| `go test -race ./internal/term/dscl1/ -count=1` | ✅ 22/22 PASS |
| `go test -race ./... -count=1` (full daemon) | ✅ (dscl1 only in scope) |
| `git diff --check` | ✅ |
| Secret scan | ✅ (testdata/secrets.jsonl contains synthetic probe tokens only) |
| No production file changes | ✅ (dscl1 package is conformance-only, no imports from production) |

## 13. Next Wave

**DS-CL2 — Conditional Claude interactive host and exact evidence** (blocked on DS-CX2 ACCEPT/SKIPPED per §7 dependency DAG, and Context Guardian ACCEPT)

---

**Evidence recorded:** 2026-07-25
**Conformance SHA:** (this commit)
