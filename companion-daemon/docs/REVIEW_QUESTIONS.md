# DevRemote 검토 질문 — 2026-07-01 (새 에이전트 인수인계)

> 이전 대화에서 이어서 작업. 브랜치: `companion-daemon`

## 아키텍처 평가

전체적으로 RustDesk 포크 → Go 클린룸 피벗은 완전히 옳은 방향이다.
JSONL 직접 파싱 접근법은 탁월한 통찰이다. MIT 스택만 사용해 저작권 문제도 원천 해결됨.

## 확인 필요한 핵심 질문

### 1. 엔드투엔드 검증 상태

- APK가 실제 폰에서 설치/실행되었는가?
- WebRTC P2P 연결이 실제로 폰↔Mac 간에 성공했는가?
- 승인(approve/deny) 버튼 → stdout relay가 실제 Claude 세션에서 작동했는가?
- BUGREPORT에 기록된 버그들은 해결 후 **재검증**되었는가? 
  특히 BUG-001(SSL→HTTP REST 전환)과 BUG-003(ICE/SDP 순서)이 수정 후 정상 작동하는가?

### 2. iOS WebRTC 백그라운드

VISION.md에 "미검증"으로 표시된 내용. iOS에서 WebRTC Data Channel이 앱 백그라운드 상태에서도 유지되는지 확인 필요.
이게 안 되면 푸시 알림 → 앱 열기 → 재연결 플로우의 UX가 매우 나빠짐.

### 3. Expo vs EAS Build

현재 Expo SDK 56 dev client로 APK 빌드 중인데:
- WebRTC는 Expo Go에서 지원이 제한적이다. EAS Build(네이티브 빌드)로 전환해야 하는가?
- Expo Go만으로 충분한가? 어떤 네이티브 모듈이 필요한가?

### 4. Windows 지원

- `wrap_signal_windows.go` / `interrupt_windows.go` 구현됨 → 실제 빌드/테스트 완료?
- Windows Defender가 pion/webrtc의 UDP 소켓을 차단하지 않는가?
- `GenerateConsoleCtrlEvent` → 실제 Claude Code Windows 버전에서 Ctrl-C가 제대로 전달되는가?

### 5. 시그널링 서버 장애 대응

Oracle `168.107.59.177:9173`이 단일 장애점이다.
- P2P 연결 수립 후에도 시그널링 서버가 필요한가? 아니면 WebRTC Data Channel만으로 유지되는가?
- 시그널링 서버가 다운되면 이미 연결된 세션은 유지되는가?
- Oracle 프리티어 인스턴스가 예고 없이 정지될 경우 대응 계획?

### 6. 멀티 AI CLI (Phase 4)

- `devremote hook`이 bash/zsh 쉘에서 alias로 claude/codex/gemini/aider를 인터셉트하는데,
  fish shell, PowerShell 사용자는? 현재 bash/zsh만 지원.
- 여러 AI CLI가 동시에 실행될 때 JSONL 파일 충돌은 없는가?

### 7. Play Store 배포

- Expo APK를 Play Store 심사 제출한 경험이 있는가?
- WebRTC P2P + 백그라운드 실행 + 푸시 알림 권한이 Play Store 정책에 저촉되지 않는가?
- 개인정보처리방침(Privacy Policy) 페이지 준비되었는가?

## 요약

전체 아키텍처와 방향성은 완벽하다. 
가장 시급한 건 **실제 폰에서 엔드투엔드 연결 + 승인 테스트 완료**.
iOS 빌드와 Windows 검증은 그 다음.
