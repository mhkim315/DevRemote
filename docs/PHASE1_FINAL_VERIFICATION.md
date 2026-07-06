# Phase 1 전역 Registry 제거 최종 검증

- 검증일: 2026-07-06
- 구현 기준: `821e3ea` 이후 직접 보완
- 최초 기준: `b7ded3d`
- 판정: **ACCEPT**
- 다음 단계: **Phase 2 진행 가능**

## 1. 최종 구현

- mutable package-level Registry/Runtime singleton 제거
- Registry를 main composition root에서 생성
- HTTP request context와 background 함수 인자로 Registry 전달
- Registry context 누락 시 panic 대신 HTTP 500
- `FindSession(ctx, id)`와 cancellable singleflight refresh
- Registry가 create/terminate mutation과 cache invalidation 소유
- tmux adapter의 Registry 의존 제거
- cmux health dependency를 adapter → session → stream으로 전달
- cmux polling 연속 실패 시 소속 Registry만 refresh
- telemetry와 HTTP history에 caller context 전달
- IPC server 시작 오류 logging 복구
- generated binary를 Phase 기준 blob으로 복구
- 중복 `.gitignore` 제거
- 전체 Go source gofmt 적용

## 2. 추가한 회귀 테스트

- create 성공 시 Registry cache invalidation
- create 실패 시 cache 보존
- terminate 성공 시 Registry cache invalidation
- terminate 실패 시 cache 보존
- `FindSession` cancellation 조기 반환
- cmux 연속 screen failure health refresh 1회
- cmux health adapter name과 force 값
- nil cmux RegistryHealth 생성 거부
- cmux adapter가 health dependency 저장
- Registry context 누락 오류
- Registry context 누락 HTTP 500
- 서로 다른 handler의 Registry 격리
- 기존 WebSocket 테스트 병렬 실행

새 cancellation test는 실제 결함을 하나 발견했다. `Refresh` cancellation이
`FindSession`에서 일반적인 session-not-found 오류로 덮이던 문제를 수정해
`context.Canceled`가 caller에게 전달되도록 했다.

## 3. 자동 검증

```text
go vet ./...: PASS
go test ./...: PASS
go test -race ./...: PASS
gofmt: PASS
git diff --check b7ded3d: PASS
generated devremote_bin diff vs b7ded3d: 0
```

정적 검색:

```text
mutable Registry/Runtime globals: 0
ignored RegistryFromContext errors: 0
unchecked RegistryFromContext chaining: 0
direct adapter mutation from HTTP handler: 0
cmux placeholder health comment: 0
adapter registration init functions: 0
```

## 4. 실환경 검증

LaunchAgent:

```text
state: running
CMUX_SOCKET_PATH: ~/.local/state/cmux/cmux.sock
```

세션:

```text
cmux: 3 sessions
tmux: 9 sessions
all stale=false
all lastError=null
```

cmux:

```text
surface:2 history: 101,879 bytes
recent Broken pipe/socket/tree errors: 0
```

Registry mutation E2E:

```text
POST tmux:phase1-e2e
  response: ok
  immediate session count: 1

DELETE tmux:phase1-e2e
  response: ok
  immediate session count: 0
```

임시 tmux session은 검증 후 삭제했다.

## 5. Phase 1 완료 기준 판정

```text
Runtime mutable state ownership: PASS
Registry instance isolation: PASS
Adapter registration explicit: PASS
Mutation invalidation ownership: PASS
Cmux health ownership: PASS
Caller context propagation: PASS
HTTP missing dependency safety: PASS
WebSocket test isolation: PASS
Formatting/full diff: PASS
Unit/race: PASS
Live tmux/cmux regression: PASS
Decision: ACCEPT
```

## 6. Phase 2 인수인계

Phase 2는 현재 명시적 Registry wiring을 `App`과 private `http.ServeMux`로 묶는
작업이다. Phase 1에서 확보한 다음 동작은 변경하지 않는다.

- Registry 생성과 adapter 명시 등록
- request context 또는 명시적 parameter dependency
- Registry-owned mutation invalidation
- cmux stream health callback
- canonical session ID
- REST/WebSocket 외부 protocol

Phase 2에서 다시 `Default`, `R`, `GetInstance` 같은 singleton wrapper를 만들지
않는다.
