# M2.5-3 — Device Challenge Authentication and Short Session Plan

Status: ready after M2.5-2 is accepted
Scope: protocol and implementation contract; no REST/WebSocket middleware yet
Depends on: M2.5-1 host identity/device registry and accepted M2.5-2 pairing
Feeds: M2.5-4 unified REST/WebSocket authentication

## Decision

Normal remote access authenticates a paired device, not an account. A phone
proves possession of its registered P-256 private key to the daemon, then
receives a short-lived opaque daemon session.

```text
paired Android device key
        │ sign challenge transcript
        ▼
daemon verifies active registry key
        │
        ▼
in-memory opaque session
        │
        ▼
M2.5-4 REST / WebSocket authorization middleware
```

This phase does not expose lifecycle endpoints to unauthenticated remote
clients. It establishes the reusable auth service and its API only.

## Threat boundary

- Cloudflare/WSS is trusted for transport confidentiality in the personal MVP;
  M2.5-3 is not E2EE or Noise.
- Tunnel URL possession, a captured challenge response, a stale token, and a
  revoked device must not authenticate a caller.
- The local `0600` Unix socket remains a separate trusted-local boundary for
  `pokit run`; it does not mint remote device sessions.
- A device display name is never authentication or authorization evidence.

## Algorithms and encodings

- P-256 ECDSA with SHA-256, reusing the registered canonical PKIX/DER public
  key from M2.5-1.
- ECDSA signatures use ASN.1 DER as produced by platform crypto APIs.
- random values use `crypto/rand`: 32-byte nonces, 32-byte challenge IDs, and
  32-byte opaque session tokens.
- values exposed over JSON use unpadded base64url. Do not use hex in one API
  and base64 in another without an explicit versioned conversion rule.
- signature inputs are canonical length-prefixed binary fields. Never sign a
  JSON encoding or a concatenation with ambiguous field boundaries.

## Protocol v1

### 1. Start challenge

```http
POST /api/device-auth/challenge
Content-Type: application/json

{
  "version": 1,
  "hostId": "<paired host id>",
  "deviceId": "<registered device id>",
  "clientNonce": "<32-byte base64url>"
}
```

The daemon performs cheap validation before storing state: version, bounded
body, fixed nonce length, matching host ID, and active non-revoked device.
Unknown/revoked devices receive the same non-enumerating failure response.

Successful response:

```json
{
  "version": 1,
  "challengeId": "<32-byte base64url>",
  "hostId": "...",
  "hostKeyFingerprint": "...",
  "daemonBootId": "<random boot-local id>",
  "serverNonce": "<32-byte base64url>",
  "expiresAt": "RFC3339 UTC",
  "hostSignature": "<DER ECDSA signature>"
}
```

The daemon stores only the pending challenge metadata:

```text
challengeId digest, deviceId, clientNonce, serverNonce,
hostId, daemonBootId, issuedAt, expiresAt, failedAttempts, consumed
```

`hostSignature` proves that the endpoint possesses the host private key that
the phone pinned during M2.5-2. The phone verifies host ID/fingerprint and this
signature before it signs or sends a response.

### 2. Verify challenge and issue a session

```http
POST /api/device-auth/verify
Content-Type: application/json

{
  "version": 1,
  "challengeId": "...",
  "deviceId": "...",
  "signature": "<DER ECDSA signature>"
}
```

The phone signs the exact domain-separated transcript below using its Android
Keystore private key. The daemon verifies with the current active registry key.

```text
"pokit-device-auth-v1"
length(hostId)         || hostId
length(deviceId)       || deviceId
length(daemonBootId)   || daemonBootId
length(challengeId)    || challengeId bytes
length(clientNonce)    || clientNonce bytes
length(serverNonce)    || serverNonce bytes
length(expiresAtUnix)  || fixed-width UTC unix milliseconds
```

The host signs the same transcript with the literal role field `"host"`; the
phone signs it with `"device"`. The role field must be the first
length-prefixed field after the domain separator. This prevents reflected host
and device signatures.

The daemon atomically checks and consumes the challenge before issuing a token:

```text
exists ∧ not expired ∧ not consumed ∧ matching device
→ verify signature
→ mark consumed
→ create session
```

Signature failure increments a small per-challenge attempt count; any terminal
failure consumes/deletes the challenge. A challenge cannot be retried after a
successful verification or used to mint two sessions.

Successful response:

```json
{
  "token": "<32-byte opaque base64url token>",
  "tokenType": "Bearer",
  "expiresAt": "RFC3339 UTC",
  "deviceId": "...",
  "permissions": ["sessions:read", "terminal:input"]
}
```

The raw token is returned exactly once over HTTPS/WSS-protected transport. The
daemon stores only its SHA-256 digest and never logs it.

## Session manager contract

`DeviceSessionManager` is daemon-owned, in-memory, concurrency-safe, and
independent of HTTP handlers.

```text
DeviceSession
  tokenDigest
  sessionId              // opaque server correlation ID, never a token
  deviceId
  hostId
  daemonBootId
  permissions snapshot
  issuedAt
  expiresAt
  lastSeenAt
  revokedAt?             // normally deletion is sufficient in memory
```

Required operations:

- `CreateAfterVerifiedChallenge`;
- `AuthenticateBearer` returning `Principal` without exposing the raw token;
- `RevokeDevice(deviceID)` invalidating every matching session immediately;
- `PurgeExpired(now)`;
- `InvalidateAllForBoot` by construction on daemon restart.

`Principal` contains device ID, immutable permission snapshot, session
correlation ID, authentication time, and host ID. It is the only input M2.5-4
authorization middleware needs.

No token persistence is permitted in MVP. Daemon restart requires a fresh
device-key challenge. The client retains a token only in protected runtime
storage for its active app session; durable refresh-token behavior is out of
scope.

## Authorization boundary prepared for M2.5-4

M2.5-3 must define, but not yet attach, this boundary:

```text
HTTP bearer token
  → DeviceSessionManager.AuthenticateBearer
  → Principal in request context
  → Authorize(principal, action, adapter capability, lifecycle state)
  → existing handler
```

M2.5-4 applies the same principal model to every remote REST endpoint and
terminal WebSocket before upgrade/subscription. It must not retain a second
Supabase-only authorization path for product remote access.

Browser/WebView WebSockets cannot reliably use arbitrary Authorization headers.
M2.5-4 therefore uses a derived one-time `wsTicket`, not the bearer token in a
long-lived URL:

```text
authenticated POST /api/device-auth/ws-ticket
  → 30-second, single-use opaque ticket bound to device/session/target host
  → /term/ws?ticket=... consumes before WebSocket upgrade
```

Ticket query parameters must be redacted from daemon/proxy diagnostics. They
are an explicit short-lived compatibility exception, not a general token query
policy.

## Local and development configuration

- Production remote listener: device-session authentication is mandatory once
  M2.5-4 ships; `dev-token` is never accepted there.
- `--insecure-local-only`: may support local development only when binding is
  demonstrably loopback-only and tunnel ingress is disabled. It must not become
  a remote fallback.
- Existing Supabase verifier may remain temporarily for legacy builds, but its
  identity must not be combined ambiguously with a device `Principal`. M2.5-4
  chooses one explicit product auth mode per listener/configuration.

## Required M2.5-3 tests

### Product-boundary tests

1. registered active device completes start → host proof verification → device
   proof → one session token;
2. phone rejects a response with wrong host fingerprint, wrong host signature,
   wrong boot ID, wrong challenge, or expired timestamp;
3. replayed verify request cannot issue a second token;
4. unknown, revoked, malformed-key, wrong-device, expired, and consumed
   challenges fail closed without token issuance;
5. signature with another P-256 key or altered canonical field fails;
6. session expiry rejects bearer authentication;
7. device revoke invalidates all of that device's sessions immediately;
8. daemon restart has no valid prior sessions;
9. raw token, nonce, signature, and token digest never appear in API errors or
   normal logs;
10. race tests: concurrent verifies issue at most one session; concurrent
    revoke/verify cannot leave an authenticated revoked principal.

### Contract tests for the future middleware

- `Principal` contains only the expected device/permission/session fields;
- no handler needs to parse a token directly;
- local Unix socket creation remains functional without a remote bearer token;
- no new remote endpoint accepts `dev-token` in production configuration.

## Acceptance criteria

M2.5-3 is accepted when:

- both sides prove the intended host/device key possession using canonical,
  domain-separated transcripts;
- challenge and token replay are impossible under concurrent requests;
- every token is short-lived, opaque, digest-stored, device/host/boot-bound,
  and invalidated on revoke/restart;
- failure paths are non-enumerating and secret-safe;
- the implementation exposes a handler-independent `Principal` and session
  manager suitable for M2.5-4;
- full Go race/build gates pass.

## Explicit non-goals

- no REST/WebSocket middleware migration yet;
- no mobile QR scanner or Android Keystore implementation in this phase;
- no refresh tokens, account login, cloud authorization, or multi-device UI;
- no E2EE/Noise/per-frame encryption;
- no biometric step-up or per-session ACL;
- no persistent authenticated sessions across daemon restart.

## Rollback

Feature-gate the device-auth endpoints and session manager. If disabled, they
must not change Recorder, lifecycle, local IPC, or pairing behavior. Do not
fall back from a failed device-auth request to `dev-token` on a remote listener.
