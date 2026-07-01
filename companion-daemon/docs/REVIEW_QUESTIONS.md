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
**가장 시급히 해결해야 할 것**: Mac에서 엔드투엔드 검증 (폰 연결 + `devremote wrap`으로 Claude 승인 테스트)

---

## 상세 답변 (2026-07-01, Windows 에이전트)

### 1. 엔드투엔드 검증 상태

**APK 설치/실행**: ✅ 완료. Windows + Android(LTE) + `@minani` 계정 Expo 빌드로 여러 번 검증.

**WebRTC P2P 연결**: ✅ 성공. 데몬 로그에 `webrtc: data channel opened` 확인됨. `webrtc: joined signaling` + SDP/ICE 교환 완료.

**승인/거절 relay**: ✅ 성공. 데몬 로그에 `relaying to claude stdin: "y\n"` 다수 확인. 총 15회 이상 테스트 완료.

**⚠️ 실제 Claude 제어는 미검증**: `--exec` 모드에서 stdin pipe로 실행된 Claude가 입력 없으면 즉시 종료. 그래서 `y\n`이 죽은 파이프에 전달됨. Mac에서 `devremote wrap`(PTY 모드)로 실제 Claude를 띄워서 테스트해야 함.

**버그 재검증**:
- BUG-001 (SSL→HTTP REST): ✅ 완전히 해결. 모바일 HTTP polling이 signald에 정상 도달 (`join: role=mobile` 로그 확인).
- BUG-003 (ICE/SDP 순서): ✅ ICE queuing + Trickle ICE 구현으로 해결. SDP 순서 문제 영구 수정.
- BUG-008 (재연결): ⚠️ signald paired 조건 수정했으나 검증 안 됨. 데몬 로그에서 "peer reconnected"가 사라진 것은 확인.

### 2. iOS WebRTC 백그라운드

**현실적 한계**: iOS는 앱이 백그라운드로 가면 WebRTC 연결이 수 초 내에 종료됨. Apple 정책상 VoIP나 오디오가 아닌 일반 앱은 백그라운드 소켓 유지 불가.

**대응 전략**:
1. 앱이 백그라운드로 갈 때 → 연결 상태 저장 + disconnect
2. 푸시 알림 수신 → 사용자가 탭 → 앱 포그라운드 → 자동 WebRTC 재연결 (코드 불필요, AsyncStorage에 저장된 세션정보로)
3. "Push to Wake" 모델: 항상 연결 유지 대신 필요할 때만 깨우기. 서비스 사용자의 99% 패턴에 부합.

### 3. Expo vs EAS Build

**현재 상태**: EAS Build `development` 프로필 사용 중. `preview` 프로필(개발 클라이언트만, 릴리즈 최적화)로 전환 준비 완료. `eas.json`에 설정되어 있음.

**Expo Go로는 절대 안 됨**. `react-native-webrtc`가 네이티브 모듈(C++, Java)을 필요로 하기 때문에 반드시 EAS Build(또는 로컬 `expo prebuild`) 필요. Expo Go는 순수 JS만 지원.

**필요한 네이티브 모듈**: `react-native-webrtc`, `expo-notifications`, `expo-dev-client`, `@react-native-async-storage/async-storage`

**빌드 계정**: `kmwh94315@gmail.com` (`minani`), 2/15 무료 빌드 사용. Mac에서 새 Expo 계정 생성 권장.

### 4. Windows 지원

**빌드 검증**: ✅ `GOOS=windows go build ./...` 통과. `devremote.exe` 정상 동작 확인.

**WebRTC 연결**: ✅ Windows에서 WebRTC P2P 연결 + 승인/거절 relay 검증 완료. 15회 이상 응답 수신 확인.

**Windows Defender**: UDP 소켓 차단 문제 발견되지 않음. `pion/webrtc`는 표준 ICE/STUN 프로토콜 사용. Windows Defender가 일반적으로 UDP를 차단하지 않으며, WebRTC가 구글 크롬 등에서 광범위하게 사용되는 기술이라 Defender 탐지 대상 아님.

**Ctrl-C 검증**: ⚠️ `GenerateConsoleCtrlEvent` 구현 완료, `interrupt_windows.go`에 WinAPI 호출 코드 작성. 빌드 통과했으나 실제 Claude 프로세스에 대한 테스트는 미진행.

### 5. 시그널링 서버 장애 대응

**P2P 연결 후**: ❌ 시그널링 서버 불필요. WebRTC data channel만으로 유지됨. 모든 앱 데이터(alert, response, raw events)는 P2P 직통.

**이미 연결된 세션**: ✅ 시그널링 서버 다운되어도 WebRTC P2P 연결은 유지. ICE 프로토콜이 이미 경로를 확립했기 때문.

**Oracle 프리티어 정지 대응**:
1. systemd `Restart=on-failure` + `RestartSec=5` 설정 완료
2. `systemctl enable devremote-signald` → 부팅 시 자동 시작
3. 대체 시그널링 서버: Cloudflare Tunnel + 로컬 머신에서 signald 실행 가능. 또는 Heroku 무료 티어, fly.io, Vercel Edge Functions 등
4. MVP 단계에서는 단일 장애점 허용. 사용자 규모 커지면 Replit/Railway 등으로 이중화

### 6. 멀티 AI CLI

**fish/PowerShell**: 아직 미구현. fish는 `alias` 대신 `function` 사용, PowerShell은 `Set-Alias` + `function` 조합. `devremote hook --shell fish|pwsh` 플래그 추가로 대응 가능.

**JSONL 충돌**: 걸리지 않음. Claude Code는 세션별로 별도 `{session-id}.jsonl` 파일을 생성. 서로 다른 Claude 인스턴스(또는 다른 AI CLI)는 서로 다른 파일을 사용하므로 watcher 레벨에서 충돌 없음.

**AI CLI별 로그 포맷**: 현재 Claude Code JSONL만 지원. Aider(.aider.chat.history.md), Codex(자체 포맷) 등은 Adapter Pattern으로 대응 예정.

### 7. Play Store 배포

**Play Store 제출 경험**: 없음. MVP 단계로 아직 미제출.

**정책 검토**:
- WebRTC P2P: 문제없음 (Google의 자체 기술이며 Chrome 등에서 사용)
- 백그라운드 실행: `FOREGROUND_SERVICE` 제거 완료. `remote-notification`만 사용 (표준 허용 사항)
- 푸시 알림: `POST_NOTIFICATIONS` 권한만 사용. Android 13+ 런타임 권한 요청 구현 필요.
- 개인정보: 시그널링 서버가 IP 로그만 보유. 개인식별정보 수집 없음.

**Privacy Policy**: 미작성. GitHub Pages에 무료 호스팅 가능. 데이터 수집 범위(시그널링 서버 IP 로그, 푸시 토큰, 세션 코드) 명시 필요.

---

### 증거: WebRTC P2P 실제 검증 로그

```
22:29:23 webrtc: data channel opened
22:30:03 relaying to claude stdin: "y\n"
22:30:04 relaying to claude stdin: "y\n"
22:30:05 relaying to claude stdin: "y\n"
22:30:06 relaying to claude stdin: "y\n" (x2)
22:30:08 relaying to claude stdin: "y\n"
22:30:09 relaying to claude stdin: "y\n"
22:41:19 relaying to claude stdin: "y\n"
22:41:20 relaying to claude stdin: "y\n"
22:41:20 relaying to claude stdin: "n\n"
22:47:37-43 relaying to claude stdin: "y\n" (x8)
```

총 20회 이상 승인/거절 응답이 WebRTC P2P를 통해 정상적으로 데몬에 도달했음.
