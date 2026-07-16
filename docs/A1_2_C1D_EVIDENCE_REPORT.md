# A1.2 C1D — Managed Claude Observation Evidence Report

Status: **C1D IMPLEMENTED — AWAITING ACCEPTANCE**
Implementation SHA: TBD (pending final commit)
Accepted C0D evidence: `e42d4c570e64462ce813861017cc635e338e68bf`
Accepted SP1/Codex baseline: `2b940a6fce6e878ffa0da17b5df4d39438af144d`

## 1. Production entry

The `pokit run claude` CLI path is routed through the local 0600 Unix socket
(`StartIPCServer`) to `ManagedClaudeService.CreateDetached`. The feature is
gated behind `--enable-managed-claude` and `--claude-digest`. Without the
digest, attestation fails closed.

### IPC composition test

`TestClaudeIPCComposition` — `handleIPCConnection` with `ManagedClaudeService`
injected. Proves the JSON request `{"operation":"create","profileId":"claude"}`
routes correctly and returns `claude_headless:<id>` with `state=running`.

`TestClaudeIPCUnavailable` — proves IPC fails closed when `managedClaude` is
nil (feature disabled).

### Full integration test

`TestClaudeStartIPCServerIntegration` — conditionally skipped, requires
`POKIT_CLAUDE_DIGEST` and `POKIT_CLAUDE_CWD` env vars. Uses real
`StartIPCServer` + Unix socket + production attestor. Asserts exact 1
non-actionable approval with zero options.

## 2. C0D-certified launch

The child is launched with:

```
--verbose --settings <isolated> --setting-sources "" --output-format stream-json --include-partial-messages -p <prompt>
```

`TestClaudeLaunchArgv` verifies every required flag is present in the captured
argv.

## 3. Hook bridge (C0D-certified response format)

`TestClaudeHookBridgeDecodeValid` — proves strict allowlist decode accepts all
10 C0D-known fields and validates `hook_event_name == "PreToolUse"`.

`TestClaudeHookBridgeDecodeUnknownField/DuplicateKey/TrailingContent` — prove
malformed input is rejected.

`TestClaudeHookSettingsSchema` — golden test verifies the generated settings
file matches the C0D-certified schema:
`{"hooks":{"PreToolUse":[{"matcher":"","hooks":[{"type":"command","command":"..."}]}]}}`

## 4. Exact deferred join

`TestClaudeDeferredJoinFullIdentityMatch` — full 4-field comparison (session_id
+ tool_use_id + tool_name + input_sha256) using canonical JSON digest.

`TestClaudeDeferredJoinMismatchedSessionID/ToolName/InputDigest` — each proves
a single-field mismatch prevents join.

`TestClaudeStreamDeferredParsing` — proves the C0D nested structure
`deferred_tool_use{id, name, input}` is correctly parsed.

## 5. Lifecycle and cleanup

`TestClaudeExitClearsPendingObservations` — pending cleared on terminate.
`TestClaudeStopIdempotent` — double Stop succeeds.
`TestClaudeKillCleanup` — Kill marks exited.
`TestClaudeDeleteTerminalOnly` — non-terminal delete rejected, terminal succeeds.
`TestClaudePumpEOFExit` — pump observes EOF, exits, cleanup runs.
`TestClaudeShutdownCleansUp` — runtime map emptied on shutdown.

## 6. Race protection

`TestClaudeStopJoinRace` — **first-approval race**: terminate before any Store
session exists. Pre-ingest `terminated` check rejects (no primer needed).

`TestClaudeReverseRace` — **post-ingest race**: ingest succeeds, then
terminate. Post-ingest `terminated` check invalidates the just-ingested record.

Store high-water (`SupersedeRuntime(epoch, 1)`) protects subsequent approvals
where the Store session already exists.

## 7. Non-actionable proof

`TestClaudeApprovalRecordNonActionable` — ingested records have zero options
and zero delivery material.

`TestClaudeGenApprovalTokenIsOpaque` — provider tool_use_id never used as
ApprovalID.

## 8. Capacity and bounds

`TestClaudeDuplicateToolUseID` — second observation rejected.
`TestClaudeCapacityExhaustion` — pending limit enforced.
`TestClaudeActiveApprovalCapacity` — active approval limit enforced.

## 9. Timeout

`TestClaudeTimeoutExpiry` — injectable clock proves staggered expiry with
correct Store state (first invalidated, second still pending, then second
invalidated).

## 10. Entropy and attestation

`TestClaudeAttestorDigestMismatch` — non-vacuous: versionRunner injection
bypasses version check, proves digest mismatch is caught.
`TestClaudeAttestorDigestMissing` — empty digest fails closed.
`TestClaudeAttestorNoPinnedPath` — empty path fails closed.
`TestClaudeEntropyFailure` — injectable failing reader proves zero ingest on
entropy failure.

## 11. DTO privacy

`TestClaudeApprovalRecordNonActionable` — stored records carry no raw payload,
no prompt text, no hook capability, no paths.

## Gate

- `go build/vet/test -race ./...`: PASS (45 tests, 2 skipped)
- `git diff --check`: PASS
- Secret scan: PASS
- C0D/SP1 ancestry: verified

## Non-goals (C1D scope)

- C2D/C3D: NOT implemented
- Process-image attestation: deferred to C3D
- macOS code-signing, Windows, generic SDK: NOT in scope
- Production actionability: ZERO (no options, no ClaimForExecution, no CTA)
