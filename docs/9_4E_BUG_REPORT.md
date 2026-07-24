# 9.4-E Base Alpha — Bug Report & Root Cause Analysis

**Date:** 2026-07-24
**Test Device:** SM-S926N (R3CX106PTFD) / Android 16
**macOS:** 26.5.1 (25F80) arm64
**Evidence:** `~/Desktop/pokit-device-runs/20260724T115018Z-R5-physical-smoke/`

## Summary

12 bugs found during 9.4-E physical-device security lifecycle verification.
1 critical, 2 high, 3 medium, 6 low.

---

## P0 — Critical

### 1. Tunnel + Daemon 분리 실행 (PATH 문제)

**파일:** `companion-daemon/cmd/devremote/app.go:936-938`

**원인:**
LaunchAgent가 daemon만 실행하고 cloudflared tunnel은 별도 수동 실행이 필요함.
Daemon은 `app.go:936-938`에서 `!InsecureLocalOnly`일 때 `startTunnel()`을 호출해
cloudflared를 자동 실행하려고 시도하지만, LaunchAgent의 `PATH`에
`/opt/homebrew/bin`이 없어 `exec.LookPath("cloudflared")`가 실패함.

```
로그: Failed to start cloudflared: exec: "cloudflared": executable file not found in $PATH
```

**증상:**
- QR 페어링(LAN HTTP)은 성공하지만 운영 연결(tunnel HTTPS)이 불가
- 앱이 "daemon unreachable" 표시 후 카메라 화면에 멈춤
- `cloudflared tunnel run devremote &` 수동 실행으로 해결 가능

**수정 방향:**
LaunchAgent plist에 `PATH` 환경변수 추가:
```xml
<key>EnvironmentVariables</key>
<dict>
    <key>PATH</key>
    <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
</dict>
```
또는 `startTunnel()`에서 cloudflared 절대경로 `/opt/homebrew/bin/cloudflared` 사용.

---

## P1 — High

### 2. QR 페어링 후 앱 카메라 화면 멈춤

**파일:**
- `mobile/src/lib/authMode.ts:165-170` (`pairThenConnect`)
- `mobile/src/lib/connectPairing.ts:63-68` (`pairFromScannedQR`)
- `mobile/src/screens/ConnectScreen.tsx:57-87` (`handleBarCodeScanned`)

**원인:**
페어링 성공 후 `pairThenConnect`가 `onPaired`(trusted state 설치) → `connect()`(tunnel 운영 연결) 순서로 실행됨.
`onPaired`가 성공해도 `connect()`가 tunnel 연결 실패로 throw되면, `authMode.ts:167-170`에서
에러가 조용히 catch됨. `isConnected`가 false이고 앱은 `ConnectScreen`(카메라)에 머무름.

```typescript
// authMode.ts:165-170
try {
    await args.connect();       // tunnel 연결 실패
} catch {
    // 에러를 삼키고 카메라 화면에 머무름 — scanner 잠금 유지
}
```

앱 재시작 시 `ConnectionProvider`가 AsyncStorage에서 `BASE_URL`을 복원하고
`probeDaemon()` 재시도 → tunnel이 살아있으면 성공.

**증상:**
- QR 스캔 → daemon 승인 → 앱이 카메라 화면에 멈춤
- "daemon unreachable" 또는 "server returned 410" 오류
- 앱 강제종료 + tunnel 실행 + 재시작 → owner로 진입

**수정 방향:**
`connect()` 실패 시:
1. 일정 횟수 재시도 (exponential backoff)
2. "Tunnel 연결 대기 중..." 상태 표시
3. `pairThenConnect`가 `onError` 콜백으로 에러 전파

---

### 3. 앱에서 Claude `claude_headless` 세션 생성 실패

**파일:**
- `companion-daemon/internal/term/create.go:68-96`
- `companion-daemon/internal/term/managed_claude.go`

**원인:**
앱이 `POST /api/sessions {"profileId":"claude"}`로 HTTP API를 호출하면
`createFromProfile`이 `ManagedClaude.CreateDetached()`를 호출함.
이 과정에서 Claude binary certify(버전/digest/경로 검증)가 실패해 `LifecycleFailed` 반환.

CLI(`pokit run claude`)는 IPC 소켓으로 직접 `ManagedClaude.CreateDetached()`를
호출하며(ipc.go:207-212), HTTP 경로와 동일한 코드 패스를 사용함.

실패 원인은 다음 중 하나:
- Claude binary digest 불일치 (`--claude-digest` 미설정 또는 불일치)
- Claude binary가 pinned path(`~/.local/share/claude/versions/2.1.209`)에 없음
- 버전 불일치 (daemon이 `2.1.209` 기대, 실제 설치 `2.1.218`)

**증상:**
앱에서 "New Session" → "Claude" 선택 시 "server returned an unexpected response" 오류.

**수정 방향:**
1. Claude 버전/경로/digest 설정 검증
2. `LifecycleFailed` 응답에 상세 에러 메시지 포함
3. 앱에서 실패 원인을 사용자에게 표시

---

## P2 — Medium

### 4. Claude 버전 하드코딩 (5곳)

**파일:**
| 파일 | 라인 | 값 |
|------|------|-----|
| `claude_attestor.go` | 26-28 | `Version: "2.1.209"`, `PinnedPath: ".../2.1.209"` |
| `managed_claude_activation.go` | 31 | `const certifiedClaudeAuthorityVersion = "2.1.209"` |
| `managed_claude.go` | 979-980 | `Version: "2.1.209"`, `AuthorityVersion: "2.1.209"` |
| `claude_catalog_classifier.go` | 43 | `Version: "2.1.209"` |
| `create.go` | 149 | `version = "2.1.202"` ← **틀린 버전** |

**원인:**
Claude 버전이 5개 파일에 분산되어 문자열 리터럴로 하드코딩됨.
`create.go:149`는 `"2.1.202"`로 설정되어 있어 다른 파일의 `"2.1.209"`와 불일치.
Claude가 업데이트될 때마다 모든 파일을 수동으로 수정해야 함.

**수정 방향:**
버전 상수를 `claude_attestor.go`의 `certifiedClaudeAuthorityVersion` 한 곳에서만 정의하고,
모든 곳이 이를 참조하도록 변경.

---

### 5. Claude `claude_headless` I/O UI 미구현

**파일:** `mobile/src/screens/FeedScreen.tsx:95-98`

```typescript
// codex_app_server만 ManagedSessionView로 라우팅
if (props.session.startsWith('codex_app_server:')) {
    return <ManagedSessionView ... />;
}
// claude_headless는 LegacyFeedScreen(터미널 WebView)로 fallback → 빈 화면
```

**원인:**
`FeedScreen`이 `codex_app_server:` 어댑터만 `ManagedSessionView`로 라우팅하고,
`claude_headless:`는 라우팅되지 않아 터미널 WebView(`LegacyFeedScreen`)로 떨어짐.
Claude headless 세션은 PTY/터미널이 없어 빈 화면만 표시됨.
CLI에서 `pokit run claude`로 생성된 세션은 카드만 보이고 I/O 불가.

**수정 방향:**
`claude_headless:` 어댑터도 `ManagedSessionView`나 전용 Claude 뷰로 라우팅.

---

### 6. Dashboard "↻ RESCAN" 버튼 혼동

**파일:** `mobile/src/screens/dashboard/DashboardScreen.tsx:199-201`

```tsx
<TouchableOpacity onPress={async () => { await disconnect(); }}>
  <Text style={{color: '#f85149'}}>↻ RESCAN</Text>
</TouchableOpacity>
```

**원인:**
- `↻` 심볼이 새로고침(refresh)으로 오인됨
- `disconnect()` 호출 → `BASE_URL` 삭제, device auth 제거, 카메라 화면으로 이동
- 이미 paired 상태에서는 불필요하고 위험한 동작
- 빨간색(`#f85149`)이 중요/위험 버튼으로 보이게 함
- SNIPPETS 버튼 바로 옆 헤더에 위치해 잘못 누르기 쉬움

**수정 방향:**
1. RESCAN 제거하거나 설정 메뉴로 이동
2. 아이콘을 `↻` → `📷`로 변경, 레이블을 "RE-PAIR"로
3. 이미 paired + connected 상태에서는 숨김

---

## P3 — Low

### 7. Transcript 줄바꿈 불일치

**파일:** `mobile/src/components/TranscriptRenderer.tsx:64`, `mobile/src/screens/terminalHtml.ts:19`

| | Transcript | Terminal |
|---|-----------|----------|
| fontSize | 13 | 12 |
| 컨테이너 | React Native `FlatList` + `paddingHorizontal: 12` | xterm.js viewport |
| 줄바꿈 | RN `Text` 컴포넌트 | xterm.js |

**수정 방향:** Transcript의 `fontSize`를 12로, 컨테이너 폭 계산을 터미널과 동기화.

---

### 8. 터미널 입력창 중복 + Send 버튼 미작동

**파일:** `mobile/src/screens/FeedScreen.tsx:1059-1095`, `mobile/src/screens/terminalHtml.ts:56-57`

**원인:** 두 개의 분리된 입력 경로가 존재:
```
xterm.js → term.onData() → WebRTC DataChannel → daemon
TextInput → doSend() → pokitSendInput → WebSocket → daemon
```
`terminalInputEnabled = actionPolicy.inputEnabled && deviceCanInput`가 false이면
Send 버튼이 비활성화됨. xterm.js에 직접 입력 시 DataChannel로 전송됨.

**수정 방향:** 입력 경로를 한 가지로 통합 (WebSocket 또는 DataChannel).

---

### 9. "byte stream projection suppressed" 빨간 경고

**파일:**
- `companion-daemon/internal/transcript/arbitration.go:85`
- `companion-daemon/internal/transcript/service.go:175`
- `mobile/src/screens/FeedScreen.tsx:1007-1012`

**원인:** 터미널 입력 후 byte-stream을 영구 억제하는 의도된 설계.
그러나 UI가 빨간 배경(`backgroundColor: '#3a1a1a'`)으로 렌더링되어
에러 메시지처럼 보임.

**수정 방향:** 배경색을 중립색(회색)으로 변경, informational 스타일 적용.

---

### 10. Transcript/Terminal 내용 불일치 + Transcript 미갱신

**파일:** `companion-daemon/internal/transcript/arbitration.go:8-9,79-87`

**원인:** Source Separation 설계.
- AgentEvent(Codex/Claude 출력)가 primary → byte-stream(PTY 출력)은 fallback 채널로 분리
- `permanentSuppress = true` 이후 새 byte-stream 영구 억제
- Transcript엔 AgentEvent만, Terminal엔 전체 PTY 출력

**수정 방향:** "fallback" 채널을 사용자에게 선택적으로 노출하거나,
Transcript 탭에 "터미널 출력 보기" 토글 추가.

---

### 11. `claude_headless` 세션 삭제 불가

**파일:** `companion-daemon/internal/term/create.go:86-96`

**원인:** Claude headless 세션이 `Lifecycle.Register`를 호출하지 않아
lifecycle 관리가 `ManagedClaude` 서비스에 위임됨.
Stop/Delete API가 `claude_headless` 어댑터를 처리하지 못함.

```go
// create.go:86-87
// PA2c: no Lifecycle.Register — the Claude provider service owns its
// lifecycle record; ManagedRuntimeCatalog is the read path.
```

**수정 방향:** `ManagedClaude`에 Stop/Kill/Delete 메서드 추가.
HTTP DELETE 핸들러에서 `claude_headless` 어댑터 라우팅 추가.

---

### 12. Cloudflared 중복 실행

**파일:** `companion-daemon/cmd/devremote/app.go:936-938`

**원인:** Daemon이 재시작될 때마다 `startTunnel()`이 새 cloudflared 프로세스를 생성.
이전 프로세스가 정리되지 않아 2개 이상의 cloudflared 인스턴스가 동시에 실행됨.
`startTunnel()`에 중복 실행 방지 로직이 없음.

**수정 방향:**
1. `startTunnel()`에서 기존 cloudflared PID 확인
2. LaunchAgent가 tunnel도 lifecycle 관리
3. 또는 daemon이 `KeepAlive`로 재시작될 때 기존 tunnel 재사용

---

## Test Environment

| 항목 | 값 |
|------|-----|
| macOS | 26.5.1 (25F80) arm64 |
| Device | SM-S926N / Android 16 / BP4A.251205.006.S926NKSSGDZG1 |
| APK | versionCode=1 versionName=1.0.0 |
| Daemon | 94a0b9f5 (vcs.revision=7433c77e4, vcs.modified=false) |
| Formula | pokit.rb pin 04b32ec74 (sha256 f1167765) |
| CLI | 0.0.1-dev-build10 (sha=ebabf6832) |
| Claude | 2.1.218 (daemon expects 2.1.209) |
| Codex | 0.144.1 |
| cloudflared | 2026.7.2 |
