# PB-DG-R4 Device-Gate Closeout Evidence

**Status:** R4.1 + R4.2 + R4.3 COMPLETE — R4.4 BLOCKED (requires SM-S926N)

**IMPL SHA (candidate):** `c5b179a0a02342a44c468fe3563c6f272feb70f5`
**EVID SHA:** (this commit)
**Branch:** `feature/canonical-timeline-foundation`
**Date:** 2026-07-23

## R4.1 — Pairing V1 Transport Closure (Verified)

Production pairing handlers at `internal/devicetrust/pairing.go`:

| Requirement | Status |
|-------------|--------|
| Release cleartext retained (LAN HTTP one-time QR) | ✅ |
| Exact `/pair` path match | ✅ |
| Reject `/pair/`, other paths, wrong method | ✅ |
| Redirects rejected for all pair paths | ✅ |
| No bearer token over LAN HTTP pairing | ✅ |
| Operational origin HTTPS/WSS enforced | ⏳ R4.4 device smoke |
| Bootstrap-token single use | ✅ |
| Expiry | ✅ |
| Host-key proof | ✅ |
| Device-key proof | ✅ |
| Operator approval | ✅ |

Mobile pairing client (`mobile/src/lib/pairingClient.ts`):
- Phase 1 POST: `redirect: 'error'` via `pairingPOST` helper ✅
- Phase 2 POST: `redirect: 'error'` via `pairingPOST` helper ✅
- Result poll GET: `redirect: 'error'` added (`pairingClient.ts:149`) ✅

## R4.2 — Mandatory Automated Tests

### Go tests (`internal/devicetrust/pairing_test.go`)

| Test | Scenarios | Status |
|------|-----------|--------|
| `TestPairing_PathVariantRejection` | Exact POST /pair control + 8 path/method variants all rejected | ✅ PASS |
| `TestPairing_NoRedirectOnPairPaths` | All 3 pair paths reject 3xx via CheckRedirect | ✅ PASS |
| `TestPairing_NoBearerTokenOverCleartextOrigin` | Invalid bootstrap + valid bearer → rejected | ✅ PASS |
| `TestPairing_QuerylessPairedWebViewBootstrap` | Full 2-phase pairing → registry verification | ✅ PASS |

### Mobile tests (`mobile/__tests__/pairingClient.test.ts`)

| Test | Status |
|------|--------|
| Phase1 POST redirect → `network_error` | ✅ PASS |
| Phase2 POST redirect → `network_error` | ✅ PASS |
| Phase1 fetch options include `redirect: 'error'` | ✅ PASS |
| Result poll fetch options include `redirect: 'error'` | ✅ PASS |

Existing 13 Go pairing tests + all mobile pairing tests continue to PASS.

## R4.3 — Candidate Freeze + Artifact Build

### Candidate

```
SHA:       c5b179a0a02342a44c468fe3563c6f272feb70f5
Branch:    feature/canonical-timeline-foundation
Worktree:  0 untracked/modified files (fresh clone)
```

### Daemon

```
Go:        go1.26.4
VCS rev:   c5b179a0a02342a44c468fe3563c6f272feb70f5 (exact match ✅)
VCS mod:   false ✅
SHA-256:   529b4f8d0db243a4761918714c493cce9123bc6c3df96d3b93ea07523a46cb02
```

### APK

```
Build:     EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST unset (production)
           expo prebuild + gradle assembleRelease
SHA-256:   950d4af5cb480e4757ae834b49aacda306664b967e939b9de0b4e67f24cd383b
Size:      160,876,067 bytes
Assets:    PB_DEVICE_CANDIDATE_SHA.txt + DAEMON_SHA256.txt embedded
```

### Gate Results

```
go build ./...      PASS
go vet ./...        PASS
gofmt -l .          CLEAN
go test -race ./... ALL PASS
npx tsc --noEmit    PASS
npx jest            PASS (20/20 pairing client)
```

## R4.4 — SM-S926N Bounded Smoke

**STATUS: BLOCKED — requires physical Samsung SM-S926N device with Orca mobile app installed.**

## Artifact Preservation

```
/tmp/pokit-pb-device-artifacts/
├── PB_DEVICE_CANDIDATE_SHA.txt   (c5b179a0a02342a44c468fe3563c6f272feb70f5)
├── DAEMON_SHA256.txt             (529b4f8d0db243a4761918714c493cce9123bc6c3df96d3b93ea07523a46cb02)
├── APK_SHA256.txt                (950d4af5cb480e4757ae834b49aacda306664b967e939b9de0b4e67f24cd383b)
├── pokit-daemon                  (12,349,410 bytes)
└── pokit-app-release.apk         (160,876,067 bytes)
```

## Identity Book

| Identity | Value |
|----------|-------|
| `PB_DEVICE_CANDIDATE_SHA` | `c5b179a0a02342a44c468fe3563c6f272feb70f5` |
| `PB_ACCEPT_SHA` | **UNSET** (only independent verifier assigns) |
