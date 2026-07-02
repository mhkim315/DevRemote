# DevRemote Product Roadmap

DevRemote는 현재 **"개인용 완벽 연결 관제탑 (1:1 P2P)"** 단계를 달성했습니다.
앞으로는 현재 확보된 안정적인 통신망 위에서 **단일 사용자의 경험(UX)을 극대화하는 편의 기능**을 먼저 완성한 뒤, 마지막에 **SaaS(다중 사용자) 아키텍처**를 덧붙이는 순서로 개발을 진행합니다.

---

## 🚀 Phase 4: 코어 UX 극대화 (Single-Tenant)
Supabase 연동 전에, 로컬 환경에서 개발자의 편의성을 200% 끌어올릴 핵심 기능들을 구현합니다.

### 1. 🤖 다중 에이전트 동적 대시보드 (tmux 기반)
- **목표:** Mac에 띄워둔 여러 터미널/AI 세션을 폰에서 실시간으로 보고 골라서 들어간다.
- **구현 방식:**
  - Mac 데몬이 `tmux list-sessions` 명령어를 스캔.
  - 데몬에 `GET /api/sessions` 엔드포인트를 추가하여 활성 세션 목록 반환.
  - 폰 앱(`DashboardScreen.tsx`)이 해당 API를 호출하여 하드코딩된 리스트 대신 실제 세션 목록(예: `aider-backend`, `claude-frontend`)을 렌더링.

### 2. 🪝 Claude/Aider 완벽 후킹 & 상호작용
- **목표:** 터미널을 보지 않고도 AI의 질문을 폰으로 받고, 버튼 하나로 답변을 전송한다.
- **구현 방식:**
  - 단순 `[Approval Required]` 텍스트 감지를 넘어, 프롬프트를 가로채는 래퍼(Wrapper) 스크립트 작성 (`devremote hook`).
  - AI가 질문을 던지면 폰에 모달 알림이 뜨고, 사용자가 앱에서 **[Yes / No]** 버튼을 누르면 터미널로 `Y\n`이 자동 주입됨.

### 3. ⌨️ 모바일 코더 전용 키보드 매크로
- **목표:** 스마트폰 키보드의 한계를 극복하고 터미널 조작 스트레스를 없앤다.
- **구현 방식:**
  - `FeedScreen.tsx` 터미널 뷰 상단에 퀵 액션 버튼바 추가.
  - 필수 단축키: `Ctrl+C`, `Enter`, `Y/N`, `Esc`, `방향키(↑, ↓)` 제공.

---

## 🌍 Phase 5: 상용 SaaS 전환 (Multi-Tenant)
단일 사용자 경험이 완벽해지면, 전 세계 개발자들이 쓸 수 있도록 아키텍처를 확장합니다.

### 1. 계정 및 인증 시스템 연동
- 삭제되었던 **Supabase Auth**를 복구하여 GitHub / 이메일 로그인 도입.
- 모바일 앱과 Mac 데몬(`devremote login`) 양쪽 모두 인증 상태 유지.

### 2. 동적 터널링 (Dynamic URL Mapping)
- `term.fullcount.kr` 같은 개인 고정 주소 대신, 다수를 위한 동적 할당 도입.
- 데몬 실행 시 랜덤 터널(예: `trycloudflare.com`) 생성 후, 발급된 URL을 Supabase DB의 `machines` 테이블에 자동 업데이트 (`user_id`, `machine_name`, `url`).
- 모바일 앱은 DB에서 내 기기들의 현재 접속 주소를 동적으로 읽어와 연결.

### 3. 상태 관리 및 푸시 중앙화
- **Heartbeat:** 데몬이 1분마다 DB에 생존 신고. 끊기면 앱에서 🔴 `Offline` 처리.
- **Push Server:** 데몬이 직접 폰으로 알림을 쏘지 않고, Supabase Edge Function을 거쳐 안전하게 FCM/APNs 푸시 발송.
