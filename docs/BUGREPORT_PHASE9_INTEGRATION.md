# Phase 9 Integration Bug Report

> 실기기 테스트 (2026-07-05) 중 발견된 통합 이슈.

## 테스트 환경

- **데몬**: `devremote_bin daemon` (named tunnel, `term.fullcount.kr`)
- **세션**: `pokit run claude` (native mux)
- **모바일**: POKIT APK (Android 실기기, LTE)
- **인증**: Supabase (`test@mhk.dev`) 로그인 성공, JWT 발급 확인

## 통과 항목

| # | 항목 | 상태 |
|---|------|------|
| 1 | 데몬 실행 + named tunnel | ✅ HTTP 200 |
| 2 | QR 코드 `term.fullcount.kr` 출력 | ✅ |
| 3 | Supabase 로그인 (AuthScreen) | ✅ |
| 4 | ConnectScreen 수동 연결 버튼 | ✅ |
| 5 | Dashboard 세션 목록 (`claude` 표시) | ✅ |
| 6 | `pokit run claude` TUI 정상 렌더링 | ✅ (TERM fix 적용) |
| 7 | WebSocket 연결 (`WS [claude] connected`) | ✅ |

## 실패 항목

### BUG-1: 터미널 입력 불통 (CRITICAL)

**증상**: 모바일 터미널에서 키보드 입력이 Claude 세션에 전달되지 않음.  
또한 `cmux`에서 `pokit run claude` 실행 중 로컬 키보드 입력도 불통.

**확인된 사실**:
- WebSocket은 정상 연결됨 (`WS [claude]: [::1]:59063 connected`)
- JWT 토큰 검증 통과 (`DEV: accepted token`)
- `/debug/cmd` POST는 수신됨 (`CMD POST [claude]: "echo WS_TEST_123"`)
- 그러나 해당 명령어가 실제 PTY에서 실행되지 않음

**의심 원인**:
1. `s.Write(msg)` → PTY write 경로가 막혀있거나 실패 (에러 로그 없음)
2. Native mux `s.PTY`가 IPC 클라이언트와 WS에서 동시에 write 되어 충돌
3. `AddListener` 채널 패턴과 직접 PTY write 간의 동기화 문제

### BUG-2: `/term/size` 요청 실패 (MEDIUM)

**증상**: `fitTerminal()`의 `/term/size` POST 요청이 반복적으로 "Incoming request ended abruptly: context canceled" 오류 발생

**의심 원인**: 
- JWT 토큰이 URL 쿼리 파라미터로 전달되어 URL이 너무 김 (~2000자)
- Cloudflare/통신사에서 긴 URL을 끊어버릴 가능성
- `term/size` 핸들러가 request body를 기다리지만 POST body가 없어서 timeout

### BUG-3: ConnectScreen `trycloudflare.com` 전용 검사 (FIXED)

**증상**: QR 스캔이 `trycloudflare.com` URL만 허용하여 `term.fullcount.kr` 거부

**수정**: `data.startsWith('https://')`로 변경 + 수동 연결 버튼 추가 (`5811845bb`)

### BUG-4: IPC `size:` 파싱 중 키 입력 유실 (FIXED)

**증상**: 이전 에이전트 코드에서 `size:` 라인 파싱 시 `continue`로 인해 같은 TCP 패킷에 포함된 키 입력이 버려짐

**수정**: 헤더 기반 파싱으로 재구현 (`8d51f9f47`)

## 현재 ground-zero 커밋

```
5811845bb feat(mobile): add manual Connect button
3f7596e02 fix(mobile): accept any HTTPS URL in QR scanner
48fffd079 verify: Phase 9 hotfix confirmed
8d51f9f47 fix(core): complete overhaul of tunnel logic and IPC
69970e657 fix(mobile): add Rescan button
4e1812587 fix(daemon): pass environment + sync PTY size
```

## 다음 에이전트를 위한 디버깅 가이드

1. **BUG-1 최우선**: `pty.go` HandleWS의 `s.Write(msg)` (277행)에 로그 추가. PTY write가 실제로 성공하는지 확인.
2. Native mux의 `Write()` 메서드 (`mux/session.go` 155행)가 에러 반환하는지 확인.
3. IPC 클라이언트와 WS 리스너가 같은 PTY를 공유할 때 발생할 수 있는 경쟁 상태 검토.
4. `term/size` 핸들러가 JWT 토큰이 포함된 긴 URL을 처리할 수 있는지 확인.

---

*검증일: 2026-07-05 | 검증자: Claude Code (verification agent)*
