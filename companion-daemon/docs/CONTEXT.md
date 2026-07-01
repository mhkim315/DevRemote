# DevRemote Session Context — 2026-07-01

> Mac에서 이어서 작업할 때 참고. 마지막 커밋: `67ef8aa`

## 현재 상태

- **브랜치**: `companion-daemon`
- **APK 빌드됨**: `@minani/devremote` Expo 프로젝트 (2/15 사용)
- **시그널링 서버**: Oracle 168.107.59.177:9173 (ARM64, systemd)
- **도메인 프록시**: api.fullcount.kr/signal/ (nginx)

## 완료된 기능

- Go 데몬: JSONL watcher, tool_use detector, WebRTC P2P, PTY proxy
- 모바일: Expo SDK 56, WebSocket LAN, WebRTC 원격, HTTP REST 시그널링
- 승인/거절 relay (WebRTC → daemon → stdin)
- Raw 피드 뷰 + 채팅 입력 (피드 일부 버그 있음)
- Shell hook (`devremote hook`), PTY wrap (`devremote wrap`)

## 남은 버그

1. **재연결 실패**: signald paired 조건 수정했으나 Mac에서 검증 필요
2. **피드 빈 화면**: raw 이벤트 포맷만 개선, 실제 전달은 검증 안 됨
3. **피드 채팅 미전달**: data channel sendMessage 경로 확인 필요
4. **Windows Ctrl-C**: GenerateConsoleCtrlEvent 구현했으나 미검증

## Mac에서 할 일

1. `go build -o devremote ./cmd/devremote/`
2. `./devremote --project ~/.claude/projects/... --signaling http://168.107.59.177:9173 --exec`
3. 폰에 APK 설치 → 코드 입력 → WebRTC 연결
4. 실제 Claude 실행해서 승인 테스트 (`devremote wrap`으로)
5. iOS 빌드 (EAS Build, 새 계정)

## 핵심 아키텍처

```
Claude Code → JSONL 파일 → Daemon(watcher+detector) → WebRTC P2P → Mobile
                                                           ↑
                                         시그널링 서버(Oracle:9173)
                                                           ↓
                              Mobile ← HTTP REST polling (/join, /send, /poll)

Mobile 승인 → WebRTC Data Channel → daemon onResponse → stdinWriter → Claude stdin
```

## 경쟁 포지셔닝

"Self-Hosted Manus" — 내 PC를 AI 클라우드로. 소스코드 외부 유출 0.
타겟: 바이브코더, Claude Code 사용자. Manus 대비 $0 서버비용 + 로컬환경 접근.

---

## 인수인계 (2026-07-01, 새 Claude 세션)

이전 세션에서 RustDesk 포크 관련 논의 → 이 레포(`companion-daemon`)로 피벗 완료.
아키텍처/방향성 검토 완료. 자세한 질문 목록은 `docs/REVIEW_QUESTIONS.md` 참고.

### 인수인계자가 확인한 것

- ✅ Go 데몬 코드 (`cmd/devremote/main.go`, `internal/`) — 구조 깔끔, MIT 전부
- ✅ PROJECT.md / VISION.md — 방향 명확, B2C 바이브코더 타겟
- ✅ BUGREPORT.md — 7개 버그 기록 + 해결 완료
- ✅ JSONL 접근법 — 화면 캡처보다 훨씬 우월한 접근

### 가장 시급한 질문 (빠른 답변 필요)

1. APK 실제 폰에서 엔드투엔드 테스트 완료했는가? (연결→승인→stdin relay)
2. 버그 수정 후 재검증 완료했는가?
3. iOS WebRTC 백그라운드 유지 가능한가?
4. 시그널링 서버 다운 시 이미 연결된 세션 유지되는가?
5. Windows 빌드/테스트 해봤는가?

전체 질문 목록: `docs/REVIEW_QUESTIONS.md`
