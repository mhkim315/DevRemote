# E8 Execution Plan — Connection Correctness First

Date: 2026-07-08

## Current accepted baseline

Accepted:

- A5~A10 platform foundation
- P1a Live Dashboard Core
- P2 Core Feed Taxonomy
- E6 no-login local test branch
- E8 instrumentation package
- Desktop runtime E8DIAG evidence
- Desktop reconnect / PTY replay evidence

Not yet accepted:

- Mobile WebView E8DIAG validation
- Final terminal duplication fix
- Mobile confirmation of `term.clear()` behavior

## Why the priority changed

Mobile validation is currently ambiguous because connection failures are hidden
as empty product state.

Observed failure mode:

```text
Stored BASE_URL
→ app marks connected
→ Dashboard opens
→ listSessions() fails
→ sessions=[]
→ only NEW AGENT is shown
```

This makes a broken connection look like a healthy daemon with no sessions.
That blocks E8, because missing E8DIAG could mean any of:

- stale saved BASE_URL;
- dead Cloudflare tunnel;
- daemon not reachable;
- wrong daemon process;
- auth failure;
- WebView failure;
- actual terminal bug.

Before Mobile E8 validation, the app must distinguish connection failure from
empty session list.

## Revised execution sequence

### E8a — Connection State Correctness

Goal:

```text
Connection failure must never render as "no sessions".
```

Required behavior:

- Restored `BASE_URL` is verified before `isConnected=true`.
- Manual/QR connect verifies daemon reachability before saving `BASE_URL`.
- Dashboard fetch failure displays a connection error, not an empty dashboard.
- Empty sessions are shown only after a successful `/api/sessions` response.
- User has a visible retry/rescan path.

Allowed implementation scope:

- mobile connection provider health check;
- dashboard connection error display;
- connect screen connection error display;
- retry/rescan affordance;
- focused tests or documented runtime evidence.

Explicitly out of scope:

- onboarding redesign;
- new tunnel manager;
- new pairing protocol;
- new daemon adapter/backend;
- new `--listen-addr` / bind-all implementation;
- terminal duplication fix acceptance.

Acceptance evidence:

- build gate passes;
- unreachable saved URL does not enter Dashboard as connected;
- unreachable manual/tunnel URL shows a connection error;
- successful `/api/sessions` with empty array is distinguishable from fetch failure;
- no secrets are logged or committed.

### E8b — Mobile Connectivity Restore

Goal:

```text
Establish one reliable phone-to-daemon route for Mobile E8 validation.
```

Approved routes:

1. Emulator / USB Android:
   - daemon remains `127.0.0.1:9171`;
   - use `adb reverse tcp:9171 tcp:9171`;
   - verify `/api/sessions` from the app.

2. LTE physical device:
   - daemon remains `127.0.0.1:9171`;
   - use a public HTTPS tunnel to `127.0.0.1:9171`;
   - verify the tunnel URL returns `/api/sessions` before Terminal testing.

Do not weaken `--insecure-local-only`. It must remain local-only.

If LAN binding is needed, it is a future explicit option such as
`--listen-addr` or `--dev-bind-all-interfaces`, with warning logs and separate
review. It is not an E8 hotfix.

### E8c — Mobile WebView E8DIAG Capture

Goal:

```text
Capture mobile WebView terminal behavior with E8DIAG.
```

Required scenarios:

- initial Terminal tab open;
- scroll;
- tab switch away/back;
- refresh;
- duplication occurrence if reproducible.

Evidence format:

```text
Before action:
E8DIAG connectCount=... closeCount=... msgCount=... totalBytes=... rawLen=... wasReconnect=...

After action:
E8DIAG connectCount=... closeCount=... msgCount=... totalBytes=... rawLen=... wasReconnect=...

Conclusion:
...
```

### E8d — Terminal Duplication Fix Acceptance

Goal:

```text
Accept or reject the terminal duplication fix using objective evidence.
```

Current candidate:

```javascript
if (wasReconnect) { term.clear(); wasReconnect = false; }
```

Already supported:

- desktop reconnect causes exact PTY replay (`msgCount` and `totalBytes`
  doubled);
- desktop scroll does not trigger WebSocket/backend replay.

Still required:

- mobile confirmation, or a clearly scoped desktop-only acceptance;
- scrollback preservation check;
- no-data-loss check;
- before/after evidence that duplication is removed.

## Reviewer rule

Do not accept Mobile E8 validation until connection state is trustworthy.

Do not reject E8a because physical-device validation remains. E8a is an
execution-track unblocker and can be accepted with source/build/runtime evidence.

