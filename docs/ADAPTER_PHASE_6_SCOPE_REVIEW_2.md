# Adapter Phase 6 Scope Review 2

Date: 2026-07-07

Executor commit: `1815d6fd5`

Verifier decision: **BLOCKED**

Next implementation permission: **BLOCKED**

## Scope reviewed

- `docs/06-phase6-backend-candidates.md`
- `docs/07-phase6-localpty-scope.md`
- Prior review: `docs/ADAPTER_PHASE_6_SCOPE_REVIEW.md`

This review only evaluated Phase 6 scope documents. No runtime implementation
was reviewed.

## Automated verification

```sh
git diff --check
```

Result: PASS

## Progress since previous review

Accepted improvements:

- `docs/07-phase6-localpty-scope.md` now removes the stale
  `zellij_adapter.go -> localpty_adapter.go` instruction.
- `docs/06-phase6-backend-candidates.md` now adds `LocalPTY` as a dedicated
  candidate.
- The final recommendation now says `Phase 6: LocalPTYAdapter`.

These changes move the scope in the right direction, but the candidate document
still contains conflicting selection signals.

## Blocking findings

### 1. Candidate document still presents zellij as the top candidate

`docs/06-phase6-backend-candidates.md` still begins the candidate list with:

- `### 1. zellij`
- `평가: ★★★★★ 최상위 후보`

Later in the same document, zellij appears again as:

- `### 8. zellij (deferred alternative)`
- `LocalPTYAdapter 이후 Phase 7+에서 검토`

This creates an internal contradiction. The same backend is both the first
five-star top candidate and a deferred alternative. A future implementation
agent could reasonably read the top half and start zellij work even though the
bottom recommendation says LocalPTY.

Expected correction:

- Make LocalPTY the only Phase 6 top candidate.
- Move the original zellij entry to the deferred section, or rewrite it so it
  is clearly not the Phase 6 selection.
- Remove the duplicate zellij candidate entry.

### 2. Candidate numbering is inconsistent

`docs/06-phase6-backend-candidates.md` now has two `### 7` sections:

- `### 7. Native SSH / PTY`
- `### 7. LocalPTY (child process PTY)`

This is not just cosmetic because the document is being used as the execution
source of truth for Phase 6. The ordering should clearly communicate the
selected backend and deferred alternatives.

Expected correction:

- Renumber the candidate/deferred sections consistently.
- Prefer a structure such as:
  - `Selected Phase 6 backend: LocalPTYAdapter`
  - `Deferred alternatives: zellij, wezterm, kitty, screen, iTerm2, SSH`

## Non-blocking observations

The LocalPTY scope document itself is now coherent:

- backend name is `localpty`;
- app-owned create-only discovery is explicit;
- feature flag default-off registration is explicit;
- unsupported screen/history capabilities are explicit;
- create/list/terminate and WebSocket live I/O E2E steps are specified.

Once the candidate document is made internally consistent, the LocalPTY scope
can be accepted.

## Verdict

Phase 6 scope is not accepted at `1815d6fd5`.

Do not start implementation yet. Clean up the candidate document so it has one
unambiguous Phase 6 selection: `LocalPTYAdapter`.
