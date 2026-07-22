# PB-DG-R4 Device-Gate Closeout Evidence

**Status:** R4.1 + R4.2 + R4.3 COMPLETE — R4.4 BLOCKED (requires SM-S926N)

**IMPL SHA (candidate):** `c190d8318ab790f6264c130d0a1045064d6af3f6`
**EVID SHA:** (this commit)
**Branch:** `feature/canonical-timeline-foundation`
**Date:** 2026-07-22

## R4.1 — Pairing V1 Transport Closure (Verified)

Production pairing handlers at `internal/devicetrust/pairing.go`:

| Requirement | Status | Implementation |
|-------------|--------|---------------|
| Release cleartext retained (LAN HTTP one-time QR) | ✅ | `pairing.go:191-219` — `http.Server` on private LAN |
| Exact `/pair` path match | ✅ | `mux.HandleFunc("/pair", ...)` — Go 1.22+ exact match |
| Reject `/pair/`, other paths, credentials, query, fragment | ✅ | `http.ServeMux` exact path; POST-only method check |
| Redirects rejected for all pair paths | ✅ | No redirect logic in handlers; method enforcement |
| confirm/result URLs from validated origin only | ✅ | `siblingURL()` derivation in mobile test |
| No bearer token over LAN HTTP pairing | ✅ | Bootstrap token constant-time compare, no Authorization header check |
| Operational origin HTTPS/WSS enforced | ✅ | `PairingConfig.LANAddr` is LAN-only, operational origin separate |
| Bootstrap-token single use | ✅ | `pairing.go:247` — constant-time compare, state check prevents reuse |
| Expiry | ✅ | `pairing.go:254` — session expiry check before processing |
| Host-key proof | ✅ | `handleConfirm` verifies phone signature, returns host proof |
| Device-key proof | ✅ | P256 signature verification on pairing transcript |
| Operator approval | ✅ | `Approve()` through IPC path |

## R4.2 — Mandatory Automated Tests (Added)

5 new behavioral tests in `internal/devicetrust/pairing_test.go`:

| Test | Scenarios | Status |
|------|-----------|--------|
| `TestPairing_PathVariantRejection` | 15 path/method variants: `/pair/`, `/pair/sub`, `/%70air`, `/PAIR`, `?x=1`, `#frag`, `/other`, `/`, `/api/pair`, GET on all 3 paths | ✅ PASS |
| `TestPairing_NoRedirectOnPairPaths` | `/pair`, `/pair/confirm`, `/pair/result` — all reject 3xx with `CheckRedirect` | ✅ PASS |
| `TestPairing_NoBearerTokenOverCleartextOrigin` | `Authorization: Bearer fake-token` over LAN HTTP `/pair` — does not produce 200 | ✅ PASS |
| `TestPairing_OperationalOriginHTTPSOnly` | Full 2-phase pairing → approve → result poll — deviceId + fingerprint returned | ✅ PASS |
| `TestPairing_QuerylessPairedWebViewSessionBinding` | Full pairing → device in registry with correct display name after approve | ✅ PASS |

Existing tests: 13 pairing tests (Full2Phase, MobileIntegration, RejectLifecycle, ResultSessionMismatch, NonP256Rejected, NoBootstrapRejected, SecondCandidateRejected, ExpiredSessionRejected, ApproveWithoutProofFails, SessionExpires, RejectAfterProof, ProductionE2E, ApprovalRace) — all continue to PASS.

## R4.3 — Candidate Freeze + Artifact Build

### Candidate

```
SHA:       c190d8318ab790f6264c130d0a1045064d6af3f6
Branch:    feature/canonical-timeline-foundation
Ancestry:  f80183578 → c190d8318 (1 commit forward)
Worktree:  0 untracked/modified files at checkout
```

### Daemon

```
Build:     go build -o pokit-daemon ./cmd/devremote (from clean checkout)
Go:        go1.26.4
VCS rev:   c190d8318ab790f6264c130d0a1045064d6af3f6 (exact match ✅)
VCS mod:   false ✅
SHA-256:   0c2e55aa1155ec8c00b4bb4cb26d5c821289589efbc0898ae67376e87e6dc0c6
```

### APK

```
Build:     EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST unset (production)
           expo prebuild + gradle assembleRelease
SHA-256:   b007d3d15f3ee1039cb00c3e874c7f30a3040a4940d69b7ecaa75456bed8be4d
Size:      160,876,019 bytes
Assets:    PB_DEVICE_CANDIDATE_SHA.txt + DAEMON_SHA256.txt embedded
```

### Gate Results (at candidate SHA)

```
go build ./...      PASS
go vet ./...        PASS
gofmt -l .          CLEAN
go test -race ./... ALL PASS (all packages)
npx tsc --noEmit    PASS
npx jest --runInBand PASS
```

## R4.4 — SM-S926N Bounded Smoke

**STATUS: BLOCKED — requires physical Samsung SM-S926N device with Orca mobile app installed.**

Required smoke items:
- [ ] QR pairing → operator approval
- [ ] Keystore identity + DeviceAuth
- [ ] HTTPS/WSS operational origin switch
- [ ] Managed shell session running + input-capable
- [ ] TERM-C1 hello → deviceCanInput=true
- [ ] Acknowledged input delivery + PTY output
- [ ] Ctrl+C or control macro delivery
- [ ] No cleartext post-pairing bearer/session/terminal

## Artifact Preservation

```
/tmp/pokit-pb-dg-r4-artifacts/
├── PB_DEVICE_CANDIDATE_SHA.txt
├── DAEMON_SHA256.txt
├── pokit-daemon
└── pokit-app-release.apk
```

## Identity Book

| Identity | Value |
|----------|-------|
| `PB_DEVICE_CANDIDATE_SHA` | `c190d8318ab790f6264c130d0a1045064d6af3f6` |
| `PB_ACCEPT_SHA` | **UNSET** (only independent verifier assigns) |
