# PB Device Gate — Final Evidence

**Date**: 2026-07-22  
**Device**: SM-S926N (Android 16, SDK 36)  
**Branch**: feature/canonical-timeline-foundation  
**Fix SHA**: `0f9f7056d`

## Root Cause

TERM-C1 control bridge `expectedSession` computed from `location.search`.
Paired-device HTML is loaded via `source={{html, baseUrl}}` which strips
the `?session=controlled_pty:shell-...` query parameter from the WebView URL.
The bridge fell back to `expectedSession = "devremote"`.

Daemon hello frame contained the REAL session ID (`controlled_pty:shell-...`).
Bridge rejected hello as `hello_identity` (session mismatch).
React Native FeedScreen never received the hello → `caps` remained `[]` →
`deviceCanInput = false` → `terminalInputEnabled = false` →
"View only — terminal input not authorized".

## Fix

**2 files, 12 lines**:

### `companion-daemon/internal/term/pty.go` (line 586)

Before:
```javascript
var expectedSession=(function(){var m=location.search.match(...);try{return m?decodeURIComponent(...):"devremote"}catch(_){return ""}})();
```

After:
```javascript
// TERM-C1-R3: paired-device HTML is loaded via source={{html,baseUrl}}
// which strips the original ?session= query parameter from location.search.
// The native TerminalController bootstrapper sets __pokitExpectedSession
// before the page loads so the bridge can match the exact daemon hello
// sessionId. Fall back to location.search for explicit_local_dev direct
// URI loads where the query IS present.
var expectedSession=(function(){
  if(typeof window.__pokitExpectedSession==="string"&&window.__pokitExpectedSession)
    return window.__pokitExpectedSession;
  var m=location.search.match(/(?:^|[?&])session=([^&]+)/);
  try{return m?decodeURIComponent(m[1].replace(/\+/g," ")):"devremote"}catch(_){return ""}
})();
```

### `mobile/src/lib/terminalController.ts` (line 69)

Before:
```typescript
const ticketScript = `<script>${ptySize}
(function(){
```

After:
```typescript
const ticketScript = `<script>${ptySize}
// TERM-C1-R3: paired-device HTML loses the ?session= query parameter
// when loaded via source={{html,baseUrl}}. Seed the exact session ID
// so the served-page control bridge can validate the daemon hello frame.
window.__pokitExpectedSession=${JSON.stringify(sessionId)};
(function(){
```

## Diagnostic Evidence (SM-S926N logcat)

```
[PB-DIAG] fetchSession: rowFound=true adapterCaps=[...,"input",...] lifecycleState=running
[PB-DIAG] inputGate: sessionCanInput=true deviceCanInput=false lifecycleState=running
```

After fix:
```
[PB-DIAG] inputGate: sessionCanInput=true deviceCanInput=true lifecycleState=running
```

## Device Gate Results

| # | Scenario | Result |
|---|----------|--------|
| 1 | Production QR pairing (WiFi LAN) | PASS |
| 2 | DeviceAuth challenge/verify | PASS |
| 3 | Android Keystore identity | PASS |
| 4 | HTTPS/WSS tunnel path | PASS |
| 5 | Owner role + terminal:input permission | PASS |
| 6 | Managed Shell session creation | PASS |
| 7 | Terminal WebView writable (no "view only") | PASS |
| 8 | `printf 'POKIT_DEVICE_OK'` → PTY execution | PASS |
| 9 | Ctrl+C → interrupt delivered | PASS |
| 10 | Paste → input delivered | PASS |
| 11 | Enter macro → input delivered | PASS |
| 12 | Codex ANSI output | PASS |
| 13 | Claude ANSI output | PASS |
| 14 | Scroll duplication | PASS (resolved) |

## Known Issues (pre-existing, not part of this fix)

| Issue | Status |
|-------|--------|
| Send button (RN path) — keyboard Enter works | KNOWN |
| Reconnect garbage characters in PTY | KNOWN |
| Transcript byte-stream suppressed after input | KNOWN |
| Keyboard-macro gap / terminal space | KNOWN |
| Geometry fixed (horizontal swipe design) | DESIGN |

## Commits

| SHA | Description |
|-----|-------------|
| `0f9f7056d` | TERM-C1-R3: inject __pokitExpectedSession for paired-device HTML |
