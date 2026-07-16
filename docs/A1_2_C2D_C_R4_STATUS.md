# C2D-C R4 Remediation Status Report

SHA: `cdea8bd30059d72c2e4bcf1b33979318a3147f75`
Date: 2026-07-17

## Current Status: BLOCKED — R5 remediation incomplete

### Production Changes Made (R3-A/B/C)
- Claim-owned terminal result (TerminalResult channel)
- Canonical response bytes (claudeHookResponseBytes)
- Immutable resume context (resumeContext)
- PostToolUse handler in hook bridge
- ResumeForApproval spawn method

### Verified Working
- `TestClaudeDelivery_CompositionAllowAccepted`: PASS — real hook HTTP exchange, PostToolUse witness, accepted receipt, Store commit
- `TestClaudeDelivery_DenyPostToolUseCannotCommit`: PASS — PostToolUse after deny correctly fails
- `TestClaudeDelivery_EarliestHookAfterSpawn`: PASS
- Term tests ×5: PASS

### Remaining Blockers

1. **Deny witness routing not working**: `TestClaudeDelivery_CompositionDenyAccepted` fails with "got conflict" after 5s timeout. The deny JSON written to stdout is not processed by the pump because:
   - Root cause: `ResumeForApproval` creates the runtime without setting `rt.sessionID`. The pump starts but the scanner reads from a pipe whose writer end may not be properly connected (pipe connection issue in `compFakeLauncher`). 
   - The test currently uses a pipe write that the pump cannot read from.
   
2. **Deferred lifecycle tests hanging**: `TestClaudeDelivery_DeferredExitThenStopClearsIdentity` and `TestClaudeDelivery_DeferredExitThenDeleteClearsIdentity` hang — writing to stdout pipe for the deferred event causes deadlock because the pump goroutine cannot process it.

3. **SimulateGracefulExit removed but test still references old patterns**: The test infrastructure needs a deterministic stdout reader (e.g., pre-loaded buffer or channel-backed reader) instead of timing-sensitive io.Pipe coordination.

4. **Resume cwd hardcoded**: `ResumeForApproval` uses `/tmp` instead of original cwd.

### Recommended Fix Approach
1. Fix `ResumeForApproval` to set `rt.sessionID` and `rt.cwd` on the runtime
2. Replace `io.Pipe` in tests with a deterministic `bytes.Buffer` or `strings.Reader` backed stdout
3. Write deny test to directly call `coordinator.MarkWitnessedByToolUse()` after hook fires
4. Restore deferred lifecycle tests with proper deferred event injection

### Gate Status
```
internal/term focused ×5: PASS
cmd/devremote composition (allow only): PASS
cmd/devremote composition (deny): FAIL
cmd/devremote composition (deferred lifecycle): HANG
```
