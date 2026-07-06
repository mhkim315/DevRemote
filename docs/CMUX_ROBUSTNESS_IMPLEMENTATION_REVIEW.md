# cmux 안정성 구현 리뷰 및 개선 작업 방향

- 작성일: 2026-07-06
- 대상 브랜치: `feature/phase10-multi-adapter`
- 기준 커밋: `fa743c9`
- 검토 대상: 현재 worktree의 미커밋 cmux 안정성 구현
- 관련 문서:
  - `docs/CMUX_LIVE_CONNECTION_DIAGNOSIS.md`
  - Antigravity brain의 `implementation_plan.md`
  - Antigravity brain의 `walkthrough.md`

## 1. 현재 상태

현재 구현은 아래 핵심 동작을 추가했다.

- `HandleWS()`의 암묵적 tmux session 생성 fallback 제거
- cmux 명령 실행을 위한 `CommandRunner`와 `CmuxError` 도입
- `read-screen` 연속 실패 시 stream pipe를 오류와 함께 종료
- stream 및 입력 오류 발생 시 WebSocket `1011` close frame 전송
- adapter snapshot 조회 함수 추가
- Dashboard 응답에 stale/health metadata 추가
- polling 실패와 WebSocket close code에 대한 테스트 추가

호스트 권한에서 아래 검사는 통과했다.

```bash
GOCACHE=/tmp/devremote-go-cache go test -race ./...
```

단, 이 결과만으로 구현 완료로 판단하면 안 된다. 현재 테스트는 WebSocket 동시
write, runner 전면 적용, 민감한 오류 메시지 필터링, 전역 테스트 상태 격리를
검증하지 않는다. 또한 `git diff --check`는 trailing whitespace 때문에 실패한다.

## 2. 우선 수정해야 할 문제

### 2.1 CRITICAL: WebSocket concurrent writer

현재 `HandleWS()`에는 WebSocket에 쓰는 실행 흐름이 둘 이상 존재한다.

```text
output goroutine
  → BinaryMessage 또는 stream error CloseMessage

input loop
  → input error CloseMessage
```

Gorilla WebSocket connection은 동시에 하나의 writer만 허용한다. 두 흐름에서
`WriteMessage()`를 직접 호출하면 드물게 concurrent write panic, frame 손상 또는
비결정적 종료가 발생할 수 있다. 현재 테스트는 output과 input 오류를 동시에
발생시키지 않으므로 이 문제를 검출하지 못한다.

#### 권장 설계

connection write를 전용 writer goroutine 하나로 직렬화한다.

```go
type wsOutbound struct {
    messageType int
    payload     []byte
}

outbound := make(chan wsOutbound, 32)
fatalErr := make(chan error, 1)
```

```text
stream reader ─┐
               ├→ outbound channel → single WS writer
input failure ─┘
```

writer goroutine만 `WriteMessage`, `WriteControl`, `Close`를 호출한다. fatal error는
`sync.Once` 또는 buffered channel로 최초 하나만 채택한다. close frame 전송에는
짧은 write deadline을 설정해 종료가 무기한 대기하지 않게 한다.

작은 변경을 원한다면 모든 WebSocket write를 같은 mutex로 보호할 수 있다. 다만
종료 순서, deadline, 중복 close 처리는 전용 writer 방식이 더 명확하다.

#### 완료 조건

- 모든 `conn.Write*()` 호출이 한 실행 흐름으로 직렬화된다.
- output 오류와 input 오류가 동시에 발생해도 panic이 없다.
- close frame은 최대 한 번 전송된다.
- `go test -race`에서 관련 테스트가 통과한다.

### 2.2 HIGH: CommandRunner가 모든 cmux 호출을 통제하지 않는다

`CreateSession()`은 CWD 설정 문제 때문에 직접 `exec.CommandContext()`를 사용한다.
따라서 walkthrough의 “모든 직접 실행 제거” 주장은 현재 코드와 일치하지 않는다.
이 상태에서는 명령별 오류 형식과 테스트 가능성이 다시 분기된다.

#### 권장 설계

runner에 실행 옵션을 추가한다.

```go
type CommandOptions struct {
    Dir     string
    Timeout time.Duration
}

type CommandRunner interface {
    Run(ctx context.Context, opts CommandOptions, args ...string) ([]byte, error)
}
```

대안으로 cmux 전용 runner가 CWD만 선택적으로 받도록 해도 된다.

```go
Run(ctx context.Context, dir string, args ...string) ([]byte, error)
```

다음 호출은 모두 같은 runner를 사용해야 한다.

- `tree`
- `top`
- `read-screen`
- `send`
- `send-key`
- `new-surface`
- `close-surface`

#### 완료 조건

- `cmux_adapter.go`에 직접 `exec.CommandContext(..., "cmux", ...)` 호출이 없다.
- create/terminate 오류도 동일한 구조화 오류를 반환한다.
- mock runner로 create의 CWD와 인자를 검증한다.

### 2.3 HIGH: CmuxError가 진단 요구사항을 충족하지 않는다

현재 `CmuxError`는 args, exit code, stderr, 원본 오류를 보존하지만 실제 executable
경로를 포함하지 않는다. command timeout이나 executable 미발견 시 exit code의
의미도 명확하지 않다.

#### 권장 필드

```go
type CmuxError struct {
    Path     string
    Args     []string
    ExitCode int
    Stderr   string
    Duration time.Duration
    Err      error
}
```

정책:

- process가 시작되지 않았거나 timeout이면 `ExitCode = -1`
- `errors.Is(err, context.DeadlineExceeded)` 판별 가능
- args slice는 호출 후 변경되지 않도록 복사
- `Error()`는 한 줄의 제한된 메시지를 생성
- 상세 진단은 server log에만 기록

cmux path는 매번 PATH에서 암묵적으로 찾기보다 daemon 시작 시 `exec.LookPath("cmux")`
결과를 저장하는 방식을 권장한다. 이를 통해 일반 터미널과 daemon 실행 환경의 PATH
차이를 명확하게 진단할 수 있다.

### 2.4 HIGH: 내부 stderr를 WebSocket close reason으로 직접 노출한다

현재 close reason에 `CmuxError.Error()` 일부를 그대로 넣는다. 이 문자열에는 로컬
socket 경로, 사용자 홈 경로, 실행 인자 등 내부 정보가 포함될 수 있다. 또한 byte
index로 문자열을 자르면 UTF-8 rune 중간이 잘릴 수 있다.

#### 오류 정보 분리

```text
server log:
  전체 구조화 오류, path, exit code, stderr, duration

client close reason:
  "cmux screen unavailable"
  "terminal input failed"
  "session no longer exists"
```

클라이언트에는 안정적인 오류 코드가 더 유용하다. close code는 `1011`을 유지하되
짧고 비민감한 reason을 사용한다. 상세 진단이 필요하면 별도의 correlation ID를
로그와 client event에 함께 남긴다.

#### 완료 조건

- close reason은 UTF-8 유효성과 WebSocket control frame 크기 제한을 만족한다.
- 홈 경로, socket 경로, 전체 stderr가 client로 전달되지 않는다.
- server log에는 원인 분석에 필요한 상세 오류가 남는다.

### 2.5 HIGH: preflight와 최초 poll이 중복 실행된다

현재 `OpenStream()`의 preflight `ReadScreen()` 결과는 버리고, polling goroutine이
즉시 다시 `read-screen`을 호출해 최초 frame을 만든다.

```text
OpenStream:
  read-screen #1 → 결과 폐기

pollScreen:
  read-screen #2 → 최초 frame 전달
```

이 방식은 시작 latency와 subprocess 실행량을 늘리며, 첫 호출 성공 직후 두 번째
호출이 실패하면 이미 확보한 화면을 사용자에게 전달하지 못한다.

#### 권장 설계

`OpenStream()`에서 얻은 initial frame을 stream 생성 시 전달한다.

```go
initial, err := s.ReadScreen(ctx)
if err != nil {
    return nil, fmt.Errorf("preflight read-screen: %w", err)
}

stream := newCmuxStream(s, initial)
go stream.pollScreen()
```

중요한 점은 initial frame을 reader가 연결되기 전에 `io.PipeWriter`에 동기 write하지
않는 것이다. poll goroutine이 시작된 뒤 initial payload를 먼저 write하거나,
stream reader가 내부 buffered channel에서 최초 frame을 읽도록 한다.

#### 완료 조건

- 정상 연결 시작 시 `read-screen`은 최초 frame을 위해 한 번만 호출된다.
- preflight에서 얻은 화면이 그대로 첫 WebSocket binary frame이 된다.
- reader가 아직 읽지 않을 때 `OpenStream()`이 block되지 않는다.

### 2.6 MEDIUM: polling 종료와 cancellation 정책이 불완전하다

`pollOnce()`가 `io.PipeWriter.Write()`에서 대기하는 동안 `done` channel만 닫아서는
즉시 select로 돌아갈 수 없다. 현재 `Close()`가 pipe도 닫아 write를 깨우지만,
수명 관리가 pipe 구현 세부사항에 의존한다.

권장 사항:

- stream이 생성될 때 전용 context와 cancel 함수를 둔다.
- 모든 cmux command는 이 context의 child timeout을 사용한다.
- `Close()`는 cancel 후 pipe 양쪽을 닫는다.
- `pollOnce()`는 write 오류를 확인하고 종료한다.
- fatal polling error는 `CloseWithError()`로 reader에 전달한다.
- 정상 client disconnect는 불필요한 `1011`로 변환하지 않는다.

### 2.7 MEDIUM: RefreshAdapterNow의 책임과 오류 반환을 명확히 한다

현재 보완 구현은 refresh 후 snapshot의 `LastError`를 반환한다. 기존보다 낫지만,
함수 이름은 특정 adapter만 refresh하는 것처럼 보이는 반면 내부에서는
`GetAllSessionsCached()`를 호출한다.

권장 구조:

```go
func refreshAdapter(ctx context.Context, name string, force bool) (AdapterSnapshot, error)
```

- 특정 adapter 하나만 실행한다.
- singleflight key는 adapter 이름을 사용한다.
- snapshot 갱신과 반환을 한 경로에서 수행한다.
- 호출자는 같은 snapshot의 `LastError`를 받는다.
- unknown adapter와 refresh failure를 구분한다.

`GetAdapterSnapshot()`은 현재처럼 lock 아래에서 value copy를 반환해도 되지만,
`Sessions` slice까지 외부에 노출한다면 복사본을 반환해 mutation 가능성을 차단한다.

### 2.8 MEDIUM: Dashboard health metadata 의미를 고정한다

현재 `stale = LastError != nil`이다. 이 정의는 직관적이지만 API 계약으로 문서화해야
한다.

권장 의미:

```text
stale = 마지막 refresh 시도가 실패했고 과거 성공 snapshot을 반환 중
```

따라서 session이 하나도 없고 최초 refresh부터 실패한 경우에는 “stale session”이
아니라 “adapter unavailable” 상태다. session 배열 원소에만 health를 반복해서
붙이면 session이 0개일 때 adapter 장애가 사라진다.

장기적으로 응답을 아래처럼 분리하는 편이 정확하다.

```json
{
  "sessions": [],
  "adapters": {
    "cmux": {
      "available": false,
      "stale": false,
      "lastAttemptAt": "2026-07-06T01:00:00Z",
      "lastSuccessAt": null,
      "lastErrorCode": "CMUX_SOCKET_UNAVAILABLE"
    }
  }
}
```

기존 모바일 호환성을 유지해야 한다면 현재 session-level 필드를 먼저 추가하고,
adapter health는 후속 API version에서 도입한다.

### 2.9 MEDIUM: telemetry lock 범위가 너무 넓다

`HandleSessionsV2()`는 `telemetryMu`를 잡은 상태에서 `GetAllSessionsCached()`를
호출한다. refresh가 발생하면 외부 cmux 명령 timeout 동안 telemetry 상태 전체가
잠길 수 있다. 이 문제는 기존 코드에도 있었지만 이번 metadata 작업 시 함께
정리하는 것이 안전하다.

권장 순서:

```text
1. registry에서 sessions 및 adapter snapshots 조회
2. telemetryMu lock
3. 필요한 telemetryCache 값만 복사
4. telemetryMu unlock
5. JSON response 조립
```

`models.GetEvents()`도 별도 mutex를 사용하므로 telemetry lock 밖에서 호출한다.

### 2.10 LOW: 코드 품질과 보고서 정확성

현재 `git diff --check`는 trailing whitespace를 보고한다. `gofmt`만으로 trailing
whitespace가 모두 정리된다고 가정하지 말고 diff check를 다시 실행해야 한다.

walkthrough의 아래 표현도 수정해야 한다.

- “eliminated all direct exec calls”: `CreateSession()` 예외가 있으므로 현재는 거짓
- “falls back gracefully”: fallback은 제거했으므로 “fails explicitly”가 정확함
- “exact stderr reason to client”: 보안상 목표로 삼아서는 안 됨
- “all changes pass race detection”: 통과 사실과 검증 범위의 충분성을 구분해야 함

## 3. 권장 작업 순서

### Phase 1: 코드 정리와 runner 완성

1. 현재 변경에 `gofmt`를 적용한다.
2. trailing whitespace와 임시 설명 주석을 제거한다.
3. runner에 CWD/options와 executable path를 추가한다.
4. 모든 cmux command를 runner로 통합한다.
5. `CmuxError`의 timeout/exit/path semantics를 고정한다.

검증:

```bash
gofmt -w internal/mux/cmux_adapter.go \
  internal/mux/cmux_adapter_mock_test.go \
  internal/mux/registry.go \
  internal/term/pty.go \
  internal/term/pty_ws_test.go \
  internal/term/telemetry.go
git diff --check
go test ./internal/mux
```

### Phase 2: stream 수명과 최초 frame

1. preflight 결과를 stream initial frame으로 넘긴다.
2. stream 전용 context/cancel을 추가한다.
3. polling write 오류를 처리한다.
4. 정상 close와 fatal close를 구분한다.
5. 실패 횟수와 실제 종료 시간을 테스트 clock 또는 짧은 interval로 검증한다.

### Phase 3: WebSocket 단일 writer

1. outbound writer 구조를 도입한다.
2. 모든 binary/close write를 직렬화한다.
3. 최초 fatal error만 close reason으로 채택한다.
4. client disconnect 시 stream cancel이 즉시 수행되는지 검증한다.
5. server stream failure 시 `1011`을 전송하고 input read loop도 종료되는지 검증한다.

### Phase 4: health와 telemetry

1. 특정 adapter만 refresh하는 내부 함수를 만든다.
2. snapshot 조회의 copy semantics를 보장한다.
3. telemetry lock 밖에서 registry refresh 및 JSON 조립을 수행한다.
4. stale 정의를 테스트에 명시한다.
5. 최초 refresh 실패 및 빈 session 목록의 adapter health 표현을 결정한다.

### Phase 5: 통합 검증과 문서 갱신

1. 전체 unit/race 테스트를 실행한다.
2. 실제 cmux socket 실패를 재현한다.
3. WebSocket client에 비민감한 `1011` reason이 전달되는지 확인한다.
4. server log에서 path, exit code, stderr, duration을 확인한다.
5. tmux PTY 입력, 출력, resize 회귀 여부를 확인한다.
6. 실제 구현과 일치하도록 walkthrough를 수정한다.

## 4. 필수 자동화 테스트

### CommandRunner

1. 성공 시 stdout 반환
2. non-zero exit 시 path/args/exit/stderr 보존
3. executable 미발견 시 exit `-1`
4. context timeout 판별
5. create session의 CWD 전달
6. 모든 cmux operation이 runner를 통하는지 검증

### CmuxStream

1. preflight 결과가 첫 frame으로 한 번만 전달됨
2. 동일 snapshot은 중복 전송하지 않음
3. 성공 사이에 오류가 발생하면 consecutive count reset
4. 연속 실패 threshold에서 refresh 한 번 실행
5. refresh 실패 여부와 무관하게 fatal stream error 전달
6. `Close()` 후 poll command와 goroutine 종료
7. blocked write 중 close해도 종료

### Registry

1. 특정 adapter만 forced refresh
2. refresh 실패 시 stale sessions 유지
3. refresh 실패가 호출자에게 반환됨
4. snapshot getter가 내부 slice mutation을 허용하지 않음
5. concurrent cached lookup/refresh에서 race 없음

### WebSocket

1. 없는 session은 upgrade 전에 HTTP 404
2. stream fatal error는 `1011`
3. input fatal error는 `1011`
4. output과 input 오류 동시 발생 시 close frame 한 번
5. output binary write와 close write 동시 발생 시 panic 없음
6. close reason에 stderr와 사용자 홈 경로가 없음
7. client disconnect가 stream을 즉시 종료
8. tmux session의 기존 relay가 정상 동작

### Dashboard API

1. healthy adapter는 `stale=false`
2. refresh 실패 후 과거 session은 `stale=true`
3. `lastSuccessAt`은 마지막 성공 시각 유지
4. `lastError` 또는 공개 error code가 정책대로 노출됨
5. telemetry lock이 느린 adapter refresh 동안 장시간 유지되지 않음

## 5. 수동 E2E 시나리오

### 정상 cmux 연결

1. 실제 `cmux tree --all`에서 surface ID를 확인한다.
2. Dashboard에서 해당 session을 연다.
3. 첫 화면이 추가 500ms 대기 없이 표시되는지 확인한다.
4. 텍스트, Enter, 방향키, Ctrl+C 입력을 확인한다.
5. daemon log에 불필요한 반복 오류가 없는지 확인한다.

### socket 접근 실패

1. daemon과 일반 terminal의 UID, HOME, PATH, XDG_STATE_HOME을 기록한다.
2. daemon context에서 cmux socket 접근 실패를 재현한다.
3. server log에 executable path, exit code, stderr가 기록되는지 확인한다.
4. client에는 내부 경로 없이 일반화된 오류가 표시되는지 확인한다.
5. 약속된 시간 내에 WebSocket이 `1011`로 종료되는지 확인한다.

### surface 종료

1. 연결 중인 cmux surface를 로컬에서 종료한다.
2. polling이 연속 실패를 감지하는지 확인한다.
3. adapter snapshot이 갱신되는지 확인한다.
4. Dashboard에서 stale 또는 session 제거 상태가 일관되게 표시되는지 확인한다.
5. 자동 재연결이 존재한다면 사라진 session에 무한 재시도하지 않는지 확인한다.

### tmux 회귀

1. 기존 tmux session에 연결한다.
2. binary output과 입력을 확인한다.
3. resize 및 재연결을 확인한다.
4. 존재하지 않는 session 요청이 더 이상 암묵적으로 생성되지 않는 정책이 모바일
   동작과 충돌하지 않는지 확인한다.

## 6. 완료 기준

아래 조건을 모두 만족해야 cmux robustness 작업을 완료로 판단한다.

- 모든 cmux subprocess가 하나의 injectable runner를 사용한다.
- runner 오류가 path, args, exit code, stderr, timeout 원인을 보존한다.
- 최초 화면을 위한 `read-screen`이 중복 실행되지 않는다.
- polling failure가 정해진 시간 안에 stream fatal error로 전달된다.
- WebSocket write가 단일 writer 규칙을 만족한다.
- client close reason에 민감한 host 정보가 포함되지 않는다.
- session lookup 실패가 tmux session 생성으로 변환되지 않는다.
- adapter stale/health semantics가 API와 테스트에 고정되어 있다.
- `git diff --check`가 통과한다.
- `go test ./...`와 `go test -race ./...`가 통과한다.
- 실제 Mac/cmux E2E와 tmux 회귀 검증이 완료된다.

현재 구현은 P0 문제 해결의 기반은 갖췄지만, 위 조건 중 WebSocket write 직렬화,
runner 전면 적용, 오류 정보 분리와 일부 lifecycle 검증이 남아 있다. 이 항목을
해결하기 전에는 walkthrough를 최종 완료 보고서로 사용하지 않는다.
