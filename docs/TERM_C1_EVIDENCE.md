# TERM-C1 — Single control bridge evidence

Implementation commit: `473ba2f89a798fe4be2cda09de9222b5514aac03`

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

- `term_c1_served_page_goja_test.go` gets the HTML from the production
  `HandleHTML` handler, extracts its literal inline page script, and executes
  it in Goja. The only test host objects are browser/WebView boundaries
  (`WebSocket`, `Terminal`, and `ReactNativeWebView.postMessage`); Go does not
  model, duplicate, or call the page control bridge or `sendInput` logic.
- The real served `term.onData`, `pokitSendInput`, `connect()`, and
  `__pokitControlBridge` produce the captured WebSocket and native bridge
  events. This covers direct Ctrl+C (`0x03`), paste, every macro, and
  two-frame text+Enter, with an `accepted` result delivered to the native
  bridge for every frame.
- Invalid/missing/unknown capability hellos, malformed controls, and wrong
  session/connection/generation identities fail closed: direct keyboard and
  native page sends emit zero `terminal_input` frames (and therefore zero
  terminal writes). Wrong-identity results never reach the native bridge.
- An identical duplicate hello is delivered once without resetting state. A
  different identity on the same socket revokes authority and posts read-only;
  the page's actual reconnect creates a replacement socket, rejects stale old
  socket controls, and accepts the replacement hello.
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
- `go test ./internal/term -run 'TestTERM_C1_ServedPageGoja' -count=1`
- `go test ./internal/term`
- `go vet ./...`
- `go run scripts/archgate.go`
- `sh scripts/build-gate.sh` — build, vet, race tests, mobile typecheck/tests,
  Android Kotlin compile, invariant and secret scans

Static diff scan found zero new `mux.Registry` or `GetRecorder` references.
