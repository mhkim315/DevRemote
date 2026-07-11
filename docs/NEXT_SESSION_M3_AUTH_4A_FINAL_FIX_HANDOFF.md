# Pokit Executor Onboarding — M3-auth-4A Final Production Fix

Status: **REJECTED; implementation remediation required**

Branch: `feature/phase10-multi-adapter`

Current baseline: `3277dbb16`

This is the authoritative context-reset handoff for the next execution agent.
Read it before editing. Do not begin M3-auth-R1, M3a re-verification, iOS work,
Transcript, or unrelated cleanup. The immediate task is to finish Android
WS-ticket production Terminal integration without weakening the already
accepted daemon authentication or Recorder architecture.

## 1. Product model and invariants

Pokit is a local-first agent runtime and mobile cockpit:

```text
pokit run <command-or-agent>
        ↓
controlled PTY
        ↓
session-owned Recorder = the only PTY reader
        ↓
local / mobile / web subscribers
```

Hard invariants:

1. Recorder subscriber bytes are raw PTY output only.
2. Local/mobile/web viewers never independently read the PTY.
3. Live Terminal and Transcript are separate contracts.
4. Remote REST uses a device bearer; remote `/term/ws` uses a short-lived,
   one-time, session-bound WS ticket.
5. Bearers, Supabase JWTs, and dev tokens must never enter a remote Terminal
   URL, WebView state, HTML, log, or WebSocket URL.
6. `explicit_local_dev` is the only mode allowed to use the legacy Terminal URL.
7. Mobile mirrors authoritative host PTY geometry; it must not resize the shared
   PTY merely to fit the phone viewport.
8. WebSocket disconnect is viewer lifecycle, not process lifecycle authority.

Do not redesign accepted M2.5-3/M2.5-4 middleware, bearer sessions, ticket
grants, permission vocabulary, connection registry, or Recorder ownership.

## 2. Current accepted foundation

The following are already implemented and should be preserved:

- Android hardware-backed DeviceKey provider and strict P-256 wire contracts.
- LAN QR pairing protocol and host/device key-possession proofs.
- Device challenge authentication and short bearer sessions.
- Explicit mobile `AuthMode`:
  - `paired_device`
  - `explicit_local_dev`
  - `initializing`
  - `pairing_required`
  - `failed`
- `deriveTerminalAuth()` is the authoritative Terminal auth decision.
- Exact WS ticket format: `^[0-9a-f]{64}$`.
- Daemon ticket binding to host, device, bearer session, and target session.
- Replacement/revoke/expiry invalidation and active WebSocket closure.
- `TerminalController` bootstrap, connId, attempt binding, and reconnect ticket
  singleflight foundation.
- Recorder raw-stream contamination introduced during geometry work was reverted
  at `889b7cb76`; do not reintroduce typed JSON into `Recorder.broadcast()`.

Read before changing code:

- `docs/M3_AUTH_MOBILE_DEVICE_CLIENT_PLAN.md`
- `docs/M3_AUTH_CROSS_PLATFORM_DEVICE_KEY_PLAN.md`
- `docs/M2_5_4_UNIFIED_AUTH_ACCEPTANCE.md`
- `docs/ROADMAP_AFTER_E10B.md`
- `mobile/src/lib/authMode.ts`
- `mobile/src/lib/terminalController.ts`
- `mobile/src/screens/FeedScreen.tsx`
- `companion-daemon/internal/term/pty.go`
- `companion-daemon/internal/term/recorder.go`

## 3. Current independent-verification verdict

Baseline `3277dbb16` remains **REJECTED**. Build gates passing does not replace
the missing product-path contracts below.

### Blocker A — raw input and control framing still collide

Daemon geometry control is recognized only on a WebSocket TextMessage, but the
terminal page still sends xterm input with `ws.send(d)`, where `d` is a string.
That is also a TextMessage. Pasting the exact text below is swallowed as control
instead of reaching the PTY:

```json
{"type":"geometry-poll"}
```

Required result:

```text
client → server binary = raw terminal input
client → server text   = closed control-frame vocabulary
server → client binary = raw PTY output
server → client text   = closed control-frame vocabulary
```

Change the actual terminal page/input path, not only a test helper. Preserve
compatibility intentionally and add a regression proving the exact JSON bytes
reach the PTY when sent as binary input.

### Blocker B — geometry response is also written as terminal output

The injected script adds a geometry `message` listener, but the daemon page also
assigns `ws.onmessage = ... term.write(...)`. Both handlers receive the text
geometry event, so the JSON can be rendered in the mobile Terminal.

Required result:

- one authoritative page-level demultiplexer;
- binary payload goes to `term.write`;
- text `geometry` payload calls `term.resize` and returns without writing;
- unknown/malformed control frames fail closed and are not interpreted as PTY
  output;
- listener installation cannot be overwritten by later page assignments.

### Blocker C — pairing flow ordering is not authoritative

`pairing_required` now renders `ConnectScreen`, which is good. However
`ConnectScreen` invokes `onPaired?.()` without awaiting completion and then calls
`connect(base)`. The App callback asynchronously reloads the pairing and creates
the TokenManager. Connection state and trusted auth state can race.

Required result:

- make the callback awaitable, for example `onPaired: () => Promise<boolean>`;
- save pairing first;
- reload and validate canonical operational origin;
- create DeviceKey/TokenManager;
- atomically enter `paired_device`;
- only then connect/navigate;
- failure keeps the scanner visible and does not enter a partially paired state.

### Blocker D — geometry polling lifecycle needs a precise contract

Current polling interval starts at WebSocket construction, not on `open`.
Required result:

- send an immediate poll on `open`;
- start exactly one bounded interval after open;
- clear it on close/error/page replacement;
- every reconnect socket owns its own timer;
- no timer survives unmount or session switch;
- host geometry changes are reflected on mobile within the documented bound;
- shared PTY geometry is never changed by the mobile viewer.

### Blocker E — production generated-script tests are still missing

The current test extracts `wsURL`, then rewrites an equivalent function inside
the test. That tests the copy, not the generated production script.

Execute the actual generated script in a controlled fake browser/runtime. The
test must prove:

1. initial ticket A reaches the first absolute `wss://.../term/ws` constructor;
2. A is consumed exactly once and is absent from navigation history;
3. disconnect requests a new ticket;
4. B reconnects exactly once, then C on the next reconnect;
5. concurrent reconnect signals cause one issuance;
6. stale session/attempt/connId signals cause no issuance;
7. binary PTY output is written unchanged;
8. text geometry resizes and is not written;
9. exact control-looking binary input reaches the PTY;
10. timers are cleaned up on close/unmount.

String containment assertions are supplementary only.

## 4. Required implementation order

Work in this order to avoid another partial-fix loop:

1. Write failing execution tests for the full framing/reconnect script.
2. Define the binary-data/text-control WebSocket contract in one place.
3. Update daemon terminal HTML and server read loop together.
4. Implement authoritative page message demultiplexing.
5. Fix geometry poll timer ownership.
6. Make pairing completion awaitable and atomic.
7. Add component/controller tests for the actual pairing-required path.
8. Run targeted race tests repeatedly.
9. Run the full build gate.
10. Commit and push, then stop for independent verification.

Do not report completion after only helper tests, typecheck, or string searches.

## 5. Required automated acceptance proofs

### Mobile production boundary

- unpaired launch renders the real pairing scanner;
- approved QR calls `pairAndSave` and awaits trusted-state installation;
- failed save/DeviceKey/TokenManager creation does not connect;
- paired launch selects ticket auth, never legacy auth;
- WebView is absent until bootstrap and ticket validation succeed;
- malformed/failed ticket does not open WebView;
- bearer never appears in WebView source, HTML, URL, navigation state, or error;
- session switch and unmount discard late work;
- reconnect rotates A → B → C without reuse or parallel issuance.

### WebSocket framing

- binary client input reaches `WriteInput` byte-for-byte;
- text geometry poll never reaches `WriteInput` or Activity;
- binary PTY output remains byte-for-byte raw;
- text geometry response never reaches `term.write`;
- read-only viewer may read geometry but cannot send PTY input;
- response send exits on writer/request cancellation.

### Recorder regression

- local attach receives no geometry JSON;
- Recorder bootstrap contains no control frames;
- Activity/Transcript receive no geometry records;
- Recorder remains the only controlled-PTY reader;
- WS close does not stop the session.

### Existing daemon security regression

Retain evidence for valid ticket upgrade, replay rejection, wrong host/session,
replacement, revoke, expiry, connection close, permission denial, and bounded
ticket capacity.

## 6. Verification commands

Run targeted tests while iterating, then finish with:

```sh
sh scripts/build-gate.sh
```

Also run the relevant mobile controller/component tests repeatedly and the
daemon term/device-auth tests with `-race`. If an Android emulator/device is
available, run the native gate and a real WebView smoke. Lack of a device may
leave only the physical smoke as M-track; it does not excuse missing automated
script/controller tests.

The final report must list:

- commit hash and pushed branch;
- exact production files changed;
- framing contract;
- pairing state-transition contract;
- test names and what each proves;
- full gate result;
- physical-device items honestly deferred;
- clean `git status --short`.

## 7. Explicit non-goals

Do not begin:

- M3-auth-R1 or authenticated M3a re-verification;
- M3-auth-1B/iOS Secure Enclave work;
- Transcript projection;
- Windows support or speculative Windows abstractions;
- cmux transcript tuning;
- authentication architecture redesign;
- helper extraction unrelated to a production contract.

Windows remains deferred. Shared wire contracts must remain OS-neutral, but no
ConPTY, CNG/TPM, DPAPI, Named Pipe, Service, or Windows host-key abstraction is
required.

## 8. Completion condition

M3-auth-4A is complete only when all production-path proofs above pass and an
independent verifier returns `ACCEPT`.

After acceptance, the next sequence is:

```text
M3-auth-R1  → authenticated Android M3a end-to-end re-verification
M3-auth-1B  → iOS Secure Enclave DeviceKey provider
M3-auth-2B  → iOS pairing/bearer/WS-ticket/Terminal integration
```

Platform naming remains `A = Android`, `B = iOS`.
