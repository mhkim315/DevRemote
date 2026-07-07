# Terminal Adapter Expansion Plan

- 작성일: 2026-07-06
- 기준 브랜치: `feature/phase10-multi-adapter`
- 동작 기준 커밋: `dc026d5`
- 문서 목적: 구현 지시가 아니라, 구현 전에 검증할 확장 전략과 단계별 합격 기준 정의
- 현재 지원 백엔드: tmux, cmux

## 1. 목표

새 터미널 백엔드 하나를 추가할 때 기존 tmux/cmux 구현, HTTP handler, telemetry
state machine, 모바일 화면을 수정하지 않는 구조를 만든다.

확장성은 interface 개수로 판단하지 않는다. 아래 시나리오가 실제로 성립해야 한다.

```text
새 adapter package 작성
  → composition root에 한 번 등록
  → 세션 탐색/표시/history/live I/O/resize/종료가 capability에 따라 동작
  → 지원하지 않는 기능은 명시적으로 disabled
  → 기존 tmux/cmux 회귀 없음
```

최종 증명은 세 번째 실제 터미널을 바로 지원하는 것이 아니라, 먼저
`contract fixture adapter`를 추가해 공통 계약을 통과시키고 그 다음 실제 후보
하나를 얇은 vertical slice로 연결하는 것이다.

## 2. 현재 구조 평가

이미 확보된 기반:

- `App`이 `Registry`를 소유하고 composition root에서 adapter를 등록한다.
- `Registry`가 adapter별 snapshot, singleflight refresh, stale cache를 관리한다.
- canonical ID가 `<adapter>:<local-id>` 형태다.
- live stream, screen, history, process, create/terminate가 capability interface로 분리돼 있다.
- tmux와 cmux가 모바일에서 탐색, 출력, 양방향 입력까지 실기기 검증됐다.

아직 확장 비용을 만드는 결합:

1. `Adapter.ListSessions()`에 `context.Context`가 없어 취소/timeout 계약이 불완전하다.
2. `GetSession(id)`가 존재하지 않는 세션도 객체로 만들 수 있어 discovery와 lookup
   의미가 모호하다.
3. canonical ID 조립/분해가 Registry, handler, mobile 정규식에 분산돼 있다.
4. `term/pty.go`에 tmux fallback과 tmux 전용 초기 snapshot 분기가 남아 있다.
5. `mux/session.go`, `mux/tracker.go`, `term/gemini_resolver.go`에 tmux 직접 호출이 남아 있다.
6. cmux만 Registry refresh callback을 역으로 참조한다.
7. create API의 기본 backend와 모바일 표시가 tmux/cmux를 암묵적으로 가정한다.
8. adapter availability, degraded reason, capability metadata가 API 계약으로 노출되지 않는다.
9. 공통 contract test가 없어 두 구현이 같은 의미를 지키는지 자동 증명할 수 없다.

따라서 현 상태는 “복수 adapter 동작 성공” 단계이며, “세 번째 adapter를 플러그인처럼
추가 가능” 단계는 아니다.

## 3. 설계 원칙

- core는 특정 터미널 명칭이나 CLI를 알지 않는다.
- adapter는 자신의 외부 프로세스, ID 변환, I/O 전략을 전부 소유한다.
- 필수 계약은 작게 유지하고 선택 기능은 capability로 표현한다.
- unsupported와 temporarily unavailable을 구분한다.
- adapter 오류는 빈 성공으로 변환하지 않는다.
- transient discovery 실패는 마지막 정상 snapshot을 보존한다.
- session identity와 display title을 구분한다.
- 외부 API의 canonical ID는 안정적으로 유지한다.
- backend별 출력 품질 차이(snapshot/ANSI/live stream)는 capability로 명시한다.
- 새 backend 추가가 기존 backend 파일 수정으로 이어지면 해당 단계는 실패다.

## 4. 목표 계약 초안

계획 검증 중 정확한 이름은 바꿀 수 있지만 의미는 고정한다.

```go
type Adapter interface {
    Descriptor() Descriptor
    ListSessions(ctx context.Context) ([]Session, error)
}

type Descriptor struct {
    Name         string
    DisplayName  string
    Capabilities CapabilitySet // adapter가 제공할 수 있는 capability의 상한
}

type Session interface {
    LocalID() string
    Title() string
}
```

`LookupSession`은 필수 계약으로 두지 않는다. Registry의 정상 lookup 경로는 adapter 하나의
list snapshot을 refresh한 뒤 local ID를 찾는 방식으로 통일한다. enumeration 없이
안전하게 직접 조회할 수 있는 backend가 실제로 필요할 때만 선택 capability
`SessionLookup`을 추가하며, 이 경로도 list lookup과 동일한 not-found/unavailable 의미를
지켜야 한다.

adapter 이름은 등록 시 Registry가 소유하며 session이 중복 보고하지 않는다. Registry는
`SessionRef{Adapter, LocalID}`를 session과 함께 운반하거나 API DTO를 만들 때 결합한다.
`Descriptor.Capabilities`는 adapter가 제공 가능한 기능의 상한이고, API/UI가 사용하는
effective capability는 실제 session이 구현한 capability에서 산출한다. 두 집합이
불일치하면 contract test가 실패해야 한다.

선택 capability:

- `LiveStreamer`: 양방향 terminal stream과 resize
- `ScreenReader`: 현재 화면 snapshot
- `HistoryReader`: scrollback
- `ProcessProvider` / `ProcessSnapshotProvider`: agent telemetry용 프로세스 정보
- `SessionCreator`
- `SessionTerminator`
- 필요 시 `KeyWriter`; 일반 입력과 의미가 중복되면 제거 후보

`OutputStream`, `InputWriter`, `Resizer`, `Closer`, `TerminalStream`의 중복은 Phase 2에서
정리한다. 계약은 “한 기능을 표현하는 표준 경로가 하나”여야 한다.

## 5. 단계별 계획

### Phase 0 — 기준선과 회귀 행렬 고정

작업:

- `dc026d5`의 tmux 9개 + cmux 3개 실기기 성공 상태를 기준선으로 기록한다.
- discovery, history, initial screen, live output, text, Enter, Ctrl+C, resize,
  disconnect/reconnect, transient adapter failure를 행렬화한다.
- adapter별 현재 지원 여부와 출력 품질을 기록한다.
- 외부 API schema와 canonical ID 예제를 golden fixture로 고정한다.

합격 기준:

- tmux/cmux에 동일한 검증 항목이 적용된다.
- “현재 우연히 동작하는 behavior”와 “보장할 contract”가 구분된다.
- 구현 변경은 없다.

권장 커밋:

```text
test: lock terminal adapter behavior matrix
```

### Phase 1 — Identity와 discovery 계약 확정

작업:

- `SessionRef{Adapter, LocalID}`를 canonical ID의 유일한 parser/formatter로 만든다.
- local ID에는 `:`가 포함될 수 있음을 테스트한다 (`tmux:aider` 회귀).
- snapshot refresh 후 lookup을 기본으로 하고, 선택 `SessionLookup`을 도입할 조건과
  동일한 오류 의미를 정의한다.
- not found, unavailable, unsupported, timeout을 구분할 typed/sentinel error를 정한다.
- duplicate adapter name 등록은 명시적으로 실패시킨다.
- deterministic ordering은 Registry 정책으로 유지한다.
- adapter name 문법과 빈 adapter/local ID, control character를 거부하는 규칙을 정한다.
- create 결과는 local ID로 통일하고 Registry/API 경계에서 정확히 한 번 canonicalize한다.
- stale snapshot의 session을 발견한 경우 live I/O 전 강제 refresh 성공 시 not-found와
  refresh 실패 시 unavailable을 구분한다.

합격 기준:

- handler/mobile/core가 정규식이나 문자열 split으로 backend를 추측하지 않는다.
- 빈 목록과 adapter 조회 실패가 구분된다.
- 콜론 포함 이름, Unicode 이름, URL encode/decode, duplicate local ID, 세션 종료 race가
  테스트된다.

### Phase 2 — Capability 모델 정리

작업:

- live stream 관련 중복 interface를 하나의 계약으로 축소한다.
- initial screen을 tmux 이름이 아니라 `ScreenReader` capability로 선택한다.
- create/delete/history/resize가 concrete adapter가 아닌 capability만 검사하게 한다.
- unsupported 응답의 HTTP status와 JSON error 형식을 고정한다.
- `/api/sessions`에 최소 capability metadata를 추가할 필요를 검증한다.
  추가한다면 모바일은 metadata만 소비하고 backend 이름을 분기하지 않는다.
- API에는 기존 필드를 유지하면서 additive `displayId`, effective `capabilities`,
  adapter health를 추가하고 구버전 payload를 읽는 모바일 호환 테스트를 둔다.
- 모바일의 backend 이름 정규식 제거와 capability 기반 버튼 처리를 이 Phase에서
  완료한다. fixture와 실제 세 번째 backend보다 뒤로 미루지 않는다.

합격 기준:

- `internal/term` production code에 `tmux`/`cmux` 조건문이 없다.
- capability가 없는 fixture session도 panic, reconnect storm, silent fallback 없이 동작한다.
- 기존 API 호환이 유지되거나 명시적 versioning 계획이 있다.
- 미지의 adapter와 capability 필드가 없는 구버전 응답을 모두 안전하게 표시한다.

### Phase 3 — 실행 환경과 adapter lifecycle 표준화

작업:

- command 실행을 adapter별 injectable runner로 통일한다.
- binary discovery, environment, socket path, timeout, stderr 보존 정책을 정의한다.
- availability probe와 runtime health를 분리한다.
- adapter가 Registry를 역참조하는 cmux refresh callback을 event/invalidation 계약으로
  치환할지 검증한다.
- adapter가 Registry refresh를 동기 호출하지 않도록 one-way invalidation/health event의
  소유권, backpressure, 종료 순서를 정의한다.
- LaunchAgent, foreground terminal, GUI socket 환경 차이를 health diagnostics에 반영한다.

합격 기준:

- 테스트가 실제 tmux/cmux 바이너리 없이 주요 오류 경로를 재현한다.
- 명령 실패가 `nil, nil` 또는 빈 세션 성공으로 변환되지 않는다.
- 한 adapter 고장이 다른 adapter 목록과 I/O를 막지 않는다.
- adapter refresh를 독립적인 timeout 아래 병렬화해 느린 adapter가 다른 adapter의
  discovery 응답을 지연시키지 않는다.
- stale snapshot 여부와 마지막 오류가 관측 가능하다.

### Phase 4 — 공통 contract test harness

작업:

- adapter factory를 받아 동일 테스트를 실행하는 reusable suite를 만든다.
- 필수 항목:
  - descriptor/name 안정성
  - list/snapshot lookup identity 일치
  - context cancellation과 timeout
  - not-found 의미
  - concurrent list 안전성
  - transient failure 후 stale snapshot
  - colon/Unicode local ID
  - invalid/empty ID, URL round-trip, duplicate adapter/local ID
  - stale session의 종료 race와 unavailable/not-found 구분
  - create가 local ID를 반환하고 API가 한 번만 canonicalize함
  - 한 adapter의 hang/error가 다른 adapter 응답을 지연·차단하지 않음
- capability별 suite:
  - live read/write/resize/close
  - screen/history
  - create→discover→terminate
  - process snapshot key identity
  - descriptor 상한과 session effective capability 일치
- tmux/cmux mock runner가 이 suite를 통과하게 한다.

합격 기준:

- 새 adapter 작성자는 자체 테스트 외에 공통 suite 한 줄 등록으로 계약을 검증한다.
- tmux/cmux 모두 같은 필수 suite를 통과한다.
- `go test -race ./...`가 통과한다.

### Phase 5 — Contract fixture adapter로 무수정 확장 증명

작업:

- 외부 프로그램에 의존하지 않는 in-memory 또는 child-PTY fixture adapter를 만든다.
- 최소 discovery + live stream + resize를 구현한다.
- 선택 capability 하나는 의도적으로 지원하지 않아 unsupported 경로를 검증한다.
- production 기본 등록은 하지 않는다. test composition root에서만 등록한다.
- HTTP/WebSocket/telemetry/mobile schema 경계까지 end-to-end test를 만든다.

합격 기준:

- fixture 추가 시 tmux/cmux 파일 변경이 0줄이다.
- term handler 변경 없이 세션이 API와 WebSocket에 나타난다.
- unsupported capability가 예측 가능한 UI/API 결과를 낸다.
- 이 단계가 통과해야 실제 세 번째 backend 작업을 승인한다.

Phase 5 scope 참고사항 (2026-07-07 검증 과정에서 확정):

- **Resize**: production에는 서버 측 `/term/size` Go handler가 존재하지 않으며,
  tmux/cmux 모두 xterm.js client-side resize에 의존한다. 이는 fixture adapter의
  한계가 아니라 아키텍처 선택이다. Phase 5에서는 `TerminalStream.Resize`
  인터페이스 계약을 mux-level contract test로 검증하며, HTTP boundary를 통한
  resize 증명은 scope out 한다. 서버 측 resize handler 추가는 Phase 6+에서
  검토한다.

- **Mobile schema**: mobile 코드에는 `adapter: string` 필드를 사용하므로
  알려지지 않은 adapter 이름("fixture" 등)이 TypeScript 컴파일을 통과한다.
  `SessionTelemetry` JSON 응답의 필수 키 검증은 Go backend에서 수행하며,
  mobile 전용 typed fixture test infrastructure는 존재하지 않으므로
  `npx tsc --noEmit` 통과로 mobile schema 경계를 충족한 것으로 간주한다.

### Phase 6 — 실제 세 번째 backend vertical slice

후보 선택 기준:

- 공식적이고 자동화 가능한 API/CLI가 있는가
- session discovery와 양방향 I/O가 가능한가
- 사용자 권한 범위에서 인증 가능한가
- macOS GUI lifecycle에 종속될 경우 환경 요구를 탐지할 수 있는가
- 테스트용 mock/fixture를 만들 수 있는가

작업 순서:

1. 후보 조사 문서만 작성하고 하나를 선택한다.
2. discovery + screen/history read-only slice를 먼저 구현한다.
3. live output과 input을 추가한다.
4. resize/reconnect/termination은 지원 가능한 capability만 추가한다.
5. production composition root 등록은 feature flag 뒤에서 시작한다.

합격 기준:

- core, tmux, cmux production 파일을 수정하지 않고 등록 지점만 한 줄 추가한다.
- 공통 contract suite와 실제 smoke test를 통과한다.
- backend가 설치되지 않은 사용자의 기존 동작에 영향이 없다.

### Phase 7 — 운영 진단과 문서

작업:

- Phase 2에서 확정한 모바일 capability-driven UX를 실제 세 번째 backend로 smoke한다.
- unavailable/degraded/ended 상태 표현과 구버전 daemon 연결을 회귀 검증한다.
- 설치 조건, 권한, socket, foreground/LaunchAgent 지원 범위를 adapter별 문서화한다.
- adapter 진단 endpoint 또는 CLI의 필요성을 검증한다.

합격 기준:

- 모바일 코드에 지원 backend 이름 목록이 없다.
- 미지의 adapter도 이름, 상태, 가능한 기능을 안전하게 표시한다.
- 사용자가 “세션 없음”과 “adapter 연결 실패”를 구분할 수 있다.

## 6. 단계 운영 규칙

역할은 분리한다.

- 실행 에이전트는 승인된 Phase만 구현하고 커밋을 제출한다.
- 검증 에이전트는 실행 커밋을 독립적으로 검토하고 테스트하며 검증 문서에 코멘트를
  남긴다.
- 이 문서를 이어받는 다음 세션의 에이전트는 검증 에이전트다.
- 검증 에이전트는 사용자가 명시적으로 역할을 변경하지 않는 한 구현을 대신하지 않는다.
- REJECT 판정 시 검증 에이전트는 파일/line, 재현 근거, 기대 동작, 수정 방법을 포함한
  corrective guide를 커밋·푸시한다.
- 실행 에이전트는 해당 corrective guide를 반영한 새 커밋을 제출한다.
- 검증 에이전트가 ACCEPT를 문서화하기 전에는 다음 Phase를 시작하지 않는다.

각 Phase는 별도 커밋으로 제출하며 다음 Phase 구현 전에 검증 판정을 받는다.

```text
Decision: ACCEPT | REJECT
Scope:
Contract:
Backward compatibility:
Automated tests:
Race tests:
Host integration:
Mobile smoke:
Blocking findings:
Deferred findings:
```

공통 자동 검증:

```bash
git diff --check
cd companion-daemon
gofmt -w <변경 파일>
GOCACHE=/tmp/devremote-adapter-go-cache go vet ./...
GOCACHE=/tmp/devremote-adapter-go-cache go test ./...
GOCACHE=/tmp/devremote-adapter-go-cache go test -race ./...
cd ../mobile
npx tsc --noEmit
```

금지 사항:

- Phase 승인 전에 다음 Phase 일부 구현
- backend 이름 조건문을 capability 분기라고 주장
- 오류를 빈 성공으로 변환
- 실제 바이너리가 필요한 테스트를 이유 없이 skip
- 기존 tmux/cmux behavior를 동시에 재설계
- 세 번째 backend를 먼저 만들고 나중에 contract를 맞추는 순서

## 7. 전체 완료 정의

다음 조건을 모두 만족해야 “확장 기반 완료”로 판정한다.

- tmux/cmux가 공통 contract suite를 통과한다.
- fixture adapter가 기존 backend 및 handler 수정 없이 E2E로 동작한다.
- core와 mobile에 tmux/cmux 이름 기반 분기가 없다.
- adapter 장애 격리, stale cache, diagnostics가 자동 검증된다.
- 실제 세 번째 backend가 feature flag 아래에서 최소 read/write vertical slice를 통과한다.
- 기존 모바일 실기기 tmux/cmux 양방향 동작에 회귀가 없다.
