# TERM-C1 — Single control bridge evidence

Implementation commit: `6e598ec386904b22b4f4c213b1f748807eae42c5`

This remediation supersedes the rejected evidence associated with
`c5cb19342fccb1162c5158c8e39c28ccc2343111`.

Scope: TERM-C1 from the device-gate terminal remediation plan. This evidence
does not constitute independent TERM-C1 acceptance or device-gate approval.

## Implemented authority boundary

- The served daemon terminal page now has one `__pokitControlBridge` per active
  WebSocket. It owns the `hello` capability snapshot, effective read-only/input
  state, connection/session/generation identity, and terminal geometry.
- The injected mobile ticket wrapper only binds the exact raw socket. It no
  longer parses or forwards control frames, removing duplicate delivery beside
  the daemon page handler.
- Direct xterm keyboard input, native Send/paste/macros, and generated
  `input_pending` notifications all route through the bridge's acknowledged
  `terminal_input` sender. No production raw-binary fallback was added.
- Bootstrap and live geometry both enter through the bridge. Viewport fitting
  no longer changes terminal rows or columns.
- A repeated identical `hello` leaves pending Input-B acknowledgements intact;
  a genuine replacement socket still marks unresolved delivery as unknown.
- A second connection/session/generation identity on the same WebSocket
  revokes the bridge and native read-only state. Only `bind()` for a
  replacement socket may establish a new identity.

## Focused assertions

- `TestTERM_C1_ServedPageGojaRealWebSocketToNativeDelivery` obtains the
  literal production `HandleHTML` script and executes it in Goja. Its
  `WebSocket.send` writes to a ticketed real gorilla connection targeting
  production `HandleWS`; TEXT controls read from that same connection are
  passed into the page's real `ws.onmessage`, then its real
  `ReactNativeWebView.postMessage`. No Go helper constructs `terminal_input`
  frames or fabricates input ACKs in this test.
- That one chain covers paste, every macro (including Ctrl+C), and two-frame
  text+Enter. Every positive surface produces a real `HandleWS` `WriteInput`
  and an actual ACK delivered back through the served page. A direct keyboard
  Ctrl+C test separately proves the literal `term.onData` path sends exactly
  byte `0x03` through the same live chain.
- The native message receiver used by this integration test applies the same
  Input-B delivery boundary as FeedScreen: it reports **Delivered to terminal**
  only after both real text and Enter ACKs. The production FeedScreen Jest test
  independently executes the component and verifies that same two-ACK rule.
- A viewer's real `HandleWS` hello omits `terminal:input`; the served page then
  denies every macro with zero `terminal_input` frames and zero PTY writes.
  Existing real-WS negative tests additionally cover wrong session/generation
  and assert zero `WriteInput` calls.
- Invalid/missing/unknown capability hellos, malformed controls, and wrong
  session/connection/generation identities fail closed: direct keyboard and
  native page sends emit zero `terminal_input` frames (and therefore zero
  terminal writes). Wrong-identity results never reach the native bridge.
- The duplicate/reconnect integration starts with an actual pending input. It
  replays the literal received hello before the real ACK is pumped and proves
  the page forwards it once while retaining pending state. It then sends a
  second input, waits until real `WriteInput` completes but deliberately does
  not pump its ACK, closes the gorilla peer, runs the served page's actual
  reconnect path, and proves the replacement `HandleWS` hello changes the
  native result to **Possible partial delivery**.
- `input_b_ack_test.go` exercises production `HandleWS` and its terminal
  writer: an exact authorized input reaches `WriteInput` and produces the
  acknowledged result, while rejected controls make zero writes. Together
  with the Goja test, this proves the composed WebSocket → served daemon page
  → WebView native-message contract.
- `feedScreenInput.test.ts` proves the React Native FeedScreen reports
  Delivered only after both text and Enter frames are accepted, and that a
  duplicate hello cannot clear a pending acknowledgement.
- `terminalController.test.ts` proves bootstrap geometry invokes the control
  bridge rather than calling `term.resize` directly.

## Verification run against the implementation commit

All passed:

- `npm test -- --runInBand terminalGeneratedScript.test.ts feedScreenInput.test.ts terminalController.test.ts`
- `npm run typecheck`
- `npm test -- --runInBand` — 35 suites, 518 tests
- `go test ./internal/term -run 'TestTERM_C1_ServedPageGoja(Real|Pending)' -count=1`
- `go test ./internal/term`
- `go vet ./...`
- `go run scripts/archgate.go`
- `sh scripts/build-gate.sh` — build, vet, race tests, mobile typecheck/tests,
  Android Kotlin compile, invariant and secret scans

Static diff scan found zero new `mux.Registry` or `GetRecorder` references.
