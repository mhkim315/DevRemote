# DevRemote Vision — Self-Hosted Manus

> "네 PC가 너만의 AI 클라우드. 폰은 리모컨."

## 포지셔닝

Manus의 UX를 카피. 연산은 클라우드 대신 **내 PC**.

| | Manus | DevRemote |
|---|---|---|
| 연산 | 클라우드 VM ($200/월) | 내 PC ($0) |
| 로컬 접근 | 불가 | localhost, DB, 파일 전부 |
| 소스코드 | 클라우드 업로드 | 외부 유출 0 |
| 보안 | 클라우드 의존 | WebRTC E2EE |

## 아키텍처

```
폰 ── WebRTC P2P ──→ 내PC
  ↕                     ↕
Chat UI              PTY Proxy
승인 버튼            Process Manager
파일 뷰어            File System RPC
```

## 로드맵

| Phase | 내용 | 상태 |
|---|---|---|
| 1-3 | PTY, WebRTC, 시그널링, 승인, 피드, Ctrl-C | ✅ 완료 |
| 4 | Chat UI + 양방향 대화 | ⬜ |
| 5 | Multi-Agent Process Manager + Adapter | ⬜ |
| 6 | File System RPC + 코드 뷰어 | ⬜ |
| 7 | Always-On + 시스템 트레이 | ⬜ |
| 8 | Enterprise (팀, 감사, SSO) | ⬜ |

## 기술 검증 (완료 95%)

**가능:** PTY 제어, WebRTC P2P, 절전 방지, 원격 프로세스 기동, JSONL 승인 감지, 채팅→stdin 주입

**발견된 함정 + 대응:**

| 함정 | 대응 |
|---|---|
| Wake-on-LAN 불안정 | Always-On PC 전용 |
| 채팅 충돌 (y/N 중 입력) | 데몬 State Machine (idle 체크 후 주입) |
| 파일 동기화 OOM 크래시 | Lazy Load + 16KB 청크 |
| 자식 프로세스 좀비 | Job Objects / Setpgid |

**폐기:** WoL. **해결 가능:** 채팅 큐잉, 청크 전송, 프로세스 트리. **미검증:** iOS WebRTC 백그라운드, Adapter Pattern.

## 수익

| 티어 | 가격 |
|---|---|
| Free | $0 (1 PC, LAN only) |
| Pro | $10/월 (무제한, 원격, 푸시) |
| Team | $30/월 (대시보드, 감사) |
