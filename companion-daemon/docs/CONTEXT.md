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
