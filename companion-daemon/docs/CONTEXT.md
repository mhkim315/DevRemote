# DevRemote Session Context — 2026-07-01

> Mac에서 이어서 작업할 때 참고. 마지막 커밋: `284ba6d`

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

## 새 에이전트를 위한 인수인계

> Windows에서 Mac으로 전환. 아래 내용을 먼저 확인하세요.

### 자주 묻는 질문

**Q: 프로젝트 구조가 어떻게 되나요?**
A: `companion-daemon/` (Go 데몬) + `mobile/` (React Native Expo). 두 개가 한 쌍입니다. `companion-daemon/docs/PROJECT.md` 에 전체 구조가 문서화되어 있습니다.

**Q: 어떤 테스트가 진행되었나요?**
A: Windows에서 WebRTC P2P 원격 연결 성공. 승인/거절 relay 검증 완료. LAN WebSocket 부분 성공. 피드(raw feed)와 채팅(stdin)은 미검증.

**Q: 현재 어떤 버그가 있나요?**
A: BUGREPORT.md 참고. 주요 이슈: signald 재연결, 피드 빈 화면, 피드 채팅 미전달, --exec 모드에서 Claude 파이프 종료.

**Q: Oracle 서버 접속 정보는?**
A: `ssh -i ~/.ssh/oracle.key opc@168.107.59.177`, signald는 `/usr/local/bin/signald`, systemd `devremote-signald.service`

**Q: Expo 빌드는 어디서 하나요?**
A: Windows `nanimi` 계정 15회 소진. `minani` 계정(kmwh94315@gmail.com) 2/15 사용. Mac에서 새 계정으로 가능.

**Q: 실제 Claude 승인 테스트는 어떻게 하나요?**
A: `devremote wrap` (PTY 모드)로 해야 합니다. `--exec` 모드는 pipe 때문에 Claude가 바로 죽습니다.

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

### 이전 에이전트 답변 요약 (2026-07-01 업데이트)

| 질문 | 답변 |
|---|---|
| Windows 빌드/테스트? | ✅ Windows에서 WebRTC P2P 원격 연결 성공, 승인/거절 relay 검증 완료 |
| APK 폰 테스트? | ⚠️ Windows에서는 성공. **Mac에서는 아직 검증 안 됨** |
| `--exec` 모드? | ❌ pipe 때문에 Claude가 바로 죽음. **`devremote wrap` (PTY)로 해야 함** |
| 피드/채팅? | ❌ 미검증 (raw 피드 빈 화면, 채팅 stdin 미전달) |
| Expo 빌드 계정? | `minani`(kmwh94315@gmail.com) 2/15 사용. Mac에서 새 계정 가능 |

### 여전히 답변 없는 질문

1. iOS WebRTC 백그라운드 유지 가능한가? (VISION.md "미검증" 상태)
2. 시그널링 서버(Oracle) 다운 시 이미 연결된 WebRTC 세션은 유지되는가?
3. Expo Go vs EAS Build 결정했는가?
4. BUG-001~007 수정 후 재검증 완료했는가?
5. Play Store 배포 준비 상태? (개인정보처리방침 등)

### 수정 필요: Mac에서 할 일

`--exec`가 작동하지 않으므로 아래로 교체:
```bash
# --exec 대신 devremote wrap 사용
go build -o devremote ./cmd/devremote/
./devremote --project ~/.claude/projects/... --signaling http://168.107.59.177:9173
# 다른 터미널에서:
devremote wrap claude
```

전체 질문 목록: `docs/REVIEW_QUESTIONS.md`
