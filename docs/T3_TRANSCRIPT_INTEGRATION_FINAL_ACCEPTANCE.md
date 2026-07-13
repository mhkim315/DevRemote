# T3 Transcript Integration — Final Acceptance

Status: **ACCEPT**

Accepted implementation tip:

```text
9ad6f834f70e88b800e60124c8e408d38bca9d2b
```

Branch: `feature/phase10-multi-adapter`

## Accepted production contract

T3 now provides an executable, authenticated Transcript path:

```text
accepted, correctly correlated AgentEvent
  -> semantic projector
  -> bounded session store
  -> authenticated session-scoped API
  -> strict mobile DTO validation
  -> FeedScreen -> TranscriptRenderer -> transcriptClassify

Recorder copied output / cmux snapshot
  -> explicitly separate degraded fallback channel
```

The accepted boundary preserves these invariants:

- Recorder remains the only PTY reader and its subscriber stream remains raw PTY
  bytes only;
- AgentEvent semantics and terminal fallback are not heuristically merged;
- semantic authority requires an accepted Codex/Claude adapter, exact version
  authority, session binding, and accepted correlation;
- input content is never stored, and byte-stream/snapshot projection is suppressed
  after an explicit input boundary so PTY echo cannot re-enter Transcript;
- projection queues, adapter reads, cursors, DTOs, metadata, and stored segments are
  bounded and fail closed;
- Transcript does not become lifecycle, approval-action, or process authority;
- the API is protected by device-principal `sessions:read` authorization in remote
  mode and the mobile client keeps semantic and fallback channels separate;
- the tested renderer is the renderer imported by the production `FeedScreen`.

## Independent verification

The final verification checked the complete remediation ancestry through
`9ad6f834f70e88b800e60124c8e408d38bca9d2b`, including the last production
renderer wiring fix. Reproduced evidence:

```text
focused transcript + term Go race tests      PASS
focused mobile Transcript Jest tests         PASS (36)
mobile TypeScript                            PASS
go build / vet / test -race                  PASS
Android Kotlin module compile                PASS
invariant and secret scans                   PASS
scripts/build-gate.sh                        ALL GATES PASSED
local HEAD == origin branch tip              PASS
worktree clean                               PASS
```

The final blocker was closed by removing the private `FeedScreen` renderer copy
and importing `mobile/src/components/TranscriptRenderer.tsx`, whose classification
logic is covered by the production-path tests.

## Deferred work

T3 does not implement:

- S1 rich runtime status;
- A1 approval action/authorization UX;
- O1 orchestration;
- another provider adapter;
- Windows runtime support;
- physical-device Transcript smoke beyond the automated product-path evidence.

Proceed to S1 only through:

```text
docs/NEXT_SESSION_S1_RUNTIME_STATUS_HANDOFF.md
```

