# Phase 2 `a7e8ad767` 재검증과 최종 수정 가이드

## 1. 판정

- 검증 대상: `a7e8ad7672f9d174896633af0ab3d535a6bc831f`
- 기준 커밋: `5012838`
- 판정: **REJECT — 핵심 lifecycle 구현은 진전됐으나 오류 보존과 acceptance test가 미완료**

자동 검증:

```text
git diff --check 5012838..a7e8ad767  PASS
go vet ./...                         PASS
go test ./...                        PASS
go test -race ./...                  PASS
```

샌드박스 내부의 최초 `go test`는 `httptest` localhost bind 제한으로 실패했다.
네트워크 bind를 허용한 동일 소스 검증에서는 전체 test와 race test가 통과했다.
이는 코드 실패가 아니라 검증 환경 제약이었다.

## 2. 이번 커밋에서 승인된 부분

다음 구현은 올바른 방향이며 다시 되돌리지 않는다.

- `term.Handlers{Registry}`와 receiver handler
- daemon 전용 `http.NewServeMux`
- 명시적인 `http.Server`
- `signal.NotifyContext`
- watcher 기능 복원과 `App` ownership
- cloudflared 시작 경로 복원
- `term.IPCServer` 도입
- IPC `Close()` idempotency와 `Wait(ctx)`
- listener close 시 accept loop 정상 종료
- telemetry done channel
- HTTP → telemetry → watcher → IPC 종료 순서 구현
- `InjectRegistry`와 `requireRegistry` 제거

이제 Phase 2 구조를 다시 설계하지 않는다. 아래의 제한된 correctness와 test
보완만 수행한다.

## 3. 연기 항목 판정

실행 에이전트가 연기한 다음 항목은 **승인된 연기**다.

| 항목 | 예정 단계 | Phase 2 판정 |
|---|---:|---|
| `term.OwnerUUID`, `term.SupabaseProjectRef` | Phase 3 | 연기 허용 |
| `telemetryCache` | Phase 4/5 | 연기 허용 |
| `sessionLinks` | Phase 4/5 | 연기 허용 |
| `term.OnApproval` | Phase 5 | 연기 허용 |

최종 수정에서 이 전역들을 함께 객체화하지 않는다. 범위를 늘리면 auth와 store
동작까지 동시에 변경돼 Phase 2 검증이 어려워진다.

단, `term.OnApproval` 때문에 두 App을 완전히 독립적으로 병렬 실행할 수 없다는
제약은 테스트와 문서에 명시한다. 이 제약을 숨기거나 private mux 격리와 혼동하지
않는다.

## 4. 차단 문제와 수정 계약

### 4.1 HTTP server 오류가 유실됨

현재 `App.Run`은 `ListenAndServe()` 결과를 `serveErr`에 저장하고 log하지만,
shutdown 이후 다음 값만 반환한다.

```go
return a.Shutdown(shutdownCtx)
```

따라서 port bind 실패나 listener failure가 발생해도 shutdown이 성공하면
`Run()`은 `nil`을 반환한다. 상위 `runDaemon`은 정상 종료로 오인하며 성공
메시지를 출력한다.

수정 계약:

1. `http.ErrServerClosed`는 정상 종료로 정규화한다.
2. 그 외 HTTP 오류는 보존한다.
3. shutdown 오류도 함께 보존한다.
4. HTTP 오류가 발생해도 background resource 정리를 끝까지 수행한다.
5. `errors.Join` 또는 동등하게 `errors.Is`/`errors.As`가 가능한 형태로 반환한다.

권장 형태:

```go
if errors.Is(serveErr, http.ErrServerClosed) {
    serveErr = nil
}

shutdownErr := a.Shutdown(shutdownCtx)
return errors.Join(serveErr, shutdownErr)
```

단순히 `fmt.Errorf("%v", err)`로 문자열화하면 원인 오류를 검사할 수 없으므로
사용하지 않는다.

필수 테스트:

- HTTP server가 sentinel error로 종료되면 `Run()` 반환값에 그 오류가 포함된다.
- 동시에 shutdown resource가 실패하면 두 오류가 모두 포함된다.
- `http.ErrServerClosed`만 발생한 경우에는 실패로 반환하지 않는다.

### 4.2 shutdown 테스트가 실제 resource lifecycle을 검증하지 않음

현재 `TestApp_ShutdownWithoutBackgroundResources`는 resource를 시작하지 않은
App을 종료하고, 오류가 발생해도 `t.Logf`만 호출한다.

현재 `TestApp_ShutdownRespectsDeadline`도 반환값을 기록할 뿐 assertion이 없다.
두 테스트 모두 production shutdown 동작이 망가져도 통과한다.

수정 계약:

- lifecycle resource에 최소 interface 또는 함수 dependency를 둔다.
- 테스트용 fake가 호출 순서와 반환 오류를 기록하게 한다.
- production 기본 dependency는 현재 구현을 그대로 사용한다.
- 범용 DI framework는 도입하지 않는다.

예시:

```go
type stoppable interface {
    Stop(context.Context) error
}

type appDependencies struct {
    startWatcher func() (watcherResource, error)
    startIPC     func(string, *mux.Registry) (ipcResource, error)
    startTunnel  func(context.Context) (tunnelResource, error)
}
```

실제 타입 구조는 달라도 되지만 테스트가 실제 호출 순서와 실패 전파를 관찰할 수
있어야 한다.

필수 assertion:

```text
HTTP -> telemetry -> watcher -> IPC
```

- 앞 단계가 실패해도 뒤 resource가 정리된다.
- deadline 초과가 오류로 반환된다.
- shutdown을 두 번 호출할 수 있도록 설계했다면 두 번째 호출도 안전해야 한다.
  중복 shutdown을 지원하지 않는다면 API contract와 테스트에서 명확히 제한한다.

### 4.3 registry isolation 테스트가 데이터 격리를 증명하지 않음

현재 테스트는 다음 두 사실만 확인한다.

- registry 포인터가 서로 다름
- 두 `httptest.Server`에 HTTP 요청이 가능함

응답 status, body 또는 registry별 session 차이를 검사하지 않으므로 handler가
잘못된 shared registry를 사용해도 통과할 수 있다.

수정 계약:

1. 서로 다른 session을 제공하는 두 test adapter를 만든다.
2. registry A에는 session A만, registry B에는 session B만 등록한다.
3. 각 private mux의 `/api/sessions` 응답 body를 decode한다.
4. A 응답에 B session이 없고 B 응답에 A session이 없음을 assert한다.
5. status code와 JSON decode 성공도 assert한다.
6. 존재하지 않는 session의 기존 404 contract도 유지한다.

단순 pointer 비교는 보조 assertion으로 남겨도 되지만 acceptance 근거로는
충분하지 않다.

### 4.4 tunnel resource ownership이 불완전함

현재 `startTunnel()`은 `cmd.Start()` 후 `*exec.Cmd`를 반환한다. `App`은
`tunnelCmd`에 저장하지만 이후 `Wait`, signal 또는 kill을 호출하지 않는다.
주석의 “App can manage its lifecycle”과 실제 동작이 일치하지 않는다.

기존 구현에는 `cmd.Wait()`가 있어 child process를 회수하고 종료 오류를
기록했다. 현재 코드는 이 동작도 제거했다.

Phase 2에서는 cloudflared supervisor를 새로 만들 필요가 없다. 다음 중 하나를
명확히 선택한다.

#### 최소 보존안

- 기존처럼 별도 goroutine에서 `cmd.Wait()`를 호출한다.
- App shutdown에서 tunnel 종료까지 책임지지 않는다는 기존 제약을 명시한다.
- `tunnelCmd`를 “App-owned resource”라고 표현하지 않는다.

#### 명시적 ownership안

- App이 tunnel process를 signal/terminate한다.
- `Wait()`로 child를 회수한다.
- shutdown context deadline을 지킨다.
- 이미 종료된 process와 start 실패를 정상적으로 처리한다.

대규모 restart policy, exponential backoff, cloudflared supervisor는 이번
Phase에서 구현하지 않는다.

어느 방식을 선택하든 필수 테스트:

- insecure local mode에서는 tunnel start가 호출되지 않는다.
- production mode에서는 정확히 한 번 호출된다.
- start failure 정책이 기존 동작과 동일하거나 명시적으로 오류 반환된다.
- `exec.Command`로 실제 cloudflared를 실행하지 않고 fake dependency를 사용한다.

### 4.5 IPC pathname ownership과 오류 처리가 불완전함

현재 `/tmp/pokit.sock`이 `Run`과 `Shutdown`에 각각 하드코딩되어 있다.
`Shutdown`은 `os.Remove` 오류도 무시한다.

수정 계약:

- IPC path를 `Config` 또는 `App` field 한 곳에 보관한다.
- production default는 기존 `/tmp/pokit.sock`을 유지한다.
- 테스트에서는 `t.TempDir()` 아래 경로를 주입한다.
- listener close 및 `Wait`가 완료된 뒤 pathname을 제거한다.
- `os.IsNotExist`는 정상으로 처리한다.
- 그 외 remove 오류는 shutdown 오류에 포함한다.
- App shutdown 후 같은 경로로 새 IPC server를 시작할 수 있어야 한다.

현재 `TestIPCServer_RebindAfterClose`는 테스트가 직접 `os.Remove`를 호출하므로
App의 pathname cleanup을 검증하지 않는다. 다음 App-level test가 추가돼야 한다.

```text
App starts IPC(path)
App shuts down
new IPC server binds the same path without test-side remove
```

IPC protocol framing과 `handleIPCConnection`은 변경하지 않는다.

### 4.6 오류 집계가 원인 오류를 보존하지 않음

현재 shutdown은 다음 형태로 오류 slice를 문자열화한다.

```go
fmt.Errorf("shutdown errors: %v", errs)
```

이 방식은 `errors.Is`와 `errors.As`를 사용할 수 없게 만든다.

수정 계약:

- Go 버전이 지원하는 `errors.Join(errs...)`를 사용한다.
- 각 resource 오류에 `%w` context를 추가하는 것은 허용한다.
- remove, watcher, IPC close/wait, telemetry deadline 오류를 버리지 않는다.
- 한 오류가 발생해도 나머지 cleanup은 계속한다.

## 5. 구체적인 테스트 목록

최소한 다음 테스트가 이름과 동등한 의미로 존재해야 한다.

```text
TestApp_RunReturnsHTTPServeError
TestApp_RunJoinsServeAndShutdownErrors
TestApp_ShutdownOrder
TestApp_ShutdownContinuesAfterError
TestApp_ShutdownRespectsDeadline
TestApp_ShutdownRemovesIPCPathAndAllowsRebind
TestHandlers_RegistryDataIsolation
TestApp_TunnelStartByMode
TestPrivateMux_NoDefaultMuxUsage
TestIPCServer_CloseIdempotent
TestIPCServer_WaitAfterClose
TestStartTelemetryLoop_StopsOnCancel
```

테스트 품질 조건:

- 결과를 `t.Logf`만 하고 끝내지 않는다.
- 예상 status, body, error 및 호출 순서를 assert한다.
- production port `9171`이나 `/tmp/pokit.sock`을 사용하지 않는다.
- 실제 cmux, tmux, cloudflared 설치 여부에 의존하지 않는다.
- 가능한 테스트는 `t.Parallel()`로 실행하되 승인된 전역 변수 제약이 있는
  테스트는 그 이유를 주석으로 남기고 직렬 실행한다.

## 6. 권장 작업 순서

1. `errors.Join`으로 shutdown 오류 집계를 수정한다.
2. `Run`에서 `serveErr`와 `shutdownErr`를 함께 반환한다.
3. IPC path를 App-owned field/config로 이동한다.
4. 최소 lifecycle dependency seam을 추가한다.
5. 실제 shutdown 순서·오류 지속·deadline 테스트를 작성한다.
6. registry별 test adapter와 response isolation assertion을 추가한다.
7. tunnel 최소 보존안 또는 명시적 ownership안을 선택하고 mode test를 추가한다.
8. 전체 정적 검사와 race test를 실행한다.
9. 실제 daemon smoke test를 수행한다.

Phase 3 auth 객체화, Phase 4/5 store 및 approval 작업은 시작하지 않는다.

## 7. 정적·자동 검증

`companion-daemon`에서 실행:

```bash
gofmt -w <변경한 Go 파일>
git diff --check a7e8ad767
go vet ./...
go test ./...
go test -race ./...
```

추가 검색:

```bash
rg -n 'http\.DefaultServeMux|http\.HandleFunc\(' cmd/devremote internal
rg -n 'InjectRegistry|requireRegistry' internal cmd
rg -n '"/tmp/pokit\.sock"' cmd/devremote internal
rg -n 'tunnelCmd|startTunnel|\.Wait\(\)' cmd/devremote
rg -n 't\.Logf.*Shutdown|Don.t fail on error' cmd/devremote/*_test.go
```

판정:

- daemon route의 default mux 등록이 나오면 실패
- registry context injection이 다시 나오면 실패
- socket path가 여러 lifecycle 위치에 중복 하드코딩되면 실패
- tunnel command를 저장만 하고 회수하지 않으면 실패
- shutdown 결과를 assertion 없이 log만 하는 테스트가 남으면 실패

## 8. 실제 smoke test

자동 검증 후 기존 사용자 동작을 확인한다.

1. insecure-local daemon을 test port/socket으로 시작한다.
2. `/api/sessions`에서 tmux와 cmux session discovery를 확인한다.
3. cmux session 화면 조회가 stale/error 없이 갱신되는지 확인한다.
4. 휴대폰에서 입력과 carriage return 명령 실행을 확인한다.
5. daemon을 SIGTERM으로 종료한다.
6. HTTP, telemetry, watcher, IPC 종료 log 순서를 확인한다.
7. 종료 후 IPC path 재사용이 가능한지 확인한다.
8. production tunnel command는 실제 환경을 변경하지 않는 범위에서 기존
   arguments가 유지됐는지 확인한다.

실제 smoke test는 자동 test를 대체하지 않는다. 두 검증이 모두 필요하다.

## 9. 실행 에이전트 제출 형식

재검증 요청에 다음을 포함한다.

```text
Commit:
Diff stat:

HTTP error preservation:
Shutdown error aggregation:
Shutdown order test:
Registry data isolation test:
IPC App-level rebind test:
Tunnel lifecycle choice:

go vet:
go test:
go test -race:
Smoke test:

Deferred items:
```

연기 항목은 이번 문서의 승인 목록과 동일해야 한다. 새로운 연기가 생기면 이유와
사용자 동작 영향도를 별도로 설명한다.

## 10. 최종 ACCEPT 기준

다음 조건이 모두 충족돼야 Phase 2를 승인한다.

- `a7e8ad767`에서 확보한 composition root 구조 유지
- HTTP 비정상 종료 오류 보존
- shutdown 오류의 원인 chain 보존
- 실제 resource를 대상으로 한 shutdown 순서 및 deadline test
- registry response data 격리 test
- App-level IPC pathname cleanup/rebind test
- tunnel 시작 조건 및 process 회수 정책 명확화
- watcher, telemetry, IPC 기존 기능 유지
- 전체 vet/test/race 통과
- tmux/cmux 실제 smoke test 통과
- 승인된 전역 변수만 후속 Phase로 연기

그 전에는 Phase 3을 시작하지 않는다.
