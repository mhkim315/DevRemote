# Phase 7 — Operational Verification & Documentation

Date: 2026-07-07
Baseline: Phase 6 accepted (`7849f53de`)

## 1. Goal

Phase 7은 새 backend를 추가하지 않는다. Phase 0-6에서 구축한 adapter architecture가
사용자와 운영자에게 **이해 가능한 형태**인지 검증하고, 부족한 부분을 문서화한다.

대상 backend: `tmux`, `cmux`, `localpty`

## 2. Non-Goals (명시적 금지)

- 새 backend 추가 금지
- LocalPTY feature 확장 금지 (기능 완성도가 아니라 현재 상태 검증)
- `/term/size` 같은 새 공통 runtime 기능은 별도 승인 없이 제외
- adapter 이름 기반 분기 추가 금지 (capability-driven UX 회귀)

## 3. Verification Axes

### 3.1 Capability-Driven UX

핵심 질문: **"모바일 코드에 backend 이름 목록이 있는가?"**

검증 항목:

| # | 항목 | 검증 방법 |
|---|------|----------|
| 1 | `adapter === "tmux" / "cmux"` 분기 존재 여부 | mobile/ 코드 grep |
| 2 | 버튼/액션이 adapter 이름이 아니라 capability로 결정되는지 | client.ts 분석 |
| 3 | unknown adapter (`"future"`)가 crash 없이 표시되는지 | JSON golden fixture |
| 4 | capability 누락 legacy 응답도 fallback 되는지 | `capabilities: []` golden |
| 5 | LocalPTY (live_stream only, no screen/history) 정상 표시 | localpty fixture JSON |

필요시 golden fixture JSON 추가:

```json
// tmux: live_stream, screen, history
{"id":"tmux:dev","adapter":"tmux","capabilities":["live_stream","screen","history"],...}
// cmux: live_stream, screen, history, process_snapshot
{"id":"cmux:surface:1","adapter":"cmux","capabilities":["live_stream","screen","history","process_snapshot"],...}
// localpty: live_stream only
{"id":"localpty:lp1","adapter":"localpty","capabilities":["live_stream"],...}
// unknown (future adapter)
{"id":"future:x1","adapter":"future","capabilities":["live_stream"],...}
// legacy (no capabilities field)
{"id":"tmux:old","adapter":"tmux",...}
```

### 3.2 Status Taxonomy

API/mobile/문서가 같은 의미로 사용해야 하는 상태:

| State | 의미 | 식별 방법 |
|-------|------|----------|
| `empty` | adapter 정상, 세션 없음 | `ListSessions` 성공 + 결과 0개 |
| `unavailable` | adapter 사용 불가 (binary/socket/auth) | `LastError != nil` + `ErrAdapterUnavailable` |
| `degraded` | adapter 응답하나 일부 stale/partial | `Stale == true` |
| `ended` | 세션 종료됨 | `FindSession` → `ErrSessionNotFound` |
| `unsupported` | 요청한 기능을 session이 지원하지 않음 | capability type assertion 실패 |

Phase 7에서 중요한 건 내부 enum 추가가 아니라, **API/mobile/문서가 같은 의미로 말하는지** 확인.

### 3.3 Adapter Operational Requirements

| Adapter | 필요 조건 | 실패 원인 | LaunchAgent 이슈 | 진단 방법 |
|---------|----------|----------|-----------------|----------|
| tmux | tmux binary, session | binary 없음, PATH 문제 | PATH 차이 | `tmux list-sessions` |
| cmux | cmux config/socket | socket 없음, auth/config | config path 차이 | `cmux status` / `cmux config` |
| localpty | PTY/shell 권한 | shell 실행 실패, PTY 불가 | env 차이 | create smoke test |

### 3.4 Diagnostics Endpoint/CLI Assessment

바로 구현하지 않고 필요성을 먼저 검토:

1. `/api/sessions`만으로 `unavailable`과 `empty` 구분 가능한가?
   → 현재는 `LastError` / `Stale` 필드로 구분 가능. 추가 endpoint 불필요.

2. adapter별 `lastError`가 노출되는가?
   → `SessionTelemetry.LastError`에 노출됨 (Phase 2). 충분.

3. mobile이 "설치 필요 / 권한 문제 / 세션 없음"을 구분할 데이터가 있는가?
   → `adapter` + `stale` + `capabilities` 조합으로 가능. 검증 필요.

4. 진단 CLI가 필요한가, debug endpoint로 충분한가?
   → `/debug/dump` (Phase 0) + `/api/sessions`로 충분. 별도 CLI 불필요.

**결론**: 현재 telemetry/API로 충분. Diagnostics endpoint/CLI는 Phase 7 scope out.

## 4. Phase 6 Follow-ups (Deferred to Phase 7)

Phase 6 acceptance의 non-blocking items:

- LocalPTY ended session map cleanup (Signal goroutine 종료 후 정리)
- 정상 WebSocket EOF 로그 정리 (`WS stream read err: EOF` → debug level)
- LocalPTY context cancellation 강화 (`CreateSession` ctx 존중)
- production `WriteInput` 직접 검증 강화

이들은 Phase 7의 주 목표가 아니므로, UX/상태/진단을 흐릴 정도로 커지면 별도 Phase 7.x로 분리.

## 5. Work Items

1. **Scope 문서** (this file) — 작성 완료 후 검증
2. **UX/Capability Smoke** — mobile 코드 grep + golden fixture JSON
3. **상태 표현 정리** — API response 기준 taxonomy 문서화
4. **Adapter 운영 조건 표** — 위 표를 최종 문서로 확정
5. **진단 endpoint 필요성 검토** — 결론: 현재 API로 충분
6. **Compatibility smoke** — legacy no-capabilities 응답 → mobile fallback 확인
7. **Phase 6 follow-up 분류** — 각 항목 Phase 7 vs Phase 7.x 판단

## 6. Acceptance Criteria

- 모바일 코드에 backend 이름 allowlist/분기 없음 증명
- unknown adapter + legacy no-capabilities 응답에 대한 golden fixture 존재
- LocalPTY (live_stream only) session 표시 검증됨
- empty/unavailable/degraded/ended/unsupported 의미가 문서화됨
- adapter별 설치 조건/권한/socket/LaunchAgent 표 완성
- diagnostics endpoint/CLI 필요성 판단 완료
- 새 backend 추가 0건
- 승인 없는 공통 runtime 기능 확장 0건
