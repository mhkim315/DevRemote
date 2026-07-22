# PB-DG-R4 Device-Gate Closeout Evidence

**Status:** R4.1 + R4.2 + R4.3 COMPLETE — R4.4 BLOCKED (requires SM-S926N)

**IMPL SHA (candidate):** `c190d8318ab790f6264c130d0a1045064d6af3f6`
**EVID SHA:** (this commit)
**Branch:** `feature/canonical-timeline-foundation`
**Date:** 2026-07-23

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
| Operational origin HTTPS/WSS enforced | ⏳ R4.4 device smoke | Automated HTTPS operational-origin infrastructure is not available; see explicit R4.2 scope note below |
| Bootstrap-token single use | ✅ | `pairing.go:247` — constant-time compare, state check prevents reuse |
| Expiry | ✅ | `pairing.go:254` — session expiry check before processing |
| Host-key proof | ✅ | `handleConfirm` verifies phone signature, returns host proof |
| Device-key proof | ✅ | P256 signature verification on pairing transcript |
| Operator approval | ✅ | `Approve()` through IPC path |

## R4.2 — Mandatory Automated Tests (Added)

4 automated behavioral tests in `internal/devicetrust/pairing_test.go`, plus one explicitly deferred HTTPS device-smoke assertion:

| Test | Scenarios | Status |
|------|-----------|--------|
| `TestPairing_PathVariantRejection` | Exact `POST /pair` control plus trailing-slash, sub-path, wrong-case, unrelated/nested path, and wrong-method rejection through real HTTP requests | ✅ PASS |
| `TestPairing_NoRedirectOnPairPaths` | `/pair`, `/pair/confirm`, `/pair/result` — all reject 3xx with `CheckRedirect` | ✅ PASS |
| `TestPairing_NoBearerTokenOverCleartextOrigin` | `Authorization: Bearer fake-token` over LAN HTTP `/pair` — does not produce 200 | ✅ PASS |
| HTTPS operational origin | Behavioral automated test deferred: no automated HTTPS operational-origin server infrastructure is available in this scope | ⏳ R4.4 SM-S926N smoke |
| `TestPairing_QuerylessPairedWebViewBootstrap` | Full pairing and approval → owner permission projection → native managed PTY session → bound WS ticket → real `Handlers.HandleWS` upgrade → hello session/generation/connectionId validation → wrong-session `session_not_found` with zero sequence → correct-session accepted ACK with matching identity and sequence 1 | ✅ PASS |

### Explicit HTTPS Scope Change

The prior `TestPairing_OperationalOriginHTTPSOnly` did not start HTTPS infrastructure or send an operational request; it only completed pairing over HTTP and checked the returned device identity. Retaining that name as automated HTTPS evidence would therefore be misleading. A behavioral automated HTTPS operational-origin test is deferred to R4.4 device smoke because automated HTTPS server infrastructure is not available in the allowed R4.2 test scope. R4.4 must verify on the SM-S926N that post-pairing REST and terminal traffic switch to the configured HTTPS/WSS operational origin and that the cleartext pairing origin receives no bearer, session, or terminal traffic.

Existing tests: 13 pairing tests (Full2Phase, MobileIntegration, RejectLifecycle, ResultSessionMismatch, NonP256Rejected, NoBootstrapRejected, SecondCandidateRejected, ExpiredSessionRejected, ApproveWithoutProofFails, SessionExpires, RejectAfterProof, ProductionE2E, ApprovalRace) — all continue to PASS.

### R4 T2 Escalation Verification

```
go test ./internal/devicetrust -run '^TestPairing_QuerylessPairedWebViewBootstrap$' -count=1 -v  PASS
go test ./internal/devicetrust -run '^(TestPairing_|TestMobilePairing)' -count=1                PASS
go test ./internal/devicetrust -count=1                                                        PASS
go test -race ./internal/devicetrust -run '^TestPairing_QuerylessPairedWebViewBootstrap$' -count=1 PASS
go test ./...                                                                                  PASS
```

## R4.3 — Candidate Freeze + Artifact Build

### Candidate

```
SHA:       c190d8318ab790f6264c130d0a1045064d6af3f6
Branch:    feature/canonical-timeline-foundation
Ancestry:  f80183578 → c190d8318 (1 commit forward)
Worktree:  0 untracked/modified files at checkout (fresh clone, verified vcs.modified=false)
```

### Daemon

```
Build:     go build -o pokit-daemon ./cmd/devremote (from clean checkout)
Go:        go1.26.4
VCS rev:   c190d8318ab790f6264c130d0a1045064d6af3f6 (exact match ✅)
VCS mod:   false ✅
SHA-256:   22a9501887bab49ad01169e406996ee4b5eef69fdccfcb0cbd0073f3a1fe3ca9
```

### APK

```
Build:     EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST unset (production)
           expo prebuild + gradle assembleRelease
SHA-256:   c34ae0e236d3cec4932570b4c18b3729977468809c85bdd230cb3c9ce6c9b38b
Size:      160,876,015 bytes
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
