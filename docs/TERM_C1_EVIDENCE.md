# TERM-C1 — Single control bridge evidence

Implementation commit: `c5cb19342fccb1162c5158c8e39c28ccc2343111`

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
  a changed connection identity still marks unresolved delivery as unknown.

## Focused assertions

- `terminalGeneratedScript.test.ts` proves exactly-once forwarding of `hello`,
  `read_only`, `input_pending`, `input_result`, and `geometry`; it also proves
  reconnect fails closed, retains the last valid geometry, and ignores an old
  socket.
- `feedScreenInput.test.ts` proves a repeated identical `hello` cannot clear a
  pending acknowledgement.
- `terminalController.test.ts` proves bootstrap geometry invokes the control
  bridge rather than calling `term.resize` directly.

## Verification run against the implementation commit

All passed:

- `npm test -- --runInBand terminalGeneratedScript.test.ts feedScreenInput.test.ts`
- `npm run typecheck`
- `npm test -- --runInBand` — 35 suites, 517 tests
- `go test ./internal/term`
- `go vet ./...`
- `go run scripts/archgate.go`
- `sh scripts/build-gate.sh` — build, vet, race tests, mobile typecheck/tests,
  Android Kotlin compile, invariant and secret scans

Static diff scan found zero new `mux.Registry` or `GetRecorder` references.
