# 📱 실기기(Physical Device) 테스트 가이드 및 주의사항

지금까지의 에뮬레이터 테스트를 넘어, 실제 스마트폰(LTE/5G 환경)에서 DevRemote의 모든 기능(터널링, 푸시 알림, UX)이 정상 작동하는지 검증하기 위한 가이드라인입니다. 검증 에이전트 및 테스터는 아래 사항을 숙지하고 테스트를 진행해 주십시오.

---

## 1. 🌐 네트워크 및 터널링 (NAT Traversal) 검증

에뮬레이터 환경에서는 `10.0.2.2`(로컬호스트)를 통해 통신이 가능했지만, 실기기에서는 완벽한 외부망 통신이 필수적입니다.

- **주의사항:** 실기기(스마트폰)의 Wi-Fi를 끄고 **LTE/5G 데이터 네트워크** 상태에서 테스트를 진행해야 합니다. (NAT 방화벽 우회 능력을 검증하기 위함입니다.)
- **확인 항목:**
  1. Mac에서 데몬 실행 시 `cloudflared`가 정상적으로 `term.fullcount.kr` 도메인과 `:9171` 포트를 매핑하고 있는지 확인.
  2. 폰에서 앱을 열었을 때 Dashboard에 세션 목록이 정상적으로 로드되는지 확인 (`GET https://term.fullcount.kr/api/sessions`).
  3. 세션 입장 시 WebSocket(`wss://term.fullcount.kr/term/ws?session=...`)이 끊김 없이 0.1초 내외의 응답성을 보여주는지 확인.

## 2. 🔔 네이티브 Push 알림 검증 (핵심)

iOS 시뮬레이터나 일부 안드로이드 에뮬레이터에서는 Push 알림이 동작하지 않지만, 실기기에서는 정확히 동작해야 합니다.

- **주의사항:** 앱 최초 실행 시 나타나는 **알림 권한 요청을 반드시 "허용(Allow)"** 해야 합니다.
- **테스트 방법:**
  1. Mac 터미널에서 `tmux new -s aider` 로 세션을 엽니다.
  2. 폰에서 `aider` 방에 입장한 뒤, 앱을 **백그라운드**로 내립니다 (홈 화면으로 나감).
  3. Mac 터미널(aider 세션)에서 아래 명령어를 타이핑하여 AI의 질문 상황을 시뮬레이션합니다.
     ```bash
     echo "Agent: Do you want to run 'npm install'? (y/n)"
     ```
  4. 폰에 즉시 알림이 울리는지, 그리고 **알림 내용(Body)에 "Do you want to run 'npm install'? (y/n)" 이라는 원문이 정확히 파싱되어 있는지** 확인합니다.

## 3. ⌨️ 모바일 UX 및 Quick Actions Bar 검증

폰 화면은 작고 터치가 불편하므로, Phase 4-1에서 구현한 매크로 키보드의 실효성을 검증합니다.

- **주의사항:** 실기기의 소프트 키보드(OS 키보드)가 올라왔을 때 터미널 화면이 가려지지 않는지(Keyboard Avoiding) 확인합니다.
- **확인 항목:**
  1. 하단의 `[Y]`, `[N]`, `[Enter]`, `[Ctrl+C]` 버튼을 터치했을 때 Mac 터미널에 즉각적으로 반응이 오는지 확인.
  2. 스크롤 제스처가 터미널 뷰와 충돌하지 않는지 확인.

---

## 🚀 실기기 테스트 빌드 및 실행 방법

### 방법 A: Expo Go 앱 활용 (빠른 테스트)
개발용 앱인 `Expo Go`를 통해 스마트폰에서 즉시 실행할 수 있습니다.
```bash
cd mobile
npx expo start
```
- 터미널에 표시된 QR 코드를 스마트폰의 카메라(iOS) 또는 Expo Go 앱(Android)으로 스캔합니다.

### 방법 B: APK 직접 빌드 (안드로이드 실기기 설치용)
```bash
cd mobile/android
JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home" \
ANDROID_HOME="$HOME/Library/Android/sdk" \
./gradlew assembleRelease
```
- 생성된 `app-release.apk`를 카카오톡이나 구글 드라이브로 폰으로 전송하여 설치합니다.

---

> [!WARNING]
> **알려진 이슈 대처법:** 만약 앱에서 세션 목록이 로딩 상태로 멈춰 있다면, Mac의 Cloudflare 터널이 죽은 상태일 가능성이 높습니다. 이 경우 데몬을 `Ctrl+C`로 끄고 `cloudflared tunnel cleanup devremote` 명령어 실행 후 데몬을 재시작하십시오.
