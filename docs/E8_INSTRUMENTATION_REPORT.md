# E8 Instrumentation Report

Date: 2026-07-08
Commit: fe7b021ef

## Capture path

```
Terminal WebView setInterval (5s)
→ POST /debug/e8diag?connectCount=...&closeCount=...&msgCount=...
→ daemon HandleE8Diag → log.Printf("E8DIAG ...")
→ daemon stdout (or /tmp/daemon.log)
```

## How to capture

```sh
devremote daemon --insecure-local-only > /tmp/daemon.log 2>&1 &
grep E8DIAG /tmp/daemon.log
```

## Counter interpretation

| Counter | Meaning | Duplication signal |
|---------|---------|-------------------|
| connectCount | WebSocket opens | increases → reconnect happening |
| closeCount | WebSocket closes | increases → connection drops |
| msgCount | term.write calls | increases without backend output → replay |
| totalBytes | bytes written to terminal | increases without new data → duplication |
| lastMsgSize | size of last message | large spike → history replay |
| rawLen | cumulative raw buffer | grows faster than msgCount → large payloads |
| wasReconnect | last open was a reconnect | true → reconnect happened |

## Security

- `/debug/e8diag` behind AuthMiddleware
- POST-only
- Digits-only counters (safeNum) + bool-only wasReconnect (safeBool), length-limited to 20 chars

## Status

- [x] instrumentation code complete
- [x] daemon capture path working
- [x] POST-only + auth + input validation
- [x] build gate ALL 8 PASSED
- [x] desktop browser evidence collected
- [ ] mobile WebView evidence (connectivity blocked)
- [ ] terminal duplication root cause confirmed
- [ ] terminal duplication fix accepted

## Runtime evidence

Source: `/tmp/daemon5.log`, 2026-07-08 17:41 KST
Method: Desktop browser, `http://127.0.0.1:9171/term/?session=tmux:ai`
Note: Mobile WebView evidence could not be collected due to emulator connectivity issues.

### Scenario 1: Initial terminal open

```
E8DIAG connectCount=1 closeCount=0 msgCount=9 totalBytes=3926 lastMsgSize=147 rawLen=3926 wasReconnect=false
```

WebSocket connected. 9 messages, ~4KB of data. First connection (wasReconnect=false).

### Scenario 2: Scroll (xterm.js internal)

Before scroll:
```
E8DIAG connectCount=1 closeCount=0 msgCount=9 totalBytes=3926 lastMsgSize=147 rawLen=3926 wasReconnect=false
```

After scroll (5 samples over 25s):
```
E8DIAG connectCount=1 closeCount=0 msgCount=9 totalBytes=3926 lastMsgSize=147 rawLen=3926 wasReconnect=false
E8DIAG connectCount=1 closeCount=0 msgCount=9 totalBytes=3926 lastMsgSize=147 rawLen=3926 wasReconnect=false
E8DIAG connectCount=1 closeCount=0 msgCount=9 totalBytes=3926 lastMsgSize=147 rawLen=3926 wasReconnect=false
E8DIAG connectCount=1 closeCount=0 msgCount=9 totalBytes=3926 lastMsgSize=147 rawLen=3926 wasReconnect=false
E8DIAG connectCount=1 closeCount=0 msgCount=9 totalBytes=3926 lastMsgSize=147 rawLen=3926 wasReconnect=false
```

**Observation**: All counters unchanged during scroll.
**Conclusion**: xterm.js scrolling is purely internal — it does not trigger WebSocket events, data replay, or backend resend. Scroll itself is NOT a duplication source.

### Scenario 3: Browser refresh (Cmd+R)

After refresh (new page load):
```
E8DIAG connectCount=1 closeCount=0 msgCount=9 totalBytes=3926 lastMsgSize=147 rawLen=3926 wasReconnect=false
```

Counters reset with new page. Fresh WebSocket connection, fresh data. wasReconnect=false on initial page load.

### Desktop vs Mobile

- Desktop browser: E8DIAG capture works. Scroll does not trigger reconnect.
- Mobile WebView: E8DIAG capture NOT confirmed (emulator connectivity blocked).
- Desktop browser does NOT reproduce duplication during scroll — this does not rule out mobile-specific duplication from WebView remount on tab switch.

## Root cause analysis (working hypothesis)

**Likely cause: WebView remount or WebSocket reconnect.**

When the Terminal WebView remounts (tab switch, navigation), it re-fetches the HTML page and establishes a new WebSocket connection. The daemon replays the PTY buffer, and xterm.js appends it on top of existing terminal content, causing duplication.

xterm.js internal scrolling is confirmed NOT a duplication source (Scenario 2).

## Candidate fix (not yet accepted)

`term.clear()` on reconnect (pty.go:326):
```javascript
if(wasReconnect){ term.clear(); wasReconnect=false; }
```

This clears the display before PTY replay on reconnect, preventing duplication. Needs mobile WebView evidence to confirm.
