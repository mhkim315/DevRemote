# DevRemote Ground Zero — 인수인계 (2026-07-03)

## 현재 상태

**터미널 미러링 작동 확인 완료!**

```
Phone WebView → Cloudflare HTTPS → Daemon (:9171) → WebSocket → PTY bash
```

- `echo` 명령어: 폰에서 실행, 화면에 출력 확인 ✅
- Spy: AI가 폰 화면을 실시간 모니터링 가능 ✅
- Command Injection: AI가 `curl /debug/cmd -d 'ls'`로 명령 주입 가능 ✅

## 브랜치: `ground-zero`

GitHub: `https://github.com/mhkim315/DevRemote`, branch `ground-zero`

## 파일 구조

```
devremote/
├── companion-daemon/
│   ├── cmd/devremote/main.go    (8줄 — HTTP 서버)
│   ├── internal/term/pty.go     (90줄 — WS PTY + spy + cmd injection)
│   ├── go.mod, go.sum
│   └── devremote                (빌드된 바이너리)
├── mobile/
│   ├── App.tsx                  (10줄 — FeedScreen 직통)
│   ├── src/screens/FeedScreen.tsx (40줄 — WebView + TextInput)
│   ├── assets/terminal.html     (로컬 xterm.js)
│   └── android/                 (Expo prebuild + release APK)
├── cloudflared                  (Cloudflare tunnel 바이너리)
└── HANDOVER.md                  (이 문서)
```

## 실행 방법

### 1. Daemon 시작
```bash
cd companion-daemon
go build -o devremote ./cmd/devremote/
./devremote
# → http://localhost:9171/term/ 에서 xterm.js 터미널
```

### 2. Cloudflare 터널 (원격 접속용)
```bash
./cloudflared tunnel --url http://localhost:9171
# → https://xxxx.trycloudflare.com URL 출력
```

### 3. APK 빌드
```bash
cd mobile/android
JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home" \
ANDROID_HOME="$HOME/Library/Android/sdk" \
./gradlew assembleRelease
# → app/build/outputs/apk/release/app-release.apk
```

FeedScreen.tsx의 `TERM_URL`을 Cloudflare URL로 변경 후 빌드.

### 4. Spy 모니터링
```bash
# 폰 화면 보기
grep 'PHONE:' /tmp/gz.log | tail -3

# 명령 주입
curl -X POST http://127.0.0.1:9171/debug/cmd -d 'echo hello'

# WS 데이터 보기
grep 'WS recv' /tmp/gz.log
```

## 핵심 API

| Endpoint | Method | 설명 |
|---|---|---|
| `/term/` | GET | xterm.js HTML + spy 코드 |
| `/term/ws` | WebSocket | PTY bash 연결 |
| `/debug/dump` | POST | 폰 화면 덤프 수신 |
| `/debug/cmd` | GET | 다음 명령어 반환 (spy poll) |
| `/debug/cmd` | POST | 명령어 큐에 추가 |

## 알려진 이슈

1. **Cloudflare 터널**: daemon 재시작 시 터널도 재시작 필요 (URL 변경됨)
2. **에뮬레이터**: 불안정 (crash 반복). 실제 폰 테스트 권장
3. **Android cleartext**: `network_security_config.xml` + `usesCleartextTraffic` 설정됨
4. **터미널 깨짐**: ANSI 코드/스피너 출력이 지저분할 수 있음 (정상)

## 다음 할 일 (우선순위)

1. **Omnara UI 통합**: `omnara-mobile/` → 대시보드, AgentFleet, CommandDeck
2. **FCM 푸시 알림**: Expo Push Token → 승인 요청 시 알림
3. **Shell Hook**: `devremote hook` → 터미널 기생
4. **Play Store 배포**: 앱 서명 + 스토어 등록
5. **iOS 빌드**: EAS Build로 iOS 버전

## 검증 방법

내일 아침 결과 확인:
1. Daemon 실행 중인지: `curl http://127.0.0.1:9171/term/`
2. Cloudflare 터널 살아있는지: `curl https://xxxx.trycloudflare.com/term/`
3. APK 최신 버전: `ls -la mobile/android/app/build/outputs/apk/release/app-release.apk`
4. Spy 작동: `grep 'PHONE:' /tmp/gz.log | tail -1`
