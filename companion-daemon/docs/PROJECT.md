# DevRemote — Project Manifesto

> 마지막 업데이트: 2026-06-30. 이 문서는 모든 피벗을 거친 최종 방향성을 담고 있습니다.

## 1. 제품 정의 (What We Build)

**AI 에이전트 작업을 모바일에서 감독하고 통제하는 Human-in-the-Loop 대시보드.**

단순 알림이 아니다. 모바일 화면에 최적화된 "비동기 AI 작업 큐" 관리 도구다. GitHub 모바일 앱에서 PR을 리뷰하고 Merge 버튼을 누르듯, 내 PC에서 돌고 있는 Claude Code의 작업을 모바일로 리뷰하고 승인한다.

## 2. 타겟 고객

**바이브 코더 (Vibe Coders)** — 터미널보다 GUI에 익숙한, AI에게 일을 시키고 결과를 기다리는 모든 사람.

핵심 페인포인트:
- 퇴근 전 Claude에 작업 지시 → 지하철에서 결과 확인하고 싶다
- 외출 중 AI가 "이 파일 덮어쓸까요?" 물어봄 → AnyDesk 켜기도 귀찮고 결국 PC 앞으로 간다
- 텔레그램 봇은 현상만 알려줄 뿐 "판단"은 못 내리게 한다

## 3. 피벗 히스토리 (What We Killed)

| 시도 | 결과 | 교훈 |
|---|---|---|
| RustDesk 포크 (AGPL) | 폐기 | 90만줄 유지보수 불가, 라이선스 리스크 |
| DevKeypad + FAB | 보관 | 모바일 키보드로 코딩 제어는 한계 |
| 60fps 화면 스트리밍 | 폐기 | 폰에서 27인치 화면을 보는 건 무의미 |
| Firebase RTDB 중계 | 보류 | 추후 외부 접속 필요할 때 검토 |
| 중간 AI 요약 레이어 | 폐기 | Claude가 JSONL에 이미 충분한 정보를 남김 |
| CLAUDE.md 태그 주입 | 폐기 | 사용자 파일 수정 없이 JSONL에서 추출 가능 |

## 4. 현재 아키텍처

```
Claude Code (PC)                Go Daemon                  React Native (Mobile)
────────────────                ──────────                ──────────────────────
  JSONL 파일에 모든              watcher/                   WebSocket 연결
  이벤트 기록                    └─ thinking 블록           SessionScreen
     ↓                           └─ tool_use 블록           └─ EventCard 리스트
     ↓                           └─ text 응답               └─ 음성 입력 버튼 (예정)
                                                           └─ 위험도 뱃지 (예정)
   detector/
   └─ 3s 타임아웃 감지
   └─ AskUserQuestion 즉시 알림

   server/ws.go
   └─ WebSocket 브로드캐스트
```

**핵심 원칙: Claude가 이미 JSONL에 모든 정보를 남긴다. 우리는 파싱만 하면 된다.**
별도 요약 AI도, CLAUDE.md 수정도, 태그 주입도 필요 없다.

## 5. 기술 스택 (최종)

| 컴포넌트 | 기술 | 라이선스 |
|---|---|---|
| 데몬 | Go + fsnotify + gorilla/websocket | MIT |
| 모바일 | React Native + TypeScript | MIT |
| 통신 | WebSocket (동일 WiFi) | — |
| 푸시 (미래) | Firebase RTDB + FCM | — |

RustDesk, Flutter, AGPL — 완전히 제거됨.

## 6. 경쟁 환경

| 경쟁자 | 그들이 하는 것 | 우리의 차별점 |
|---|---|---|
| Hermes Agent / OpenClaw | 다중 에이전트 오케스트레이션, 100% 자동화 목표 | **사람이 판단해야 하는 순간**에 특화된 모바일 UX |
| 텔레그램 봇 | 텍스트 알림 + 버튼 | **모바일 전용 대시보드** (요약 카드, 음성 입력, 위험도 시각화) |
| AnyDesk / 원격 데스크톱 | 화면 미러링 | **구조화된 데이터** (화면 픽셀이 아닌 의미 단위 전달) |

**전략:** Hermes/OpenClaw와 경쟁하지 않는다. 그들의 "부족한 Human-in-the-Loop UX"를 우리가 채운다. "에이전트 오케스트레이션은 Hermes 쓰세요. 모바일 감독은 DevRemote로."

## 7. 현재 상태

| 컴포넌트 | 상태 |
|---|---|
| Go 데몬 (watcher + detector + server) | 빌드 완료, 테스트 통과, 실제 Claude 로그로 검증 |
| React Native 앱 (Home + Session + EventCard) | TypeScript 통과 |
| WebSocket 통신 (데몬 ↔ 앱) | 기본 구조 완료 |
| 음성 입력 | 예정 |
| 위험도 트리아지 | 예정 |
| Firebase 외부 접속 | 미래 |

## 8. 즉시 할 일

1. 데몬의 parser를 thinking 블록에서 작업 설명 추출하도록 개선
2. EventCard UI를 "이 작업이 무엇이고, 왜 승인이 필요한지" 보여주도록 고도화
3. 음성 → 텍스트 입력 추가 (React Native voice recognition)
4. 위험도 뱃지 (파일 경로/명령어 기반 자동 분류)
