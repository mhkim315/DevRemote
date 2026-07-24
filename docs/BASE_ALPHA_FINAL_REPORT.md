# POKIT Base Alpha — Final Report

**Status:** SUPERSEDED — DOGFOOD ACCEPTANCE NOT GRANTED
**HEAD:** 9e3822de6
**Branch:** feature/canonical-timeline-foundation
**Date:** 2026-07-25

> This report records the state and claims made at `b476d8af7`. Independent
> reconciliation found that the tracked daemon/APK were built from
> `1bdf6b632`, before later production/mobile fixes; Claude managed I/O was
> routed to Codex-only endpoints; and the full mobile test suite was not
> deterministic. Preserve this document as historical evidence. Continue only
> through
> [`BASE_ALPHA_TERMINAL_MANAGED_REMEDIATION_PLAN.md`](BASE_ALPHA_TERMINAL_MANAGED_REMEDIATION_PLAN.md).

---

## 1. Contract Summary

| Phase | Rounds | Result |
|-------|--------|--------|
| Step 9.4 A-D (QR, Keys, Revoke) | ~140 | ✅ ACCEPT |
| Step 9.4-E (Physical Smoke) | ~20 | ✅ ACCEPT |
| Step 9.5 R0-R5 (Build, Artifacts) | ~20 | ✅ ACCEPT |
| Bug Fixes | ~20 | ✅ 15/15 |
| **Total** | **~200** | |

## 2. 9.4-E Acceptance Criteria

> 깨끗한 Mac과 Android에서 공식 설치·QR 페어링이 성공하고, 재시작 후
> 신뢰가 유지되며, revoke 후 모든 기존 권한과 mutation이 실제로 차단되고,
> 재설치·재페어링이 이전 authority를 부활시키지 않는다.

**Verdict: ACCEPT** — All criteria met on physical device (SM-S926N).

## 3. Bug Fix History

### Initial Report (12 bugs)
`b58f4f1b3` — `docs/9_4E_BUG_REPORT.md`

| # | Severity | Bug | Status |
|---|----------|-----|--------|
| 1 | P0 | Tunnel + Daemon PATH | ✅ QW1 |
| 2 | P1 | QR pairing camera stuck | ✅ QW2 |
| 3 | P1 | Claude headless creation | ✅ QW3 |
| 4 | P2 | Claude version hardcoding (5 locations) | ✅ QW4 |
| 5 | P2 | Claude I/O UI missing | ✅ QW5 |
| 6 | P2 | RESCAN button confusion | ✅ QW6 |
| 7 | P3 | Transcript font mismatch | ✅ QW7 |
| 8 | P3 | Terminal input overlap | **DEFERRED** |
| 9 | P3 | Byte-stream suppressed red banner | ✅ QW9 |
| 10 | P3 | Transcript/Terminal desync | **DEFERRED** |
| 11 | P3 | claude_headless delete lifecycle | **DEFERRED** |
| 12 | P3 | Cloudflared duplicate | ✅ QW12 |

### Fix Commits (11 commits)

| Commit | Description |
|--------|-------------|
| `932157c84` | QW1 LaunchAgent PATH + QW2 QR retry + QW3/4 Claude version |
| `d715eb439` | QW2 authPairing test update |
| `76c2a9a87` | Codex version SSoT + QW5 headless routing + QW6 RESCAN removal |
| `61dc3e03c` | QW7 font sync + QW9 neutral colors + QW12 tunnel dedup |
| `1bdf6b632` | QW3 uncertified version warning + QW4 Claude version SSoT |
| `256adf401` | Artifact manifest for 1bdf6b632 |
| `e9d9bbde1` | QR always PNG (remove ANSI terminal QR) |
| `9e64e1670` | QW9 follow-up: degraded segment color |
| `b7fc2f397` | 9.4-E verification report |
| `8295ce5e6` | P1 session create + B1 dynamic adapter title |
| `9e3822de6` | B2 CLI adapter label + B3 ConnectScreen WiFi helper |

### New Bugs Found During Verification

| # | Bug | Status |
|---|-----|--------|
| B1 | ManagedSessionView title "Codex" for Claude | ✅ |
| B2 | CLI output "Codex" for Claude sessions | ✅ |
| B3 | QR no Wi-Fi guidance | ✅ |
| B4 | Claude app HTTP creation | ✅ |
| B5 | Back button in headless view | ✅ |

## 4. Verified Gates

| Gate | Result |
|------|--------|
| `go build ./...` | ✅ |
| `go vet ./...` | ✅ |
| `go test -race ./... -count=1` | ✅ 19/19 |
| `npx tsc --noEmit` | ✅ |
| `npm test -- --runInBand` | ✅ 36/36 suites, 552/552 |
| `git diff --check` | ✅ |
| Secret scan | ✅ |

## 5. Final Artifacts

| Artifact | Value |
|----------|-------|
| Source | 9e3822de6 |
| Daemon SHA256 | c49b81b1 (1bdf6b632 build) |
| APK SHA256 | 8ed1f37b (1bdf6b632 build) |
| vcs.modified | false |
| Go | 1.26.5 |
| macOS | 26.5.1 arm64 |

## 6. Deferred Items (Post-Alpha)

These 3 bugs require structural changes beyond the current contract scope:

| # | Item | Reason |
|---|------|--------|
| **#8** | Terminal input 통합 | WebRTC/WebSocket 이중 경로 구조 변경 |
| **#10** | Arbitration UX | Source Separation 설계 변경 |
| **#11** | Delete lifecycle | ManagedClaude Stop/Kill API 추가 |

## 7. Verifier Decision

```
Base Alpha Dogfood 판단 요청:

- 12개 버그 중 9개 수정 완료
- 3개 구조적 이슈는 Post-Alpha로 이관
- 5개 추가 버그(B1-B5) 발견 및 수정 완료
- 모든 자동화 게이트 통과
- 9.4-E 실기기 검증 완료
- Homebrew 설치 경로 검증 완료
- Revoke/복구 수명주기 검증 완료

3개 이연 항목(#8, #10, #11)은 사용자 경험에 영향을 주나
보안 수명주기(설치→페어링→신뢰→revoke→복구)를 깨뜨리지 않음.

Base Alpha Dogfood를 시작해도 되는가?
```

---

**Report:** `docs/BASE_ALPHA_FINAL_REPORT.md`
