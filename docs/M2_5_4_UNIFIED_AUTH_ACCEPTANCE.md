# M2.5-4 Unified REST / WebSocket Authentication Acceptance

Status: implementation complete

Scope: daemon-side device bearer authorization and one-time WebSocket ticket
boundary. Mobile device-key storage and client migration remain M3 work.

## Product contract

Pokit uses one authentication mode per listener configuration.

### Local development mode

`InsecureLocalOnly=true`:

- binds the daemon to loopback;
- does not start the tunnel;
- keeps the legacy `dev-token`/Supabase middleware for local development;
- retains local diagnostics and the existing terminal HTML flow.

### Remote device-auth mode

`InsecureLocalOnly=false`:

- challenge and verify are the only public bootstrap routes;
- operational REST routes require a short-lived device bearer;
- `/term/ws` requires a short-lived, one-time WS ticket;
- legacy `dev-token` and Supabase credentials do not authorize the product
  route group;
- raw dump and E8 instrumentation routes are not registered.

## Remote permission matrix

| Route | Permission |
|---|---|
| `GET /api/sessions` | `sessions:read` |
| `POST /api/sessions` | `sessions:create` |
| `DELETE /api/sessions` | `history:delete` |
| `POST /api/sessions/{id}/stop` | `sessions:stop` |
| `POST /api/sessions/{id}/kill` | `sessions:kill` |
| `DELETE /api/sessions/{id}` | `history:delete` |
| `GET /api/session-profiles` | `sessions:read` |
| approval action | `terminal:input` |
| links GET / POST / DELETE | `sessions:read` / `sessions:create` / `history:delete` |
| terminal size and HTML | `sessions:read` |
| push registration | `sessions:read` |
| command bridge | `terminal:input` |
| redacted diagnostics | `sessions:read` |

Authentication failure is `401`. An authenticated principal without the
declared permission receives `403` before the product handler executes.

## WebSocket ticket grant

The store holds only the SHA-256 digest of a random 32-byte ticket. A grant is
immutably bound to:

- host ID;
- device ID;
- target terminal session ID;
- authorizing bearer-session ID;
- permission snapshot;
- `min(ticket TTL, bearer expiry)`.

Ticket policy:

- single-use;
- wrong host/session attempts consume the ticket;
- replacement, revoke, bearer expiry, and daemon restart invalidate it;
- missing host identity fails closed before WebSocket upgrade;
- permissions are defensively copied at issue and consume boundaries;
- default capacity is three pending tickets per device and 64 globally;
- limits and TTL are injectable for tests and future policy changes;
- expiry, consume, and revoke release capacity immediately.

The established connection is registered under the actual device ID.
Replacement/revoke close it through `AuthenticatedConnRegistry`; a connection
timer closes it at exact bearer expiry even between session-purge ticks.

## Failure isolation and ownership

- Recorder remains the sole PTY reader.
- Authentication failure occurs before recorder subscription and WebSocket
  upgrade.
- Read-only principals may receive terminal output but cannot write input.
- Rejected input does not create an Activity event.
- Session-manager callbacks run outside its mutex and may safely close network
  connections.
- Every expiry removal path (authenticate, inline capacity purge, periodic
  purge) emits the same invalidation callback.

## Evidence

Automated coverage includes:

- real app/router local and remote route matrices;
- legacy credential rejection in remote mode;
- owner/member method-level permission boundaries;
- real controlled-PTY creation and ticket-only WebSocket upgrade;
- exact bearer-expiry WebSocket closure;
- missing-host fail-closed behavior;
- wrong-binding one-shot consumption;
- replacement/revoke ticket and connection invalidation;
- exact effective-expiry response;
- per-device/global capacity and concurrent issuance;
- capacity release after expiry and consumption;
- race coverage for all session-expiry removal paths.

`scripts/build-gate.sh` passes backend build/vet/race tests, mobile typecheck,
architecture invariants, and secret scanning.

## Explicit follow-ups

These are outside M2.5-4 rather than authentication bypasses:

- M2.5-5: paired-device list/revoke UX and minimal local audit;
- M3: Android Keystore identity, challenge login, bearer refresh, WS-ticket
  acquisition, and mobile route migration;
- release hardening: decide whether Cloudflare-visible TLS is sufficient or
  add application-layer E2EE/Noise.

