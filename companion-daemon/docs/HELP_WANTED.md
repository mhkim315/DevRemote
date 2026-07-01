# DevRemote — 도움 요청 (2026-07-02)

## 현재 상태 (요약)

RustDesk 포크 폐기 → Go 클린룸 + Expo로 피벗 완료.
핵심 파이프라인(Claude Code ↔ Go 데몬 ↔ WebRTC ↔ 모바일)은 작동 중.
하지만 **모바일 UX 3가지 버그**에 막혀 있음.

## 아키텍처

```
Claude Code (Mac)
    │
    ▼
wrap.go (PTY proxy: stdin/stdout 제어)
    │ POST /stdin (localhost HTTP)
    │ POST /pty   (localhost HTTP, PTY 출력 미러링)
    ▼
daemon/main.go (JSONL watcher + WebRTC + WebSocket 허브)
    │ WebRTC Data Channel P2P
    │ 시그널링: Oracle 서버 (168.107.59.177:9173)
    ▼
React Native Expo SDK 56 (Android APK)
    ├── SessionScreen.tsx (알림 탭: 승인/거절 버튼)
    ├── FeedScreen.tsx    (피드 탭: 터미널 미러링 + 채팅)
    └── WebRTCTransport.ts (P2P + AsyncStorage)
```

## 작동하는 것

- Go 데몬 빌드 OK (Mac arm64)
- Android APK 로컬 빌드 OK (debug 207MB, release 112MB)
- WebRTC P2P 연결 OK (폰 ↔ 데몬 data channel opened)
- 시그널링 서버 Oracle 배포 OK (systemd, 자동 재시작)
- 승인/거절 버튼 → `y\n`/`n\n` → 데몬 → wrap → PTY stdin 경로 OK
- PTY stdout → ANSI 정리 → /pty POST → 데몬 → WebRTC → 폰 피드 경로 OK
- wrap ↔ daemon IPC: localhost HTTP 브릿지 (wrap 랜덤포트 stdin 서버 + daemon이 IPCState.json 읽어서 연결)

## 막힌 버그 3개

### BUG 1: 피드(Feed) 탭 갔다가 뒤로가면 알림(Alert) 탭이 더 이상 알림을 받지 못함

**원인 파악 완료, 수정 적용, APK 빌드 완료 — 아직 테스트 안 함**

SessionScreen.tsx:61 — `useEffect` cleanup에서 `transportRef.current.disconnect()`를 호출.
피드로 전환할 때 SessionScreen이 unmount되면서 WebRTC 연결이 **완전히 끊어짐**.
돌아와도 재연결되지 않음 (데몬은 새 코드를 생성하지 않음).

**수정**: disconnect() 제거. 아직 폰에서 재테스트 안 함.

### BUG 2: 피드에서 채팅 입력이 안 됨 (stdin 미전달)

데몬 로그에 `stdin from mobile`이 한 번도 안 찍힘.

의심되는 원인:
- WebRTCTransport.ts의 DataChannel `readyState`가 'open'이 아니어서 `sendMessage`가 silent drop
- 또는 `handleMessage`에서 `type: "stdin"` 감지가 안 됨
- BUGREPORT.md의 BUG-010: alertCh 버퍼(256) overflow → silent drop

디버깅 필요: 폰에서 `console.log` 찍어서 `sendMessage` 호출 여부와 data channel 상태 확인.

### BUG 3: 알림 폭탄 — 모든 작은 PTY 출력이 알림으로 옴

원인: PTY 출력이 100ms마다 /pty로 전송 → 각 청크가 `hub.SendRaw` 호출 → 모든 WebSocket/WebRTC 클라이언트가 알림으로 처리.

**수정 적용, 아직 테스트 안 함**:
- Batch rate: 100ms → 500ms
- 10글자 미만 청크 필터링
- ANSI 코드 정리

하지만 근본적으로: 모바일에서 "pty" 타입 이벤트는 알림 없이 피드에만 추가되고, "alert" 타입만 알림을 발생시켜야 함. 현재는 모든 데이터 채널 메시지가 동일하게 처리되는 것 같음.

## 모바일 코드 상태

- SessionScreen.tsx: 알림 리스트 + 승인/거절 버튼 (FlatList, 최대 20개)
- FeedScreen.tsx: 터미널 미러링 + 채팅 입력 (BUG로 작동 안 함)
- WebRTCTransport.ts: P2P 연결 + AsyncStorage 키 저장 + DataChannel
- WebSocketTransport.ts: LAN WebSocket
- types.ts: Transport 인터페이스

## 환경

```
Mac: arm64, Go 1.26.4, Android Studio, SDK 36, NDK 27.1
Oracle: 168.107.59.177:9173 (signald, systemd)
Expo: SDK 56, 계정 duboo82, EAS project minani
Repo: github.com/mhkim315/DevRemote, branch companion-daemon
```

## 가장 도움이 필요한 것

1. **FeedScreen + SessionScreen 전환 시 WebRTC 연결 유지** — disconnect 없이도 unmount/remount 대응
2. **피드 채팅 stdin 전달** — data channel → daemon → wrap 경로 디버깅
3. **알림 vs 피드 구분** — 모바일에서 pty 이벤트는 알림 없이 피드에만, alert 이벤트만 알림 발생
