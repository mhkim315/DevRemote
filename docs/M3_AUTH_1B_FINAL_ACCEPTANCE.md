# M3-auth-1B Final Acceptance

Status: **ACCEPT**

Accepted branch: `feature/phase10-multi-adapter`

Accepted commit:

```text
a173fcffc474490a9f96fbfa10c8ac9e12557d35
```

Accepted baseline: M3-auth-R1 `a834955c15a9e95535364184a7467a07587745ed`.

M3-auth-1B replaces the fail-closed iOS stub with a Secure Enclave P-256
DeviceKey provider that preserves the accepted Android and daemon wire contract.
The implementation and three narrowly scoped remediations were independently
reviewed before this acceptance.

## Accepted product and security contract

- The private key is created in the iOS Secure Enclave and is never exported to
  JavaScript or persisted as software key material.
- The provider accepts only Secure-Enclave-backed P-256 signing keys and fails
  closed when the enclave or key is unavailable.
- Public identity is the canonical 91-byte P-256 SPKI used by Android and Go;
  `deviceId = hex(SHA-256(SPKI))`.
- Signing uses SHA-256 with P-256 ECDSA and returns X9.62/ASN.1 DER compatible
  with Go `ecdsa.VerifyASN1`.
- Only an exact `key_missing` result grants `ensureKey()` creation ownership.
  Existing inaccessible or incompatible identities are never silently replaced
  or deleted.
- `hasKey()` returns `false` only for a missing key. Other native failures retain
  a typed error code across the Expo bridge.
- A failed validation rolls back only a key created by that invocation, and a
  rollback failure is surfaced instead of hidden.
- There is no iOS software-key or JavaScript-private-key fallback.
- Shared pairing, challenge, bearer, permission, revoke, audit, and WS-ticket
  wire contracts remain operating-system neutral. No daemon protocol change was
  required.

## Accepted verification evidence

- Standalone Swift typecheck of `PokitDeviceKeyStore.swift`: PASS.
- Mobile TypeScript and Jest DeviceKey contract tests: PASS.
- Go mobile-vector compatibility tests: PASS; real-device fixture remains
  skip-if-absent by design.
- Full `scripts/build-gate.sh`: **ALL GATES PASSED**.
- The iOS native gate now:
  - opts the CocoaPods `Tests` test spec into the generated Podfile;
  - requires a physical-device UDID;
  - requires the generated XCTest scheme;
  - preserves the real `xcodebuild` exit status;
  - rejects zero-test and skipped-enclave runs;
  - requires the Secure Enclave sign/verify test to run and pass.

Implementation and remediation history:
`docs/M3_AUTH_1B_IMPLEMENTATION_REPORT.md`.

## Remaining release gate

Running `sh scripts/ios-native-gate.sh` on a connected physical iPhone and the
subsequent real-device product smoke remain explicit **M-track** gates. They were
not claimed as executed in the implementation environment. This does not reopen
the accepted code review, but an iOS beta must not ship until those hardware
checks pass and the iOS-to-Go fixture is captured.

## Next phase

`M3-auth-2B` — iOS QR pairing, bearer, WS-ticket, and Terminal end-to-end
integration. Use `docs/NEXT_SESSION_M3_AUTH_2B_HANDOFF.md`.
