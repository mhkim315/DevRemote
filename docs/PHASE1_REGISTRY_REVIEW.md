# Phase 1 Mux Registry 리팩터링 검증 결과

- 검증일: 2026-07-06
- 대상 커밋: `49912f2`
- 기준 문서: `docs/GLOBAL_STATE_REFACTOR_PLAN.md`
- 판정: **REJECT**
- 다음 Phase 진행: **금지**

## 1. 요약

`49912f2`는 `Registry` 구조체를 도입하고 adapter/snapshot/singleflight를 instance
field로 옮겼다. `init()` 자동 adapter 등록도 제거했고 독립 Registry 조회 테스트를
추가했다.

그러나 실제 production과 일부 test 경로는 새 package-level `mux.Default`를 통해
동일한 process singleton을 계속 사용한다. adapter도 create/terminate/stream failure
시 자신이 속한 Registry가 아니라 `Default`를 직접 갱신한다.

따라서 “Registry 타입 추가”는 완료됐지만 “Registry 상태의 instance ownership”은
완료되지 않았다.

## 2. 자동 검증 결과

호스트 환경:

```text
go vet ./...: PASS
go test -race ./...: PASS
```

정적 검사:

```text
git diff --check: FAIL
  companion-daemon/internal/mux/tmux_adapter.go:
  new blank line at EOF
```

`mux.Default` production/test 참조는 25곳에서 확인됐다.

## 3. 발견 사항

### P0 — mutable package singleton 잔존

파일:

- `companion-daemon/internal/mux/registry.go`

현재 코드:

```go
var Default = NewRegistry()
```

이는 계획서가 금지한 `DefaultRegistry`/`GetInstance()` 형태의 singleton wrapper와
동일하다. 테스트에서 `NewRegistry()`를 사용할 수 있다는 사실만으로 production
상태가 instance-owned가 되지는 않는다.

### P0 — adapter가 잘못된 Registry를 invalidate

파일:

- `companion-daemon/internal/mux/tmux_adapter.go`
- `companion-daemon/internal/mux/cmux_adapter.go`

현재 동작:

```text
custom Registry
  └─ adapter CreateSession/TerminateSession
       └─ mux.Default.Invalidate()
```

custom Registry의 snapshot은 만료되지 않고 관련 없는 Default snapshot만 변경된다.
기존 isolation test는 조회만 검사하므로 이 결함을 잡지 못한다.

cmux screen polling 3회 실패 경로도 `Default.Refresh()`를 직접 호출한다.

### P1 — caller context 손실

`Registry.FindSession()`이 context를 받지 않고 내부에서
`context.Background()`을 생성한다. telemetry도 전달받은 daemon context 대신
background context로 session discovery를 실행한다.

이 구조에서는 shutdown이나 request cancellation이 adapter command까지 전달되지
않는다.

### P1 — WebSocket 테스트가 singleton 공유

`pty_ws_test.go`는 mock adapter를 `mux.Default`에 등록한다. 테스트 종료 시 제거하지
않으며 독립 test server가 독립 registry를 갖지 않는다.

현재 race test가 통과해도 테스트 순서와 adapter 이름에 따라 state leak이 발생할
수 있다.

### P1 — 필수 diff 검사 실패

`tmux_adapter.go` 끝에 불필요한 blank line이 추가돼 `git diff --check`가 실패한다.

### P2 — 생성 바이너리 포함

Phase 0과 Phase 1 커밋에 `companion-daemon/devremote_bin` 변경이 포함됐다. source
architecture refactor 검토에 생성 바이너리 변경을 섞지 않는다.

### P2 — 부정확한 API 주석

`Registry.Register()` 주석은 session list를 즉시 refresh한다고 설명하지만 실제
구현은 adapter map 등록만 수행한다.

## 4. 보완 작업 지시

아래 항목을 하나의 Phase 1 보완 작업으로 수행한다.

1. `var Default`를 제거한다.
2. `main()`에서 local `registry := mux.NewRegistry()`를 생성한다.
3. HTTP, WebSocket, telemetry, IPC, linker에 registry를 명시적으로 전달한다.
4. `FindSession(ctx, id)`가 caller context를 사용하게 한다.
5. telemetry가 `Sessions(ctx)`를 호출하게 한다.
6. adapter에서 모든 `Default.Invalidate/Refresh` 호출을 제거한다.
7. Registry가 create/terminate 성공 후 자신의 cache를 invalidate하게 한다.
8. cmux stream health callback은 소속 Registry에 주입한다.
9. WebSocket 테스트마다 독립 Registry를 생성한다.
10. custom Registry mutation/health isolation 테스트를 추가한다.
11. EOF blank line을 제거한다.
12. generated binary 변경을 보완 commit에서 되돌린다.
13. 실제 동작과 일치하도록 `Register` 주석을 수정한다.

구현의 상세 signature와 허용 설계는 개정된
`docs/GLOBAL_STATE_REFACTOR_PLAN.md`의 “Phase 1 실행 순서”를 따른다.

## 5. 재검증 명령

```bash
rg -n 'mux\.Default|\bDefault\b|GlobalRegistry|GetRegistry' \
  companion-daemon --glob '*.go'

rg -n 'func init\(' companion-daemon/internal/mux --glob '*.go'

git diff --check

cd companion-daemon
GOCACHE=/tmp/devremote-go-cache go vet ./...
GOCACHE=/tmp/devremote-go-cache go test ./...
GOCACHE=/tmp/devremote-go-cache go test -race ./...
```

예상 정적 결과:

```text
mux.Default references: 0
adapter registration init functions: 0
git diff --check: PASS
```

## 6. 재검증 완료 조건

- mutable Registry package global 0개
- production/test call site가 명시적으로 Registry를 받음
- adapter mutation이 소속 Registry만 invalidate
- cmux failure health refresh가 소속 Registry만 갱신
- caller context가 discovery/refresh까지 전달
- independent WebSocket registry test
- vet/unit/race/diff 검사 통과
- tmux/cmux session ID와 외부 API 불변

모든 조건이 충족되기 전에는 Phase 2를 시작하지 않는다.
