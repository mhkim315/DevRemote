# BF-4 — Deferred Safety Decision Report

**Date:** 2026-07-25
**Author:** T2 (DeepSeek)
**SHA:** `43ee32f4c` (analysis baseline)

## Item #8 — Dual Input Paths (xterm.js + POKIT Prompt)

### Classification: **ACCEPT / SAFE_TO_DEFER**

### Current State

Two input paths exist for `controlled_pty:*` sessions:

| Path | Source | Transport |
|---|---|---|
| xterm.js keyboard | `term.onData` → `pokitSendInput` | WebSocket BINARY frame → binary path → `WriteInput` |
| POKIT command bar | `doSend` / `submitLine` | WebSocket TEXT frame (`terminal_input` v1) → `handleTerminalInput` → `WriteInput` |
| Mobile macros (Ctrl+C, etc.) | `sendMacro` | Same as command bar |
| Native keyboard (soft KB Enter) | `handleChangeText` → `submitLine` | Same as command bar |

Both paths converge on `TerminalTransport.WriteInput()`, which calls `ClaimInput()` before every write. Single-writer ownership is enforced at this boundary:

```go
// TerminalTransport.WriteInput (terminal_transport.go:273)
owner, claimed := t.ClaimInput(deviceID, "")
if !claimed {
    return 0, fmt.Errorf("%w: input owned by %s", ErrInputNotOwner, owner.DeviceID)
}
```

### Verification

- **Single writer**: `ClaimInput` uses mutex-guarded CAS at the transport layer. First write wins; non-owner gets `ErrInputNotOwner`.
- **Same device, same connection**: The `deviceID` and `connID` bind the claim to the exact WebSocket connection.
- **Mobile awareness**: The hello frame includes `inputOwner` with `isSelf`; the mobile UI shows read-only bar when not owner.
- **Race safety**: 20-goroutine concurrent test (DS-ARB4) proves exactly one winner.

### Decision

**SAFE TO DEFER**. Single-writer authority is proven at the transport layer for all input paths. No additional implementation needed.

---

## Item #10 — Arbitration UX (Visual Polish)

### Classification: **ACCEPT / SAFE_TO_DEFER**

### Current State

| UX Element | Status |
|---|---|
| Ownership indicator (`inputOwner` in hello frame) | Implemented (DS-ARB4) |
| Mobile "Input: You" banner | Implemented (R2) |
| Read-only bar (non-owner) | Implemented (R2) |
| `delivery_unknown` status on ACK loss | Implemented (Input-B) |
| Interrupt endpoint (`POST .../interrupt`) | Implemented (DS-ARB4) |
| Ownership transfer via `claim-input` | Implemented (R2) |
| Stale generation rejection | Implemented (PA4.3) |

### Verification

- **Internal correctness**: Race-condition tests pass (20 concurrent claimants, 1 winner). Stale generation writes return 0,nil (fail-closed). Expired ownership allows re-claim.
- **Mobile routing**: DS-UI3 capability-driven routing replaces prefix-based. Capability fields (`terminalSurface`, `transcriptSurface`) are server-authoritative.
- **Claude approval**: DS-CLA2 bridge handles PreToolUse/PermissionRequest with CAS first-response-wins.

### Deferrable Visual Polish

| Item | Reason to defer |
|---|---|
| Ownership transfer confirmation dialog | Internal correctness proven; dialog is cosmetic |
| Animated ownership transition | Purely visual |
| "Input owned by device-XXXX" display | Current shows generic message; device ID is available in hello frame but not yet shown |
| Transfer timeout countdown | Ownership already expires after 30s; visual countdown is polish |

### Decision

**SAFE TO DEFER**. Internal arbitration correctness is proven by DS-ARB4 tests. Visual polish (transfer confirmations, animations, device ID display) can be added in a future UI refinement packet without changing any authority contract.

---

## Item #11 — Delete Lifecycle Cleanup

### Classification: **PARTIALLY DEFERRED — Bounded Fix Required**

### Current State

| Resource | Cleanup on Delete | Status |
|---|---|---|
| PTY process | `e.handle.Kill()` + `proc.Wait()` in `terminate()` | ✅ Handled |
| Recorder | `recorder.Stop()` in `Delete()` | ✅ Handled |
| Transcript | `ClearTranscript()` in `Delete()` | ✅ Handled |
| TerminalTransport | `transport.Retire()` on generation replacement | ✅ Handled |
| Hook directory (Claude interactive) | `os.RemoveAll(hookDir)` on CREATE FAILURE only | ❌ **GAP** |
| Bridge HTTP server (Claude interactive) | `bridge.stop()` on CREATE FAILURE only | ❌ **GAP** |
| JSONL normalizer (Claude interactive) | `normalizer.Stop()` never called on delete | ❌ **GAP** |
| Session list (CatalogEntry) | `delete(o.entries, id)` in `Delete()` | ✅ Handled |
| IPC connections | `conn.Close()` on handler return | ✅ Handled |

### Gap Analysis

The `ClaudeInteractiveHost` and `CodexTUIHost` create runtime resources (bridge, normalizer, hookDir) that are NOT cleaned up when the session is deleted via the lifecycle service. The create-failure path cleans them up via explicit rollback, but the normal delete path does not.

**Impact:**
- Bridge HTTP server listener leaks (port remains open until daemon restart)
- JSONL normalizer goroutine leaks (continues tailing a deleted session's file)
- Hook directory remains on disk (temp dir cleanup on OS reboot only)

**Severity:** Medium — resource leak, not a security vulnerability. Does not affect other sessions.

### Proposed Bounded Fix Packet

**Scope:** `claude_interactive_host.go`, `codex_tui_host.go`, `owned_pty_runtime.go`

**Changes:**
1. Add `Close()` method to `ClaudeInteractiveHost` that stops all bridges + normalizers + removes hook dirs
2. Add `Close()` method to `CodexTUIHost` that stops all tailers
3. Call `Close()` from `OwnedPTYRuntime.Delete()` for sessions with those hosts

**Estimated size:** ~30 lines of Go, plus tests.

**Not deferrable because:** Resource leaks accumulate over daemon lifetime. In a long-running daemon with many session create/delete cycles, leaked listeners and goroutines will exhaust ports and memory.

**Deferrable:** List UX for deleted sessions (showing "deleted" vs removing from list immediately) is cosmetic and can be deferred.

### Decision

**Process/PTY/socket/proxy/hook cleanup: NOT DEFERRABLE**. A bounded fix packet is required for Claude interactive and Codex TUI host cleanup on session delete. List UX display of deleted sessions: **SAFE TO DEFER**.

---

## Summary

| Item | Classification | Action |
|---|---|---|
| #8 Dual input paths | ACCEPT/SAFE_TO_DEFER | No action; single-writer authority proven |
| #10 Arbitration UX | ACCEPT/SAFE_TO_DEFER | No action; internal correctness proven, visual polish deferred |
| #11 Delete lifecycle | PARTIALLY DEFERRED | Bounded fix packet for hook/bridge/normalizer cleanup; list UX deferred |
