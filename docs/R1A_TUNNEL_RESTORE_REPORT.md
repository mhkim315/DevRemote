# R1a Tunnel Restore Report

Date: 2026-07-09 (KST, session timestamp ~07:37)

## Symptom

```
$ curl https://term.fullcount.kr/api/sessions
HTTP 530
error code: 1033
```

Cloudflare Error 1033: Argo Tunnel is not connected to an origin.

## Root Cause

**cloudflared process was not running.**

The tunnel daemon (`cloudflared`) had been started as a foreground process in a
terminal session at some point. When that terminal session ended (or the Mac
was restarted), the cloudflared process terminated and was never restarted.

There was no LaunchAgent installed to keep cloudflared running persistently.

## Resolution

Restarted cloudflared manually:

```
$ /Users/mhk/.gemini/antigravity/scratch/DevRemote/devremote/cloudflared \
    tunnel --config /Users/mhk/.cloudflared/config.yml \
    run cba53b5e-9d53-4b8a-9a71-341d5b10801f
```

Tunnel registered 4 QUIC connections (icn01, hkg09 x2, icn05).
All connectivity pre-checks passed.

## Verification

| Endpoint | Before | After |
|----------|--------|-------|
| `localhost:9171/api/sessions` | 200 (daemon OK) | 200 (daemon OK) |
| `https://term.fullcount.kr/api/sessions` | 530 Error 1033 | **200, 6 sessions** |
| `https://term.fullcount.kr/term/?session=tmux:ai` | 530 Error 1033 | **200 (HTML served)** |
| `https://term.fullcount.kr/api/sessions?activity=cmux:surface:2` | 530 | **200, 1 event** |

## Tunnel Configuration

```
tunnel: cba53b5e-9d53-4b8a-9a71-341d5b10801f
credentials-file: /Users/mhk/.cloudflared/cba53b5e-9d53-4b8a-9a71-341d5b10801f.json

ingress:
  - hostname: term.fullcount.kr
    service: http://127.0.0.1:9171
  - service: http_status:404
```

- Binary: `/Users/mhk/.gemini/antigravity/scratch/DevRemote/devremote/cloudflared`
- Version: 2026.6.1
- Config: `/Users/mhk/.cloudflared/config.yml`
- Protocol: QUIC (auto-negotiated)

## Recommendation

Install a LaunchAgent so cloudflared starts automatically on boot and
restarts if it crashes.

```xml
<!-- ~/Library/LaunchAgents/com.cloudflare.cloudflared.plist -->
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.cloudflare.cloudflared</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/mhk/.gemini/antigravity/scratch/DevRemote/devremote/cloudflared</string>
        <string>tunnel</string>
        <string>--config</string>
        <string>/Users/mhk/.cloudflared/config.yml</string>
        <string>run</string>
        <string>cba53b5e-9d53-4b8a-9a71-341d5b10801f</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/cloudflared.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/cloudflared.err</string>
</dict>
</plist>
```

To install:
```
cp the-plist ~/Library/LaunchAgents/com.cloudflare.cloudflared.plist
launchctl load ~/Library/LaunchAgents/com.cloudflare.cloudflared.plist
```

## Current Status

- Tunnel: **RUNNING** (PID 78903, foreground)
- Daemon: **RUNNING** (PID 80908, port 9171)
- term.fullcount.kr: **REACHABLE** (HTTP 200 on all endpoints)

⚠️ The tunnel is currently running in the foreground. If this terminal session
ends, the tunnel will stop again. Install the LaunchAgent above for persistence.
