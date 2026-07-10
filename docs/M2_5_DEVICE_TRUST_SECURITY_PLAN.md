# M2.5 — Minimum Device Trust Security Plan

Status: approved architecture and staged execution plan
Branch: `feature/phase10-multi-adapter`
Baseline: M2 accepted at `eae1e96c9`

## Product security goal

Pokit is a personal, local-first product:

```text
my computer ↔ my paired phone
```

It does not require email/password/OAuth account infrastructure. The root of
authorization is a device explicitly paired and approved on the computer.

## Explicit transport trust statement

For personal MVP and initial closed beta:

> Cloudflare is trusted as the HTTPS/WSS transport confidentiality processor.
> It may technically observe proxied terminal, Transcript, and control traffic.
> Cloudflare does not register devices, issue Pokit authorization, own sessions,
> or execute commands. Pokit daemon makes the final authorization decision.

M2.5 does not claim end-to-end confidentiality from Cloudflare. That requires
future application-layer E2EE/Noise work.

## Why M2.5 precedes M3

The current static `dev-token` must not protect LTE Create/Input/Stop/Kill/Delete
in a product build. Adding device identity after M3 would require rewriting every
REST call, WebSocket creation path, and lifecycle action.

M2.5 installs the stable boundaries now:

```text
AuthenticateDevice
→ Principal{deviceId, permissions}
→ Authorize(action)
→ existing handler

PokitSecureClient
→ authenticated REST
→ authenticated terminal WebSocket
```

Product screens and lifecycle handlers must not construct credentials directly.

## Security scope now

### M2.5-0 — Security contract

Status: completed by this document.

- threat/trust assumptions;
- Cloudflare confidentiality statement;
- device identity rather than account identity;
- short-lived session-token decision;
- release/deferred security boundary;
- protocol versioning and no remote dev-token rule.

### M2.5-1 — Host identity and device registry

Host identity:

```text
hostId
P-256 public key
P-256 private-key storage reference
key version
createdAt
```

Device registry:

```text
deviceId
P-256 public key
public-key fingerprint
display name (untrusted presentation only)
permissions/role field
createdAt
lastSeenAt
revokedAt
push token (future, optional)
```

Requirements:

- use standard P-256 ECDSA/SHA-256 primitives;
- never log or export private keys;
- public keys use one canonical encoding;
- first approved phone may receive owner permissions;
- unknown/revoked keys fail closed;
- device registry persists atomically with owner-only file permissions;
- host-key loss/reinstall means re-pairing in MVP;
- no network pairing endpoint yet.

Host private-key storage may start with a strictly permissioned local store if
native Keychain integration would expand the phase. Keychain migration is a
release-hardening follow-up; storage abstraction and file permissions are part
of M2.5-1.

### M2.5-2 — LAN-only QR pairing

Pairing mode is explicit and time-bounded. Suggested local command:

```text
pokit pair
```

It creates a pairing session and shows the QR plus pending-device approval in a
local computer UI/CLI.

QR bootstrap fields:

```text
protocol version
local pairing endpoint
host ID
host public key/fingerprint
pairing session ID
256-bit one-time secret
expiration
```

Required flow:

```text
phone generates hardware-backed P-256 key
→ submits public key + one-time secret + phone nonce
→ host sends host nonce/challenge
→ phone signs the pairing transcript
→ computer displays device fingerprint or short confirmation code
→ user approves locally
→ registry stores public key
→ one-time secret/challenge are destroyed
```

Boundary requirements:

- pairing listener is separate from tunnel-routed HTTP/WSS;
- enabled only during pairing mode;
- bound only to the intended LAN interface/port;
- LAN/IP address is not treated as authentication;
- pairing endpoint is absent from Cloudflare ingress;
- expiry, one pending candidate, rate/body/connection limits;
- pairing token is atomic and non-replayable;
- friendly device name is not approval evidence.

### M2.5-3 — Challenge authentication and short session

Normal remote login is device-key challenge-response, not account login.

```text
phone requests challenge
daemon returns:
  protocol version
  host ID
  daemon boot/session ID
  device ID
  256-bit challenge
  expiration

phone signs domain-separated transcript
daemon verifies registered non-revoked key
daemon issues random opaque session token
```

Session token contract:

- random 256-bit value;
- 10–30 minute lifetime;
- stored only in daemon memory, preferably as a digest;
- client retains it only for the active authenticated session;
- bound to host ID, device ID, permissions, and daemon boot ID;
- all sessions invalidated on device revoke or daemon restart;
- reauthentication always requires a fresh single-use challenge;
- never accepted over plaintext transport;
- not placed in logs or long-lived URL query parameters.

Challenges are single-use, short-lived, rate-limited, and deleted after success,
failure threshold, or expiry. Token issuance itself requires the paired device
signature; possession of the tunnel URL is insufficient.

Detailed canonical transcript, host-proof, session-manager, replay, and
negative-test contract:

- `docs/M2_5_3_DEVICE_CHALLENGE_AUTH_PLAN.md`
- `docs/NEXT_SESSION_M2_5_3_HANDOFF.md`

### M2.5-4 — Unified REST and WebSocket authentication

REST:

- one common device-session middleware;
- Principal placed in request context;
- all remote read/control/lifecycle endpoints use it;
- request/body/connection limits occur before expensive work;
- static `dev-token` is rejected on the remote product path.

WebSocket:

- authentication is completed before terminal subscription/output;
- connection is bound to the device and target host/session;
- reconnect uses a currently valid short session or fresh challenge;
- avoid persistent tokens in query strings;
- for the current WebView architecture, design either an authenticated secure
  cookie or a short-lived, single-use WebSocket ticket derived from the device
  session. The ticket must expire quickly and be consumed once.

M2.5 trusts Cloudflare WSS confidentiality. Per-frame E2EE, replay counters, and
Noise are deferred to release hardening.

### M2.5-5 — Revoke and minimal audit

Revoke:

- local computer can list and revoke paired devices;
- revoke immediately invalidates all active short sessions;
- revoked key cannot reauthenticate;
- re-add requires a new LAN pairing session.

Minimal local audit records:

```text
timestamp
deviceId
action
sessionId (if applicable)
result/status
request/session correlation ID
```

Audit must not store terminal content, input, Transcript, private keys, tokens,
pairing secrets, signatures, or push tokens.

## Minimum authorization model

The first paired phone may be owner, but the internal permission field is
required now so M3 does not hard-code universal authority.

Minimum action permissions:

```text
sessions:read
transcript:read
terminal:input
sessions:create
sessions:stop
sessions:kill
history:delete
devices:manage
```

Authorization decision:

```text
device permission
AND adapter/session capability
AND lifecycle state
```

Detailed multi-device role UI is deferred.

## Immediate remote containment

Until M2.5-4 is accepted:

- `--insecure-local-only` and static `dev-token` are development-only;
- do not expose state-changing product endpoints through an unprotected public
  tunnel;
- USB/LAN may be used for execution validation;
- if LTE testing is essential, use a temporary external perimeter such as
  Cloudflare Access and document that it is development infrastructure, not the
  final Pokit identity model;
- do not attempt to distinguish tunnel traffic using `RemoteAddr` because the
  tunnel reaches the localhost origin.

## Deferred security hardening

The following are explicitly not blockers for M3 after M2.5 acceptance:

- Cloudflare-blind E2EE;
- Noise Protocol;
- per-frame encryption/replay sequencing;
- hardware key attestation;
- biometric step-up for destructive actions;
- advanced owner/read-only/temporary device UI;
- host key rotation;
- encrypted host identity backup/recovery;
- fullcount.kr discovery/routing service;
- Expo Push;
- session-specific ACLs;
- public external penetration review.

These become Security Hardening / Public Release gates. Push remains wake-up
only and never becomes an authorization channel.

## M2.5 acceptance gate

Before M3 remote lifecycle UX:

- phone private key remains hardware-backed/non-exported where supported;
- pairing endpoint is LAN-only and excluded from tunnel ingress;
- QR bootstrap expires and cannot permanently authenticate;
- explicit local approval is required;
- unknown/revoked devices cannot authenticate;
- challenge replay and pairing replay are rejected;
- short session expires, is device-bound, and is purged on revoke/restart;
- REST and terminal WebSocket share the device-auth boundary;
- Create/Input/Stop/Kill/Delete require authenticated device permissions;
- remote static `dev-token` is unavailable in product configuration;
- local `0600` Unix socket `pokit run` remains functional;
- all logs and errors are secret-safe;
- full build/race/mobile gates pass.

## Rollback

- security phases are additive around existing handlers, not embedded in each
  lifecycle implementation;
- local Unix socket remains an independent trusted-local path;
- remote device-auth rollout can be feature-gated while USB/LAN testing remains;
- pairing/auth store schema is versioned from the first commit;
- no deferred E2EE field is fabricated before its protocol exists.
