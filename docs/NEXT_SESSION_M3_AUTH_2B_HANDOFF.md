# Pokit Executor Onboarding — M3-auth-2B iOS Authenticated Product Integration

Status: **READY — begin only from the accepted M3-auth-1B baseline**

Canonical repository:

```text
https://github.com/mhkim315/DevRemote.git
```

Branch:

```text
feature/phase10-multi-adapter
```

Required accepted baseline:

```text
a173fcffc474490a9f96fbfa10c8ac9e12557d35
```

Baseline subject:

```text
fix(M3-auth-1B): capture xcodebuild's real exit code in the iOS native gate
```

M3-auth-1B verdict: **ACCEPT**. Acceptance record:
`docs/M3_AUTH_1B_FINAL_ACCEPTANCE.md`.

This is the authoritative onboarding document for the next execution agent.
The task is `M3-auth-2B`: make the already shared QR pairing, challenge/bearer,
WS-ticket, and Terminal product path operate correctly with the accepted iOS
Secure Enclave provider. Do not redesign accepted Android or daemon security.

## 0. Mandatory repository recovery

If this commit or document is not visible locally, do not reconstruct the task
from conversation memory. Run exactly:

```sh
git remote set-url origin https://github.com/mhkim315/DevRemote.git
git fetch origin feature/phase10-multi-adapter
git show a173fcffc474490a9f96fbfa10c8ac9e12557d35 --stat
git show origin/feature/phase10-multi-adapter:docs/NEXT_SESSION_M3_AUTH_2B_HANDOFF.md
```

For a clean worktree on the feature branch:

```sh
git switch feature/phase10-multi-adapter
git rebase origin/feature/phase10-multi-adapter
```

Then verify:

```sh
git rev-parse HEAD
git merge-base --is-ancestor a173fcffc474490a9f96fbfa10c8ac9e12557d35 HEAD
```

The ancestry command must exit `0`. If it does not, stop. If local changes
exist, preserve them; do not reset, force-checkout, or discard another agent's
work.

## 1. Product context and accepted chain

Pokit owns a controlled PTY on the daemon host. One session-owned Recorder is
the only PTY reader and fans raw output to local/mobile/web viewers.

The Android track has already accepted this product chain:

```text
hardware-backed DeviceKey
→ LAN QR pairing and pinned host identity
→ host-signed challenge verification
→ device-signed authentication transcript
→ short-lived device bearer
→ host-bound authenticated REST
→ one-time, host/session/bearer-bound WS ticket
→ ticket-only Terminal WebSocket
→ replacement/revoke/expiry invalidation
```

The iOS Secure Enclave provider now implements the same DeviceKey interface and
wire bytes. M3-auth-2B must reuse the shared chain and close only actual iOS
product-integration gaps.

## 2. Required reading before editing

Read these files in order:

1. `docs/M3_AUTH_1B_FINAL_ACCEPTANCE.md`
2. `docs/M3_AUTH_1B_IMPLEMENTATION_REPORT.md`
3. `docs/M3_AUTH_MOBILE_DEVICE_CLIENT_PLAN.md`
4. `docs/M3_AUTH_CROSS_PLATFORM_DEVICE_KEY_PLAN.md`
5. `docs/M3_AUTH_4A_FINAL_ACCEPTANCE.md`
6. `docs/M3_AUTH_R1_FINAL_ACCEPTANCE.md`
7. `mobile/modules/pokit-device-key/src/index.ts`
8. `mobile/modules/pokit-device-key/ios/PokitDeviceKeyStore.swift`
9. `mobile/src/lib/{deviceIdentity,qrParser,pairingClient,pairingStore,pairAndSave}.ts`
10. `mobile/src/lib/{authClient,authTransport,wsTicket,terminalController,authMode}.ts`
11. `mobile/App.tsx`, `mobile/src/screens/ConnectScreen.tsx`, and
    `mobile/src/screens/FeedScreen.tsx`
12. daemon pairing/auth/ticket handlers under
    `companion-daemon/internal/devicetrust/` and
    `companion-daemon/internal/term/pty.go`

First trace the production path. Do not assume that a shared helper is reachable
from the iOS UI merely because it exists.

## 3. Scope and required outcome

Prove or minimally fix the real iOS path:

```text
pairing_required
→ ConnectScreen QR scan
→ pairAndSave
→ Secure Enclave DeviceKey sign
→ local operator approval/result
→ canonical paired origin persisted
→ atomic paired_device AuthContext
→ challenge/verify bearer
→ authenticated REST/session discovery
→ one-time WS ticket
→ Terminal bootstrap and ticket-bound WebSocket
→ fresh ticket on every reconnect
```

Required behavior:

- iOS uses `ios_secure_enclave`; it never falls back to a JS/software key.
- Pairing success is committed only after host proof, local approval, exact
  device identity, role, and canonical operational origin validation.
- Cold start restores the same Secure Enclave public identity and validates the
  stored host/origin before creating `TokenManager`.
- The bearer is memory-only and appears only in the authenticated REST
  `Authorization` header.
- Terminal URLs and WebView state contain only the one-time ticket and minimum
  canonical session context—never bearer, Supabase JWT, dev token, private key,
  nonce, or signature.
- Each connection/reconnect receives a fresh ticket. Session switch, unmount,
  and background transitions discard stale asynchronous results.
- Pairing/auth/ticket failure keeps or returns the user to a recoverable,
  fail-closed UI. No legacy remote fallback is allowed.
- Replacement, revoke, expiry, wrong host/session/device, and replay continue to
  fail closed under the accepted daemon contract.

## 4. Invariants that must not change

1. Recorder remains the only controlled-PTY reader.
2. Recorder subscriber bytes remain raw PTY output only.
3. Client/server binary frames are terminal bytes; text frames use the accepted
   closed control vocabulary.
4. Mobile viewer disconnect never becomes process-lifecycle authority.
5. Remote mode uses device bearer for REST and WS ticket for Terminal only.
6. `explicit_local_dev` is the only legacy-token mode.
7. Host and session identity come from accepted principals/tickets, never
   request-provided role or permission fields.
8. Do not weaken Android behavior to accommodate iOS.
9. Shared wire DTOs remain OS-neutral. Platform-specific key storage stays in
   native provider code.

## 5. Work method

1. Establish an evidence matrix for each production transition: caller,
   credential, origin, persistence, failure state, and existing automated proof.
2. Run the existing relevant tests before editing.
3. Identify concrete iOS-only gaps. Prefer shared production code when it is
   genuinely platform-neutral; otherwise make the smallest native/iOS boundary
   change.
4. Add production-path tests, not copies of helper logic.
5. Keep commits narrow and reviewable. Do not mix unrelated cleanup.
6. Run the complete gate, write an implementation report, commit, push, and
   stop for independent verification.

Do not claim completion based only on TypeScript types, Jest mocks, or existence
of helper functions.

## 6. Automated acceptance evidence

At minimum add or retain tests proving:

- production ConnectScreen invokes the real pairing/save transition;
- iOS provider selection is `ios_secure_enclave` and unsupported/missing native
  module fails closed;
- pairing transcript, host proof, stored identity, and origin binding use the
  shared accepted bytes;
- pairing failure never enters `paired_device`;
- cold-start restore retains the same identity and rejects corrupted/mismatched
  pairing state;
- challenge/verify uses the canonical transcript and strict DTO validation;
- bearer refresh is singleflight and stale 401 cannot erase a newer bearer;
- authenticated REST never sends the bearer to another origin;
- ticket format is exactly lowercase 64-hex and ticket issuance is session-bound;
- malformed/failed ticket issuance never opens the Terminal;
- initial connection and reconnect rotate tickets A → B → C without reuse;
- session switch, unmount, and background recovery discard stale attempts;
- no bearer, reusable credential, key material, or signature enters URLs,
  persisted state, logs, snapshots, or errors;
- accepted daemon replay/wrong-session/replacement/revoke/expiry proofs remain
  green.

Run:

```sh
sh scripts/build-gate.sh
sh -n scripts/ios-native-gate.sh
```

Where a physical iPhone is available, also run:

```sh
sh scripts/ios-native-gate.sh
```

Capture `ios_signature_fixture.json`, place it at the documented Go testdata
path, and run the Go interoperability test. If hardware is unavailable, report
the native/product gates as explicitly unexecuted M-track evidence—never PASS or
implicit success.

## 7. Physical iOS product gate

Before an iOS beta, verify on a real iPhone:

- Secure Enclave support and key creation;
- app restart preserves SPKI/deviceId;
- native signature verifies in Go and wrong message/key/signature fails;
- LAN QR pairing plus local approval;
- HTTPS operational origin and cold-start restore;
- challenge/verify bearer without Supabase;
- session list/create and one-time ticket Terminal;
- bidirectional terminal IO and geometry mirroring;
- reconnect with fresh tickets after background/foreground;
- replacement/revoke/expiry closes active Terminal access;
- delete/app-data wipe requires re-pairing and never resurrects old identity.

If this cannot be run in the executor environment, leave it as an explicit
release gate with honest evidence. Do not replace it with Simulator results.

## 8. Explicitly out of scope

- Android redesign or weakening accepted Android gates;
- daemon authentication/protocol redesign without a demonstrated production
  defect;
- M3b Stop/Kill/Delete UX;
- Transcript/R1 runtime-signal implementation;
- biometric step-up UX;
- Windows implementation or speculative Windows abstractions;
- ConPTY, CNG/TPM, DPAPI, Named Pipes, Windows Services, or host-key providers.

Windows forward compatibility means only that shared protocols remain OS-neutral.
Do not add Windows acceptance blockers.

## 9. Done and stop conditions

The executor may report M3-auth-2B code complete only when:

- the real production path is wired and automatically evidenced;
- all available gates pass;
- physical-device evidence is either actually recorded or honestly marked as an
  outstanding M-track release gate;
- `docs/M3_AUTH_2B_IMPLEMENTATION_REPORT.md` records files, contracts, tests,
  gate output, limitations, and a verifier prompt;
- changes are committed and pushed to `feature/phase10-multi-adapter`;
- the worktree is clean.

Then stop. Do not begin M3b, Transcript, Android rework, or Windows work before
independent verification.
