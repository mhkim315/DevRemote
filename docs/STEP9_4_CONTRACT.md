# STEP 9.4 — Secure Accountless Onboarding Contract

**Status:** IMPLEMENTATION CONTRACT — ACCEPTED at `238f063a8`

**Branch:** `feature/canonical-timeline-foundation`  
**Prerequisite:** Step 9.3 accepted at `f7033b86c`  
**Purpose:** accountless onboarding from a clean macOS install through a trusted
Android device, without adding an account-registration authority.

## 1. Scope and frozen authority boundary

Step 9.4 packages and composes existing production-live trust primitives. It
does not add a user account, cloud identity, a second pairing authority, or a
new terminal/approval authority.

The authoritative reuse seams are `internal/devicetrust`:

- `HostIdentity` and `DeviceRegistry` remain the host and device authorities.
- `ChallengeStore` remains the single-use challenge authority.
- `DeviceSessionManager` remains the bearer/session authority.
- `AuthHandler`, `RequirePrincipal`, and the existing pairing protocol remain
  the only device-auth admission paths.

**Must remain unchanged:** Timeline writer/projection, Transcript, Terminal
authority, approval authority, runtime lifecycle authority, and `Recorder`.
Implementation may import existing production-live code only; it must not copy
or fork trust logic into a new registry or protocol.

**Amendment 1 (9.4-B QR mediation):** internal/devicetrust PairingRequest may
add optional QR metadata fields (host ID, daemon boot ID, challenge ID,
expires-at). These fields are verified by the QR bridge before the existing
pairing protocol proceeds. PairingHost must reject a candidate whose QR
metadata does not match the bridge stored binding. This is a narrow mediation
seam; it does not authorize a second pairing authority, alternate device
registration, or weakening of ChallengeStore/DeviceSessionManager/
DeviceRegistry/HostIdentity ownership.

## 2. 9.4-A — macOS installation and bootstrap

### 2.1 Distribution path

The supported release path is Homebrew:

```text
brew install pokit
pokit doctor
pokit daemon install
pokit daemon start
```

The formula installs the signed/versioned `pokit` CLI and daemon artifact.
**Alpha note:** code signing infrastructure is deferred; Base Alpha uses
source-built artifacts with SHA-256 verification. The CLI is the only public
bootstrap interface; it owns path discovery, version
reporting, and delegation to the daemon service manager. No installer may ask
for, create, or transmit an account credential.

### 2.2 LaunchAgent lifecycle

`pokit daemon install` installs a per-user LaunchAgent. It must use a
versioned daemon path, a user-writable state directory with restrictive file
permissions, and a deterministic label. `start`, `stop`, `status`, and
`uninstall` must be idempotent. A failed install must remove a newly created
plist/state only when this invocation created it; it must not remove a known
working prior installation.

### 2.3 Upgrade, uninstall, rollback

| Operation | Required behavior |
|---|---|
| Upgrade | Stop only the managed LaunchAgent, atomically replace the binary/formula version, start it, then run readiness checks. Preserve host identity and registered devices. |
| Upgrade failure | Restore the immediately preceding executable/service definition before reporting failure. Never silently run an unknown partial binary. |
| Uninstall | Stop and remove the LaunchAgent and installed executable. Device trust state is removed only by an explicit `--purge-trust` confirmation. |
| Bootstrap failure | Return a typed failure with failed phase, retain diagnostic logs, and leave no running duplicate daemon. |

### 2.4 Readiness diagnosis

`pokit doctor` must report separate, non-authoritative diagnostics for: CLI
version/path, daemon LaunchAgent state, daemon listener/auth readiness, host
identity availability, device-registry readability, and detected provider
readiness. Provider absence is a diagnosis, not a reason to weaken pairing or
start a terminal authority. Diagnostics must redact tokens, QR bootstrap
material, private keys, and bearer credentials.

## 3. 9.4-B — QR pairing and device trust

### 3.1 QR payload and bindings

The QR code is bootstrap material, not a bearer credential. Its minimum
payload is protocol version, LAN origin, host public-key/host identifier,
daemon boot identifier, session/challenge identifier, expiry, and one-time
bootstrap value. It must not include user data, device private material,
session bearer tokens, approval data, Timeline/Transcript content, or a
long-lived device credential.

The existing pairing/challenge transcript binds **host identity, device
identity/public key, daemon boot ID, challenge/session ID, client nonce,
server nonce, and expiry**. The device verifies the host proof before treating
the origin as paired; the host verifies device proof of possession before
registering the device or issuing a bearer.

### 3.2 Single use, expiry, replay, and cancellation

- A challenge is consumed exactly once by `ChallengeStore.Consume`; successful
  verification makes any replay reject.
- Expired, boot-mismatched, host-mismatched, device-mismatched, malformed, or
  already-consumed material fails closed and issues no session.
- Cancellation, timeout, rejection, and host shutdown invalidate the pending
  pairing state and issue no device record/token.
- Concurrent attempts are isolated by challenge ID. At most one confirmation
  wins a particular single-use challenge; all losers receive a typed rejected,
  expired, or consumed result. They must not overwrite the winner's device
  record or session.
- The host operator approval/rejection boundary remains explicit where the
  existing pairing flow requires it. Scanning a QR code alone never grants
  authority.

## 4. 9.4-C — Android non-exportable key lifecycle

The Android implementation uses Android Keystore for a hardware-backed when
available, non-exportable private signing key. Application code receives only
the public-key encoding, key alias/reference, and signing operation; it must
never serialize private-key bytes into AsyncStorage, logs, QR data, backups,
or network payloads.

1. First pairing generates a fresh key and stable device identifier, then
   proves possession by signing the existing `internal/devicetrust` transcript.
2. Restart reopens the same key alias and re-verifies the persisted pairing
   against that key before installing a device bearer.
3. Missing alias, key invalidation, unreadable keystore, pairing corruption,
   or key/public-identity mismatch is a terminal local trust failure: clear
   only unusable local bearer state, show recovery, and require a new pairing.
4. Reinstall is a new device-key identity unless a platform-supported secure
   restore demonstrably preserves the same non-exportable key. It never falls
   back to a software/exportable key or silently reuses a stale bearer.

## 5. 9.4-D — revoke, recovery, replacement, and frozen concurrency contract

### 5.1 Revocation and recovery

Revoking a device removes its registry authorization, device push binding, and
all bearer/session validity for that device. A revoked bearer fails at the next
authenticated request and cannot be refreshed. Lost-phone recovery is: use
another authorized local control surface, revoke the lost device, then pair a
replacement. Replacement devices always produce a fresh proof/key identity;
they do not inherit the old device's bearer.

### 5.2 Frozen epoch model

**This concurrency contract is frozen before implementation.** Each device
record owns a monotonically increasing authorization epoch. Every issued
device session/bearer is bound to `(deviceID, epoch, hostID, bootID)`.

- Pair/replace reads the current epoch and commits a new device record/session
  only if that epoch is still current at commit.
- Revoke increments the epoch before removing authorization. Any in-flight
  pair, refresh, push registration, or authenticated operation captured at an
  older epoch must fail stale and must not recreate authority.
- A device session manager validates the current registry epoch on admission
  and refresh; token expiry alone is not a revocation substitute.
- No packet may add ad-hoc cross-service mutexes to “solve” this race. The
  epoch check is the ordering primitive; package-local locks may protect only
  a single data structure's memory safety.
- Outcomes are linearized as either `committed at epoch N` or `stale/revoked
  at epoch < N`; there is no successful ambiguous outcome.

Required race tests cover revoke vs verify, revoke vs refresh, replacement vs
old-device use, concurrent confirmations, daemon restart/boot change, and
push registration after revoke.

## 6. 9.4-E — physical-device clean-install verification

This packet is a release gate, not an emulator substitute. The target path is
**clean macOS installation → Homebrew CLI/LaunchAgent → QR pairing → clean
SM-S926N Android install → Keystore proof → authenticated reconnect →
revoke/replacement recovery**.

Every physical-device action is **USER_ACTION_REQUIRED**. Automation may
prepare instructions and collect non-secret diagnostics, but must not claim
that a person scanned a QR code, unlocked a device, granted OS permission, or
observed a physical device result.

The validation record must contain exact artifacts: macOS version/architecture,
Homebrew/formula version and SHA, `pokit`/daemon version and SHA, LaunchAgent
label/status, host public-key fingerprint, daemon boot ID, Android model
`SM-S926N`, Android build fingerprint, APK application ID/versionCode/SHA-256,
Keystore public-key fingerprint (never private material), pairing timestamps,
and redacted pass/fail diagnostics for every journey phase.

## 7. Implementation sequencing and gates

1. Freeze the A bootstrap interface and the D epoch contract in this document.
2. Implement A interface/readiness; then B and C may proceed in parallel only
   with disjoint writers and the same frozen trust primitives.
3. Implement D only after B/C identities are available; integrate all packets
   at one SHA before review.
4. Run code/test gates, then execute E with `USER_ACTION_REQUIRED` evidence.

## 8. Stop conditions

Stop and reject if a change:

- adds account registration, cloud identity, or an alternate pairing authority;
- exports, logs, backs up, or serializes an Android private key;
- issues a device bearer without a single-use, bound proof of possession;
- allows replay, expiry/boot mismatch, cancellation, or stale epoch to create
  or restore authority;
- replaces the frozen epoch model with unbounded/ad-hoc locking;
- weakens `internal/devicetrust`, approval, Timeline, Transcript, Terminal, or
  runtime authority boundaries;
- treats emulator evidence as SM-S926N physical-device proof; or
- claims a physical-device action without `USER_ACTION_REQUIRED` evidence.

