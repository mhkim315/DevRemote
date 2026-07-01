# DevRemote — Project Manifesto

> 마지막 업데이트: 2026-07-01. 이 문서는 모든 피벗과 현재 아키텍처를 담고 있습니다.

## 1. 제품 정의 (What We Build)

**AI CLI 도구를 모바일에서 감독하고 통제하는 Human-in-the-Loop 대시보드.**

단순 알림이 아니다. 모바일 화면에 최적화된 "비동기 AI 작업 큐" 관리 도구다. 내 PC에서 돌고 있는 Claude Code(또는 Codex, Gemini CLI 등)의 작업을 모바일로 리뷰하고 승인한다. 터미널을 몰라도 된다.

## 2. 타겟 고객

**바이브 코더 (Vibe Coders)** — 터미널보다 GUI에 익숙한, AI에게 일을 시키고 결과를 기다리는 모든 사람.

핵심 페인포인트:
- 퇴근 전 Claude에 작업 지시 → 지하철에서 결과 확인하고 싶다
- 외출 중 AI가 "이 파일 덮어쓸까요?" 물어봄 → AnyDesk 켜기도 귀찮고 결국 PC 앞으로 간다
- SSH, tmux, PTY 같은 건 모른다. 앱 하나 깔고 코드 한 번 입력하는 게 한계
- AI CLI 도구 여러 개 (Claude, Codex, Gemini) 쓰는데 각각 다 켜놓고 관리 못 함

**수익 모델 (검토 중):**
- 무료: 1세션, LAN only, 기본 알림
- Pro ($3-5/월): WebRTC 원격, 멀티세션, 푸시 알림, 세션 히스토리

## 3. 피벗 히스토리 (What We Killed)

| 시도 | 결과 | 교훈 |
|---|---|---|
| RustDesk 포크 (AGPL) | 폐기 | 90만줄 유지보수 불가, 라이선스 리스크 |
| DevKeypad + FAB | 보관 | 모바일 키보드로 코딩 제어는 한계 |
| 60fps 화면 스트리밍 | 폐기 | 폰에서 27인치 화면을 보는 건 무의미 |
| Firebase RTDB 중계 | 폐기 | Expo Push로 대체, 중앙 서버 불필요 |
| 중간 AI 요약 레이어 | 폐기 | Claude가 JSONL에 이미 충분한 정보를 남김 |
| CLAUDE.md 태그 주입 | 폐기 | 사용자 파일 수정 없이 JSONL에서 추출 가능 |

## 4. 핵심 통찰: JSONL vs PTY

### 기존 SSH/터미널 원격의 한계

SSH와 터미널 미러링(AnyDesk)은 **PTY 제어**를 통해 전체 터미널을 원격으로 본다. 그러나:
- 설치 복잡 (SSH 서버, 키 설정, 포트 개방)
- 폰에서 CLI 화면은 UX가 끔찍함
- Claude의 생각/도구사용/응답이 구분되지 않고 raw 텍스트로 섞임
- 모든 터미널에 통합 기생하는 건 표준 인터페이스 부재로 사실상 불가능

### DevRemote의 JSONL 접근법

Claude Code는 모든 이벤트(thinking, tool_use, tool_result, text)를 JSONL 파일에 구조화된 JSON으로 기록한다. 우리는 이 파일만 읽으면:
- Claude 생각 → 별도 카드
- 도구 실행 → 승인 버튼
- 응답 텍스트 → 말풍선
- 결과 → 펼쳐보기

**핵심 원칙: Claude가 이미 JSONL에 모든 정보를 구조화해서 남긴다. 우리는 파싱만 하면 된다.**
별도 요약 AI도, CLAUDE.md 수정도, 태그 주입도, PTY도 필요 없다.

### JSONL vs PTY 비교

| | JSONL (DevRemote) | PTY (SSH) |
|---|---|---|
| 설치 | 없음 | sshd, 키, 포트 |
| 보는 범위 | Claude 이벤트만 | 터미널 전체 |
| 구조화 | ✅ 자동 분류 | ❌ raw 바이트 |
| stdin 제어 | --exec 모드로 가능 | 완전 가능 |
| 어떤 터미널이든 | ✅ JSONL만 있으면 | ❌ 터미널마다 다름 |
| 대역폭 | KB/분 | KB/s |
| 바이브코더 접근성 | ✅ 앱+코드1회 | ❌ 불가능 |

## 5. 터미널 기생 전략: `npx devremote hook`

모든 터미널, 모든 AI CLI에 기생하는 방법:

```bash
# .bashrc / .zshrc 에 딱 한 줄
eval "$(devremote hook)"

# 이후 ai-cli를 치면 데몬이 가로챔:
claude    → devremote가 spawn → stdin 파이프 확보 → 로그 읽기
codex     → devremote가 spawn → stdin 파이프 확보  
gemini    → devremote가 spawn → stdin 파이프 확보
```

| | 우리 터미널 | shell hook 기생 |
|---|---|---|
| 사용자 변경 | 새 거 써야 함 | **아무 변경 없음** |
| stdin 제어 | ✅ | ✅ |
| 모든 터미널 | ❌ | ✅ (bash/zsh) |
| 모든 AI CLI | ❌ | ✅ |
| 채택 장벽 | 높음 | **eval 한 줄** |

이 방식이면 사용자는 기존 터미널 환경을 그대로 쓰면서 DevRemote가 자연스럽게 모든 AI CLI를 intercept 한다.

## 6. 현재 아키텍처

```
                         ┌─ Signaling Server (Oracle, port 9173)
                         │
Claude Code (PC) ──── Go Daemon ─────────── React Native App (Mobile)
  │                      │                      │
  ├─ JSONL 파일 기록      ├─ watcher: .jsonl 감시   ├─ 구조화 뷰 (카드+승인)
  ├─ stdin ← relay       ├─ detector: tool_use 감지 ├─ 원시 피드 뷰 (예정)
  │                      ├─ server/ws.go: LAN WS   ├─ WebSocketTransport (LAN)
  │                      ├─ webrtc/session.go: P2P  ├─ WebRTCTransport (원격)
  │                      ├─ signald: 시그널링 서버   ├─ AsyncStorage (키 저장)
  │                      └─ peer_key: 영구 페어링    └─ ExpoPushToken (푸시 알림)
```

### 통신 경로 (3가지)

```
1. LAN (같은 WiFi):
   Daemon ←── ws://192.168.x.x:9171/ws ──→ Mobile

2. 원격 (WebRTC P2P):
   Daemon ←── Signaling(9173) ──→ Mobile ──→ WebRTC Data Channel

3. 백그라운드 (푸시 알림):
   Daemon ── POST exp.host ──→ FCM/APNs ──→ Mobile 잠금화면
```

### 페어링 플로우

```
최초 1회:
  Daemon 시작 → 6자리 코드 + 영구 키 생성 → peer_key 파일 저장
  폰에 코드 입력 → 시그널링 → P2P 연결 → AsyncStorage에 키 저장

이후 자동:
  폰 앱 켜기 → 저장된 키 로드 → 시그널링에 자동 join → 수 초 P2P 연결
  코드 재입력 불필요
```

### 응답 relay (`--exec` 모드)

```
Mobile 승인/답변 → WebSocket/WebRTC → Daemon onResponse
  → toolName 확인
  → AskUserQuestion: 답변 텍스트를 stdin에 write
  → 일반 tool_use: 승인=y, 거절=n 을 stdin에 write
  → Claude stdin이 읽어서 처리
```

## 7. 기술 스택

| 컴포넌트 | 기술 | 라이선스 |
|---|---|---|
| 데몬 | Go + fsnotify + gorilla/websocket + pion/webrtc/v4 | MIT |
| 시그널링 서버 | Go + gorilla/websocket (signald) | MIT |
| 모바일 | React Native + TypeScript + Expo SDK 56 | MIT |
| LAN 통신 | WebSocket (로컬 WiFi, port 9171) | — |
| 원격 통신 | WebRTC P2P (STUN: Google, 시그널링: Oracle port 9173) | — |
| 푸시 알림 | Expo Push (APNs + FCM) | — |
| 키 저장 | Daemon: peer_key 파일, Mobile: AsyncStorage | — |

## 8. 배포 인프라

```
Oracle Cloud (168.107.59.177, aarch64, Oracle Linux 9)
├── fullcount-api (systemd)       → :8000 → nginx :443  ← Baseball
├── PostgreSQL                     → :5432
├── widget_worker                  → :8001
└── devremote-signald (systemd)   → :9173              ← DevRemote (NEW)
    └── /usr/local/bin/signald
```

시그널링 서버는 기존 Baseball 서비스에 전혀 영향을 주지 않는 별도 systemd 서비스.

## 9. 프로젝트 구조

```
remote_control/
├── companion-daemon/
│   ├── cmd/
│   │   ├── devremote/main.go         ← 데몬 진입점
│   │   └── signald/main.go           ← 시그널링 서버 (Oracle 배포)
│   ├── internal/
│   │   ├── watcher/tailer.go         ← JSONL 파일 감시
│   │   ├── detector/event.go         ← tool_use 감지 (Alert 생성)
│   │   ├── server/ws.go              ← WebSocket Hub + Listener
│   │   └── webrtc/session.go         ← WebRTC 피어 (pion/webrtc)
│   └── docs/PROJECT.md               ← 이 문서
│
└── mobile/
    ├── App.tsx                        ← 진입점 (Expo)
    ├── app.json                       ← Expo 설정
    ├── package.json                   ← SDK 56 의존성
    └── src/
        ├── screens/
        │   ├── HomeScreen.tsx         ← LAN + 원격 접속
        │   └── SessionScreen.tsx      ← Alert 스트림 + 승인
        └── services/
            ├── types.ts               ← Transport 인터페이스
            ├── WebSocketTransport.ts  ← LAN WebSocket
            └── WebRTCTransport.ts     ← 원격 WebRTC + AsyncStorage
```

## 10. 경쟁 환경

| 경쟁자 | 그들이 하는 것 | 우리의 차별점 |
|---|---|---|
| Hermes Agent / OpenClaw | 다중 에이전트 오케스트레이션, 100% 자동화 | **사람이 판단해야 하는 순간**에 특화된 모바일 UX |
| 텔레그램 봇 | 텍스트 알림 + 버튼 | **구조화된 모바일 대시보드** (카드, 승인, 피드) |
| AnyDesk / 원격 데스크톱 | 화면 픽셀 미러링 | **의미 단위 구조화 데이터** (픽셀 아님) |
| SSH / tmux | PTY 원격 제어 | **설치 0, 바이브코더 접근 가능** |

**전략:** 에이전트 오케스트레이션으로 경쟁하지 않는다. "Human-in-the-Loop 모바일 UX"에 집중한다.

## 11. 현재 상태

| 컴포넌트 | 상태 |
|---|---|
| Go 데몬 (watcher + detector + server) | ✅ 빌드 완료, 테스트 통과 |
| WebSocket LAN 통신 | ✅ 완료 |
| WebRTC P2P 원격 통신 | ✅ 구현 완료 |
| 시그널링 서버 Oracle 배포 | ✅ active (running) |
| Expo SDK 56 마이그레이션 | ✅ 완료 |
| Expo Push 푸시 알림 토큰 등록 | ✅ 완료 |
| --exec stdin relay (Claude 제어) | ✅ 완료 |
| Persistent key 페어링 | ✅ 완료 |
| APK 빌드 (dev client) | ✅ 빌드 성공 |
| iOS 빌드 | ⬜ 예정 |
| shell hook (터미널 기생) | ⬜ 예정 |
| 멀티 AI CLI 지원 | ⬜ 예정 |
| 원시 피드 뷰 (raw JSONL stream) | ⬜ 예정 |
| 멀티세션 대시보드 | ⬜ 예정 |

## 12. 로드맵

```
Phase 1 ✅: 단일 Claude, LAN WebSocket, 기본 승인
Phase 2 ✅: Expo SDK 56, WebRTC P2P, 오라클 시그널링, 푸시 토큰
Phase 3 🔄: APK 테스트, iOS 빌드, --exec 엔드투엔드 검증
Phase 4 ⬜: shell hook (devremote hook), 멀티 AI CLI intercept
Phase 5 ⬜: 원시 피드 뷰 + 구조화 뷰 토글, 멀티세션
Phase 6 ⬜: 프로덕션 배포 (Play Store 심사)
```
