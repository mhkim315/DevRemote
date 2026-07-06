# 전역 상태 리팩터링 최종 검증

- 검증일: 2026-07-06
- 기준 커밋: `53b8147c1`
- 브랜치: `feature/phase10-multi-adapter`
- 판정: **ACCEPT**

## 1. 정적 검사

### 1.1 Go mutable package global

```text
$ grep -rn '^var [A-Za-z]' companion-daemon --include='*.go' | grep -v '_test.go' | grep -v 'regexp\|ErrRegistry\|upgrader'
(none)
```

남은 package-level var는 모두 불변 항목: `regexp.MustCompile`, `ErrRegistryMissing`, `upgrader`.

### 1.2 init() 자동 등록

```text
$ grep -rn 'func init()' companion-daemon/internal/mux --include='*.go'
(none)
```

### 1.3 Singleton wrapper

```text
$ grep -rn 'Default\|GetInstance\|GlobalRegistry\|GetRegistry' companion-daemon --include='*.go'
(none)
```

### 1.4 http.DefaultServeMux

```text
$ grep -rn 'http\.DefaultServeMux\|http\.HandleFunc(' companion-daemon/cmd companion-daemon/internal --include='*.go' | grep -v '_test.go'
(none)
```

Daemon 경로에서 DefaultServeMux 사용 없음. `http.NewServeMux()`만 사용.

### 1.5 Daemon goroutine os.Exit

```text
$ grep -rn 'os\.Exit' companion-daemon/cmd/devremote --include='*.go' | grep -v '_test.go'
hook.go:31  (CLI subcommand — not daemon path)
client.go:20,72,81  (CLI subcommand — not daemon path)
```

### 1.6 Mobile mutable config.BASE_URL

```text
$ grep -rn 'config\.BASE_URL\s*=' mobile/src --include='*.ts' --include='*.tsx'
(none)
```

### 1.7 Mobile AsyncStorage BASE_URL (outside ConnectionProvider)

```text
$ grep -rn 'AsyncStorage.*BASE_URL\|BASE_URL.*AsyncStorage' mobile/src --include='*.ts' --include='*.tsx' | grep -v connection.tsx
(none)
```

## 2. 자동 검증

```text
companion-daemon:
  go vet ./...        PASS
  go test ./...       PASS
  go test -race ./... PASS

mobile:
  npx tsc --noEmit    PASS (검증 에이전트 확인)
```

## 3. Phase별 판정 요약

| Phase | 최종 커밋 | 판정 |
|---|---|---|
| 1 | `ffe44e8` (선행) | ACCEPT |
| 2 | `694edacb3` | ACCEPT |
| 3 | `831e7a9d1` | ACCEPT |
| 4 | `0149a4f5b` | ACCEPT |
| 5 | `5a7f48974` | ACCEPT |
| 6 | `da2622670` | ACCEPT |
| 7 | `53b8147c1` | ACCEPT |

## 4. 제거된 mutable global 요약

| 영역 | Before | After |
|---|---|---|
| Registry | `mux.Default`, package-level map | `App.registry` (*mux.Registry) |
| Auth | `term.OwnerUUID`, `SupabaseProjectRef`, `InsecureLocalOnly` | `SupabaseVerifier.config` |
| JWKS | `term.jwksCache`, `jwksCacheExpiry`, `jwksCacheMu` | `SupabaseVerifier.keys`, `.expires`, `.mu` |
| Events | `models.eventsCache`, `eventsMu` | `EventStore` interface |
| Links | `term.sessionLinks`, `linkerMu` | `LinkStore` interface |
| Commands | `term.pendingCmds`, `cmdMu` | `CommandBroker` interface |
| Telemetry | `term.telemetryCache`, `telemetryMu` | `TelemetryService.sessions`, `.mu` |
| Callback | `term.OnApproval func(string)` | `Notifier` interface |
| HTTP | `http.DefaultServeMux` + `http.HandleFunc` | `http.NewServeMux()` (private) |
| Mobile | `config.BASE_URL` mutation | `ConnectionProvider` |
| Mobile | AsyncStorage scattered reads/writes | `ConnectionProvider` single point |

## 5. 최종 아키텍처

```
main.go
  └─ runDaemon(Config)
       └─ App
            ├─ SupabaseVerifier (auth + JWKS)
            ├─ Registry (adapters + snapshots)
            │    ├─ CmuxAdapter
            │    └─ TmuxAdapter
            ├─ EventStore (agent events, in-memory)
            ├─ LinkStore (session links, file-backed)
            ├─ CommandBroker (pending commands)
            ├─ TelemetryService (state machine loop)
            ├─ IPCServer (Unix socket)
            ├─ Watcher (JSONL tailing)
            ├─ Tunnel (cloudflared)
            └─ Notifier (push notifications)

HTTP Handlers{Registry, Verifier, Events, Links, Cmds, Telemetry}
```

의존 방향: `cmd → app → service interfaces → adapters/stores`

## 6. 계획 완료 기준 충족

| 기준 | 상태 |
|---|---|
| Runtime mutable state가 App 하위 객체에 귀속 | PASS |
| Registry가 인스턴스이며 init() 자동 등록 없음 | PASS |
| 인증 설정과 JWKS cache가 verifier 인스턴스에 귀속 | PASS |
| Event/Link/Command/Telemetry state가 명시적 owner 보유 | PASS |
| HTTP server가 private ServeMux 사용 | PASS |
| Daemon lifecycle이 context로 종료 | PASS |
| Mobile base URL과 API 호출이 client 계층에 중앙화 | PASS |
| go vet, unit test, race test, tsc 통과 | PASS |
| Protocol 또는 보안 정책 암묵적 변경 없음 | PASS |

## 7. 실환경 Smoke Test (Section 6.3)

### 7.1 macOS Daemon

```text
Daemon PID:      22748
Listening:       localhost:9171 (TCP)
cmux socket:     ~/.local/state/cmux/cmux.sock (srw-rw-rw-)
IPC socket:      /tmp/pokit.sock (managed by App.ipcPath)
```

Session 상태:

```text
/api/sessions → 12 sessions (cmux 3, tmux 9)
All stale:      false / N/A
All lastError:  null / N/A
cmux history:   surface:1 → 1 event, actual terminal content returned
daemon log:     no Broken pipe, no cmux tree failed
```

### 7.2 Mobile 실기기 Smoke

검증자: 사용자 실기기 수동 확인

PASS:

- cmux desktop ↔ mobile live sync
- cmux mobile text input
- cmux mobile Enter execution
- cmux session monitoring reacts to chat activity
- cmux history/activity
- tmux mobile text input
- tmux Enter
- tmux Ctrl+C
- tmux terminal color output
- tmux activity/history output
- cmux ↔ tmux session switching
- short background → foreground reconnect/continuity

NOT TESTED:

- approval Y/N
- daemon restart reconnect

발견된 안정성 이슈:

- dead/stale session에서 `can't find session` 계열 메시지가 반복 출력될 수 있음
- dashboard session card order가 polling refresh마다 뒤바뀔 수 있음
- session animation이 주기적으로 모두 idle처럼 멈출 수 있음
- tmux Activity 탭은 정상이나 Terminal 탭 live output이 지연/누락될 수 있음
- cmux terminal output은 현재 plain-text snapshot 기반이며 ANSI color/style preservation은 지원하지 않음

Smoke-fix 조치:

- `/api/sessions` 응답 순서 안정화를 위해 server-side stable sort 추가
- telemetry transient sampling failure 1~2회는 이전 state를 보존하도록 완화
- tmux live stream attach 전 preflight를 수행해 사라진 session의 `can't find session` 출력이 terminal에 직접 흘러가지 않도록 차단
- tmux WebSocket 연결 직후 initial screen snapshot을 전송해 terminal reconnect/초기 화면 지연을 완화
- 모바일 FeedScreen이 session list에서 현재 session이 사라진 것을 감지하면 WebView를 unmount하고 `SESSION ENDED` 상태를 표시

## 8. 후속 작업

- Adapter contract 정리 (별도 follow-up): capability interface 문서화, 공통 contract test
- daemon restart reconnect smoke test
- approval Y/N smoke test
- cmux ANSI/color preservation은 adapter capability로 문서화
