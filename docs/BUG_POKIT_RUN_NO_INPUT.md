# BUG: `pokit run claude` 터미널 양방향 입력 불통 이슈 (해결됨)

## 증상

데몬을 실행하고 `pokit run claude` (또는 모바일 앱에서 WS 연결)로 세션을 열었을 때:
1. `Claude Code`의 TUI(화면)는 클라이언트에 정상적으로 출력됨.
2. 클라이언트에서 키보드 입력을 보내면 (예: `hello`), 데몬은 이를 정상적으로 수신하고 PTY 소켓에 씀(`s.PTY.Write`).
3. 하지만 화면에 입력이 에코되지 않으며, `Claude` 프로세스가 입력을 처리하지 않고 TUI가 완전히 멈추는 현상 발생.

## 원인: `vt.SafeEmulator` 파이프 교착상태 (Deadlock)

`devremote` 데몬은 백그라운드 세션의 화면 상태를 캡쳐하기 위해 `github.com/charmbracelet/x/vt` 패키지의 `SafeEmulator`를 사용합니다. 
`session.go`의 읽기 루프는 PTY 마스터 소켓에서 읽어들인 출력을 클라이언트에게 브로드캐스트하기 직전에 `term.Write(chunk)`를 호출하여 가상 터미널 에뮬레이터에 상태를 반영합니다.

```go
term := vt.NewSafeEmulator(80, 24)

// ...
n, err := ptm.Read(buf)
chunk := buf[:n]
term.Write(chunk) // <-- 여기서 교착상태 발생
```

`Claude Code` CLI (ink 라이브러리 사용)는 시작할 때 다크 모드/라이트 모드 등을 감지하기 위해 터미널에 **OSC 11 (배경색 질의)** 제어 문자인 `\x1b]11;?\a`를 전송합니다.
`vt` 에뮬레이터가 이 `term.Write` 과정에서 OSC 질의 문자를 만나면, 해당 질문에 대한 응답(예: `\x1b]11;rgb:0000/0000/0000\a`)을 생성하여 자신의 내부 `io.PipeWriter`로 기록합니다.

문제는 **아무도 `term`(`SafeEmulator`)의 `io.PipeReader`에서 데이터를 읽어가지 않는다는 점**이었습니다.
`devremote`는 단순히 화면 캡쳐(`term.String()`) 용도로만 `term`을 사용했기 때문에, 내부 파이프가 꽉 차서 `term.Write(chunk)` 호출이 **영원히 블록(Block)되는 데드락**이 발생했습니다.
결과적으로 PTY 출력을 읽어들이는 고루틴 전체가 멈춰버렸고, 클라이언트의 화면 갱신은 물론 모든 키보드 에코 처리가 완전히 정지된 것입니다.

## 해결 방법

`term`의 응답 파이프를 지속적으로 비워주어(drain) `Write`가 블록되지 않도록 수정했습니다.

**`internal/mux/session.go` 수정:**
```go
import "io"

// ...

term := vt.NewSafeEmulator(80, 24)
term.SetScrollbackSize(10000)

// MUST drain the terminal's response pipe so it doesn't deadlock on OSC queries (e.g. background color queries)
go io.Copy(io.Discard, term)
```

이 고루틴을 추가함으로써 `vt` 에뮬레이터가 생성하는 모든 터미널 응답(OSC 응답 등)이 즉시 버려지게 되어 데드락이 해소되었습니다.
실제 클라이언트 터미널(예: 모바일 앱의 xterm.js 또는 PC의 iTerm2)들은 데몬이 브로드캐스트한 원본 OSC 문자를 받아 각자 자신의 환경에 맞는 응답을 WS/IPC로 전송하므로, 데몬 측 에뮬레이터의 응답은 버려도 무방합니다.
