# Adapter Expansion Phase 2 Reverification

Baseline under review: `dc1bb55` (`Phase 2 수정 — 4개 이슈 해결`)

Previous verifier baseline: `0c996a0` (`docs: keep adapter phase 2 blocked after self check`)

Verdict: **REJECTED — PHASE 2 STILL NOT COMPLETE**

The revision fixes several issues from the prior review, but Phase 2 is still not accepted. The remaining blockers are narrower:

1. unsupported JSON response tests are too weak and do not pin the actual JSON shape or content type;
2. mobile `capabilities` are typed but still not used to drive behavior or explicitly documented as not currently actionable.

## Verification performed

- `git fetch origin feature/phase10-multi-adapter`
- `git merge --ff-only origin/feature/phase10-multi-adapter`
- `git diff --check 0c996a0..HEAD`
- Code review of:
  - `companion-daemon/internal/term/api_golden_test.go`
  - `companion-daemon/internal/term/pty.go`
  - `mobile/src/components/AgentCard.tsx`
- Targeted Go test:

```sh
GOCACHE=/tmp/devremote-phase2-fix-go-cache go test ./internal/term ./internal/mux -count=1
```

Initial sandboxed run failed because the sandbox blocked `httptest` listener and Unix socket binding. The same command was rerun outside the sandbox.

Result outside sandbox: **PASS**

- Mobile typecheck:

```sh
npx tsc --noEmit
```

Result: **PASS**

Full `go vet ./...`, `go test ./...`, and `go test -race ./...` were not used as acceptance gates because code review still found Phase 2 contract gaps.

## Improvements confirmed

### 1. mobile backend regex fallback was removed

`AgentCard` no longer strips `tmux|cmux` with a regex. It now renders:

```tsx
session.displayId || session.id
```

This closes the hard-coded backend-name stripping issue from the previous review.

### 2. unsupported WebSocket pre-upgrade path now returns JSON-like body

`HandleWS` now returns:

```json
{"error":"unsupported","detail":"session does not support live streaming"}
```

with HTTP `501` for sessions without `StreamOpener`.

### 3. API test now checks JSON unmarshal error

`TestAPIGolden_CapabilityLessSession_NoPanic` now checks the `json.Unmarshal` error when decoding `/api/sessions`.

## Blocking findings

### 1. unsupported JSON error contract is still not pinned strictly enough

The test only checks:

```go
if !strings.Contains(wsRec.Body.String(), "\"error\"") { ... }
```

This does not pin:

- `Content-Type`;
- JSON validity;
- exact `error` value;
- exact `detail` value;
- whether future regressions return malformed JSON containing the substring `"error"`.

Phase 2 explicitly requires the unsupported response HTTP status and JSON error format to be fixed. Substring search is not sufficient for that contract.

Required correction:

- Decode the response body into a struct or map.
- Assert:
  - status is `501`;
  - `Content-Type` contains `application/json`;
  - `error == "unsupported"`;
  - `detail` is non-empty and stable.

### 2. mobile capability-driven behavior is still not proven

`capabilities?: string[]` exists in the mobile type, but this diff still does not use it anywhere in behavior or button/action gating. The only mobile change is display name rendering.

Phase 2 requires:

> 모바일의 backend 이름 정규식 제거와 capability 기반 버튼 처리를 이 Phase에서 완료한다.

If the current mobile UI has no actionable capability-dependent controls yet, that needs to be made explicit in code/tests/docs. Otherwise, at least the relevant action should be gated by capabilities.

Required correction:

- Either:
  - use `capabilities` to gate the relevant terminal/history/screen action, with safe behavior for missing/unknown capabilities; or
  - add a short Phase 2 note explaining that current mobile card buttons are backend-neutral and no capability-gated control exists yet, then add a typed compatibility fixture/test proving missing/unknown capabilities do not break rendering.

### 3. fallback should prefer nullish coalescing

Current rendering uses:

```tsx
session.displayId || session.id
```

This is usually fine, but it treats an empty string display ID as absent. For API compatibility, `session.displayId ?? session.id` is the more precise contract.

This is not the main blocker, but it should be cleaned up with the next narrow patch.

## Required next revision

Do not mark Phase 2 complete yet. The next executor revision should be narrow:

1. Strengthen unsupported JSON response tests:
   - status;
   - content type;
   - valid JSON;
   - exact `error`;
   - stable `detail`.
2. Resolve the mobile capability behavior requirement:
   - implement capability-based gating where applicable, or
   - document/test that no current mobile action is capability-specific and missing/unknown capabilities are safe.
3. Change display fallback to `session.displayId ?? session.id`.
4. Then run:

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

Phase 2 is close, but it is not accepted until the unsupported JSON contract and mobile capability requirement are pinned.
