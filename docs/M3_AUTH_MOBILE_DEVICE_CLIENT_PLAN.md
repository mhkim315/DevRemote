# M3-auth — Mobile Device Authentication Client

Status: M3-auth-1 implemented; M3-auth-2/3 planned.
Prerequisite for: remote/LTE M3a use, and M3b.

## Why

M2.5-4 built the daemon-side device-auth boundary; the mobile app never got a
device-auth client, so it sends a `dev-token`/Supabase bearer that remote mode
rejects (401). M3-auth builds the mobile side of the M2.5 device-trust protocol
so the app authenticates as a paired device over the authenticated REST +
WS-ticket boundary.

Decisions: software P-256 via `@noble/curves` + key in `expo-secure-store` (no
native module; M2.5 defers hardware attestation and the daemon does not verify
it). Staged delivery 1 → 2 → 3, each its own commit + gate.

## The byte-exact contract (must match the Go daemon)

From `companion-daemon/internal/devicetrust`:

- **Device public key = SPKI DER** (`x509.MarshalPKIXPublicKey`): fixed 26-byte
  P-256 prefix `3059301306072a8648ce3d020106082a8648ce3d030107034200` + the
  65-byte uncompressed point `04||X(32)||Y(32)` = 91 bytes
  (`ParseP256PublicKey`, `identity.go`).
- **deviceId = hex(sha256(SPKI DER))** (`Fingerprint`, `identity.go`).
- **Signatures = ECDSA P-256 ASN.1 DER over sha256(message)**
  (`ecdsa.VerifyASN1`). In `@noble/curves` v2 this is
  `p256.sign(message, priv, { format: 'der', prehash: true })` — `prehash: true`
  makes noble hash with sha256 internally (matching Go's single-hash), and the
  result is deterministic (RFC 6979). Signing the pre-hashed digest is WRONG
  (double-hash) — verified against Go/Node crypto.
- **Pairing transcript** (`pairing.go` `buildPairingTranscript`): raw concat
  `"pokit-pair-v1:" || phoneNonce(32) || hostNonce(32) || hostPubDER || sessionId`.
- **Auth transcript** (`auth_transcript.go` `AuthTranscript.Build`): domain
  `"pokit-device-auth-v1"` + uint32-BE length-prefixed fields role("device"),
  hostId, deviceId, daemonBootId + length-prefixed 32-byte challengeId,
  clientNonce, serverNonce + two `uint32-BE(8)||int64-BE ms` fields createdAtMS,
  expiresAtMS.
- **JSON byte encoding differs by endpoint** (Go `[]byte` → base64):
  - `/pair` `PairingRequest.publicKey`, `.phoneNonce` and `/pair/confirm`
    `Confirmation.phoneSignature` → **base64** (standard, padded).
  - `ChallengeRequest`/`VerifyRequest` `clientNonce`, `challengeId`,
    `signature` → **hex**.
- **Challenge issuedAt coupling**: `AuthChallengeResponse` returns `expiresAt`
  but not `issuedAt`; challenge TTL is a fixed 5 min and verify rebuilds the
  transcript with the server times, so the device sets
  `createdAtMS = expiresAtMS − 300000`. (Optional daemon follow-up: expose
  `issuedAt` to remove the implicit coupling.)

## M3-auth-1 (DONE)

- `mobile/src/lib/crypto.ts`: `generatePrivateKey`, `publicKeyUncompressed`,
  `spkiDer`, `deviceFingerprint`, `signDer`/`verifyDer` (prehash), pure
  `toHex`/`fromHex`/`toBase64`/`fromBase64` (Hermes/Node identical; base64 is
  standard-padded = Go `base64.StdEncoding`), plus `DeviceIdentity`/
  `deriveIdentity`.
- `mobile/src/lib/deviceIdentity.ts`: `loadOrCreateDeviceIdentity` /
  `hasDeviceIdentity` storing the 32-byte scalar (hex) in `expo-secure-store`
  (`WHEN_UNLOCKED_THIS_DEVICE_ONLY`).
- Byte-exact proof from a fixed private scalar `0102..20`:
  - `mobile/__tests__/crypto.test.ts` — asserts SPKI DER, deviceId, and the
    deterministic DER signature equal the vector; base64 == Node Buffer base64.
  - `companion-daemon/internal/devicetrust/mobile_vectors_test.go` — the daemon
    `ParseP256PublicKey` + `Fingerprint` + `ecdsa.VerifyASN1` accept the SAME
    bytes. Any drift fails both.

## M3-auth-2 (PLANNED) — pairing client

- Extend `ConnectScreen` QR handling to detect a pairing payload
  (`{sessionId,hostId,fingerprint,hostPubKey,bootstrapToken,endpoint,expiresAt}`).
- POST `<endpoint>` (`/pair`) `{publicKey(b64 SPKI), displayName,
  phoneNonce(b64,32), bootstrapToken}` → `ChallengeResponse`; pin-check
  `hex(sha256(hostPublicKey)) == QR.fingerprint`; sign the pairing transcript;
  POST `/pair/confirm` `{phoneSignature(b64)}`; poll for operator approval;
  persist `{hostId, hostPubDER, deviceId, baseURL}`.

## M3-auth-3 (PLANNED) — session auth + authenticated transport

- Challenge → verify host signature against the pinned host key → verify →
  bearer `{token, expiresAt, permissions}`; store + refresh on expiry/401.
- Centralized authenticated REST client sourcing the bearer; replace the
  `token` prop origin in `App.tsx`/`connection.tsx`.
- WS ticket: `POST /api/device-auth/ws-ticket` → connect
  `/term/ws?session=&ticket=`.

## Sequence
M3-auth-1 ✅ → M3-auth-2 → M3-auth-3 → authenticated M3a re-verify → M3b.
Dev-token / Supabase remain build-time local-test only.
