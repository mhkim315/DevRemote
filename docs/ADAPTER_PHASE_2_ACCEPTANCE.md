# Adapter Expansion Phase 2 Acceptance

Accepted baseline: `7a4018b` (`Phase 2 수정 (2차)`)

Verdict: **ACCEPTED**

Phase 2 is accepted. The capability model cleanup is now sufficiently pinned at the backend API boundary and mobile rendering boundary to proceed to the next planned phase.

## What was verified

The final revision closed the remaining Phase 2 blockers:

- mobile no longer strips backend names with a `tmux|cmux` regex;
- mobile display fallback uses `displayId ?? id`;
- mobile consumes `capabilities` in a backend-neutral capability indicator;
- unsupported live-stream responses return HTTP `501` with JSON;
- unsupported response tests assert status, `Content-Type`, valid JSON, `error == "unsupported"`, and non-empty `detail`;
- API golden tests pin additive `displayId` and `capabilities`;
- capability-less sessions are covered without panic or silent fallback;
- old payloads without `displayId` and `capabilities` still deserialize;
- duplicate live-stream-adjacent interfaces were reduced to the canonical `StreamOpener` / `TerminalStream` path.

## Verification commands

### Targeted mux/term verification

Initial sandboxed run failed because the sandbox blocked `httptest` listener creation. The same command was rerun outside the sandbox.

```sh
GOCACHE=/tmp/devremote-phase2-reverify2-go-cache go test ./internal/term ./internal/mux -count=1
```

Result outside sandbox: **PASS**

### Full Go verification

Initial sandboxed run failed because the sandbox blocked local TCP listener and Unix socket binding. The same command was rerun outside the sandbox.

```sh
GOCACHE=/tmp/devremote-phase2-reverify2-go-cache go vet ./...
GOCACHE=/tmp/devremote-phase2-reverify2-go-cache go test ./...
GOCACHE=/tmp/devremote-phase2-reverify2-go-cache go test -race ./...
```

Result outside sandbox: **PASS**

### Mobile typecheck

```sh
npx tsc --noEmit
```

Result: **PASS**

## Notes for Phase 3

Phase 2 acceptance does not remove all adapter-specific implementation details from the repository. Some are intentionally backend implementations or are explicitly deferred:

- `gemini_resolver.go` still shells out to `tmux`, documented as Phase 3 deferred adapter-specific command execution.
- tmux/cmux adapter implementation files and adapter-specific tests naturally retain backend names.
- Phase 3 should focus on runner injection, binary discovery, adapter lifecycle, environment/socket/timeout policy, and replacing remaining cross-component adapter-specific command execution where appropriate.

Continue applying the same acceptance standard: behavior must be proven at production boundaries, not only helper-level tests.
