# E8 Instrumentation Report

Date: 2026-07-08
Commit: 3ca91d16f

## Capture path

```
Terminal WebView setInterval (5s)
→ POST /debug/e8diag?connectCount=...&closeCount=...&msgCount=...
→ daemon HandleE8Diag → log.Printf("E8DIAG ...")
→ daemon stdout (or /tmp/daemon.log)
```

## How to capture

```sh
# Start daemon with log capture
devremote daemon --insecure-local-only --enable-localpty --enable-agent-detection 2>&1 | grep E8DIAG

# Or from log file
tail -f /tmp/daemon.log | grep E8DIAG
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

## Instrumentation code (verified)

```
pty.go:325  onopen     → e8diag.connectCount++
pty.go:336  onmessage  → e8diag.msgCount++, totalBytes+=, lastMsgSize=, rawLen=
pty.go:348  onclose    → e8diag.closeCount++, wasReconnect=true
pty.go:400  setInterval → POST /debug/e8diag with all counters
pty.go:438  HandleE8Diag → POST-only, auth-gated, input-length-limited → log.Printf
```

## Security

- `/debug/e8diag` behind AuthMiddleware
- POST-only
- Digits-only counters (safeNum) + bool-only wasReconnect (safeBool), length-limited to 20 chars
- No raw body/string logging

## Status

- [x] instrumentation code complete
- [x] daemon capture path working
- [x] POST-only + auth + input validation
- [x] build gate ALL 8 PASSED
- [ ] runtime evidence (requires daemon restart with new binary + active Terminal session)
- [ ] terminal duplication root cause identified
- [ ] terminal duplication fix implemented
