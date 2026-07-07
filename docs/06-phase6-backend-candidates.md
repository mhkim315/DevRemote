# Phase 6 — Third Backend Candidate Research

Date: 2026-07-07

Phase 6 요구사항: core/tmux/cmux production 파일을 수정하지 않고 등록 지점 한 줄만 추가하여
실제 세 번째 backend를 vertical slice로 연결한다.

## Selection Criteria

ADAPTER_EXPANSION_PLAN.md 기준:

1. 공식적이고 자동화 가능한 API/CLI가 있는가
2. session discovery와 양방향 I/O가 가능한가
3. 사용자 권한 범위에서 인증 가능한가
4. macOS GUI lifecycle에 종속될 경우 환경 요구를 탐지할 수 있는가
5. 테스트용 mock/fixture를 만들 수 있는가

## Candidates

### 1. zellij

- **Website**: https://zellij.dev
- **Language**: Rust
- **API/CLI**: `zellij list-sessions`, `zellij attach`, `zellij action` (풍부한 CLI)
- **Discovery**: `zellij list-sessions -s` → machine-readable output
- **I/O**: `zellij action write`, `zellij action write-chars`, `zellij attach` (PTY mode)
- **Permissions**: user-level, no root required
- **macOS**: Homebrew 설치 가능, LaunchAgent에서 PATH 이슈 가능하나 `binary discovery`로 대응 가능
- **Mockability**: `zellij` CLI가 잘 정의되어 있어 mock runner 기반 테스트 가능

**평가**: ★★★★★ 최상위 후보. tmux와 가장 유사한 multiplexer 모델. CLI가 풍부하고
machine-readable output 지원.

### 2. wezterm

- **Website**: https://wezfurlong.org/wezterm
- **Language**: Rust
- **API/CLI**: `wezterm cli list`, `wezterm cli spawn`, `wezterm cli send-text`
- **Discovery**: `wezterm cli list --format json` → JSON output!
- **I/O**: `wezterm cli send-text`, PTY attach 미지원 (GUI 전용)
- **Permissions**: user-level
- **macOS**: Homebrew, GUI 앱이지만 CLI는 독립 동작
- **Mockability**: JSON output으로 파싱이 매우 쉬움

**평가**: ★★★★☆ CLI는 훌륭하나 GUI 의존성과 PTY attach 미지원이 live stream 구현을
어렵게 만듦. screen capture는 `wezterm cli get-text`로 가능.

### 3. kitty

- **Website**: https://sw.kovidgoyal.net/kitty
- **Language**: C/Python
- **API/CLI**: `kitty @ ls`, `kitty @ send-text`, `kitty @ new-window`
- **Discovery**: `kitty @ ls` → JSON output
- **I/O**: `kitty @ send-text`, PTY는 kitty 내부 프로토콜
- **Permissions**: user-level, Unix socket 필요
- **macOS**: Homebrew, GUI 앱
- **Mockability**: JSON output, Unix socket 통신

**평가**: ★★★★☆ JSON 기반 프로토콜이 훌륭하나 macOS GUI 의존성이 높고 Unix socket
경로가 환경마다 다름.

### 4. GNU Screen

- **Language**: C
- **API/CLI**: `screen -ls`, `screen -X`
- **Discovery**: `screen -ls` → 파싱 가능하나 machine-readable format 아님
- **I/O**: `screen -X stuff`, `screen -X hardcopy`
- **Permissions**: user-level
- **macOS**: 기본 설치? 아니면 Homebrew
- **Mockability**: CLI 패턴이 오래되고 출력 파싱이 까다로움

**평가**: ★★☆☆☆ 오래된 도구. tmux가 이미 screen을 대체했으므로 추가 가치 낮음.

### 5. iTerm2 (macOS only)

- **Language**: Objective-C
- **API/CLI**: AppleScript, Python API
- **Discovery**: AppleScript로 가능하나 안정적이지 않음
- **I/O**: 제한적
- **Permissions**: user-level
- **Mockability**: AppleScript 의존성으로 테스트 매우 어려움

**평가**: ★★☆☆☆ macOS 전용, API 불안정, cross-platform 전략과 맞지 않음.

### 6. tmux (already supported)

- 이미 adapter 존재. Phase 6 대상 아님.

### 7. Native SSH / PTY

- **API/CLI**: `ssh`, `sshpass`, PTY 직접 열기
- **Discovery**: `who`, `ps`, `ss` — 표준화된 세션 개념 없음
- **I/O**: PTY 직접 I/O
- **Mockability**: SSH 서버 모킹 필요

**평가**: ★★☆☆☆ "텅 빈 백엔드". session discovery 개념이 없어 adapter abstraction과
맞지 않음.

### 7. LocalPTY (child process PTY)

- **API/CLI**: `os.StartProcess`, `os/exec`, PTY 직접 제어
- **Discovery**: 앱이 생성한 세션만 in-memory 관리 (create-only model)
- **I/O**: PTY master read/write — 완전한 양방향 I/O
- **Permissions**: user-level, shell/PTY만 필요
- **macOS**: `posix_openpt` 또는 `grantpt` 기반, 추가 binary 불필요
- **Mockability**: controlled child process (`echo`, `cat`)로 mock 가능

**평가**: ★★★★★ **Phase 6 최종 선택**. 외부 daemon 불필요, tmux/cmux와 완전히
다른 session ownership 모델, 실제 PTY live I/O 지원, architecture 검증력 최상.

### 8. zellij (deferred alternative)

- **Website**: https://zellij.dev
- **Language**: Rust
- **API/CLI**: `zellij list-sessions`, `zellij attach`, `zellij action`
- **Discovery**: `zellij list-sessions -s` → machine-readable
- **I/O**: `zellij action write-chars` + `zellij attach` (PTY)
- **Mockability**: CLI 정의가 잘 되어 있어 mock runner 테스트 가능

**평가**: ★★★★☆ 우수한 후보. LocalPTYAdapter 이후 Phase 7+에서 검토.
tmux와 유사한 multiplexer 모델이므로 LocalPTY보다 architecture 검증력은 낮음.

## Recommendation

**Phase 6: LocalPTYAdapter**

- 외부 의존성 최소화 (shell + OS PTY)
- tmux/cmux와 다른 session ownership 모델 — architecture 검증력 최고
- create-only discovery + live PTY I/O → adapter abstraction 최종 검증
- 이후 Phase 7+에서 zellij, wezterm 등 실제 multiplexer 검토 가능
