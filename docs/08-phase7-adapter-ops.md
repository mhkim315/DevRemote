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

**중요**: `/api/sessions`는 session 목록을 반환하며, adapter 목록을 직접 반환하지 않는다.
세션이 0개인 adapter는 `/api/sessions` 응답에 adapter 정보가 나타나지 않을 수 있다.

| 상황 | Session 목록 | Snapshot.LastError | Stale | 의미 |
|------|-------------|-------------------|-------|------|
| adapter 정상, 세션 있음 | 세션 표시됨 | nil | false | 정상 동작 |
| adapter 정상, 세션 없음 | 빈 목록 (해당 adapter 없음) | nil | false | `empty` — adapter는 정상이나 세션 없음. `/api/sessions`만으로는 확인 불가, Registry snapshot 필요 |
| adapter 연결 실패 | 마지막 성공 snapshot (있을 경우) | non-nil | true | `unavailable` — binary/socket/PATH 문제. 기존 session이 있으면 stale로 표시됨 |
| adapter 일시적 오류 | 마지막 성공 snapshot | non-nil | true | `degraded` — adapter 살아있으나 일부 갱신 실패 |
| 세션 종료됨 | `FindSession` → 404 | nil | - | `ended` |
| 기능 미지원 | WS/history → 501/404 | - | - | `unsupported` |

**Known limitation**: adapter에 세션이 0개이고 연결이 실패한 경우(unavailable + zero sessions),
`/api/sessions` 응답만으로는 `empty`(adapter 정상, session 0개)와 구분할 수 없다.
이 경우 Registry snapshot(`LastError`)을 확인해야 한다. 향후 adapter-level health
endpoint가 추가되면 이 한계를 해소할 수 있다.

## 5. 사용자 진단 가이드

"왜 세션이 안 보이지?" → 확인 순서:

1. `/api/sessions` 확인 → 예상한 adapter의 session이 있는가?
2. session이 있고 `stale: true`면: `unavailable` 또는 `degraded` — adapter 연결 실패
3. session이 아예 없으면:
   a. `empty` — adapter 정상, 생성된 session 없음 → session 생성 필요
   b. `unavailable` — adapter 연결 실패 + 기존 session도 없음 → `/api/sessions`만으로 구분 불가, `lastError` 확인 필요
4. adapter binary/socket 확인 (위 설치 조건 표 참조)

## 6. Mobile Legacy Display Rule

`AgentCard.tsx:86`: `session.adapter !== 'native'` — display-only filter.
- `"native"` → `[native]` tag 숨김 (NativeSession은 내부용)
- 그 외 모든 adapter (`"tmux"`, `"cmux"`, `"localpty"`, `"future"`) → `[adapter]` tag 표시
- Backend 동작 분기 없음. 순수 display 결정.
