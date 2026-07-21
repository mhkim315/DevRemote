# TERM-C1 — Single control bridge evidence

Implementation commit: `262d38b8884448c132169bab97318f12fd2083a8`

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

- `feedScreenInput.test.ts` now contains the single composed mobile test:
  `runs the real daemon → served page → WebView → FeedScreen acknowledgement
  chain`. Jest starts an isolated loopback `devremote daemon` process, creates
  a daemon-owned shell session, fetches production `HandleHTML`, and executes
  its literal inline script in the WebView VM.
- The test's WebSocket is a real Node `ws` connection to that daemon's
  production `HandleWS`. Its only browser adaptation converts Node text and
  binary events into the WebSocket event shape a WebView supplies; it neither
  parses nor creates terminal control frames. The actual page
  `pokitSendInput` sends both line frames, real daemon ACKs re-enter the
  page's `onmessage`, and its actual `ReactNativeWebView.postMessage` is passed
  to the rendered production FeedScreen `onMessage` callback.
- The test invokes the actual FeedScreen Send control and asserts exactly two
  native `input_pending` plus two `input_result` messages, then asserts the
  rendered status is **Delivered to terminal**. This is the required single
  WebSocket → daemon page → WebView → React Native composition, rather than a
  test-only Go delivery model.
- The explicit `--insecure-local-only` daemon snapshot now advertises
  `terminal:input` for its existing dev-token input path. Remote/no-ticket
  connections remain fail-closed; the change makes local server permission
  announcement and actual input enforcement agree.
- When `--insecure-local-only` is set, a supplied `--listen-addr` must parse
  as an IP loopback address. Wildcard, LAN, and hostname addresses are rejected
  during application construction before the HTTP server can bind them.
- The composed mobile test starts the daemon on `127.0.0.1:0`, parses the
  pre-bound address from the daemon's startup log, and installs that exact
  dynamic `location.protocol` and `location.host` in the executed served page.
  Consequently the page's unmodified bridge opens its WebSocket to the same
  listener that served its HTML; no `9171` test endpoint remains.
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
- The Goja integration retains coverage for every macro, direct Ctrl+C,
  zero-PTY-write denial, and duplicate/reconnect boundary. The mobile test is
  the production FeedScreen proof for the two-ACK delivery result.
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
- `feedScreenInput.test.ts` also retains the component-level duplicate hello
  and two-ACK state-transition coverage.
- `terminalController.test.ts` proves bootstrap geometry invokes the control
  bridge rather than calling `term.resize` directly.

## Verification run against the implementation commit

All passed:

- `npm test -- --runInBand --silent feedScreenInput.test.ts` — 1 suite,
  16 tests
- `npm run typecheck`
- `npm test -- --ci --runInBand --silent` — 35 suites, 519 tests
- `go test ./internal/term -run 'TestTERM_C1_ServedPageGoja(Real|Pending)' -count=1`
- `go test ./internal/term`
- `go vet ./...`
- `go run scripts/archgate.go`

Static diff scan found zero new `mux.Registry` or `GetRecorder` references.
