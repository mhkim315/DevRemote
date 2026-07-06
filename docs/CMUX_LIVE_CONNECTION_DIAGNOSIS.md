# cmux 라이브 터미널 접속 장애 진단 및 개선안

- 작성일: 2026-07-06
- 대상 브랜치: `feature/phase10-multi-adapter`
- 기준 커밋: `3de7d4a`
- 상태: 원인 진단 완료, 수정 미적용

## 1. 요약

Dashboard에는 tmux/cmux 세션이 표시되지만, 세션에 들어가면 라이브 화면과
입력이 정상적으로 동작하지 않는다. tmux는 상대적으로 연결되는 반면 cmux는
빈 화면으로 남거나 연결되지 않는 현상이 반복된다.

cmux CLI와 트리 파서 자체가 주원인은 아니다. 실제 호스트 환경에서 아래 명령은
정상 동작했으며 `surface:38`의 터미널 내용도 반환되었다.

```bash
cmux tree --all
cmux read-screen --surface surface:38
```

현재 가장 가능성 높은 장애 흐름은 다음과 같다.

```text
Dashboard
  → stale cache에 남은 cmux 세션 표시
  → 사용자가 cmux 세션 선택
  → 데몬 실행 환경에서 cmux socket 또는 read-screen 접근 실패
  → polling 코드가 오류를 무시
  → WebSocket은 열린 상태로 유지
  → 모바일에는 빈 화면만 표시
```

캐시 탐색까지 실패하면 더 위험한 흐름이 발생한다.

```text
cmux 세션 탐색 실패
  → HandleWS가 Native/tmux fallback 실행
  → cmux ID를 이름으로 가진 엉뚱한 tmux 세션 생성/접속
  → 빈 화면 또는 잘못된 세션 표시
```

## 2. 검증된 사실

### 2.1 현재 테스트 상태

다음 검사는 통과했다.

```bash
GOCACHE=/tmp/devremote-go-cache go test ./internal/mux ./internal/term
GOCACHE=/tmp/devremote-go-cache go vet ./internal/mux ./internal/term
```

단, 현재 테스트는 실제 WebSocket-cmux 연결 실패 경로를 다루지 않는다.
유닛 테스트 통과는 라이브 접속 성공을 의미하지 않는다.

### 2.2 실행 컨텍스트에 따라 cmux socket 접근 결과가 다르다

제한된 실행 환경에서는 다음 오류가 재현되었다.

```text
Failed to connect to socket at
/Users/mhk/.local/state/cmux/cmux.sock
(Operation not permitted)
```

동일한 명령을 실제 호스트 권한으로 실행하면 성공했다. 따라서 데몬의 사용자,
권한, macOS 실행 컨텍스트, `HOME`, `PATH`, `XDG_STATE_HOME` 또는 socket 접근
조건을 일반 터미널과 비교해야 한다.

현재 보고된 `exit status 1`만으로는 정확한 원인을 확정할 수 없다. cmux 호출이
`Output()`을 사용해 stderr를 버리고 있기 때문이다.

## 3. 코드에서 확인된 결함

### 3.1 CRITICAL: cmux polling 오류가 완전히 은폐된다

파일: `companion-daemon/internal/mux/cmux_adapter.go`

`CmuxStream.pollScreen()`은 `cmux read-screen` 실패 시 아무 작업도 하지 않는다.

```go
out, err := exec.CommandContext(
    ctx, "cmux", "read-screen", "--surface", s.session.surfaceID,
).Output()

if err == nil {
    // 변경된 화면만 pipe에 기록
}
```

현재 동작:

- stderr가 기록되지 않는다.
- 연속 실패 횟수를 세지 않는다.
- session refresh를 시도하지 않는다.
- stream과 WebSocket을 종료하지 않는다.
- 모바일에 오류를 전달하지 않는다.

그 결과 read-screen이 영구 실패해도 연결은 살아 있는 것처럼 보이며 화면만
영원히 비어 있게 된다.

### 3.2 CRITICAL: 세션 탐색 실패가 잘못된 tmux fallback으로 연결된다

파일: `companion-daemon/internal/term/pty.go`

`HandleWS()`는 `mux.FindSession(session)`이 실패하면 `mux.NewSession()`을 호출한다.
현재 `NewSession()`은 내부적으로 다음 형태의 tmux 명령을 실행한다.

```bash
tmux new-session -A -s <requested-session-id>
```

따라서 `cmux:surface:38` 탐색 실패가 cmux 오류로 끝나지 않고, 해당 문자열을
이름으로 사용한 tmux 세션 생성/접속으로 바뀔 수 있다.

adapter prefix가 포함된 세션은 탐색 실패를 그대로 반환해야 한다. 새 Native/tmux
세션 생성은 별도의 명시적 API에서만 수행해야 한다.

### 3.3 HIGH: Dashboard의 cmux 목록이 최신 상태라는 보장이 없다

파일: `companion-daemon/internal/mux/registry.go`

Registry는 adapter refresh 실패 시 마지막 정상 `Sessions`를 유지한다. 이
stale-on-error 동작 자체는 일시 장애에 유용하지만 API가 아래 정보를 노출하지
않는다.

- `stale`
- `lastSuccessAt`
- `lastAttemptAt`
- `lastError`

따라서 Dashboard에 cmux 세션이 표시되어도 현재 데몬이 cmux에 접근할 수 있다는
뜻은 아니다.

### 3.4 HIGH: `FindSession()`은 adapter의 `GetSession()`을 사용하지 않는다

파일: `companion-daemon/internal/mux/registry.go`

현재 탐색 순서는 다음과 같다.

```text
cache lookup
  → 실패 시 adapter refresh
  → cache lookup 재시도
  → 실패 반환
```

`cmuxAdapter.GetSession()`은 라이브 접속 경로에서 호출되지 않는다. 캐시에서 찾은
객체가 실제로 살아 있는지 접속 시점에 별도로 검증하지 않으며, stale 객체도
그대로 반환될 수 있다.

### 3.5 HIGH: 초기 read-screen 결과를 버린다

파일: `companion-daemon/internal/mux/cmux_adapter.go`

`CmuxSession.OpenStream()`은 preflight로 `ReadScreen()`을 호출하지만 결과를
버린다. 이후 ticker가 처음 실행되는 500ms 뒤에야 첫 화면을 다시 읽는다.

정상 상황에서도 최초 화면 표시가 지연된다. 첫 polling까지 실패하면 사용자는
계속 빈 화면만 보게 된다.

### 3.6 HIGH: 입력 전달 실패도 무시된다

파일: `companion-daemon/internal/term/pty.go`

WebSocket 입력 루프는 `InputWriter.WriteInput()`의 반환 오류를 확인하지 않는다.
따라서 `cmux send` 또는 `cmux send-key`가 실패해도 서버와 모바일 모두 성공처럼
처리한다.

### 3.7 MEDIUM: PTY와 snapshot polling을 동일한 stream처럼 취급한다

tmux는 실제 PTY 스트림을 연다.

```text
tmux attach → PTY → 지속 출력 → WebSocket
```

cmux는 500ms마다 전체 화면 snapshot을 읽어 stream처럼 변환한다.

```text
cmux read-screen → 전체 화면 비교 → 변경 시 pipe 기록 → WebSocket
```

두 방식은 오류 복구, resize, 자원 수명, 공유 방식이 다르다. 현재처럼 둘 다
`StreamOpener`로 감추면 상위 계층이 적절한 정책을 선택하기 어렵다.

## 4. 개선 우선순위

### P0-1. 모든 cmux 명령을 공통 실행기로 통합한다

`tree`, `top`, `read-screen`, `send`, `send-key`가 동일한 실행기와 오류 형식을
사용해야 한다.

```go
func runCmux(ctx context.Context, args ...string) ([]byte, error) {
    cmd := exec.CommandContext(ctx, cmuxPath, args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return nil, fmt.Errorf(
            "cmux %v failed: %w: %s",
            args,
            err,
            strings.TrimSpace(string(out)),
        )
    }
    return out, nil
}
```

진단 로그에는 다음만 포함한다.

- 실행 인자
- cmux 절대 경로
- UID
- `HOME`, `PATH`, `XDG_STATE_HOME`
- socket 경로 및 접근 가능 여부
- exit code와 stderr
- 실행 시간

토큰이나 전체 환경변수는 로그에 남기지 않는다.

### P0-2. `HandleWS()`의 암묵적 tmux fallback을 제거한다

```go
s, err := mux.FindSession(session)
if err != nil {
    http.Error(w, "session not found", http.StatusNotFound)
    return
}
```

세션 생성은 `/api/sessions` 등의 별도 명시적 생성 요청에서만 허용한다.

### P0-3. polling 실패 정책을 구현한다

권장 정책:

```text
1~2회 연속 실패:
  오류 기록 후 backoff 재시도

3회 연속 실패:
  해당 cmux adapter 강제 refresh 및 session 재검증

5회 연속 실패:
  stream 종료 및 WebSocket close reason 전달
```

오류가 발생한 상태에서 빈 stream을 무기한 유지하면 안 된다.

### P0-4. 초기 화면을 즉시 전송한다

`OpenStream()`의 preflight 결과를 버리지 않고 첫 frame으로 기록한 뒤 polling을
시작한다.

```text
ReadScreen 1회
  → 초기 화면 즉시 전달
  → 이후 adaptive polling 시작
```

### P0-5. 입력 오류를 처리한다

`WriteInput()` 실패 시:

1. 원인과 session ID를 기록한다.
2. `surface not found`이면 adapter refresh를 수행한다.
3. 복구할 수 없으면 WebSocket을 명확한 오류로 종료한다.
4. 모바일 UI에 입력 실패 상태를 전달한다.

### P1-1. 데몬 실행 환경을 고정하고 시작 시 health check를 수행한다

시작 시 다음을 검사한다.

```text
cmux 바이너리 발견
  → cmux socket 존재 및 접근 가능
  → cmux tree --all 성공
  → adapter healthy 표시
```

Homebrew 경로를 사용하는 환경에서는 `/opt/homebrew/bin/cmux`를 우선 확인하고,
일반 터미널과 데몬의 UID 및 환경을 비교한다.

### P1-2. adapter health와 stale 상태를 API에 노출한다

예시:

```json
{
  "id": "cmux:surface:38",
  "adapter": "cmux",
  "stale": true,
  "lastSuccessAt": "2026-07-06T12:00:00Z",
  "lastError": "cmux socket: operation not permitted"
}
```

모바일은 stale 세션을 정상 세션처럼 표시하지 말고 “연결 불가” 또는 “마지막 확인
N초 전”으로 표현한다.

### P1-3. 접속 시 live validation과 1회 복구를 수행한다

캐시에서 session을 찾은 뒤에도 `ReadScreen()`으로 실제 생존 여부를 확인한다.

```text
live validation 실패
  → 해당 adapter만 강제 refresh
  → session 재조회
  → validation 1회 재시도
  → 실패 시 명확한 오류 반환
```

### P1-4. PTY relay와 screen polling을 분리한다

장기적으로 WebSocket 계층이 capability에 따라 서로 다른 relay를 선택해야 한다.

```text
tmux:
  StreamOpener + InputWriter + Resizer

cmux:
  ScreenReader + InputWriter
```

cmux용 `ScreenHub`는 세션당 poller 하나만 생성하고 여러 모바일 구독자에게 결과를
fan-out해야 한다.

## 5. 필수 테스트

수정 완료 조건에 아래 테스트를 포함한다.

1. `FindSession("cmux:surface:38")` 실패 시 tmux 세션을 생성하지 않는다.
2. cmux 최초 화면이 500ms를 기다리지 않고 전달된다.
3. `read-screen` 연속 실패 시 refresh 후 stream이 명확히 종료된다.
4. cmux stderr가 최종 오류에 포함된다.
5. stale cache의 session 접속 시 live validation이 수행된다.
6. `WriteInput()` 실패가 무시되지 않는다.
7. WebSocket disconnect 후 polling goroutine과 subprocess가 종료된다.
8. 같은 cmux session에 구독자가 늘어도 poller는 하나만 존재한다.
9. cmux socket 일시 장애 후 복구되면 새 snapshot으로 정상 전환된다.
10. tmux의 기존 PTY attach, 입력, resize가 회귀하지 않는다.

## 6. 실제 E2E 체크리스트

### Dashboard

- `[cmux]`와 `[tmux]` session이 구분되어 표시된다.
- stale adapter/session은 정상 상태와 구분된다.
- 마지막 refresh 오류와 시각을 진단 로그에서 확인할 수 있다.

### tmux

- 기존 session 선택 시 현재 화면이 즉시 표시된다.
- `ls` 입력과 응답이 정상이다.
- resize와 재접속이 정상이다.

### cmux

- `cmux:surface:38` 선택 시 해당 surface의 현재 화면이 즉시 표시된다.
- 일반 텍스트, Enter, 방향키, Ctrl+C가 전달된다.
- surface 종료 후 빈 화면으로 남지 않고 명확한 오류가 표시된다.
- 모바일 재접속 시 poller 누수가 없다.

## 7. 후속 에이전트 작업 순서

1. 공통 `runCmux()`와 구조화된 오류를 추가한다.
2. 실제 데몬 로그로 socket 실패의 stderr와 실행 환경을 확정한다.
3. `HandleWS()`의 암묵적 tmux fallback을 제거한다.
4. 초기 frame 전달과 polling 연속 실패 정책을 구현한다.
5. 입력 오류 전달과 WebSocket close reason을 구현한다.
6. adapter health/stale 정보를 API와 Dashboard에 노출한다.
7. 단위 테스트, WebSocket 통합 테스트, 실제 Mac/cmux E2E를 순서대로 실행한다.

성능 캐싱과 polling 최적화는 중요하지만, 현재 단계에서는 먼저 실패를 관측하고
정확하게 종료·복구할 수 있어야 한다. 위 P0 항목이 완료되기 전에는 cmux 라이브
접속을 완료 상태로 판단하지 않는다.

## 8. 구현 계획 검토 보완사항

외부 작업 계획 `implementation_plan.md`를 현재 코드와 대조한 결과, 아래 사항을
반드시 반영해야 한다.

1. `CmuxStream`의 pipe 종료만으로 WebSocket이 닫히지 않는다. 출력 goroutine은
   종료되지만 메인 goroutine은 `conn.ReadMessage()`에서 계속 대기한다. stream
   오류를 WebSocket relay까지 전달하고 close code `1011`과 짧은 reason을 보낸
   뒤 connection을 닫아야 한다.
2. `RefreshAdapterNow()`는 현재 실제 adapter refresh가 실패해도 `nil`을 반환한다.
   polling 복구 정책에서 사용하기 전에 refresh 오류를 호출자에게 반환하도록
   수정해야 한다.
3. `AdapterSnapshot`에는 이미 `LastSuccessAt`, `LastAttemptAt`, `LastError`가 있다.
   필드를 중복 추가하지 말고 lock을 지키는 snapshot 조회 API를 제공해야 한다.
4. Dashboard session JSON의 실제 타입은 `models/events.go`가 아니라
   `internal/term/telemetry.go`의 `SessionTelemetry`다. `stale`,
   `lastSuccessAt`, `lastError`는 이 응답 조립 경로에 추가해야 한다.
5. 최초 `ReadScreen()` 결과를 reader가 없는 `io.PipeWriter`에 동기적으로 쓰면
   `OpenStream()`이 교착될 수 있다. 초기 frame을 stream 상태에 보관하거나 polling
   goroutine이 첫 실행에서 전달하도록 구현한다.
6. 500ms ticker와 명령당 3초 timeout을 함께 사용하면 연속 5회 실패 판정에 최대
   약 15초가 걸릴 수 있다. 목표 종료 시간을 정하고 timeout 및 실패 횟수를 그에
   맞춰 설정해야 한다.
7. HTTP 오류는 WebSocket upgrade 전에만 반환할 수 있다. upgrade 이후 발생한
   streaming/input 장애는 WebSocket close frame으로 전달한다.
8. stderr 문자열만으로 surface 소멸 여부를 판별하지 않는다. 공통 cmux runner가
   실행 경로, exit code, stderr를 보존하는 구조화된 오류를 반환하게 하고 이를
   기준으로 오류를 분류한다.
9. 공통 runner는 `tree`, `read-screen`, `send`, `send-key`뿐 아니라
   `new-surface`, `close-surface`에도 적용한다.
10. 실제 `cmux` 실행 파일에 의존하지 않는 테스트를 위해 command runner를
    주입할 수 있게 설계한다. WebSocket 통합 테스트에서는 stream 오류 시 `1011`
    close frame과 connection 종료를 모두 검증한다.

보정된 구현 순서는 다음과 같다.

```text
공통 runner와 테스트 주입
  → HandleWS fallback 제거
  → stream 오류 전파와 WebSocket 1011 종료
  → 최초 frame 전달
  → 입력 오류 처리
  → snapshot 조회 API와 Dashboard metadata
```
