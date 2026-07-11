# M3-auth Cross-platform Device Key Plan

Status: approved remediation plan; M3-auth-1C is NEXT

Branch: `feature/phase10-multi-adapter`

Rejected implementation baseline: `e55d835f5`

Android is the first release target, but Pokit's device identity is a
cross-platform security contract. Android and iOS must expose the same TypeScript
surface and comparable non-exportable P-256 signing guarantees. An unimplemented
platform fails closed; it never falls back to a JavaScript private key.

## 1. Verification result for `e55d835f5`

### Accepted for reuse

- canonical P-256 SPKI DER and device fingerprint contract;
- Go/mobile public-key interoperability vectors;
- removal of the JavaScript production private-key/signing path;
- strict hexadecimal decoder behavior;
- strict standard padded Base64 decoder behavior;
- Android Keystore algorithm intent: `secp256r1`, `PURPOSE_SIGN`, SHA-256,
  `SHA256withECDSA`.

### Rejected as a production device-key implementation

1. The native code is written directly into generated `mobile/android/app`
   files and manually registered in `MainApplication.kt`. A clean
   `npx expo prebuild --platform android --no-install` deleted the module,
   package, and registration. The implementation is not reproducible.
2. The repository does not contain a complete tracked Gradle project, so the
   normal build gate never compiled the Kotlin source. The submitted Kotlin also
   lacks imports for `KeyGenParameterSpec` and `KeyProperties`.
3. No Android Keystore security-level check proves TEE/StrongBox backing or
   applies an explicit unsupported-device policy.
4. `hasKey()` followed by `generateKey()` is not an atomic `ensureKey()`.
   Concurrent callers may replace the identity.
5. The JavaScript wrapper directly uses an Android-only `NativeModules` surface.
   There is no common provider contract and no explicit iOS/Web fail-closed
   implementation.
6. Jest proves protocol bytes, not Android Keystore execution. No compiled
   instrumentation or physical-device evidence exists yet.

M3-auth-2 must not start until M3-auth-1C and M3-auth-1A are independently
accepted.

## 2. Security invariants

These are hard product contracts:

- the device private scalar never crosses the native/JavaScript boundary;
- the device private scalar is never stored in SecureStore, AsyncStorage,
  files, logs, DTOs, errors, or test fixtures;
- Android uses an `AndroidKeyStore` non-exportable P-256 signing key;
- iOS uses a Secure Enclave non-exportable P-256 signing key;
- hardware attestation is a future enhancement, not a reason to allow an
  exportable software key;
- production has no JavaScript key-generation or private-key signing fallback;
- unsupported, unavailable, missing, or invalidated providers fail closed and
  disable pairing;
- an identity mismatch never silently generates a replacement key for an
  already-paired host; it requires explicit re-pairing;
- every platform returns the same canonical P-256 SPKI DER and ASN.1 DER ECDSA
  signature format accepted by the Go daemon.

## 3. Common TypeScript contract

The native alias is implementation-owned and versioned. Arbitrary aliases are
not accepted from UI or network input.

```ts
export type DeviceKeyProvider =
  | 'android_keystore'
  | 'ios_secure_enclave';

export type DeviceKeySupport =
  | 'supported'
  | 'not_implemented'
  | 'hardware_unavailable'
  | 'key_missing'
  | 'key_invalidated';

export interface DeviceKeyInfo {
  provider: DeviceKeyProvider;
  keyVersion: 1;
  deviceId: string;
  publicKeySpki: Uint8Array;
  hardwareBacked: true;
}

export interface PokitDeviceKey {
  getSupport(): Promise<DeviceKeySupport>;
  hasKey(): Promise<boolean>;
  ensureKey(): Promise<DeviceKeyInfo>;
  getPublicKeySpki(): Promise<Uint8Array>;
  sign(message: Uint8Array): Promise<Uint8Array>;
  deleteKey(): Promise<void>;
}
```

Native modules may use canonical Base64 or hex internally at the bridge, but the
TypeScript wrapper exposes bytes and validates exact lengths/encodings. Private
key material is absent from every interface.

Native key alias/version:

```text
pokit.device.identity.v1
```

## 4. M3-auth-1C — Common Expo module contract (NEXT)

Create a local Expo module, for example:

```text
mobile/modules/pokit-device-key/
  expo-module.config.json
  src/index.ts
  android/.../PokitDeviceKeyModule.kt
  ios/PokitDeviceKeyModule.swift
```

Use Expo Modules API autolinking. Do not patch generated `android/app` or
`ios/` application files and do not manually edit `MainApplication.kt`.

### Required behavior

- one common TypeScript wrapper implements `PokitDeviceKey`;
- Android provider is discoverable through the same module contract;
- iOS initially returns `not_implemented` until M3-auth-1B;
- Web returns `not_implemented`;
- every operation except support inspection fails with a typed unsupported
  error when the provider is unavailable;
- no fallback imports `@noble/curves` private-key generation/signing;
- `@noble` may remain for public verification, hashing, and protocol fixtures;
- strict codecs reject odd/non-hex, invalid Base64, non-canonical padding, and
  caller-specific length mismatches;
- SPKI identity ingestion requires exactly 91 bytes, the P-256 SPKI prefix, and
  a valid uncompressed curve point;
- remove the obsolete `pokit.device.privkey` SecureStore value. Never import it
  into a native provider. Any previously paired development identity must be
  revoked and paired again.

### M3-auth-1C acceptance

- `npx expo prebuild --clean` preserves/autolinks the local module;
- generated application files contain no hand-maintained package registration;
- TypeScript tests prove unsupported platforms fail closed;
- tests prove no production software-key fallback is reachable;
- malformed codec/SPKI inputs fail closed;
- Android and iOS expose the same method/result vocabulary;
- backend, mobile typecheck/tests, and repository gates pass.

M3-auth-1C does not claim an accepted Android or iOS key implementation.

## 5. M3-auth-1A — Android Keystore provider

Implement the Kotlin side of the local Expo module.

### Key contract

```text
provider: AndroidKeyStore
algorithm: EC / secp256r1
purpose: SIGN
digest: SHA-256
signature: SHA256withECDSA → ASN.1 DER
alias: pokit.device.identity.v1
```

`ensureKey()` must be serialized/atomic. If a valid key exists, return its
identity. Otherwise generate exactly once. Never overwrite an existing paired
identity because two callers raced.

After generation and on load:

- confirm the entry is a private-key entry;
- confirm EC/P-256 and the expected signing authorization;
- inspect `KeyInfo` security level;
- require the approved hardware policy (TEE or StrongBox for the initial
  Android release); otherwise delete the unusable new entry and return
  `hardware_unavailable`;
- return the certificate public key's canonical SPKI DER;
- verify SPKI length/algorithm before returning it;
- `PrivateKey.encoded` must be null/unavailable;
- signing runs off the UI/JS thread;
- missing/invalidated keys return typed states and never auto-repair an existing
  host pairing.

### Android automated gates

- clean Expo prebuild;
- `./gradlew :app:compileReleaseKotlin`;
- release APK assembly or the existing reproducible Android build gate;
- native instrumentation:
  - concurrent `ensureKey()` returns one identity;
  - module recreation/app restart returns the same SPKI;
  - private key is non-exportable;
  - reported security level satisfies policy;
  - signature verifies with Java and Go;
  - wrong message/key fails;
  - delete removes the identity and requires re-pairing;
- Go runtime fixture accepts a non-deterministic native signature. Exact
  signature bytes are not required.

### Android manual gate

On the target Samsung device:

- generate and persist identity;
- record provider/security level without sensitive material;
- restart app and verify the same device ID;
- sign a challenge and verify it on the daemon;
- delete/invalidate and confirm fail-closed re-pair UX;
- verify no private material appears in JS logs, Metro, Logcat, storage dumps,
  or bridge DTOs.

Missing physical-device evidence is an M-track gate, but Kotlin compilation,
autolinking, non-exportability, and automated native behavior are E-track
acceptance requirements.

## 6. M3-auth-1B — iOS Secure Enclave provider

This may follow the first Android closed beta, but it is mandatory before iOS
support or iOS pairing is advertised.

Implement the Swift side of the same local Expo module:

```text
SecureEnclave.P256.Signing.PrivateKey
→ P-256 public x9.63 representation
→ canonical SPKI DER
→ Secure Enclave signature(for: message)
→ ECDSASignature.derRepresentation
```

### iOS storage and access policy

- create the key inside Secure Enclave; never import a software private key;
- store only the Secure Enclave opaque representation/reference plus alias and
  non-secret metadata in Keychain;
- use a ThisDeviceOnly, when-unlocked accessibility policy;
- MVP does not require Face ID/Touch ID for every signature;
- preserve a future biometric step-up phase without silently changing the
  existing key policy;
- `SecureEnclave.isAvailable == false`, simulator, missing Swift implementation,
  key invalidation, or Keychain/fingerprint mismatch all fail closed;
- no Keychain-only software P-256 fallback is allowed.

### iOS acceptance

- actual iPhone Secure Enclave key generation;
- same SPKI/device ID after app restart;
- Swift signature verifies in Go;
- wrong message/key fails;
- private material is absent from JS/native API;
- delete and invalidation require re-pairing;
- simulator returns unsupported rather than generating a software key;
- pairing, bearer, and WS-ticket integration pass on a physical iPhone before
  iOS beta.

## 7. Product sequencing

```text
M3-auth-1C  common Expo module + fail-closed providers       NEXT
M3-auth-1A  Android Keystore implementation + gates
M3-auth-2A  Android QR pairing and host pinning
M3-auth-3A  Android challenge/bearer lifecycle
M3-auth-4A  Android WS-ticket transport
             ↓
authenticated M3a re-verification
             ↓
M3b/M3c Android lifecycle UX + real-device gate
             ↓
Android closed beta
             ↓
M3-auth-1B  iOS Secure Enclave implementation + iPhone gate
             ↓
iOS pairing/bearer/WS integration validation
             ↓
iOS closed beta
```

Pairing, challenge, bearer, and WS-ticket TypeScript code must depend only on
`PokitDeviceKey`. Adding iOS must not require rewriting those protocols.

## 8. Explicit non-goals

- hardware attestation validation by the daemon;
- biometric approval for every signature;
- key backup or cross-device migration;
- importing the rejected JavaScript private key into native storage;
- iOS software fallback for simulator or devices without Secure Enclave;
- M3b lifecycle actions before authenticated M3a works remotely.

## 9. Executor handoff for the next commit

Implement **M3-auth-1C only**.

Start from `e55d835f5`, retain the accepted strict codecs and protocol fixtures,
and move the Android code out of generated application directories into a local
Expo module. Add the iOS/Web unsupported providers and common TypeScript
contract. Do not implement pairing, bearer authentication, WS tickets, or the
Swift Secure Enclave provider yet.

Required executor report:

```text
M3-auth-1C commit and baseline
Local Expo module layout and autolinking proof
Common JS/native contract
Android/iOS/Web provider behavior
Software-key removal/migration behavior
Strict codec/SPKI evidence
Clean prebuild result
Typecheck/tests/build-gate result
Known M3-auth-1A follow-ups
```

Stop after commit/push and request independent verification before M3-auth-1A.
