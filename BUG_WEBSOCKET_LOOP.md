# 🚨 WebSocket 3초 간격 재연결 무한 루프

## 현상

**모든 tmux 세션(aider, frontend 등)에서 WebSocket이 약 3초 간격으로 disconnect → reconnect 반복.**

```
11:25:16 WS [frontend]: [::1]:63206
11:25:19 WS [frontend]: [::1]:63209   ← 3초 후 새 연결
11:25:22 WS [frontend]: [::1]:63211   ← 3초 후 또 새 연결
11:25:25 WS [frontend]: [::1]:63213   ← ...
```

- `WS [aider]`도 동일한 패턴 → **Claude/TUI 특정 문제가 아님**
- emulator(`10.0.2.2`)와 실기기(LTE, `term.fullcount.kr`) 모두 동일

## 시도한 해결책 (모두 실패)

| # | 시도 | 결과 |
|---|------|------|
| 1 | `useMemo`로 WebView `source` 객체 고정 | 실패 — 리렌더 시 WebView 리로드 방지했으나 disconnect 지속 |
| 2 | `postMessage`/`onMessage` WS 상태 트래킹 제거 | 실패 — 불필요한 state 업데이트 제거 |
| 3 | Go `sessionConns` 맵으로 세션당 1연결만 허용 | 실패 — PTY 중복 방지했으나 disconnect 지속 |
| 4 | JS `reconnecting` 플래그 + `window.ws.close()` 후 재연결 | 실패 — 무한 루프 완화했으나 disconnect 지속 |
| 5 | `onopen`에서 `reconnecting=false` 설정 | 실패 |
| 6 | `wsDot` UI 표시 제거 (state 업데이트 최소화) | 실패 |

## 의심되는 원인

### 1. KeyboardAvoidingView / SafeAreaView 레이아웃 충돌
- `KeyboardAvoidingView`가 WebView를 감싸고 있어, 키보드 이벤트나 레이아웃 변화 시 WebView가 리셋될 가능성

### 2. React Native WebView 컴포넌트 unmount/remount
- 상태 변화(kbHeight 등)로 인한 리렌더 시 WebView가 언마운트-리마운트
- `source`를 memoize 했지만 다른 prop 변화로 리마운트 가능성

### 3. Cloudflare Tunnel WebSocket 타임아웃
- Cloudflare가 idle WebSocket을 3초 만에 끊을 가능성 (매우 짧은 타임아웃)
- `wss://` 터널 프록시 WebSocket이 keepalive 없이 끊김

### 4. Android WebView WebSocket 한계
- Android WebView가 백그라운드/포그라운드 전환 시 WebSocket을 끊는 버그
- 특정 Android 버전에서 WebSocket 연결 유지 실패

### 5. Go 서버 측 원인
- `gorilla/websocket`의 기본 keepalive 부재
- PTY에서 read blocking 중 WebSocket timeout

## 현재 파일 상태

```
pty.go:
  - sessionConns 맵으로 세션당 1연결 관리
  - HandleWS: 새 연결 시 기존 연결 Close

pty.go (HandleHTML JS):
  - reconnecting 플래그로 중복 재연결 방지
  - 2초 재연결 간격
  - 연결 전 이전 WS close + onclose null 처리

FeedScreen.tsx:
  - useMemo로 source/termUrl 고정
  - cmdRef로 안정적 명령어 전송
  - Keyboard listeners for kbHeight
  - onMessage/postMessage 제거됨
  - multiline={false}, blurOnSubmit={false}

AndroidManifest.xml:
  - windowSoftInputMode="adjustResize" (기존 설정)
```

## 제안: 다음 에이전트 시도할 것

1. **Cloudflare 터널 WebSocket keepalive** — Go 서버에서 주기적 ping/pong 추가
2. **KeyboardAvoidingView 제거 실험** — WebView를 최상위에 단독 배치
3. **WebView `key` prop 고정** — `key="terminal"` 등으로 unmount 방지
4. **gorilla/websocket에 ReadDeadline/WriteDeadline 설정**
5. **터널 없이 로컬 테스트** — `10.0.2.2:9171` 직접 연결로 터널 문제인지 확인
