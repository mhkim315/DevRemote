# BUG: `pokit run claude` fails when WS connects first

## 증상

`./devremote_bin run claude` 실행 시 아무 출력 없이 즉시 종료됨.

## 원인

```
1. 폰이 WS로 먼저 연결 → HandleWS가 mux.NewSession("claude") 호출 → 세션 생성됨
2. 사용자가 pokit run claude 실행 → IPC가 mux.NewSession("claude") 호출 → "already exists" 오류
3. IPC 핸들러가 오류를 로깅만 하고 return → 클라이언트에 아무 응답 없음
```

## 로그

```
2026/07/05 01:30:03 failed to spawn session: session claude already exists
```

## 수정 위치

`internal/term/ipc.go` 85행:

```go
// 현재 (버그):
s, err := mux.NewSession(sessionID, termEnv, "sh", "-c", cmdStr)

// 수정:
s, ok := mux.GetSession(sessionID)
if !ok {
    var err error
    s, err = mux.NewSession(sessionID, termEnv, "sh", "-c", cmdStr)
    if err != nil {
        log.Println("failed to spawn session:", err)
        return
    }
}
```

## 재현 방법

1. 데몬 실행: `./devremote_bin daemon`
2. 폰에서 Dashboard → claude 세션 탭 (WS 연결)
3. 터미널에서: `./devremote_bin run claude`
4. → 실패

## 우회 방법

데몬 실행 직후 폰 연결 전에 먼저 `pokit run claude` 실행.
