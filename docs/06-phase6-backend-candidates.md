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

## Recommendation

**Phase 6: LocalPTYAdapter**

- 외부 의존성 최소화 (shell + OS PTY)
- tmux/cmux와 다른 session ownership 모델 — architecture 검증력 최고
- create-only discovery + live PTY I/O → adapter abstraction 최종 검증
- 이후 Phase 7+에서 zellij, wezterm 등 실제 multiplexer 검토 가능

## Selected: LocalPTY (child process PTY)

- **API/CLI**: `os.StartProcess`, `os/exec`, PTY 직접 제어
- **Discovery**: 앱이 생성한 세션만 in-memory 관리 (create-only model)
- **I/O**: PTY master read/write — 완전한 양방향 I/O
- **Permissions**: user-level, shell/PTY만 필요
- **macOS**: `posix_openpt` 또는 `grantpt` 기반, 추가 binary 불필요
- **Mockability**: controlled child process (`echo`, `cat`)로 mock 가능

**평가**: ★★★★★ 외부 daemon 불필요. tmux/cmux와 완전히 다른 session ownership
모델로 adapter abstraction 검증력 최상. 실제 PTY live I/O 지원.

## Evaluated Alternatives (Phase 7+ deferred)

### zellij

- **Website**: https://zellij.dev
- **API/CLI**: `zellij list-sessions`, `zellij attach`, `zellij action`
- **Discovery**: `zellij list-sessions -s` → machine-readable
- **I/O**: `zellij action write-chars` + `zellij attach` (PTY)
- **Mockability**: CLI 정의가 잘 되어 있어 mock runner 테스트 가능

**평가**: ★★★★☆ 우수한 multiplexer. tmux와 유사한 모델이므로 LocalPTY보다
architecture 검증력은 낮음. Phase 7+에서 검토.

### wezterm

- **Website**: https://wezfurlong.org/wezterm
- **API/CLI**: `wezterm cli list --format json` → JSON output
- **Discovery**: `wezterm cli list --format json`
- **I/O**: `wezterm cli send-text`, PTY attach 미지원 (GUI 전용)
- **Mockability**: JSON output으로 파싱 용이

**평가**: ★★★★☆ CLI 훌륭하나 GUI 의존성과 PTY attach 미지원이 제한.

### kitty

- **Website**: https://sw.kovidgoyal.net/kitty
- **API/CLI**: `kitty @ ls`, `kitty @ send-text` → JSON
- **Discovery**: `kitty @ ls` → JSON
- **I/O**: `kitty @ send-text`, Unix socket
- **Mockability**: JSON + Unix socket

**평가**: ★★★★☆ JSON 프로토콜 훌륭하나 macOS GUI 의존성과 Unix socket 경로 문제.

### GNU Screen

- **API/CLI**: `screen -ls`, `screen -X`
- **Discovery**: `screen -ls` → 파싱 까다로움
- **I/O**: `screen -X stuff`, `screen -X hardcopy`

**평가**: ★★☆☆☆ tmux가 이미 대체. 추가 가치 낮음.

### iTerm2 (macOS only)

- **API/CLI**: AppleScript, Python API
- **Discovery**: AppleScript (불안정)
- **I/O**: 제한적

**평가**: ★★☆☆☆ macOS 전용, API 불안정, cross-platform 전략과 불일치.

### Native SSH / PTY

- **API/CLI**: `ssh`, `sshpass`
- **Discovery**: `who`, `ps`, `ss` — 표준화된 세션 개념 없음
- **I/O**: PTY 직접 I/O
- **Mockability**: SSH 서버 모킹 필요

**평가**: ★★☆☆☆ session discovery 개념이 없어 adapter abstraction과 불일치.
