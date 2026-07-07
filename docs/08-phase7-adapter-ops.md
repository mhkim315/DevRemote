# Phase 7 — Adapter Operational Reference

Date: 2026-07-07

Phase 7 결과 문서. tmux / cmux / localpty 세 adapter의 운영 조건, 진단 방법,
상태 구분법을 확정한다.

## 1. Capability Matrix

| Capability | tmux | cmux | localpty |
|-----------|------|------|----------|
| `live_stream` | ✅ | ✅ | ✅ |
| `screen` | ✅ | ✅ | ❌ |
| `history` | ✅ | ✅ | ❌ |
| `process` | ❌ | ✅ | ❌ |

Mobile golden fixture JSON:

```json
// tmux
{"id":"tmux:dev","adapter":"tmux","capabilities":["live_stream","screen","history"]}

// cmux
{"id":"cmux:surface:1","adapter":"cmux","capabilities":["live_stream","screen","history","process"]}

// localpty
{"id":"localpty:lp1","adapter":"localpty","capabilities":["live_stream"]}

// unknown (future adapter)
{"id":"future:x1","adapter":"future","capabilities":["live_stream"]}

// legacy (no capabilities field — omitted in JSON)
{"id":"tmux:old","adapter":"tmux"}
```

## 2. Adapter Installation & Requirements

| Adapter | 필요 조건 | 설치 확인 | 실패 원인 |
|---------|----------|----------|----------|
| tmux | `tmux` binary in PATH | `which tmux` 또는 `tmux -V` | binary 없음, PATH 문제 |
| cmux | `cmux` binary + socket/config | `cmux status` | socket 없음, auth/config 손상 |
| localpty | PTY 지원 OS + shell | `bash -c true` | shell 실행 실패, 권한 없음 |

## 3. LaunchAgent / Daemon Context

| Adapter | PATH 이슈 | Socket 이슈 | 특이사항 |
|---------|----------|------------|---------|
| tmux | LaunchAgent PATH 제한적 → `tmux` 못 찾을 수 있음 | 없음 | `binary discovery`로 대응 |
| cmux | PATH 영향 적음 (socket 기반) | LaunchAgent GUI session socket path 다를 수 있음 | `cmux config`로 socket path 확인 |
| localpty | PATH 영향 있음 (shell 실행) | 없음 | `bash`가 `/bin/bash` 절대경로 사용 권장 |

## 4. Session Discovery 해석

| 상황 | Session 목록 | LastError | Stale | 의미 |
|------|-------------|-----------|-------|------|
| adapter 정상, 세션 있음 | 세션 표시됨 | nil | false | 정상 동작 |
| adapter 정상, 세션 없음 | 빈 목록 | nil | false | `empty` — adapter 연결됐으나 세션 없음 |
| adapter 연결 실패 | 마지막 성공 snapshot | non-nil | true | `unavailable` — binary/socket/PATH 문제 |
| adapter 일시적 오류 | 마지막 성공 snapshot | non-nil | true | `degraded` — adapter 살아있으나 일부 갱신 실패 |
| 세션 종료됨 | `FindSession` → 404 | nil | - | `ended` — session은 종료됨 |
| 기능 미지원 | WS/history → 501/404 | - | - | `unsupported` — session/adapter가 기능 미제공 |

## 5. 사용자 진단 가이드

"왜 세션이 안 보이지?" → 확인 순서:

1. `/api/sessions` 확인 → adapter가 목록에 있는가?
2. adapter가 없으면: 해당 adapter의 binary/socket이 설치/실행 중인가? (위 표 참조)
3. adapter가 있지만 세션이 0개면: `empty` — adapter는 정상, session을 생성해야 함
4. adapter의 `stale: true` / `lastError`가 있으면: `unavailable` 또는 `degraded` — adapter 연결 실패

## 6. Mobile Legacy Display Rule

`AgentCard.tsx:86`: `session.adapter !== 'native'` — display-only filter.
- `"native"` → `[native]` tag 숨김 (NativeSession은 내부용)
- 그 외 모든 adapter (`"tmux"`, `"cmux"`, `"localpty"`, `"future"`) → `[adapter]` tag 표시
- Backend 동작 분기 없음. 순수 display 결정.
