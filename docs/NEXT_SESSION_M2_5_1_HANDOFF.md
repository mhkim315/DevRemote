# Next Session Handoff — M2.5-1 Host Identity and Device Registry

Status: execution handoff
Branch: `feature/phase10-multi-adapter`
Accepted baseline: `eae1e96c9`

Read first:

- `docs/M2_5_DEVICE_TRUST_SECURITY_PLAN.md`
- `docs/M2_EAE1E96_ACCEPTANCE.md`

## Mission

Implement M2.5-1 only: host identity and persistent paired-device registry.

Do not implement network pairing, QR UI, challenge authentication, session
tokens, REST/WSS middleware rollout, mobile lifecycle UX, E2EE, Noise, or Push.

## Required host identity contract

- generate one P-256 host identity using standard library/platform primitives;
- persist stable host ID, public key, private-key storage reference/material,
  key version, and created timestamp;
- canonical public-key encoding and SHA-256 fingerprint;
- atomic owner-only storage;
- load returns the same identity across daemon restarts;
- malformed/insecure-permission storage fails closed or corrects permissions
  under an explicit tested policy;
- no private key, token, or secret in logs/errors/API output.

Use a small storage interface so a later macOS Keychain implementation can
replace the MVP owner-only file store without changing pairing/auth code. Do not
invoke a shell command with private-key material in argv.

## Required device registry contract

Versioned record:

```text
deviceId
canonical P-256 public key
fingerprint
displayName
permissions/role
createdAt
lastSeenAt
revokedAt
```

Requirements:

- public keys only; no phone private key;
- stable device ID derived/generated under an explicit policy;
- displayName is untrusted metadata;
- add/get/list/revoke operations;
- duplicate public key/device handling is deterministic;
- revoked devices remain revoked and are excluded from active lookup;
- atomic owner-only persistence;
- store corruption does not silently reset trust to empty and continue;
- first owner permission representation exists even though role UI is future;
- push token field may be omitted or optional; do not implement push behavior.

## Security boundaries

- no HTTP/API endpoint exposes private host key material;
- no pairing endpoint is opened in this phase;
- no `dev-token` behavior is expanded;
- do not treat localhost/LAN IP as device authentication;
- use standard P-256 encoding/signing primitives, not custom crypto;
- secrets and public-key fingerprints have distinct log/redaction policy.

## Required tests

- host identity create/load stability;
- distinct installations generate distinct IDs/keys;
- sign/verify round trip using the stored host identity;
- owner-only file mode and atomic replace;
- corrupted identity fails safely;
- device add/load/list/revoke persistence;
- duplicate key/device behavior;
- revoked device fails active lookup;
- malformed/non-P-256 public key rejected;
- store permission/corruption policy;
- concurrent registry operations under race detector;
- no key material in serialized public DTO/log/error fixtures;
- existing M0-M2, CLI, Recorder, adapter, mobile typecheck and build gates pass.

## Explicit non-goals

- LAN listener;
- QR generation/scanning;
- pairing token/challenge;
- Android Keystore mobile implementation;
- authentication middleware;
- short-lived session token;
- WebSocket ticket/cookie;
- mobile UI;
- E2EE/Noise;
- hardware attestation;
- key backup/rotation/recovery;
- full permissions UI;
- Push/discovery.

## Acceptance report

Return:

```text
M2.5-1 status: ACCEPT / SCOPED ACCEPT / NEEDS FOLLOW-UP
Commit: <hash>

Host identity:
- storage path/backend
- algorithm/encoding
- permission/atomicity policy

Device registry:
- schema version
- duplicate/revoke/corruption behavior

Tests/build:
- ...

Deferred to M2.5-2+:
- ...
```

Stop after committing/pushing M2.5-1. Do not begin pairing without verifier
acceptance.
