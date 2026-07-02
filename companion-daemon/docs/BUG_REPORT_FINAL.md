# DevRemote — 최종 버그리포트 (2026-07-02)

## 현재 상태

**작동하는 것:**
- ✅ Go daemon `/term/` endpoint — xterm.js + WebSocket PTY (완벽)
- ✅ WebSocket → bash — 명령어 실행 확인 (로컬 `curl` + Python 테스트)
- ✅ `window.ws` + `window.term` 전역 노출 (injectJavaScript 명령 전송용)
- ✅ Phone browser: `http://192.168.219.100:9171/term/` → xterm.js 터미널 **완벽 작동**
- ✅ APK 빌드: release/debug 모두 성공 (AndroidManifest cleartext 설정 포함)
- ✅ Go daemon: 단일 바이너리, `go build` 성공
- ✅ WebView inline HTML 테스트: 에뮬레이터에서 데이터 도달 확인

**작동하지 않는 것:**
- ❌ Android WebView에서 HTTP URL 로딩 → 흰 화면 or cleartext 차단
- ❌ 폰 WiFi → Mac 연결 불안정 (같은 WiFi인데도 접근 불가, AP 격리 의심)
- ❌ ngrok 미인증 (ERR_NGROK_4018)
- ❌ 에뮬레이터 WebView 키보드 입력 불가 (에뮬레이터 한계로 추정)
- ❌ Omnara UI 통합 안 됨 (코드만 복사, API 연동 없음)

## 버그 #1 (CRITICAL): Android WebView HTTP cleartext 차단

**증상**: 폰 브라우저에서는 `http://192.168.219.100:9171/term/` 접속 가능하지만, 앱 WebView는 흰 화면 or `net::err_cleartext_not_permitted`.

**시도한 해결책**:
1. `android:usesCleartextTraffic="true"` → AndroidManifest.xml에 추가 ✅
2. `network_security_config.xml` → `cleartextTrafficPermitted="true"` → AndroidManifest에 `android:networkSecurityConfig` 참조 추가 ✅
3. WebView `originWhitelist={['*']}`, `mixedContentMode="always"`, `allowFileAccess={true}` 추가 ✅
4. 모든 시도 후에도 여전히 차단됨 ❌

**의심되는 원인**:
- `network_security_config.xml`이 제대로 APK에 포함되지 않았을 가능성 (`expo prebuild --clean` 필요할 수 있음)
- Android 13+에서 WebView cleartext는 별도로 WebView의 `WebViewClient.onReceivedSslError` 핸들러가 필요할 수 있음
- `android:networkSecurityConfig`가 Expo 빌드 과정에서 무시될 수 있음

**다음 시도**:
1. `expo prebuild --clean --platform android` 로 네이티브 디렉토리 재생성
2. `AndroidManifest.xml` + `network_security_config.xml` 재적용
3. `./gradlew assembleRelease` 재빌드
4. 또는 HTTPS로 daemon 서빙 (자체 서명 인증서 + WebView SSL 에러 무시)

## 버그 #2: 폰 ↔ Mac 네트워크 연결 불안정

**증상**: 같은 WiFi인데도 폰에서 `192.168.219.100:9171` 접근 불가. 이전에는 됐다가 갑자기 안 됨.

**의심되는 원인**:
- 공유기 AP 격리(Client Isolation) 활성화
- Mac IP 변경 (WiFi 재연결 시 DHCP 갱신)
- 폰이 실제로는 cellular 사용 중

**해결책**:
- ngrok으로 외부 터널 생성 (ngrok 인증 필요: `ngrok config add-authtoken <token>`)
- 또는 Cloudflare Tunnel 사용
- 또는 localhost.run (SSH 기반, 무료, 인증 불필요): `ssh -R 80:localhost:9171 localhost.run`

## 버그 #3: 에뮬레이터 문제 (낮은 우선순위)

**증상**: 
- 에뮬레이터 WebView 키보드 입력 불가 (실제 폰에선 정상)
- 에뮬레이터 crash (`libandroid-emu-tracing.dylib` not found)

**영향**: 낮음 — 실제 폰 테스트가 우선. 에뮬레이터는 디버깅 보조 도구일 뿐.

## 핵심 아키텍처 (변경됨)

```
[최종 아키텍처]
Mac: Go daemon (:9171)
  ├── /term/      → xterm.js HTML 페이지
  └── /term/ws    → WebSocket PTY (bash)

Phone: React Native APK
  └── FeedScreen → WebView → http://IP:9171/term/
                            ↓
                       xterm.js (WebSocket → PTY)
                            ↓
                       TextInput → injectJavaScript → window.ws.send()

[폐기된 아키텍처]
  WebRTC + signaling + postMessage + base64 + wrap + JSON pipeline
```

## 지금 당장 해야 할 일 (우선순위)

1. **ngrok 인증** → 폰에서 원격 접속 (WiFi 문제 우회)
   ```bash
   ngrok config add-authtoken <your-token>
   ngrok http 9171
   ```

2. **또는 localhost.run** (무료, 인증 불필요):
   ```bash
   ssh -R 80:localhost:9171 localhost.run
   ```

3. **APK cleartext 재빌드**:
   ```bash
   cd mobile && npx expo prebuild --clean --platform android
   # network_security_config.xml 재적용
   cd android && ./gradlew assembleRelease
   ```

4. **실제 폰에서 테스트**: ngrok URL을 FeedScreen에 하드코딩 → 빌드 → 설치 → 확인

## 참고

- Go daemon 소스: `companion-daemon/internal/term/pty.go`
- FeedScreen: `mobile/src/screens/FeedScreen.tsx`  
- AndroidManifest: `mobile/android/app/src/main/AndroidManifest.xml`
- Network config: `mobile/android/app/src/main/res/xml/network_security_config.xml`
- GitHub: `https://github.com/mhkim315/DevRemote`, branch `companion-daemon`
