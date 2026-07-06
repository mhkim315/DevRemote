# Adapter Expansion Phase 2 Self-Check Review

Baseline under review: `503ccc6` (`Phase 2 셀프 검증 완료`)

Previous verifier baseline: `a942a8f` (`docs: review adapter phase 2 progress`)

Verdict: **REJECTED — PHASE 2 NOT COMPLETE**

The self-check revision makes meaningful progress, but Phase 2 is not complete. Two acceptance-level blockers remain:

1. mobile still contains a hard-coded `tmux|cmux` regex fallback;
2. unsupported capability responses are not pinned to a JSON error shape.

## Verification performed

- `git fetch origin feature/phase10-multi-adapter`
- `git merge --ff-only origin/feature/phase10-multi-adapter`
- `git diff --check a942a8f..HEAD`
- Code review of:
  - `companion-daemon/internal/mux/adapter.go`
  - `companion-daemon/internal/term/api_golden_test.go`
  - `companion-daemon/internal/term/pty.go`
  - `mobile/src/components/AgentCard.tsx`
  - `docs/ADAPTER_EXPANSION_PLAN.md` Phase 2 requirements
- Targeted Go test:

```sh
GOCACHE=/tmp/devremote-phase2-selfcheck-go-cache go test ./internal/term ./internal/mux -count=1
```

Initial sandboxed run failed because the sandbox blocked `httptest` listener creation. The same command was rerun outside the sandbox.

Result outside sandbox: **PASS**

- Mobile typecheck:

```sh
npx tsc --noEmit
```

Result: **PASS**

Full `go vet ./...`, `go test ./...`, and `go test -race ./...` were not used as acceptance gates because code review still found Phase 2 contract blockers.

## Improvements confirmed

### 1. API golden tests now pin additive telemetry fields

`TestAPIGolden_GetSessions` now requires:

- `displayId`
- `capabilities`

and asserts the golden session has `displayId == "golden"` and non-empty capabilities.

### 2. capability-less session coverage was added

`TestAPIGolden_CapabilityLessSession_NoPanic` verifies:

- a bare session appears in `/api/sessions`;
- it has no capabilities;
- WebSocket access for a session without `StreamOpener` returns `501`.

This is directionally aligned with the Phase 2 requirement that capability-less sessions do not panic or silently fall back.

### 3. old payload compatibility is partially covered

`TestAPIGolden_BackwardCompat_MissingNewFields` verifies that an older JSON payload without `displayId` and `capabilities` still deserializes into `SessionTelemetry`.

### 4. duplicate live-stream-adjacent interfaces were reduced

`mux.OutputStream`, `mux.Resizer`, and `mux.Closer` were removed from `adapter.go`. The remaining canonical live stream entry point is closer to:

- `StreamOpener`
- `TerminalStream`

This addresses part of the Phase 2 live-stream interface cleanup.

## Blocking findings

### 1. mobile still uses a hard-coded backend regex fallback

`mobile/src/components/AgentCard.tsx` now adds `displayId?: string` and `capabilities?: string[]`, but rendering still falls back to a backend-specific regex:

```tsx
session.displayId || session.id.replace(/^(tmux|cmux):/, '')
```

This still violates the Phase 2 requirement:

> 모바일의 backend 이름 정규식 제거와 capability 기반 버튼 처리를 이 Phase에서 완료한다.

The fallback for old payloads must not guess known backend names. It should render the canonical ID safely:

```tsx
session.displayId ?? session.id
```

Required correction:

- Remove the `tmux|cmux` regex entirely from mobile production code.
- Render `session.displayId ?? session.id`.
- Keep compatibility with old payloads by showing canonical ID when `displayId` is absent.
- Add a small mobile test or typed fixture if the project has a test harness; at minimum keep `npx tsc --noEmit` passing.

### 2. unsupported response JSON shape is not fixed

Phase 2 requires:

> unsupported 응답의 HTTP status와 JSON error 형식을 고정한다.

The added capability-less WebSocket test only checks:

```go
if wsRec.Code != http.StatusNotImplemented { ... }
```

But `HandleWS` currently uses plain `http.Error`:

```go
http.Error(w, "Session does not support streaming", http.StatusNotImplemented)
```

That returns a text/plain body, not a fixed JSON error shape. The Phase 2 contract is therefore still not pinned.

Required correction:

- Add a shared JSON error shape for unsupported capability responses, or explicitly scope this requirement to HTTP endpoints where JSON is expected.
- Add tests asserting:
  - status code;
  - `Content-Type`;
  - JSON body keys and message/code.
- For WebSocket pre-upgrade unsupported responses, either:
  - return JSON before upgrade and test it; or
  - document why WS pre-upgrade errors intentionally remain text and add JSON tests for REST unsupported endpoints.

### 3. capability-based mobile behavior is still not proven

`capabilities?: string[]` was added to the mobile type, but no mobile behavior uses it yet in this diff. The Phase 2 plan specifically requires capability-based button handling.

Required correction:

- Identify which buttons/actions depend on stream/screen/history/process capabilities.
- Gate those actions through `capabilities`, not adapter name.
- Add safe behavior for unknown/missing capabilities.

### 4. API tests should parse JSON errors and check unmarshal errors consistently

`TestAPIGolden_CapabilityLessSession_NoPanic` calls:

```go
json.Unmarshal(rec.Body.Bytes(), &sessions)
```

without checking the error. This should be tightened while adding unsupported JSON tests.

This is not the main blocker, but it weakens the API boundary test.

## Required next revision

Do not mark Phase 2 complete yet. The next executor revision should be narrow:

1. Remove the mobile `tmux|cmux` regex fallback; use `displayId ?? id`.
2. Pin unsupported capability response JSON shape and status.
3. Use `capabilities` for at least the relevant mobile action/button gating, or clearly document that no current button requires gating yet and add safe missing/unknown capability coverage.
4. Check JSON unmarshal errors in the new API golden tests.
5. Then run:

```sh
GOCACHE=/tmp/devremote-phase2-verifier-go-cache go test ./internal/term ./internal/mux -count=1
GOCACHE=/tmp/devremote-phase2-verifier-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase2-verifier-go-cache go test ./...
GOCACHE=/tmp/devremote-phase2-verifier-go-cache go test -race ./...
```

and from `mobile/`:

```sh
npx tsc --noEmit
```

Phase 2 is closer, but it is not accepted until backend-name regex removal and unsupported JSON response contracts are complete.
