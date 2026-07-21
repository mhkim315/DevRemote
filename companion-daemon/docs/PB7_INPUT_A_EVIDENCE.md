# Input-A R5 evidence

The input authorization source is the server-issued capability snapshot.
`terminal:input` is projected only when the authenticated device principal
has that exact permission. `readOnlyReason` is display-only and is never used
as an authorization predicate.

Daemon production-path coverage:

- `TestInputA_SessionCapabilitiesArePrincipalAuthorized` drives the real
  `RequirePrincipal` middleware into `HandleSessionsV2` and proves viewer and
  owner responses differ only by the server-authorized capability.
- `TestInputA_DenialViaHandleWS` drives the real WebSocket endpoint, sends a
  binary frame with `conn.WriteMessage`, reads the text `read_only` control
  frame, and asserts `TerminalTransport.WriteInput` was called zero times.
- `TestInputA_PermissionAnnouncementCarriesCapabilities` verifies the WS
  hello frame is the same server-authorized capability projection.

Mobile production-path coverage:

- `mobile/__tests__/feedScreenInput.test.ts` renders the actual `FeedScreen`
  component with `caps: ["history"]` and asserts both the production
  TextInput and Send controls render disabled.
- The former `readOnlyReason === ""` policy test was removed; no local denial
  state can grant input.

Validation:

```text
go test -race ./... -count=1
go vet ./...
cd mobile && npm run typecheck && npm test -- --runInBand
```
