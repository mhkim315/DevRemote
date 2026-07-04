# BUG: 양방향 터미널 입력 불통 (PC + 모바일)

## 증상

- PC `pokit run claude` + 모바일 WS 터미널 모두 연결 성공
- Claude TUI 화면은 양쪽에서 정상 표시됨 (PTY → 출력 정상)
- 키보드 입력이 **양쪽 모두** Claude에 전달되지 않음
- `s.Write(msg)` 호출은 되지만 PTY에 실제로 전달 안 되는 것으로 추정

## 확인된 것

- WS 연결: 정상 (재연결 없음, 안정적)
- PTY 출력: 정상 (Claude 화면 보임)
- `s.Write()` 경로: `pty.go:277` → `session.go:155-161`
- IPC 입력 경로: `ipc.go:130`
- 두 경로 모두 `s.Write()` 호출하지만 효과 없음

## 의심 원인

1. `s.Write()`가 lock을 획득하지만 `s.running` 체크 통과 → `s.PTY.Write(p)` 반환값 무시
2. PTY master에 write는 성공하지만 PTY slave(tmux/bash)가 입력을 처리하지 않음
3. Session이 HandleWS에서 `bash`로 시작되고 IPC는 `sh -c claude`를 원함 → 명령어 불일치

## 로그

```
WS [claude]: [::1]:60123 connected
(에러/경고 없음)
```

## 다음 에이전트 디버깅 제안

1. HandleWS의 `s.Write(msg)` 후 리턴값 로깅
2. IPC input loop의 `s.Write(buf[:n])` 앞뒤로 로깅
3. PTY master에 write 후 slave에서 read 되는지 확인
4. session 생성 시점에 어떤 명령어(`bash` vs `sh -c claude`)로 실행되는지 확인
