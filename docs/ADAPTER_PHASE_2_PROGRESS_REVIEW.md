# Adapter Expansion Phase 2 Progress Review

Baseline under review: `e0c8b5b` (`Phase 2 진행 중`)

Previous accepted baseline: `056189a` (`docs: accept adapter expansion phase 1`)

Verdict: **IN PROGRESS — NOT ACCEPTED YET**

This commit is directionally aligned with Phase 2, but it is not sufficient for Phase 2 acceptance. It starts moving terminal behavior and telemetry toward capability-driven semantics, but several Phase 2 acceptance criteria remain incomplete.

## Verification performed

- `git fetch origin feature/phase10-multi-adapter`
- `git merge --ff-only origin/feature/phase10-multi-adapter`
- `git diff --check 056189a..HEAD`
- Code review of:
  - `companion-daemon/internal/term/pty.go`
  - `companion-daemon/internal/term/telemetry.go`
  - `companion-daemon/internal/term/telemetry_service.go`
  - `mobile/src/components/AgentCard.tsx`
  - `docs/ADAPTER_EXPANSION_PLAN.md` Phase 2 requirements
- Targeted term test:

```sh
GOCACHE=/tmp/devremote-phase2-progress-go-cache go test ./internal/term \
  -run "TestAPIGolden|TestHandleWS|TestTelemetry|Test.*Capability|Test.*Unsupported" \
  -count=1
```

Initial sandboxed run failed because the sandbox blocked `httptest` listener creation. The same command was rerun outside the sandbox.

Result outside sandbox: **PASS**

- Mobile typecheck:

```sh
npx tsc --noEmit
```

Result: **PASS**

Full `go vet ./...`, `go test ./...`, and `go test -race ./...` were not used as acceptance gates because this is explicitly a progress review and Phase 2 contract coverage is still incomplete.

## Improvements confirmed

### 1. initial screen selection is now capability-based

`HandleWS` no longer gates initial screen replay on `s.AdapterName() == "tmux"`:

```go
if sr, ok := s.(mux.ScreenReader); ok {
    ...
}
```

This is aligned with the Phase 2 requirement to select initial screen behavior by `ScreenReader` capability instead of backend name.

### 2. `/api/sessions` telemetry gained additive metadata

`SessionTelemetry` now includes:

- `displayId`
- `capabilities`

Snapshots now populate:

- canonical `id`
- local `displayId`
- adapter name
- computed capabilities based on implemented optional interfaces

This is aligned with the Phase 2 plan to add additive metadata while retaining existing fields.

### 3. telemetry capability detection is interface-based

`sessionCapabilities` detects optional behavior through interfaces:

- `mux.StreamOpener` -> `live_stream`
- `mux.ScreenReader` -> `screen`
- `mux.HistoryReader` -> `history`
- `mux.ProcessProvider` -> `process`

This is directionally correct for capability-driven UI and backend-neutral behavior.

## Blocking gaps before Phase 2 acceptance

### 1. mobile still strips backend names with a hard-coded regex

`mobile/src/components/AgentCard.tsx` still renders the session name with:

```tsx
session.id.replace(/^(tmux|cmux):/, '')
```

This directly violates the Phase 2 acceptance criterion:

> 모바일의 backend 이름 정규식 제거와 capability 기반 버튼 처리를 이 Phase에서 완료한다.

Required correction:

- Add `displayId?: string` and `capabilities?: string[]` to the mobile telemetry type.
- Render `session.displayId ?? session.id` instead of stripping `tmux|cmux`.
- Ensure unknown adapters display safely.
- Add or update mobile tests/type coverage for:
  - new payload with `displayId`;
  - old payload without `displayId`;
  - unknown adapter name;
  - unknown/missing capabilities.

### 2. API golden tests do not yet pin the new additive fields

The backend adds `displayId` and `capabilities`, but the API golden tests still only require the older keys:

```go
requiredKeys := []string{`"id"`, `"state"`, `"load"`, `"runner"`, `"runnerColor"`, `"adapter"`, `"events"`}
```

Required correction:

- Extend API boundary tests to assert `displayId` and `capabilities` for sessions that support known capabilities.
- Add compatibility coverage showing clients can still tolerate old payloads without these fields.
- Add a session with no optional capabilities and assert it does not panic or fabricate unsupported behavior.

### 3. unsupported response status and JSON error format are not fixed yet

Phase 2 explicitly requires:

> unsupported 응답의 HTTP status와 JSON error 형식을 고정한다.

This commit does not add tests or changes that pin unsupported behavior for create/delete/history/resize/live stream capability absence.

Required correction:

- Add handler-level tests for unsupported operations.
- Assert HTTP status code.
- Assert JSON error shape.
- Use capability absence, not adapter name, as the reason for unsupported behavior.

### 4. duplicate capability interfaces are not yet collapsed

Phase 2 includes:

> live stream 관련 중복 interface를 하나의 계약으로 축소한다.

`mux/adapter.go` still contains multiple overlapping terminal capability interfaces:

- `TerminalStream`
- `StreamOpener`
- `OutputStream`
- `InputWriter`
- `Resizer`
- `Closer`

This may be intentionally deferred within Phase 2, but it is not complete at this commit.

Required correction:

- Decide the canonical live-stream contract.
- Remove or deprecate duplicate contracts.
- Update production and tests to rely on the canonical interface.

### 5. create/delete/history/resize are not fully proven capability-based

This commit changed initial screen and telemetry metadata, but Phase 2 also requires:

> create/delete/history/resize가 concrete adapter가 아닌 capability만 검사하게 한다.

The current progress review did not find new tests proving that these paths are backend-neutral and capability-driven.

Required correction:

- Add fixture/fake sessions/adapters with and without each capability.
- Test create/delete/history/resize behavior without relying on adapter names.
- Ensure unsupported capability absence returns the fixed unsupported response.

## Required next revision

Do not mark Phase 2 complete yet. The next executor revision should focus on the Phase 2 boundary requirements:

1. Remove mobile `tmux|cmux` regex stripping and consume `displayId`.
2. Add mobile type compatibility for missing `displayId` and missing/unknown `capabilities`.
3. Extend API golden tests for `displayId` and `capabilities`.
4. Add unsupported response status/JSON tests for missing capabilities.
5. Add a capability-less fixture session/adaptor test showing no panic, no reconnect storm, and no silent fallback.
6. Continue consolidating duplicate live-stream interfaces or explicitly document the remaining migration step inside Phase 2.

Suggested verification before the next review:

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
