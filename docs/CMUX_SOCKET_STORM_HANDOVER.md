# cmux Broken Pipe 및 명령 폭주 수정 인수인계

- 작성일: 2026-07-06
- 대상 브랜치: `feature/phase10-multi-adapter`
- 기준 커밋: `d87e944`
- 상태: 코드 수정 및 자동 검증 완료, 실행 중 daemon 교체와 실제 모바일 E2E 필요

## 1. 장애 현상

- Dashboard에는 cmux session이 정상적으로 표시된다.
- tmux session은 화면과 입력이 정상 동작한다.
- cmux session은 연결되지 않거나 빈 화면으로 남는다.
- daemon 로그에는 다음 오류가 반복된다.

```text
Error: Failed to write to socket (Broken pipe, errno 32)
```

오류는 `tree`, `top`, `read-screen` 전반에서 발생했다.

## 2. 반증된 원인

초기에는 cmux가 nohup 또는 TTY 없는 백그라운드 프로세스의 CLI 접근을 차단한다는
가설이 제시됐다. 실제 호스트 검사 결과 이 가설은 반증됐다.

실행 중 daemon:

```text
PID 2223
PPID 1
USER mhk
TTY 없음
COMMAND ./devremote_bin daemon --insecure-local-only
```

동일 사용자, 최소 환경, 비대화형 실행에서도 다음 명령은 성공했다.

```text
cmux ping       → PONG
cmux tree --all → 정상 tree 반환
```

socket 권한도 daemon 사용자의 접근을 허용한다.

```text
srw------- mhk:staff /Users/mhk/.local/state/cmux/cmux.sock
```

현재 cmux CLI가 지원하는 socket override 변수는 `CMUX_SOCKET`이 아니라
`CMUX_SOCKET_PATH`다.

따라서 foreground 실행, Aqua session, launchctl이 현재 장애의 근본 해결책이라는
증거는 없다.

## 3. 확인된 근본 문제

daemon 로그에는 2초 telemetry 주기마다 다음 burst가 반복됐다.

```text
top --all --processes --format tsv
top --all --processes --format tsv
read-screen --surface surface:1
top --all --processes --format tsv
read-screen --surface surface:41
top --all --processes --format tsv
read-screen --surface surface:40
...
tree --all
```

기존 telemetry 흐름:

```text
ProcessSnapshot()으로 adapter-wide top 실행
  → snapshot에 해당 session 결과가 없거나 batch가 실패
  → 각 session의 ProcessInfo() fallback
  → ProcessInfo()가 동일한 top --all을 다시 실행
  → 모든 session의 ReadScreen() 실행
  → WebSocket polling 및 registry refresh와 중첩
```

cmux surface가 늘수록 한 telemetry 주기에 동일한 adapter-wide `top`이 N회 실행됐다.
여러 goroutine은 동일한 cmux Unix socket에 동시에 명령을 보냈다. 일시적인 socket
실패가 발생하면 session별 fallback이 명령 수를 늘려 장애를 증폭했다.

## 4. 적용한 수정

### 4.1 cmux command 직렬화

파일:

- `companion-daemon/internal/mux/cmux_adapter.go`

`serialCommandRunner`를 추가했다. 하나의 `cmuxAdapter`에서 생성한 모든
`CmuxSession`이 동일한 runner를 공유한다.

따라서 아래 호출은 같은 Unix socket에 동시에 쓰지 않는다.

- session discovery의 `tree`
- process telemetry의 `top`
- live screen과 telemetry의 `read-screen`
- `send`, `send-key`
- `new-surface`, `close-surface`

직렬화 범위는 cmux adapter 내부로 제한된다. tmux 및 다른 adapter 실행에는 영향을
주지 않는다.

### 4.2 batch provider의 session별 fallback 제거

파일:

- `companion-daemon/internal/term/telemetry.go`

`collectProcessSnapshots()`를 추가했다.

정책:

1. `ProcessSnapshotProvider`를 구현한 adapter는 telemetry 주기당 batch 호출을 최대
   한 번만 수행한다.
2. 성공한 batch에 특정 session 결과가 없어도 해당 session의 `ProcessInfo()`를
   다시 호출하지 않는다.
3. batch 실패 시에도 session별 `ProcessInfo()` fallback을 수행하지 않는다.
4. batch를 지원하지 않는 adapter만 기존 per-session `ProcessProvider` fallback을
   사용한다.

cmux의 `ProcessInfo()`도 내부적으로 `top --all`을 실행하므로 이 정책이 command
storm 차단의 핵심이다.

### 4.3 실패 주기의 screen polling 억제

adapter의 batch process snapshot이 실패한 telemetry 주기에는 해당 adapter의
`ReadScreen()` fallback을 실행하지 않는다.

다음 2초 주기에는 batch snapshot을 한 번 다시 시도한다. 성공하면 정상 telemetry와
screen fallback을 자동 재개한다. 따라서 영구 비활성화가 아니라 bounded retry다.

WebSocket에서 실제 사용자가 연 cmux stream은 별도 정책을 유지한다. 연속
`read-screen` 실패 시 기존 구현대로 stream을 `1011`로 종료한다.

## 5. 추가한 회귀 테스트

### `TestSerialCommandRunnerPreventsConcurrentSocketWrites`

두 goroutine이 동시에 runner를 호출해도 두 번째 delegate 실행이 첫 번째 완료
전에는 시작되지 않는지 검증한다.

### `TestCollectProcessSnapshotsUsesOneBatchPerAdapter`

- adapter당 batch snapshot이 정확히 한 번 호출되는지 확인한다.
- canonical `adapter:session` key 생성 여부를 확인한다.
- batch-capable adapter와 실패 adapter가 구분되는지 확인한다.

기존 cmux stream, WebSocket close, registry stale cache 테스트도 함께 통과했다.

## 6. 자동 검증 결과

```text
git diff --check      PASS
go vet ./...          PASS
go test -race ./...   PASS
go build              PASS
```

호스트 권한이 없는 sandbox에서는 `httptest.Server`의 localhost bind가 차단되므로
WebSocket 통합 테스트가 실패한다. 이는 코드 실패가 아니다. 전체 race 테스트는
호스트 권한으로 실행해 통과했다.

## 7. 실행 중 daemon 주의사항

검사 당시 실행 중 daemon은 workspace가 아니라 아래 Antigravity scratch 경로의
바이너리를 사용했다.

```text
/Users/mhk/.gemini/antigravity/scratch/DevRemote/devremote/companion-daemon/devremote_bin
```

따라서 git workspace의 소스 수정만으로 실행 중 PID `2223`에는 변경이 적용되지
않는다. 후속 에이전트는 실제 실행 경로를 확인하고 최신 커밋으로 빌드한 바이너리를
배포한 뒤 daemon을 재시작해야 한다.

사용자가 명시적으로 승인하지 않은 상태에서 기존 daemon을 강제 종료하거나 scratch
binary를 덮어쓰지 않는다.

## 8. 배포 후 검증 순서

### 8.1 빌드 및 daemon 교체

1. 원격 최신 브랜치를 fetch/pull한다.
2. `companion-daemon`에서 최신 binary를 빌드한다.
3. 현재 daemon의 실행 경로와 인자를 기록한다.
4. 기존 daemon을 정상 종료한다.
5. 동일 설정으로 새 binary를 실행한다.
6. 새 PID가 최신 binary를 열고 있는지 `lsof`로 확인한다.

### 8.2 command pattern 확인

daemon 로그에서 한 telemetry 주기에 다음을 확인한다.

- cmux `top --all`은 최대 한 번
- batch `top` 실패 후 session별 `top --all` 반복 없음
- batch 실패 주기에 모든 surface의 `read-screen` burst 없음
- 동일 시각의 cmux command 중첩 없음
- `Broken pipe`가 지속적으로 반복되지 않음

### 8.3 모바일 E2E

1. Dashboard에서 tmux와 cmux session 목록을 확인한다.
2. cmux session 하나를 연다.
3. 최초 화면이 표시되는지 확인한다.
4. 일반 텍스트, Enter, 방향키, Ctrl+C를 전송한다.
5. cmux surface를 로컬에서 종료한다.
6. 모바일이 빈 화면으로 남지 않고 WebSocket 오류 종료를 처리하는지 확인한다.
7. tmux session의 화면, 입력, resize가 회귀하지 않았는지 확인한다.

## 9. 실패가 계속될 경우

수정 적용 후에도 `Broken pipe`가 발생하면 다음 순서로 증거를 수집한다.

1. daemon PID, UID, PPID, TTY
2. daemon이 연 실제 binary 경로와 CWD
3. cmux version과 `cmux capabilities`
4. `CMUX_SOCKET_PATH`
5. socket owner와 mode
6. 오류 직전 10초의 cmux command 종류와 시작/종료 시각
7. 동시에 실행 중인 cmux subprocess 수
8. 같은 시점의 최소 환경 `cmux ping`

명령 직렬화 이후에도 daemon의 단일 `cmux ping`만 실패하고 동일 환경의 외부 ping은
성공한다면, 그때 daemon 환경 차이 또는 macOS process context를 다시 조사한다.
현재 증거만으로 launchctl/Aqua 문제를 전제하지 않는다.

## 10. 후속 최적화

이번 수정은 command storm을 차단하는 최소 안전 변경이다. 아래 항목은 후속 작업으로
분리한다.

- adapter-wide telemetry circuit breaker와 exponential backoff
- cmux screen snapshot의 session별 shared cache/ScreenHub
- 성공·실패·대기 시간·동시 실행 수 metric
- stale adapter health를 session 목록과 분리한 API
- error log rate limiting

실제 daemon 재배포 후에도 socket 장애가 재현될 때만 circuit breaker를 P0로
승격한다.
