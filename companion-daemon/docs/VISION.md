# DevRemote Vision — Self-Hosted Manus

> 마지막 업데이트: 2026-07-01. 제품의 궁극적 지향점과 로드맵.

## 한 줄 정의

**"네 방 컴퓨터가 너만의 AI 클라우드 서버다. 폰은 그걸 부리는 리모컨이다."**

## 포지셔닝

Manus, Devin 같은 클라우드 기반 AI 에이전트의 UX를 그대로 카피하되, 연산 서버를 클라우드가 아닌 **사용자의 PC**로 대체한다.

```
Manus:    폰 ──→ 클라우드VM ──→ 에이전트
DevRemote: 폰 ──→ 내PC ──→ 에이전트
           ↑ WebRTC P2P 암호화 직통
```

## 왜 "Self-Hosted Manus"인가

### 1. 로컬 환경 완전 접근

클라우드 에이전트는 절대 닿을 수 없는 것들:
- 사내 VPN, 내부 DB, `localhost:8080`
- 내 PC에 꽂힌 USB, 로컬 파일 시스템
- 내가 구축한 개발 환경 전체

### 2. 보안 & 프라이버시

- WebRTC DTLS 종단간 암호화
- 소스코드가 외부로 단 한 줄도 유출되지 않음
- 삼성, 애플 같은 극보안 기업도 도입 가능

### 3. 서버 비용 제로

- 사용자 PC의 CPU/메모리를 공짜로 사용
- 우리 서버는 경량 시그널링 서버 하나면 충분
- Manus $200/월 → 우리는 $10/월로도 영업이익 99%

## 경쟁 환경

| | Manus/Devin | DevRemote |
|---|---|---|
| 연산 위치 | 클라우드 VM | **내 PC** |
| 로컬 접근 | 불가 | **완전 접근** |
| 소스코드 | 클라우드 업로드 | **내 PC에 유지** |
| 비용 | $200-500/월 | **$10/월 목표** |
| 대기 시간 | 클라우드 큐 | **즉시 실행** |

## 기술 아키텍처 (목표 상태)

```
┌─────────────────────────────────────────────────────┐
│                    내 PC (Host)                      │
│                                                      │
│  DevRemote Daemon                                    │
│  ├── PTY Proxy: Claude, Codex, Aider, Manus-style   │
│  ├── Process Manager: 에이전트 spawn/kill/monitor   │
│  ├── File System RPC: readFile, writeFile            │
│  └── Always-On: 절전 방지, Wake-on-LAN              │
│                                                      │
│  WebRTC Data Channel (DTLS encrypted)                │
│  ├── stdin/stdout 스트리밍                           │
│  ├── 승인 요청 + 응답                                │
│  └── 파일 전송                                       │
└──────────────────┬──────────────────────────────────┘
                   │
    ┌──────────────┴──────────────┐
    │         Smartphone          │
    │  ┌────────────────────────┐ │
    │  │  Chat UI (대화창)       │ │
    │  │  "블로그에 다크모드"     │ │
    │  └────────────────────────┘ │
    │  ┌────────────────────────┐ │
    │  │  Agent Status Board     │ │
    │  │  "코드 수정 중..."      │ │
    │  └────────────────────────┘ │
    │  ┌────────────────────────┐ │
    │  │  Approval Prompt         │ │
    │  │  [승인] [거절]           │ │
    │  └────────────────────────┘ │
    │  ┌────────────────────────┐ │
    │  │  File Viewer             │ │
    │  │  코드 편집/리뷰          │ │
    │  └────────────────────────┘ │
    └─────────────────────────────┘
```

## 진화 로드맵

### Phase 1-3 (현재 완료)
- [x] JSONL watcher + detector
- [x] WebSocket LAN + WebRTC P2P 원격
- [x] HTTP REST 시그널링
- [x] PTY Proxy (`devremote wrap`)
- [x] Shell hook (`devremote hook`)
- [x] 승인/거절 + Raw 피드 뷰
- [x] Ctrl-C + stdin 텍스트 주입
- [x] AskUserQuestion 객관식 UI

### Phase 4: Chat UI + 양방향 대화
- [ ] 채팅창 메인 UI (ChatGPT 스타일)
- [ ] "블로그 만들어줘" → PTY stdin 자동 주입
- [ ] Claude 응답 → 채팅 말풍선으로 실시간 표시
- [ ] CLI 명령어와 일반 대화 구분 없이 자연어로

### Phase 5: Multi-Agent Process Manager
- [ ] `devremote start --agent claude`
- [ ] 폰에서 새 에이전트 기동/종료
- [ ] 여러 에이전트 동시 실행 + 세션별 탭
- [ ] 에이전트 상태 대시보드 (CPU, 진행률)
- [ ] Adapter Pattern: Claude/Codex/Aider 자동 감지

### Phase 6: File System RPC
- [ ] 폰에서 파일 트리 브라우징
- [ ] 파일 클릭 → 코드 뷰어로 열기
- [ ] 수정된 파일 Diff 표시
- [ ] 폰에서 간단한 파일 편집 → PC에 저장

### Phase 7: Always-On Infrastructure
- [ ] OS 절전 방지 (caffeinate / SetThreadExecutionState)
- [ ] Wake-on-LAN
- [ ] 데몬이 시스템 트레이에 상주
- [ ] 부팅 시 자동 시작

### Phase 8: Enterprise
- [ ] 팀 대시보드 (여러 PC 모니터링)
- [ ] 감사 로그 (모든 승인/거절 기록)
- [ ] RBAC 권한 관리
- [ ] SSO / OAuth 연동

## 수익 모델

| 티어 | 가격 | 기능 |
|---|---|---|
| Free | $0 | 1 PC, 1 Agent, LAN only |
| Pro | $10/월 | 무제한 PC/Agent, WebRTC 원격, 푸시 알림 |
| Team | $30/월/팀 | 멀티유저 대시보드, 감사 로그, RBAC |
| Enterprise | 맞춤 | SSO, 전용 시그널링, SLA |

## 시장 규모

- Manus 사용자: ~200만 명 (2026 추정)
- Claude Code 사용자: ~50만 명
- "코딩을 모르는 AI 사용자" (Vibe Coders): 폭발적 증가 중
- **초기 타겟**: PC에서 Claude Code 쓰는 바이브코더 10만 명
- **확장 타겟**: AI 에이전트를 쓰는 모든 개발자

## 핵심 차별점 (USP)

> "당신의 PC를 AI 클라우드로. 소스코드는 한 줄도 밖으로 안 나갑니다."
