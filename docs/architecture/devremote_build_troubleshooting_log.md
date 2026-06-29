# DevRemote Android CI/CD 빌드 트러블슈팅 로그 (개발 일지)

**문서 개요:** 
본 문서는 DevRemote(RustDesk 포크) 프로젝트의 Android APK를 GitHub Actions 환경에서 빌드하는 과정에서 겪은 6차례의 연속적인 빌드 실패와 그 해결 과정을 기록한 포스트모템(Post-mortem) 성격의 개발 일지입니다.

---

## 🛑 발단: 로컬 환경(Java 21) 호환성 문제
*   **배경:** 초기 Mac 로컬 환경에서 `flutter build apk`를 시도했을 때, 시스템에 설치된 Java 버전이 21이었기 때문에 구형 Gradle(7.3.1)과 호환되지 않아 `Unsupported class file major version 65` 에러가 발생함.
*   **잘못된 대응:** 로컬 빌드 성공을 목표로 **Android Gradle Plugin(AGP)을 최신 8.6.0으로, Gradle Wrapper를 8.14로 강제 업그레이드**함. 이 결정이 이후 일어난 모든 나비효과의 근원이 됨.

---

## 🚨 1~2차 실패: AGP 8.0+ Breaking Change (Namespace 누락)
*   **현상:** GitHub Actions에서 빌드 시도 중 `:app` 모듈 구성 단계에서 실패.
    *   `Namespace not specified. Specify a namespace in the module's build file`
*   **원인:** AGP 8.0 버전부터는 기존 `AndroidManifest.xml`의 `package="com...""` 속성을 읽는 방식이 폐지되고, 반드시 `build.gradle`의 `android { ... }` 블록 안에 `namespace`를 명시하도록 정책이 변경됨.
*   **조치:** `flutter/android/app/build.gradle`에 `namespace "com.carriez.flutter_hbb"` 코드를 추가.

---

## 🚨 3차 실패: 서드파티 플러그인(external_path) 연쇄 충돌
*   **현상:** 앱 자체의 네임스페이스는 해결되었으나, 이어지는 빌드에서 `:external_path` 플러그인 구성 중 동일한 Namespace 에러 발생.
*   **원인:** RustDesk가 사용 중이던 `external_path` 플러그인(1.0.3 버전)이 너무 오래된 버전이라 해당 플러그인 내부 `build.gradle`에도 네임스페이스가 없었음. AGP를 8.6.0으로 올려버린 탓에 구형 플러그인들이 줄줄이 호환성 오류를 뿜어내기 시작함.
*   **조치:** 임시방편으로 `pubspec.yaml`에서 `external_path`를 최신 버전(`^2.2.0`)으로 강제 업그레이드함.

---

## 🚨 4~5차 실패: Dart API Breaking Change (Null Safety)
*   **현상:** Gradle 환경 설정(Configure) 단계는 무사히 통과했으나, 실제 Flutter 코드를 컴파일하는 `compileFlutterBuildRelease` 단계에서 에러 발생.
    *   `Error: Operator '[]' cannot be called on 'List<String>?' because it is potentially null.`
*   **원인:** `external_path` 버전을 1.x에서 2.x로 올렸더니, 플러그인 내부의 Dart 함수 반환 타입이 `List<String>`에서 `List<String>?`(Nullable)로 변경되어 기존 RustDesk 코드(`native_model.dart`)와 타입 충돌이 발생함. (의존성 지옥 진입)

---

## 🚨 6차 실패: 원상 복구(Revert) 전략과 숨겨진 복병
*   **현상:** 플러그인 버전을 올리며 코드를 뜯어고치는 것은 끝이 없는 '두더지 잡기'가 될 것으로 판단. GitHub Actions 서버는 로컬(Java 21)과 달리 안정적인 **Java 17**을 사용하므로 애초에 AGP 8.x로 올릴 필요가 없었음을 깨달음.
*   **조치 1 (초심으로 복구):** AGP를 다시 7.3.1로, Gradle을 7.6.4로, `external_path`를 1.0.3으로 완벽히 롤백함.
*   **새로운 현상:** 그러나 최종 APK 패키징 단계(`processReleaseMainManifest`)에서 빌드 실패.
    *   `uses-sdk:minSdkVersion 21 cannot be smaller than version 22 declared in library [rustls:rustls-platform-verifier:0.1.1]`
*   **새로운 원인:** AGP 문제와 전혀 무관하게, 최근 RustDesk 원작자들이 보안을 위해 추가한 `rustls-platform-verifier` 라이브러리가 **최소 안드로이드 버전(minSdkVersion)을 22**로 요구하고 있었음. 하지만 Flutter 프로젝트의 기본값은 21이었기에 매니페스트 병합 중 충돌이 일어남.

---

## 🟢 7차 빌드 (최종 해결): 최소 지원 버전(minSdk) 상향
*   **조치:** `flutter/android/app/build.gradle`의 `defaultConfig` 내 `minSdkVersion`을 Flutter 기본값(21)에서 **22**로 명시적으로 상향 고정함. (안드로이드 5.1 이상 지원, 현대 기기에서는 문제없음).
*   **결과:** 모든 의존성 충돌 및 버전 에러가 해소되어 순정 상태의 안정적인 CI/CD 빌드 환경 복구 완료.

---

### 💡 레슨 런 (Lesson Learned)
1. **로컬 환경 vs CI 환경 분리 사고:** 로컬 Mac 환경의 특수성(Java 21)을 해결하기 위해 프로젝트 전역의 빌드 툴(AGP)을 함부로 업그레이드하면, CI 서버 및 수많은 구형 플러그인과 연쇄 충돌(Dependency Hell)을 일으킨다.
2. **오픈소스 포크의 주의점:** 거대한 레거시를 가진 오픈소스 프로젝트(RustDesk)의 경우, 플러그인 버전 하나를 올리는 것만으로도 Dart 코드 전반의 Breaking Change를 유발할 수 있으므로 가급적 원본의 버전을 존중(Revert)하는 우회로가 가장 빠르고 안전한 길이다.
