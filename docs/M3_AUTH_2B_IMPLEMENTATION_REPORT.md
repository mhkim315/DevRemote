# M3-auth-2B Implementation Report — iOS Authenticated Product Integration

Status: **READY FOR INDEPENDENT VERIFICATION**

Branch: `feature/phase10-multi-adapter`

Required baseline (accepted M3-auth-1B): `a173fcffc474490a9f96fbfa10c8ac9e12557d35`
— `git merge-base --is-ancestor a173fcffc HEAD` exits 0.

Task: make the already-shared QR pairing, challenge/bearer, WS-ticket, and
Terminal product path operate correctly with the accepted iOS Secure Enclave
provider (M3-auth-1B). No Android or daemon security redesign.

## 1. Finding — the chain is cross-platform; the native surface was the DeviceKey

The pairing→bearer→WS-ticket→Terminal flow is cross-platform TypeScript. There is
**no `Platform.OS` branch that excludes iOS** from the auth/pairing/terminal path
(the `Platform.OS` uses are cosmetic). `expectedProvider()` already returns
`ios_secure_enclave` on iOS. The only native surface, the DeviceKey, is accepted
(M3-auth-1B). So M3-auth-2B is production-path re-verification on iOS plus the
smallest change needed to make that path testable — not a re-implementation.

## 2. Changes

- **`mobile/src/lib/connectPairing.ts`** (new): `pairFromScannedQR(deps)` and
  `isPairingQR(data)` — the production QR-pairing transition **extracted from
  ConnectScreen** so the real UI→pairing path is testable end-to-end. It runs
  Secure-Enclave-signed pairing, persists the canonical origin, and installs
  trusted auth (`onPaired`) BEFORE connecting; any failure calls `onReject` once
  (scanner re-enabled, no partial paired state, no legacy fallback).
- **`mobile/src/screens/ConnectScreen.tsx`**: now a thin caller of
  `pairFromScannedQR` (behavior preserved; the QR branch is identical in effect).
  This is platform-neutral — Android uses the same path, so Android is not
  weakened.
- **`mobile/__tests__/m3aAuthIOSIntegration.test.ts`** (new, 14 tests): the iOS
  production-path proof (below).
- **Docs**: this report; roadmap ticks M3-auth-1B ACCEPT and M3-auth-2B
  code-complete.

No daemon change, no Android logic change, no shared-DTO change.

## 3. Exact iOS auth chain (all proven)

```text
Secure Enclave DeviceKey (ensureKey/getKeyInfo/sign; ios_secure_enclave)
  → ConnectScreen scan → pairFromScannedQR → pairAndSave → conductPairing
      (SE-signed transcript, pinned host proof) → canonical origin persisted
  → completePairing → atomic paired_device AuthContext (origin-bound)
  → TokenManager challenge/verify (SE-signed device transcript) → device bearer
  → host-bound authenticated REST (bearer only to the paired origin)
  → getWSTicket → one-time 64-hex ticket
  → TerminalController.bootstrap → ticket-only WebView (no bearer/key in HTML)
  → reconnect rotates tickets; stale attempts discarded
```

## 4. Automated evidence matrix (handoff §6)

Production functions, not helper copies. New = `m3aAuthIOSIntegration.test.ts`
(iOS platform, wrapped Secure Enclave key). Existing = accepted platform-agnostic
suites that run identically on iOS (green in the gate).

| Required proof (handoff §6) | Evidence |
|---|---|
| production ConnectScreen invokes the real pairing/save transition | ConnectScreen → `pairFromScannedQR`; New: "approved scan installs onPaired BEFORE connect (iOS key)", "rejected / thrown / missing-base fail closed" |
| iOS provider is `ios_secure_enclave`; missing native fails closed | New: "wrapped iOS DeviceKey accepted as ios_secure_enclave", "missing native module fails closed"; `deviceKey.test.ts` iOS cases |
| pairing transcript / host proof / identity / origin use shared bytes | New: "conductPairing reaches approved, SE-signed"; `pairingClient.test.ts` (full protocol, host proof, endpoints, identity/role checks) |
| pairing failure never enters `paired_device` | New: "rejected/thrown fail closed", "cold-start rejects mismatched/missing" |
| cold-start restore retains identity, rejects corrupted/mismatched | New: "completePairing matching origin → paired_device", "mismatched origin → failed / missing → pairing_required" |
| challenge/verify canonical transcript + strict DTO | New: "bearer signed by the SE key"; `authClient.test.ts` (host-sig, DTO, permission, token format) |
| bearer refresh singleflight + stale 401 cannot erase newer bearer | `authClient.test.ts` (singleflight, forceRefresh, stale-401-returns-newer) |
| REST never sends the bearer to another origin | New: "host-bound refuses iOS bearer to a non-paired origin (0 requests)"; `deviceAuthTransport.test.ts` (all URL variants) |
| ticket is exactly lowercase 64-hex, session-bound | New: "getWSTicket 64-hex ticket, session query, Bearer header"; `wsTicket.test.ts` |
| malformed/failed ticket never opens the Terminal | `wsTicket.test.ts` (rejects invalid ticket/TTL); `TerminalController.bootstrap` awaits `getWSTicket` so a throw prevents any result |
| reconnect rotates tickets A → B → C without reuse | `terminalController.reconnectTicket` singleflight; `terminalGeneratedScript.test.ts` |
| session switch / unmount / background discard stale attempts | `terminalController.shouldIssueReconnect` (connId/attemptId/session guard) + `gen` cancellation |
| no bearer/credential/key/signature in URLs/state/logs | New: "Terminal bootstrap injects ONLY the ticket — never the bearer, device SPKI, or host key" |
| daemon replay/wrong-session/replacement/revoke/expiry stay green | Go `internal/devicetrust` suites (ws-ticket contract, registry, revoke/expiry) — run under `go test -race` in the gate |

## 5. Verification

Ran here (all green):
- `npx tsc --noEmit`: PASS
- `npx jest`: PASS (18 suites, **229** tests; +14 new iOS integration)
- `sh scripts/build-gate.sh`: **ALL GATES PASSED** (backend `go test -race`,
  mobile typecheck + jest, Android Kotlin compile, invariants, security scan)
- `sh -n scripts/ios-native-gate.sh`: PASS (syntax)

Deferred to **M-track** (explicitly not claimed as passed — no Simulator
substitute):
- `sh scripts/ios-native-gate.sh` on a real iPhone (Secure Enclave XCTest;
  capture `ios_signature_fixture.json` → `internal/devicetrust/testdata/` → run
  `go test -run TestIOSFixture`).
- Physical iOS product smoke (handoff §7): SE key create + restart-persistence,
  LAN QR pair + local approval, HTTPS cold start without Supabase, session
  list/create + one-time ticket Terminal, bidirectional IO + geometry mirror,
  reconnect with fresh tickets, replacement/revoke/expiry closes Terminal,
  delete/wipe requires re-pair. Not run in this environment.

## 6. iOS config

`mobile/app.json` (Expo source of truth) is already complete for the iOS flow:
`ios.bundleIdentifier`, `ios.infoPlist.NSCameraUsageDescription`, and the
`expo-camera` plugin. The generated `mobile/ios/` project is git-ignored
(`/ios/` in `mobile/.gitignore`; regenerated by `expo prebuild`), so
Info.plist/PrivacyInfo/deployment-target edits are neither committable nor
durable. Pinning `ios.deploymentTarget=16.4` via `expo-build-properties`
(matching the podspec) is a P1/distribution hardening (adds a dependency + full
prebuild) and is not a code-correctness gap.

## 7. Out of scope

Android redesign, daemon protocol redesign, M3b Stop/Kill/Delete, Transcript/R1,
biometric step-up, Windows, iOS distribution packaging beyond the P1 note.

## 8. Verifier prompt

> Independently verify M3-auth-2B on `feature/phase10-multi-adapter`. Confirm
> `git merge-base --is-ancestor a173fcffc HEAD` exits 0. Confirm ConnectScreen
> now invokes `pairFromScannedQR` (`mobile/src/lib/connectPairing.ts`) and that
> the extraction preserves behavior (onPaired BEFORE connect; onReject once on
> any failure; no legacy fallback). Read `m3aAuthIOSIntegration.test.ts` and
> confirm it drives the REAL production functions with a wrapped iOS Secure
> Enclave DeviceKey under `Platform.OS='ios'`: provider `ios_secure_enclave` +
> missing-native fail-closed; `conductPairing` SE-signed → approved;
> pair-failure/cold-start never enters `paired_device`; `TokenManager` bearer
> signed by the SE key; 64-hex ticket; the Terminal bootstrap HTML contains ONLY
> the ticket (never the bearer/SPKI/host key); host-bound refusal to a non-paired
> origin (0 fetches). Cross-check the matrix's "Existing" citations. Run
> `sh scripts/build-gate.sh` (ALL GATES PASSED) and `sh -n scripts/ios-native-gate.sh`.
> Treat the iOS native gate and physical iOS product smoke as explicit **M-track**
> release gates — no Simulator substitute. Report a BLOCKER only for: a software
> key fallback, the bearer sent to a wrong origin or exposed in Terminal
> URL/state, a Supabase/dev token authorizing a remote op, pairing failure
> entering `paired_device`, ticket reuse, or any daemon-contract drift.
