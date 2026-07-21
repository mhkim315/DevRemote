# Input-A R6 evidence

The input authorization source is the server-issued capability snapshot.
`terminal:input` is projected only when the authenticated device principal
has that exact permission. `readOnlyReason` is display-only and is never used
as an authorization predicate.

Daemon production-path coverage:

- `TestInputA_SessionCapabilitiesArePrincipalAuthorized` drives the real
  `RequirePrincipal` middleware into `HandleSessionsV2` and proves viewer and
  owner responses differ only by the server-authorized capability.
- `TestInputA_DenialViaHandleWS` drives the real WebSocket endpoint, sends two
  rapid binary frames with `conn.WriteMessage`, reads the text `read_only`
  control frame, and asserts `TerminalTransport.WriteInput` was called zero
  times, the transcript has no new event, and no second denial is emitted in
  the one-second rate-limit window.
- `TestInputA_PermissionAnnouncementCarriesCapabilities` verifies the WS
  hello frame is the same server-authorized capability projection.
- The served terminal page begins and reconnects in read-only mode; only that
  server hello frame can enable input.

Mobile production-path coverage:

- `mobile/__tests__/feedScreenInput.test.ts` renders the actual `FeedScreen`
  component with an input-capable server session in both authorization states:
  `caps: ["history"]` disables the production TextInput and Send controls,
  while `caps: ["history", "terminal:input"]` enables them. It also parses a
  server hello capability frame through the same mounted instance's production
  WebView `onMessage` callback and observes the denied-to-authorized control
  transition. An authorized device on a session without the adapter `input`
  capability remains disabled, completing the device/session gate truth table.
- The former `readOnlyReason === ""` policy test was removed; no local denial
  state can grant input.

Validation:

```text
go test -race ./... -count=1
go vet ./...
cd mobile && npm run typecheck && npm test -- --runInBand
```
