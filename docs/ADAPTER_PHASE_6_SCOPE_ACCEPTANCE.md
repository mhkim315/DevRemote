# Adapter Phase 6 Scope Acceptance

Date: 2026-07-07

Executor commit: `38e81abb6`

Verifier decision: **ACCEPT**

Next implementation permission: **ALLOWED**

## Scope reviewed

- `docs/06-phase6-backend-candidates.md`
- `docs/07-phase6-localpty-scope.md`
- Prior reviews:
  - `docs/ADAPTER_PHASE_6_SCOPE_REVIEW.md`
  - `docs/ADAPTER_PHASE_6_SCOPE_REVIEW_2.md`

This review accepted the Phase 6 scope only. No runtime implementation was
reviewed.

## Automated verification

```sh
git diff --check
```

Result: PASS

## Acceptance findings

### 1. Phase 6 now has one unambiguous backend selection

Accepted.

`docs/06-phase6-backend-candidates.md` now states the selected Phase 6 backend
at the top:

```text
Phase 6: LocalPTYAdapter
```

The same document now separates:

- `Selected: LocalPTY (child process PTY)`
- `Evaluated Alternatives (Phase 7+ deferred)`

zellij is no longer both the top Phase 6 candidate and a deferred alternative.
It appears only in the deferred alternatives section.

### 2. Candidate document and LocalPTY scope now agree

Accepted.

Both documents now identify LocalPTY as the Phase 6 target:

- backend name: `localpty`
- model: app-owned child process PTY sessions
- discovery: create-only, in-memory active sessions
- I/O: PTY master read/write
- registration: feature flag default-off

This removes the previous execution ambiguity.

### 3. LocalPTY is evaluated as its own backend model

Accepted.

The candidate document now treats LocalPTY separately from generic
`Native SSH / PTY`. This matters because LocalPTY is not trying to discover
arbitrary OS terminals. It defines a narrower and testable ownership model:

- DevRemote creates the child PTY;
- DevRemote owns the session lifecycle;
- `ListSessions` returns only active in-memory LocalPTY sessions.

That model is acceptable for Phase 6 because the goal is to validate a real
third adapter path through API/WebSocket/telemetry while staying independent
from tmux/cmux production code.

### 4. Original screen/history read-only ordering is explicitly narrowed by backend capability

Accepted with implementation condition.

`docs/ADAPTER_EXPANSION_PLAN.md` originally listed the Phase 6 order as:

```text
discovery + screen/history read-only slice
```

The accepted LocalPTY scope intentionally does not support `ScreenReader` or
`HistoryReader` in the initial slice. That is coherent with the adapter model
only if the implementation proves the unsupported paths remain safe.

Implementation must therefore verify:

- LocalPTY sessions advertise no screen/history capability;
- history/screen requests do not panic, silently fallback, or return fake
  success;
- API/UI behavior follows the Phase 2 capability-driven model.

This is not a blocker for scope acceptance because the LocalPTY document
states the unsupported capabilities clearly.

## Required implementation gates for the next commit

The next Phase 6 implementation commit should not be accepted unless it proves:

1. `localpty` is behind a default-off feature flag.
2. Existing tmux/cmux production files are not modified.
3. LocalPTY failure does not block registry discovery for other adapters.
4. Create → list/discover → WebSocket live output/input → terminate is covered
   by tests or a clearly documented smoke test.
5. Unsupported screen/history behavior is tested.
6. Common contract suite coverage is added where applicable.
7. `go vet ./...`, `go test ./...`, `go test -race ./...`, and mobile
   TypeScript compile pass.

## Verdict

Phase 6 scope is **accepted** at `38e81abb6`.

Implementation may begin for `LocalPTYAdapter` under the accepted scope.
