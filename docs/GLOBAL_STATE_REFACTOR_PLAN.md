# 전역 상태 제거 및 애플리케이션 구조 개선 계획

- 작성일: 2026-07-06
- 대상 브랜치: `feature/phase10-multi-adapter`
- 기준 커밋: `c01d27d`
- 목적: 현재 동작하는 tmux/cmux 모바일 E2E를 보존하면서 mutable package global을
  명시적인 객체 수명과 의존성 주입 구조로 전환한다.
- 구현 담당: 후속 작업 에이전트
- 검증 담당: 별도 검증 에이전트

## 1. 배경과 현재 기준선

현재 아래 실제 사용자 흐름은 동작한다.

```text
macOS LaunchAgent daemon
  ├─ tmux adapter
  └─ cmux adapter
       ↓
REST + WebSocket
       ↓
Android 실기기
  ├─ 화면 출력
  ├─ 텍스트 입력
  └─ Enter 실행
```

cmux는 `socketControlMode: allowAll`과 사용자 전용 Unix socket mode `0600`을
사용한다. daemon은 사용자 GUI domain LaunchAgent로 실행된다. 최신 실기기
검증에서는 cmux session 탐색, 화면 history, WebSocket 화면, 텍스트 입력,
`LF/CR/CRLF → send-key enter` 변환이 성공했다.

이 리팩터링의 최우선 조건은 이 동작을 보존하는 것이다. 구조 개선을 이유로
프로토콜, 세션 ID, polling 주기, adapter 동작 또는 모바일 UX를 동시에 변경하면
안 된다.

## 2. 문제 정의

현재 mutable package global은 다음 영역에 존재한다.

| 영역 | 전역 상태 | 주요 위험 |
|---|---|---|
| mux registry | `adapters`, `snapshots`, `refreshGroup` | 테스트 오염, 숨은 adapter 등록, 수명 불명확 |
| 인증 | `OwnerUUID`, `SupabaseProjectRef`, `InsecureLocalOnly` | 실행 중 변경 가능, 테스트 저장/복원 필요 |
| JWKS | key cache, expiry, mutex | 인증 인스턴스와 cache 수명이 분리됨 |
| telemetry | `telemetryCache`, mutex | service 시작/종료와 상태 소유권 불명확 |
| events | `eventsCache`, mutex | models package가 저장소까지 담당 |
| links | `sessionLinks`, mutex | 파일 I/O와 process singleton 결합 |
| commands | `pendingCmds`, mutex | handler 사이의 암묵적 공유 상태 |
| notification | `OnApproval` callback | 호출 의존성이 숨겨짐 |
| HTTP | default mux와 package handler | 테스트 서버 격리 어려움 |
| mobile config | mutable `config.BASE_URL` | React lifecycle 밖에서 상태 변경 |

다음 package-level 값은 제거 대상이 아니다.

- `const`
- 컴파일된 정규식
- 변경되지 않는 lookup table
- React `StyleSheet`
- 읽기 전용 macro/runner 정의
- 생성 후 변경하지 않는 Supabase client

목표는 “전역변수 0개”가 아니라 “mutable runtime state의 소유자와 수명 명시”다.

## 3. 목표 아키텍처

```text
cmd/devremote/main.go                 composition root
  └─ Config                          immutable
  └─ App
      ├─ AuthVerifier                auth config + JWKS cache
      ├─ mux.Registry                adapters + snapshots
      │   ├─ TmuxAdapter
      │   └─ CmuxAdapter
      ├─ EventStore
      ├─ LinkStore
      ├─ CommandBroker
      ├─ TelemetryService
      ├─ Notifier
      ├─ IPCServer
      └─ HTTPServer                  private ServeMux
```

의존 방향은 아래 규칙을 따른다.

```text
cmd → app/server → service interfaces → adapters/stores

models는 runtime store를 소유하지 않는다.
adapter는 HTTP/mobile package를 알지 않는다.
telemetry는 Expo 구현을 직접 알지 않는다.
handler는 package global을 읽지 않는다.
```

## 4. 공통 작업 규칙

### 4.1 반드시 지킬 범위

- 단계당 하나의 상태 소유권만 변경한다.
- 외부 REST path와 JSON schema를 유지한다.
- WebSocket URL과 frame 형식을 유지한다.
- canonical session ID를 유지한다.
  - `tmux:<session>`
  - `cmux:surface:<id>`
- cmux polling 500ms, 연속 오류 임계값 3회를 이 작업에서 변경하지 않는다.
- registry refresh TTL 10초를 이 작업에서 변경하지 않는다.
- telemetry interval 2초를 이 작업에서 변경하지 않는다.
- 보안 정책 개선은 구조만 준비하고 동작 변경은 별도 작업으로 분리한다.
- 각 단계는 자체 커밋으로 끝낸다.

### 4.2 금지 사항

- 전역 registry를 유지한 채 새 registry를 하나 더 만드는 이중 상태
- singleton을 `DefaultRegistry`, `DefaultStore`, `GetInstance()`로 이름만 변경
- 테스트 통과를 위해 mutable global reset helper 추가
- `init()`에서 adapter 자동 등록 유지
- unrelated formatting 또는 대규모 파일 이동
- tmux/cmux 명령 동작 변경
- 모바일 화면 디자인 변경
- 인증 리팩터링과 인증 정책 변경을 같은 커밋에 포함
- 단계 완료 전 다음 단계 상태까지 일부 이동

### 4.3 단계별 필수 검사

```bash
gofmt -w <변경한 Go 파일>
git diff --check
cd companion-daemon
GOCACHE=/tmp/devremote-go-cache go vet ./...
GOCACHE=/tmp/devremote-go-cache go test ./...
GOCACHE=/tmp/devremote-go-cache go test -race ./...
```

모바일 변경이 있는 단계는 추가로 실행한다.

```bash
cd mobile
npx tsc --noEmit
```

샌드박스에서 local listener 또는 cmux socket 권한 때문에 실패하면 실패를 코드
결함으로 숨기지 않는다. 호스트 환경에서 같은 명령을 재실행하고 두 결과를 작업
보고서에 기록한다.

## 5. 단계별 구현 계획

## Phase 0: 기준선 고정

### 목적

구조 변경 전 현재 동작과 테스트 상태를 기록해 회귀 판단 기준을 만든다.

### 작업

1. 기준 커밋이 `c01d27d`인지 확인한다.
2. worktree가 깨끗한지 확인한다.
3. 전체 Go test와 race test를 실행한다.
4. registry 동작을 고정하는 테스트를 보강한다.
   - tmux와 cmux session을 한 목록으로 합친다.
   - 같은 compound ID를 중복 반환하지 않는다.
   - adapter refresh 실패 시 기존 snapshot을 보존한다.
   - 명시적 cmux ID 조회 실패가 tmux 생성으로 이어지지 않는다.
   - force refresh가 해당 adapter만 갱신한다.
5. 기존 테스트가 global registry 상태나 실행 순서에 의존하는 위치를 목록화한다.

### 제한

- production code 구조를 변경하지 않는다.
- 불안정한 테스트를 삭제하거나 skip하지 않는다.

### 완료 기준

- 기준 테스트 결과가 문서 또는 커밋 메시지에 기록된다.
- 이후 registry 인스턴스 테스트가 비교할 behavioral test가 존재한다.

### 권장 커밋

```text
test: lock multi-adapter registry behavior before refactor
```

## Phase 1: Mux Registry 인스턴스화

### 목적

프로젝트 핵심인 multi-adapter 상태를 package global에서 독립 객체로 이동한다.

### 목표 API

```go
type Registry struct {
    adaptersMu sync.RWMutex
    adapters   map[string]Adapter

    snapshotsMu sync.RWMutex
    snapshots   map[string]AdapterSnapshot
    refresh     singleflight.Group
}

func NewRegistry(adapters ...Adapter) *Registry
func (r *Registry) Register(adapter Adapter)
func (r *Registry) Adapter(name string) (Adapter, bool)
func (r *Registry) Adapters() []Adapter
func (r *Registry) FindSession(ctx context.Context, id string) (Session, error)
func (r *Registry) FindSessionInCache(id string) (Session, error)
func (r *Registry) Refresh(ctx context.Context, name string, force bool) (AdapterSnapshot, error)
func (r *Registry) Sessions(ctx context.Context) []Session
func (r *Registry) Snapshot(name string) (AdapterSnapshot, bool)
func (r *Registry) Invalidate()
```

정확한 이름은 Go 문맥에 맞게 조정할 수 있지만 package global wrapper를 최종 API로
남기면 안 된다.

### 작업

1. `registry.go`의 adapter map, snapshot map, mutex, singleflight를 `Registry` 필드로
   이동한다.
2. 기존 package 함수의 본문을 receiver method로 전환한다.
3. `context.Background()`을 method 내부에서 새로 만들기보다 가능한 호출자 context를
   전달한다.
4. tmux와 cmux의 `init()` 자동 등록을 제거한다.
5. 테스트는 각 case마다 `NewRegistry(fakeAdapters...)`를 생성한다.
6. 테스트를 `t.Parallel()`로 실행할 수 있는지 검토하고 안전한 case부터 적용한다.
7. registry에 adapter 등록 순서가 API 결과 안정성에 영향을 주는지 확인한다.
   필요하면 출력 정렬 정책을 명시하되 기존 모바일 표시가 바뀌지 않도록 한다.

### 주의할 결합 지점

- `term/pty.go`
- `term/telemetry.go`
- `term/ipc.go`
- `term/linker.go`
- `mux/cmux_adapter.go`의 cache invalidation
- session create/terminate 후 invalidation

이 단계에서 아직 `App`이 없으므로 registry를 사용하는 상위 객체에 생성자 인자로
전달할 최소 골격이 필요할 수 있다. 임시 package global로 되돌리지 않는다.

### 테스트

- 서로 다른 registry 두 개가 서로의 adapter/session을 보지 않는다.
- 한 registry refresh가 다른 registry snapshot을 변경하지 않는다.
- 동일 adapter concurrent refresh가 singleflight로 합쳐진다.
- stale snapshot semantics가 유지된다.
- legacy `cmux:38` ID migration이 유지된다.

### 완료 기준

- `mux` package에 mutable registry global이 없다.
- tmux/cmux adapter에 registration 목적의 `init()`이 없다.
- registry 테스트가 전역 reset 없이 독립 실행된다.
- REST와 WebSocket에서 기존 session ID를 찾는다.

### 권장 커밋

```text
refactor: make mux registry instance-owned
```

## Phase 2: App composition root와 HTTP Server 도입

### 목적

daemon 전체 의존성을 `main()`에서 명시적으로 조립하고 default HTTP mux를 제거한다.

### 목표 구조

```go
type App struct {
    config    Config
    registry  *mux.Registry
    server    *http.Server
    telemetry *TelemetryService
    ipc       *IPCServer
}

func NewApp(cfg Config, deps Dependencies) (*App, error)
func (a *App) Run(ctx context.Context) error
func (a *App) Shutdown(ctx context.Context) error
```

### 작업

1. immutable `Config`를 추가한다.
2. flag parsing은 `cmd/devremote`에만 둔다.
3. tmux/cmux adapter와 registry를 composition root에서 생성한다.
4. `http.NewServeMux()`를 사용한다.
5. HTTP handler를 `Server` receiver method로 옮긴다.
6. `http.ListenAndServe` 대신 명시적인 `http.Server`를 사용한다.
7. signal 처리는 `signal.NotifyContext`로 바꾼다.
8. goroutine에서 `os.Exit()`을 호출하지 않는다.
9. shutdown 시 HTTP → telemetry → IPC 순서와 timeout을 명시한다.

### 비목표

- handler endpoint 이름 변경
- JSON response 구조 변경
- auth 검증 알고리즘 변경
- cloudflared lifecycle 재설계

### 테스트

- 두 개의 `Server`가 서로 다른 registry를 사용한다.
- private ServeMux를 사용해 route가 테스트 사이에 중복 등록되지 않는다.
- cancel 시 server와 background service가 종료된다.
- 존재하지 않는 session은 기존과 동일하게 404를 반환한다.

### 완료 기준

- `http.DefaultServeMux`를 사용하지 않는다.
- handler 의존성이 `Server` 필드 또는 생성자 인자로 드러난다.
- `main()`은 설정 파싱, 객체 조립, 실행만 담당한다.

### 권장 커밋

```text
refactor: introduce daemon app composition root
```

## Phase 3: 인증 상태와 JWKS cache 객체화

### 목적

인증 설정, JWT 검증, JWKS cache를 하나의 수명으로 묶고 fail-closed 동작을 보존한다.

### 목표 API

```go
type AuthConfig struct {
    OwnerUUID  string
    ProjectRef string
    InsecureLocalOnly bool
}

type TokenVerifier interface {
    Verify(ctx context.Context, token string) error
}

type SupabaseVerifier struct {
    config AuthConfig
    client *http.Client

    mu      sync.Mutex
    keys    map[string]crypto.PublicKey
    expires time.Time
}
```

### 작업

1. `OwnerUUID`, `SupabaseProjectRef`, `InsecureLocalOnly` global을 제거한다.
2. JWKS cache와 mutex를 `SupabaseVerifier` 필드로 이동한다.
3. middleware가 `TokenVerifier`를 받도록 한다.
4. HTTP client timeout과 JWKS TTL을 생성자 옵션으로 둔다.
5. 테스트에서 fake verifier를 주입할 수 있게 한다.
6. 기존 issuer, audience, subject, signing method 검증을 그대로 유지한다.

### 보안 제한

이 단계는 구조 리팩터링이다. 아래 변경을 섞지 않는다.

- WebSocket query token 제거
- origin allowlist 적용
- QR pairing 변경
- insecure LaunchAgent를 production auth로 전환
- 새로운 JWT signing algorithm 도입

이 항목은 인증 객체화 완료 후 별도 보안 작업으로 수행한다.

### 테스트

- production config 누락 시 거부한다.
- insecure-local-only에서 기존 정책을 그대로 재현한다.
- owner UUID 불일치 시 거부한다.
- issuer/audience 불일치 시 거부한다.
- verifier 두 인스턴스의 JWKS cache가 독립적이다.
- 테스트가 package global 저장/복원을 하지 않는다.

### 완료 기준

- 인증 관련 mutable package global이 없다.
- auth test가 병렬 실행 가능하다.
- route별 middleware 적용 상태가 기존과 같다.

### 권장 커밋

```text
refactor: encapsulate authentication and JWKS state
```

## Phase 4: EventStore, LinkStore, CommandBroker 분리

### 목적

데이터 모델과 runtime 저장소를 분리하고 handler/service의 숨은 공유 상태를 제거한다.

### 4.1 EventStore

```go
type EventStore interface {
    Emit(session string, event AgentEvent)
    Append(session string, events []AgentEvent)
    List(session string) []AgentEvent
    Clear(session string)
}
```

- `models` package에는 `AgentEvent` 구조체만 남긴다.
- 기존 단건 100개, parsed event 500개 제한 정책을 하나의 명시된 정책으로 정리한다.
- 반환 slice는 복사본이어야 한다.

### 4.2 LinkStore

```go
type LinkStore interface {
    Load(ctx context.Context) error
    Get(sessionID string) (SessionLink, bool)
    List() []SessionLink
    Put(ctx context.Context, link SessionLink) error
    Delete(ctx context.Context, sessionID string) error
}
```

- 현재 atomic temp file + rename 방식을 유지한다.
- 디렉터리 `0700`, 파일 `0600` 정책을 유지한다.
- registry를 명시적으로 주입해 stale title 검증을 수행한다.

### 4.3 CommandBroker

```go
type CommandBroker interface {
    Put(sessionID string, command []byte)
    Take(sessionID string) []byte
}
```

- 기존 `pendingCmds` map을 이동한다.
- `Take`는 기존처럼 한 번 읽은 명령을 제거한다.
- byte slice ownership을 복사로 명확히 한다.

### 작업 순서

세 store를 한 커밋에 넣지 말고 아래처럼 나눈다.

```text
refactor: move agent events into an instance store
refactor: make session link storage instance-owned
refactor: replace pending command globals with broker
```

### 완료 기준

- `models` package가 runtime cache를 보유하지 않는다.
- link 테스트가 임시 디렉터리를 주입받는다.
- command 테스트가 서로 다른 broker 사이의 격리를 증명한다.
- 기존 history와 approval 응답 동작이 유지된다.

## Phase 5: TelemetryService 객체화

### 목적

telemetry cache, parser cursor, interval, background goroutine을 하나의 service가
소유하게 한다.

### 목표 구조

```go
type TelemetryService struct {
    registry *mux.Registry
    events   EventStore
    links    LinkStore
    notifier Notifier
    interval time.Duration

    mu       sync.RWMutex
    sessions map[string]*sessionStateData
}

func (s *TelemetryService) Run(ctx context.Context) error
func (s *TelemetryService) Snapshot() []SessionTelemetry
func (s *TelemetryService) Clear(sessionID string)
```

### 작업

1. `telemetryCache`와 mutex를 service 필드로 이동한다.
2. `StartTelemetryLoop`을 blocking `Run(ctx)` 또는 명확한 `Start/Wait` lifecycle로
   변경한다.
3. registry, stores, resolver를 생성자 주입한다.
4. ticker interval을 config로 받되 기본값은 2초로 유지한다.
5. process batch snapshot 최적화를 유지한다.
6. JSONL cursor와 parser가 session state에 귀속되는 현재 동작을 유지한다.
7. screen fallback과 approval detection 순서를 유지한다.

### Notifier

전역 `OnApproval` callback을 제거하고 interface를 사용한다.

```go
type Notifier interface {
    ApprovalRequired(ctx context.Context, event ApprovalEvent) error
}
```

구현:

- `ExpoNotifier`
- `NoopNotifier`
- test fake

### 테스트

- service 두 인스턴스의 cache가 격리된다.
- context cancel 후 ticker/goroutine이 종료된다.
- adapter batch snapshot이 cycle당 한 번만 호출된다.
- JSONL event가 기존 상태 전이를 만든다.
- notifier는 approval transition에서만 호출된다.
- `go test -race`에서 cache 접근 race가 없다.

### 완료 기준

- telemetry 관련 mutable package global이 없다.
- callback global이 없다.
- service lifecycle을 `App`이 소유한다.

### 권장 커밋

```text
refactor: make telemetry lifecycle instance-owned
```

## Phase 6: IPC server 객체화와 daemon 종료 정리

### 목적

`/tmp/pokit.sock` listener와 connection goroutine을 App lifecycle에 포함한다.

### 목표 구조

```go
type IPCServer struct {
    path     string
    listener net.Listener
    registry *mux.Registry
    links    LinkStore
}

func (s *IPCServer) Run(ctx context.Context) error
func (s *IPCServer) Close() error
```

### 작업

1. socket path를 config로 이동한다.
2. registry와 LinkStore를 주입한다.
3. context cancel 시 listener를 닫아 `Accept()`를 해제한다.
4. 정상 종료 시 자신이 만든 socket만 삭제한다.
5. socket 생성 후 권한을 명시적으로 `0600`으로 설정한다.
6. legacy plain-text protocol과 JSON link protocol을 유지한다.

### 테스트

- 임시 디렉터리의 Unix socket으로 테스트한다.
- cancel 후 listener와 goroutine이 종료된다.
- socket mode가 `0600`이다.
- JSON link/unlink와 legacy stream protocol이 유지된다.

### 완료 기준

- IPC listener가 package global 또는 fire-and-forget goroutine이 아니다.
- App shutdown 후 socket과 goroutine이 남지 않는다.

### 권장 커밋

```text
refactor: bind IPC server to daemon lifecycle
```

## Phase 7: 모바일 connection state와 API client 정리

### 목적

mutable `config.BASE_URL`과 화면별 중복 fetch/auth 코드를 제거한다.

### 목표 구조

```text
ConnectionProvider
  ├─ baseURL
  ├─ connect(url)
  ├─ disconnect()
  └─ loading

PokitClient
  ├─ listSessions()
  ├─ createSession()
  ├─ deleteSession()
  ├─ history()
  ├─ registerPushToken()
  └─ terminalURL()
```

### 작업

1. `ConnectionContext` 또는 동등한 provider를 추가한다.
2. AsyncStorage의 `BASE_URL` 읽기/쓰기를 provider로 이동한다.
3. `config.BASE_URL` mutation을 제거한다.
4. Supabase access token 조회를 공통 API client에 연결한다.
5. Dashboard, Feed, GlobalFeed, History, Approval의 fetch를 client method로 이동한다.
6. HTTP status와 JSON decode 오류를 공통 처리한다.
7. terminal WebView URL 생성도 client가 담당한다.

### 제한

- 화면 디자인 변경 금지
- navigation 구조 변경 금지
- WebSocket protocol 변경 금지
- 이 단계에서 token query 제거 금지
- APK 네이티브 dependency 추가 금지

### 테스트와 검사

- `npx tsc --noEmit`
- 저장된 URL 복원
- QR connect 후 API base URL 변경
- disconnect 후 저장 URL 제거
- Authorization header 포함
- 기존 terminal URL과 session ID 동일

### 완료 기준

- `config.BASE_URL = ...` 대입이 없다.
- 주요 API 호출이 공통 client를 통한다.
- 화면 component가 AsyncStorage를 직접 다루지 않는다.

### 권장 커밋

```text
refactor: centralize mobile connection and API state
```

## 6. 최종 통합 검증

모든 phase 완료 후 아래 순서로 검증한다.

### 6.1 정적 검사

```bash
rg -n '^var \\(' companion-daemon --glob '*.go'
rg -n '^var [A-Za-z_]' companion-daemon --glob '*.go'
rg -n 'config\\.BASE_URL\\s*=' mobile --glob '*.{ts,tsx}'
rg -n 'func init\\(' companion-daemon/internal/mux --glob '*.go'
```

허용되는 package global은 정규식, 불변 설정, 테스트 fixture뿐이어야 한다. 예외는
최종 보고서에 이유를 기록한다.

### 6.2 자동 검사

```bash
git diff --check

cd companion-daemon
GOCACHE=/tmp/devremote-go-cache go vet ./...
GOCACHE=/tmp/devremote-go-cache go test ./...
GOCACHE=/tmp/devremote-go-cache go test -race ./...

cd ../mobile
npx tsc --noEmit
```

### 6.3 macOS 실환경

1. LaunchAgent를 재설치한다.
2. daemon PID와 listening socket을 확인한다.
3. `cmux capabilities`가 `allowAll`인지 확인한다.
4. `/api/sessions`에서 tmux/cmux session이 모두 표시되는지 확인한다.
5. 모든 cmux session이 `stale=false`, `lastError=null`인지 확인한다.
6. daemon log의 `Broken pipe`, `cmux tree failed` 발생 수를 확인한다.
7. cmux history를 조회한다.

### 6.4 모바일 실기기

tmux:

- Dashboard 표시
- live 화면
- 일반 텍스트 입력
- Enter
- Ctrl+C
- resize
- 재연결

cmux:

- Dashboard 표시
- 최초 화면 즉시 표시
- 화면 변화 반영
- 일반 텍스트 입력
- `LF`, `CR`, `CRLF` Enter
- 방향키와 Escape
- session 종료 시 일관된 오류/제거

공통:

- 앱 background → foreground
- daemon 재시작 후 재연결
- 두 session 사이 전환
- approval Y/N
- history/activity 표시

## 7. 검증 에이전트 운영 규칙

각 구현 phase가 push되면 검증 담당은 다음 순서로 확인한다.

1. 기준 commit과 구현 commit을 비교한다.
2. phase 범위를 벗어난 변경이 없는지 확인한다.
3. 새 구조가 singleton wrapper인지 실제 instance ownership인지 확인한다.
4. 생성자에서 모든 의존성이 드러나는지 확인한다.
5. test가 global reset에 의존하지 않는지 확인한다.
6. error를 무시하거나 기존 테스트를 약화하지 않았는지 확인한다.
7. race test를 실행한다.
8. 해당 phase acceptance criteria를 하나씩 판정한다.
9. 실패하면 다음 phase 진행을 중단한다.

검증 결과는 다음 형식을 사용한다.

```text
Phase:
Commit:
Scope: PASS/FAIL
Build: PASS/FAIL
Unit tests: PASS/FAIL
Race tests: PASS/FAIL
Acceptance criteria: PASS/FAIL
Regression risk:
Required fixes:
Decision: ACCEPT / REJECT
```

## 8. 중단 및 롤백 기준

다음 상황에서는 다음 phase로 진행하지 않는다.

- tmux 또는 cmux session ID가 변경됨
- 모바일 화면 또는 입력 E2E가 깨짐
- cmux socket 오류가 다시 반복됨
- race detector 실패
- 테스트를 통과시키기 위해 skip 또는 sleep을 추가함
- 새 `Default*` mutable singleton이 도입됨
- 하나의 state가 global과 instance에 동시에 존재함
- handler가 dependency 없이 package 함수로 global state에 접근함

롤백은 전체 브랜치 reset이 아니라 문제 phase commit의 revert 또는 후속 수정
commit으로 수행한다. 이미 검증된 이전 phase commit은 보존한다.

## 9. 예상 커밋 순서

```text
1. test: lock multi-adapter registry behavior before refactor
2. refactor: make mux registry instance-owned
3. refactor: introduce daemon app composition root
4. refactor: encapsulate authentication and JWKS state
5. refactor: move agent events into an instance store
6. refactor: make session link storage instance-owned
7. refactor: replace pending command globals with broker
8. refactor: make telemetry lifecycle instance-owned
9. refactor: bind IPC server to daemon lifecycle
10. refactor: centralize mobile connection and API state
11. docs: record global state refactor verification
```

구현 에이전트는 여러 번호를 squash하지 않는다. 검증 담당이 각 ownership 이동의
회귀 범위를 독립적으로 확인할 수 있어야 한다.

## 10. 최종 완료 정의

아래 조건을 모두 만족해야 전역 상태 리팩터링 완료로 판정한다.

- runtime mutable state가 `App` 하위 객체에 귀속된다.
- mux registry가 인스턴스이며 `init()` 자동 등록이 없다.
- 인증 설정과 JWKS cache가 verifier 인스턴스에 귀속된다.
- event/link/command/telemetry state가 각각 명시적인 owner를 가진다.
- HTTP server가 private ServeMux를 사용한다.
- daemon background lifecycle이 context로 종료된다.
- 모바일 base URL과 API 호출이 connection/client 계층에 중앙화된다.
- `go vet`, unit test, race test, TypeScript 검사가 통과한다.
- tmux와 cmux 실제 모바일 E2E가 기준선과 동일하게 동작한다.
- 구조 개선 과정에서 protocol 또는 보안 정책이 암묵적으로 변경되지 않는다.

이 계획의 성공 기준은 파일 수나 전역변수 검색 결과가 아니다. 새 adapter, store,
auth verifier 또는 test server를 만들 때 기존 process singleton을 건드리지 않고
독립 인스턴스를 생성할 수 있어야 한다.
