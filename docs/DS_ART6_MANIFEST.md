# DS-ART6 — Matched Daemon/APK Artifact Freeze Manifest

**Status:** FROZEN CANDIDATE

**Plan reference:** `docs/BASE_ALPHA_DUAL_SURFACE_EXECUTION_PLAN.md` §DS-ART6

**Frozen SHA:** `3ded6f6ff183bafae7b42303590d8bf984230c8c`

**Branch:** `feature/canonical-timeline-foundation`

**Date:** 2026-07-25

## 1. Source Identity

| Field | Value |
|---|---|
| Commit SHA | `3ded6f6ff183bafae7b42303590d8bf984230c8c` |
| Branch | `feature/canonical-timeline-foundation` |
| Remote | `origin/feature/canonical-timeline-foundation` |
| Worktree | clean |
| vcs.modified | false |

## 2. Daemon Binary

| Field | Value |
|---|---|
| Build command | `go build -o pokit-daemon ./cmd/devremote/` |
| Go version | `go1.26.5 darwin/arm64` |
| Build mode | `exe` |
| Compiler | `gc` |
| CGO | enabled |
| GOARCH | `arm64` |
| GOOS | `darwin` |
| VCS revision (embedded) | `3ded6f6ff183bafae7b42303590d8bf984230c8c` |
| VCS time | `2026-07-24T18:28:30Z` |
| VCS modified | `false` |
| SHA-256 | `0c9d0418bcc1b771f39a6ba701f4e47513137739a58b2d8c64aee0a3e1685dc1` |
| Size | `13,013,410 bytes` |

### Go Dependencies (go version -m)
```
dep github.com/creack/pty v1.1.24
dep github.com/fsnotify/fsnotify v1.10.1
dep github.com/golang-jwt/jwt/v5 v5.3.1
dep github.com/gorilla/websocket v1.5.3
dep golang.org/x/sys v0.46.0
dep golang.org/x/term v0.44.0
dep rsc.io/qr v0.2.0
```

## 3. Mobile (Expo/React Native)

| Field | Value |
|---|---|
| Name | `pokit` |
| Version | `1.0.0` |
| Node.js | `v20.20.2` |
| Expo SDK | 56 |
| TypeScript | 5.8 |

### Jest Test Suite
```
Test Suites: 35 passed, 1 failed, 36 total
Tests:       550 passed, 2 failed, 552 total
Known flaky: __tests__/pairingStore.test.ts (2 tests, mock isolation — §2.5)
```

### TypeScript
```
npx tsc --noEmit: PASS (clean)
```

## 4. Backend Test Suite

| Package | Status |
|---|---|
| cmd/devremote | OK |
| internal/agent | OK |
| internal/agent/adapters/claude/v2_1_202 | OK |
| internal/agent/adapters/codex/v0_144_1 | OK |
| internal/agent/contract | OK |
| internal/agent/doctor | OK |
| internal/cockpit | OK |
| internal/coordination | OK |
| internal/devicetrust | OK |
| internal/notification | OK |
| internal/projection | OK |
| internal/sessionid | OK |
| internal/term | OK |
| internal/timeline/contract | OK |
| internal/timeline/writer | OK |
| internal/transcript | OK |
| internal/validation | OK |
| internal/watcher | OK |
| internal/workspace | OK |

All 19 packages pass `go test -race -count=1`.

## 5. Formula Pins

| Provider | Version | SHA-256 |
|---|---|---|
| Claude Code | `2.1.219` | (installed at /Users/mhk/.local/bin/claude) |
| Codex CLI | `0.145.0` | `134063e133f0b4244fa3b251acf973d4fe4b4aeeacbdc135211bf480f59f1477` |

## 6. Codex Conformance

| Wave | Status | Disposition |
|---|---|---|
| DS-CX1 | COMPLETE | `ACCEPT/FALLBACK_REQUIRED` |
| DS-CX2 | SKIPPED | Codex fallback modes only |

## 7. Claude Conformance

| Wave | Status | Disposition |
|---|---|---|
| DS-CL1 | COMPLETE | `ACCEPT/DUAL_SUPPORTED` |
| DS-CL2 | COMPLETE | Interactive host implemented |

## 8. Known Issues

1. **pairingStore.test.ts** — 2 tests flaky (mock isolation, order-dependent). Documented in BASE_ALPHA_TERMINAL_MANAGED_REMEDIATION_PLAN.md §2.5.
2. **Codex multi-client fan-out** — Not supported by stock binary (Category B limitation). Fallback modes preserved.
3. **Claude JSONL tail** — Best-effort partial evidence. Requires Claude session to actually create the JSONL file.

## 9. Gate Summary

| Gate | Status |
|---|---|
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go test -race ./...` (19 packages) | PASS |
| `npx tsc --noEmit` | PASS |
| `npx jest --runInBand` | 550/552 PASS (2 known flaky) |
| `git diff --check` | PASS |
| Secret scan | PASS |
| VCS identity embedded | PASS |
| Worktree clean | PASS |

## 10. Freeze Statement

This manifest records the exact source, build, toolchain and test identities for
the Base Alpha dual-surface/fallback candidate. The frozen SHA is
`3ded6f6ff183bafae7b42303590d8bf984230c8c`. No implementation remains open.

Physical device verification (DS-DEV7 / R8B) requires this exact source SHA.
Artifacts built from any other SHA are not this candidate.
