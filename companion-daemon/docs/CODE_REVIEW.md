# 🕵️‍♂️ DevRemote Multi-Agent Code Review Report (2026-07-02)

이 문서는 최신 커밋(`8bf2b92d0`, `0438566d7`)에 대해 React Native 프론트엔드 에이전트와 Go 백엔드 에이전트가 교차 검증한 심층 코드 리뷰 리포트입니다. 다음 에이전트가 코드를 수정할 때 반드시 참고해야 할 **7개의 치명적인 버그 및 병목 현상**을 담고 있습니다.

---

## 📱 프론트엔드 (React Native / WebRTC) 리뷰 결과
**위치:** `devremote/mobile/src/...`

### 🚨 1. Stale Closure로 인한 '빈 응답' 전송 버그 (치명적)
* **위치:** `SessionScreen.tsx` 내 `respond` 호출부 및 `FlatList` 렌더링
* **문제:** `AskUserQuestion` 객관식 질문 답변을 선택할 때, `setAnswer(item.id, label)` 호출 직후 바로 `respond(item, true)`를 호출합니다. React의 상태 업데이트는 비동기이므로, `respond` 함수는 상태가 업데이트되기 전의 이전 값(빈 문자열 등)을 데몬으로 전송해버립니다.
* **추가 문제:** `FlatList` 컴포넌트에 `extraData={answers}` 속성이 누락되어 있어, 타이핑을 해도 렌더링이 갱신되지 않고 `renderAlert` 콜백들이 마운트 당시의 빈 상태(Stale Closure)에 갇혀 있습니다.
* **수정 방향:** `setAnswer` 직후 `respond`를 호출하지 말고, 함수 스코프 내에서 캡처한 변수를 명시적으로 `transport.sendMessage`에 담아 보내거나 렌더링 구조를 개선해야 합니다.

### 🚨 2. 리스너 중복 등록 (Memory Leak)
* **위치:** `SessionScreen.tsx` 내 `AppState` 처리부
* **문제:** 앱이 포그라운드로 올 때마다 `connect()`를 재호출하는데, 이때 `transport.onStatusChange`와 `transport.onAlert`가 반환하는 클린업(cleanup) 함수들을 저장/실행하지 않고 버립니다.
* **영향:** 포그라운드/백그라운드 전환을 반복하면 이벤트 리스너가 끝없이 중복 등록되어 알림이 수십 개로 중복되어 나타납니다.

### ⚠️ 3. JS-Native 브릿지 성능 병목 (PTY 렌더링)
* **위치:** `FeedScreen.tsx` 내 PTY 처리부
* **문제:** 수많은 PTY 청크(Chunk)를 받을 때마다 1:1로 WebView(`xterm.js`)에 `injectJavaScript`를 호출하고 있습니다.
* **영향:** `npm install` 등 대량의 텍스트가 쏟아질 때 밀리초 단위로 JS 브릿지를 건너가게 되어 **모바일 UI 스레드가 완전히 멈춰버리는(Freezing)** 현상이 발생합니다. 50ms~100ms 단위로 데이터를 모아서 한 번에 주입하는 버퍼링(Batching)이 필수적입니다.

---

## 🖥 백엔드 (Go Daemon / PTY Wrap) 리뷰 결과
**위치:** `devremote/companion-daemon/internal/...`

### 🚨 1. WebSocket 동시 쓰기 패닉 (Data Race)
* **위치:** `server/ws.go` 내 `HandleAlert` 및 `SendRaw`
* **문제:** 클라이언트에게 메시지를 보낼 때 `go conn.WriteMessage(...)` 형태로 고루틴을 띄워 비동기 전송을 합니다.
* **영향:** Gorilla WebSocket 패키지는 동일한 커넥션(`conn`)에 대한 동시 쓰기를 엄격히 금지합니다. 짧은 간격으로 PTY 청크가 쏟아지면 Data Race가 발생하여 **데몬이 Panic을 일으키고 강제 종료**됩니다. `WriteMessage` 호출부를 뮤텍스(Mutex)로 보호하거나 전송 전용 채널(Channel) 기반의 워커(Worker)를 두어야 합니다.

### 🚨 2. PTY 리더 블로킹 및 스트리밍 마비
* **위치:** `wrap/wrap.go` 내 `notifyDaemon` (혹은 `post` 함수)
* **문제:** 모바일로 승인 프롬프트를 전달하기 위해 HTTP POST 요청을 보내는데, `http.DefaultClient`를 사용하여 Timeout 설정이 누락되어 있으며 이 호출이 `streamPTY` 루프 내부에서 동기적으로 실행됩니다.
* **영향:** 데몬 서버가 일시적으로 지연되거나 멈추면 PTY 출력을 읽어오는 메인 루프 자체가 블로킹되어 Claude CLI 화면까지 멈춰버립니다. 비동기 발송 또는 짧은 Timeout 처리가 필요합니다.

### 🚨 3. 이벤트 감지 로직 실수 누락 (기능 장애)
* **위치:** `detector/event.go` 내 `Feed` 함수
* **문제:** `tu.Name == "AskUserQuestion"`인 경우만 처리하고 바로 `return`해버립니다. 
* **영향:** `Bash`, `Write`, `Edit` 명령어들은 승인 절차를 거치지 않고 무시되므로, 모바일로 승인 요청이 아예 날아가지 않습니다. `needsApproval` 함수 등을 다시 활용하여 로직을 복구해야 합니다.

### ⚠️ 4. 정규식 매칭의 맹점 (ANSI 파편화 및 청크 잘림)
* **위치:** `wrap/wrap.go` 내 `scanAndRelay` 함수
* **문제:** 
  1) PTY 원본(raw) 텍스트를 그대로 검사하므로 ANSI 색상 코드가 섞여 있으면 `(y/n)` 패턴 매칭이 실패합니다.
  2) 배열 버퍼를 비울 때 `batch = nil`을 할당하여 가비지 컬렉터(GC) 압박을 줍니다. (`batch = batch[:0]` 사용 권장)
  3) 청크가 잘리는 경계선에서 정규식을 검사하면 매칭이 유실됩니다. Rolling buffer 로직 도입이 필요합니다.

---

### 👉 차기 에이전트를 위한 지시사항
본 리뷰 리포트에서 지적된 7가지 사항은 **앱을 프로덕션에서 정상적으로 사용하기 위해 반드시 해결해야 할 Critical Bug**입니다. 다음 에이전트는 새로운 기능을 추가하기 전에 이 문서의 항목들을 하나씩 체크하며 안정화(Refactoring) 작업을 최우선으로 진행해 주십시오.
