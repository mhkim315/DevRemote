# Input-A Evidence — Effective Permission / Read-Only UX

**Input-A IMPL SHA:** `c2109cdf7`
**PA4 ACCEPT SHA:** `74560edd`
**PB Ancestry Baseline SHA:** `abe4df1d6`

## Ancestry

```
$ git merge-base --is-ancestor 74560edd8 HEAD && echo "PA4 ACCEPT: ANCESTOR OK"
$ git merge-base --is-ancestor abe4df1d6 HEAD && echo "PB BASELINE: ANCESTOR OK"
```

## Diff Scope

```
companion-daemon/internal/term/pty.go   |  44 +++
mobile/src/screens/FeedScreen.tsx       |  18 ++
 2 files changed, 47 insertions(+), 3 deletions(-)
```

Production: daemon WS handler + terminal HTML + mobile UX. No other packages.

## Gate Results

```
go build ./...                                    exit 0
go vet ./...                                      exit 0
go test -race ./... -count=1                       ALL PASS (11 packages)
cd mobile && npx tsc --noEmit                      clean
cd mobile && npx jest --runInBand                  452/452 pass, 34 suites
```

## Requirement Trace

| §5 Requirement | Implementation | Status |
|---|---|---|
| Server must return permission state | `readOnlyDenialPayload` JSON sent as TextMessage when `!hasTicketPerm(..., PermTerminalInput)` | IMPL |
| Mobile must query effective permissions before enabling input | `readCapabilities()` in lifecycle.ts already gates on `adapterCapabilities.includes('input')` | EXISTING |
| If terminal:input missing: read-only indicator, disable input | `readOnlyBar` UI shown when `!actionPolicy.inputEnabled` | IMPL |
| Server must NOT silently discard | Coalesced `read_only` denial frame (max 1/sec) sent to client | IMPL |
| WebSocket "success" must mean server acknowledged | `read_only` denial sets `sendStatus='failed'` on client | IMPL |
| Zero WriteInput calls on denied input | `continue` after denial — never reaches `transport.WriteInput` | IMPL |
| Zero Transcript mutation on denied input | `continue` before `BeginInput` — no transcript suppression triggered | IMPL |
| Denial coalescing cannot amplify traffic | `time.Since(lastDenial) > time.Second` gate | IMPL |
| Existing binary wire and 40ms split remain compatible | No framing changes — only a new TextMessage control type | IMPL |

## Implementation Details

### Daemon (pty.go)

- `readOnlyDenialPayload`: JSON `{"type":"read_only","reason":"..."}`
- Sent as TextMessage in writer goroutine (non-blocking select)
- Coalesced at max 1 per second per connection
- Terminal HTML JS: `ws.onmessage` now forwards text control frames
  (type `read_only`) to React Native via `postMessage`

### Mobile (FeedScreen.tsx)

- `readOnlyReason` state + `onMessage` handler for `read_only` events
- Persistent read-only bar when `actionPolicy.inputEnabled === false`
- Transient denial banner when server sends `read_only` mid-session
- Send status set to `failed` on denial receipt

## No-Policy-Expansion Proof

- Owner-only input policy unchanged
- No member auto-promotion or permission-array shortcut
- Unknown/expired/mismatched permissions fail closed to read-only
- Local storage / QR role / UI labels cannot grant input permission
