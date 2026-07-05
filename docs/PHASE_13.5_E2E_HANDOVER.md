# Phase 13.5: JSONL-First Telemetry E2E Handover

본 문서는 Phase 13.5 (다중 에이전트 JSONL 상태 엔진 최적화 및 V2 결함 수정) 작업 완료 후, 실제 환경(Mac/tmux)에서 엣지 케이스들을 검증하기 위한 가이드라인입니다.

## 🚀 수정된 아키텍처 개요
기존 `tmux capture-pane` 기반의 무거운 폴링 방식에서 벗어나, **에이전트별 JSONL 로그를 최우선으로 파싱**하여 세션 상태(`idle`, `thinking`, `working`, `waiting`)를 찰나의 지연 없이 정확하게 추론하도록 하이브리드 상태 엔진을 구현했습니다.
(단, CLI에서 발생하는 Approval Prompt `(y/n)` 대기 상태를 안전하게 포착하기 위해 `capture-pane`은 여전히 백업/폴백 용도로 동작합니다.)

---

## ✅ 중점 E2E 검증 포인트 (3대 엣지 케이스)

다음 3가지 케이스는 샌드박스 유닛 테스트로는 완벽한 모사가 어려우므로, 실제 쉘(tmux) 환경에서 구동하며 `companion-daemon`이 데이터를 어떻게 파싱하는지 확인해야 합니다.

### 1. 동시 Codex 세션 구동 시 프로세스 트리 맵핑
- **시나리오**: `tmux` 안에서 여러 개의 Codex 세션을 동시에 실행시켰을 때, 데몬이 서로 엉키지 않고 각각의 CWD와 로그를 추적할 수 있는지 확인.
- **검증 방법**:
  1. 서로 다른 폴더에서 Codex 에이전트를 두 개 띄웁니다.
  2. POKIT UI (또는 `/api/v2/sessions` 엔드포인트)에 접속합니다.
  3. **체크포인트**: `tracker.go`에 구현된 `ps -p <pid> -o lstart=` 타임존(Local Time) 파싱 로직이 제대로 동작하여, 두 세션의 이벤트 ID와 로그 내역이 교차 오염 없이 정확히 독립적으로 렌더링되는지 확인합니다.

### 2. Claude Nested Array `tool_result` 처리
- **시나리오**: Claude 에이전트가 단일 텍스트가 아닌 `[]interface{}` 형태의 배열(Array) 페이로드로 툴 실행 결과를 반환할 때 이를 정확히 파싱하는지 확인.
- **검증 방법**:
  1. Claude 에이전트를 띄우고, 파일 읽기나 쉘 명령어 등 Tool Use를 유발하는 프롬프트를 입력합니다.
  2. POKIT 모바일 UI에서 확인합니다.
  3. **체크포인트**: 배열 내에 깊게 중첩된 `type=tool_result` 객체에서 `tool_use_id`와 `content`를 정확히 뽑아내어, UI 상에서 `Tool Result`라는 명시적인 이벤트 블록으로 연결되어 그려지는지 확인합니다.

### 3. Gemini Wrapper의 환경변수 등록 (`POKIT_AGENT_SESSION_ID`)
- **시나리오**: Gemini (Antigravity) 에이전트 실행 시, 데몬이 해당 세션을 식별하기 위한 환경변수 계약이 제대로 지켜지고 있는지 확인.
- **검증 방법**:
  1. Gemini 에이전트를 실행(`agy` 래퍼 등 사용)합니다.
  2. 실행 중인 쉘에서 `tmux show-environment`를 실행해 봅니다.
  3. **체크포인트**: `POKIT_AGENT_SESSION_ID=...` 값이 `tmux` 환경 변수로 명시적으로 세팅되어 있는지 확인합니다. 만약 래퍼(Wrapper) 쪽에 해당 로직이 누락되었다면, 에이전트가 켜질 때 `tmux set-environment POKIT_AGENT_SESSION_ID <id>`를 주입하도록 래퍼 코드를 보강해야 합니다.

---

## 💡 상태 엔진 타임아웃 및 폴백(Fallback) 관전 포인트

- **Idle 타임아웃 동작**: 에이전트가 명령(Tool)을 실행 중이어서 `working` 상태가 되었으나, 10초 이상 새로운 로그(JSONL) 이벤트가 발생하지 않을 때 상태가 `working → thinking → idle` 순으로 자연스럽게 떨어지는지 확인합니다. 영구적으로 고착(Lock)되지 않아야 합니다.
- **Approval Prompt 소멸**: `waiting` 상태일 때 사용자가 `y`를 누르거나 프롬프트가 사라지면, 새로운 이벤트가 즉각 찍히지 않더라도 상태가 `idle`로 잘 돌아오는지 확인합니다.

---

## 🛠️ 알려진 기술 부채 (Technical Debt)
*기능 자체에는 결함이 없으나, 차기 최적화 페이즈에서 다루면 좋은 내용입니다.*

1. **상태 엔진의 완전한 결정론(Deterministic) 보장**: `evaluateState()` 내부에 남아있는 `time.Now()`와 `time.Since()` 의존성을 인자(`now time.Time`)로 빼서, Fake Clock을 통한 100% 모의 유닛 테스트가 가능하도록 고도화.
2. **Tmux Polling 캐싱(Caching) 최적화**: 2초 주기의 `capture-pane`, `lsof`, `resolver` 호출 비용은 아직 그대로입니다. PID나 로그 파일 변경이 감지되지 않으면 이전 상태를 캐싱하고, 무거운 폴링은 최소화하거나 이벤트 훅(Event Hook) 기반으로 전환하는 최적화 작업.
