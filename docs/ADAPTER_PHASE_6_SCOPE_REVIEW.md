# Adapter Phase 6 Scope Review

Date: 2026-07-07

Executor commit: `fcfebb955`

Verifier decision: **BLOCKED**

Next implementation permission: **BLOCKED**

## Scope reviewed

- `docs/06-phase6-backend-candidates.md`
- `docs/07-phase6-localpty-scope.md`
- Phase 6 section of `docs/ADAPTER_EXPANSION_PLAN.md`

This review only evaluated the Phase 6 scope documents. No runtime
implementation was reviewed.

## Automated verification

```sh
git diff --check
```

Result: PASS

## Blocking findings

### 1. Candidate decision conflicts with the LocalPTY scope document

`docs/06-phase6-backend-candidates.md` concludes:

- line 101: `1순위: zellij`
- line 114: `Phase 6 Step 1 결정: zellij를 세 번째 backend 후보로 선택`

`docs/07-phase6-localpty-scope.md` concludes:

- line 7: `LocalPTYAdapter를 선택한 이유`
- line 21: backend name is `localpty`

These two documents give different Phase 6 targets. This is a hard blocker
because the next implementation step depends on the selected backend.

Expected correction:

- If Phase 6 is LocalPTY, update the candidate document so LocalPTY is the
  selected backend and zellij is a deferred alternative.
- If Phase 6 is zellij, remove or defer the LocalPTY scope document.

Do not proceed to implementation until both documents select the same backend.

### 2. LocalPTY is not evaluated as a first-class candidate in the candidate document

The candidate document groups `Native SSH / PTY` together and gives it a low
score because it lacks standardized discovery. That assessment may apply to
generic SSH or arbitrary host PTY attachment, but it does not match the scoped
LocalPTY model where the app owns only sessions it creates.

`docs/07-phase6-localpty-scope.md` defines a different model:

- app-created sessions only;
- in-memory discovery only;
- create → list → terminate lifecycle;
- no arbitrary OS terminal discovery.

Expected correction:

- Add `LocalPTYAdapter` as its own candidate.
- Score it against the Phase 6 criteria using the create-only/app-owned session
  model.
- Explicitly explain why this model is acceptable even though it is not a
  multiplexer-style discovery backend.

### 3. Scope document contains a stale zellij reference

`docs/07-phase6-localpty-scope.md` line 71 says:

```text
zellij_adapter.go → localpty_adapter.go (신규)
```

This appears to be a stale or copied instruction. LocalPTY should be a new
adapter file, not a rename or transformation from zellij.

Expected correction:

- Replace this with a clean file plan such as `localpty_adapter.go` and
  `localpty_adapter_test.go`.

## Non-blocking observations

The LocalPTY scope itself is mostly coherent for Phase 6 once the document
conflict is fixed:

- feature flag default-off registration is correct;
- app-owned create-only discovery is explicit;
- unsupported `ScreenReader`/`HistoryReader` capabilities are explicit;
- lifecycle and WebSocket E2E steps are the right vertical-slice shape.

## Verdict

Phase 6 scope is not accepted at `fcfebb955`.

The implementation should not start yet. First, make the Phase 6 candidate
document and LocalPTY scope document agree on the selected backend and remove
the stale zellij instruction.
