# DS-ART6v2 — Matched Daemon/APK Artifact Freeze (Revision 2)

**Status:** FROZEN CANDIDATE (replaces ART6 `50cc05cb9` — REJECTED: stale ancestry)

**Plan reference:** `docs/BASE_ALPHA_DUAL_SURFACE_EXECUTION_PLAN.md` §DS-ART6

**Frozen SHA:** `b19d7ef025b628ff273a64186d3df84755dbeb96`

**Branch:** `feature/canonical-timeline-foundation`

**Date:** 2026-07-25

## 1. Source Identity

| Field | Value |
|---|---|
| Commit SHA | `b19d7ef025b628ff273a64186d3df84755dbeb96` |
| Short SHA | `b19d7ef02` |
| Branch | `feature/canonical-timeline-foundation` |
| vcs.revision (embedded) | `b19d7ef025b628ff273a64186d3df84755dbeb96` |
| vcs.modified (embedded) | `false` |

## 2. Daemon Binary

| Field | Value |
|---|---|
| Build command | `go build -o pokit-daemon ./cmd/devremote/` |
| Go version | `go1.26.5 darwin/arm64` |
| GOARCH | `arm64` |
| GOOS | `darwin` |
| SHA-256 | `ff34d2ecca34337faef64187303b39b4ac5cfbf2dc3b01697fa7cf0ecf8f671f` |
| Size | `13,085,154 bytes` |

## 3. Mobile (Expo/React Native)

| Field | Value |
|---|---|
| Name | `pokit` |
| Version | `1.0.0` |
| Node.js | `v20.20.2` |
| Expo SDK | 56 |

## 4. Formula Pins

| Provider | Version | SHA-256 |
|---|---|---|
| Claude Code | `2.1.220` | (installed) |
| Codex CLI | `0.145.0` | `134063e133f0b4244fa3b251acf973d4fe4b4aeeacbdc135211bf480f59f1477` |

## 5. Gate Summary

| Gate | Status |
|---|---|
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go test -race ./...` (20 packages) | PASS |
| `npx tsc --noEmit` | PASS |
| VCS identity embedded | `vcs.revision=b19d7ef02`, `vcs.modified=false` |

## 6. Previous ART6 Disposition

| Manifest | SHA | Status |
|---|---|---|
| DS_ART6_MANIFEST.md | `50cc05cb9` | **REJECTED** — stale ancestry, missing DS-CX1B/DS-CX2/BF waves |

## 7. Freeze Statement

This manifest records the exact source, build, toolchain and test identities for
the Base Alpha dual-surface/fallback candidate v2. The frozen SHA is
`b19d7ef025b628ff273a64186d3df84755dbeb96`.

Physical device verification (DS-DEV7 / R8B) requires this exact source SHA.
Artifacts built from any other SHA are not this candidate.
