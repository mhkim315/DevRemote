# A1.2 C3D-C — Live Proof Evidence Report

Status: **COMPLETE — LIVE PROOF PASSED**

Implementation SHA: `995fbc5b680d3fc3cd4bcbd39c0ecb4e4b509db2`
Identity amendment: `docs/A1_2_C3D_C_R4_IDENTITY_AMENDMENT.md` (R2, `ce2dc71`)
Authority: `docs/A1_2_C3D_ACTIVATION_CONTRACT_NOTE.md` §8 (accepted R1 `98ca6d9`)

## 1. Pinned artifact identity

- PinnedPath: `~/.local/share/claude/versions/2.1.209`
- `--version`: `2.1.209 (Claude Code)`
- SHA-256: `59d2de7f49db2f75d5c33bbb46a6b8f288ad24d40b61e30602a502bb7ddc380c`

## 2. Live evidence (accepted §8)

```
── live allow ──
APPROVAL ACTION: allow_once kind=approve outcome=accepted http=200
composed route 200, committed state=approved
tool_result probe-token hits = 1 (corroboration)

── live deny ──
APPROVAL ACTION: deny kind=reject outcome=accepted http=200
composed route 200, committed state=rejected
tool_result probe-token hits = 0 (corroboration)

PASS (29.02s)
```

## 3. Production changes

| # | Change | Purpose |
|---|---|---|
| 1 | `claudeCertificationPrompt` → catalog probe | Live model turn elicits exact catalog action |
| 2 | `bridge.rt` mutex-guarded publication | Race: handler reads rt before CreateDetached finishes |
| 3 | `handleResume` real body compare+bind | Real Claude `--resume` generates new tool_use_id |
| 4 | `handlePostTool` real body witness | PostToolUse identity must match bound attempt |
| 5 | `BindResumeProcess` coordinator pre-binding | Independent launch generation validation |
| 6 | `ResumeAttemptIdentity` + `stateWitnessPending` | One-time bind + early-witness race handling |
| 7 | `bridge.close()` graceful Shutdown | PostToolUse EOF race (server.Close killed in-flight handler) |
| 8 | `WitnessOutcome` closed vocabulary | Pending/Witnessed/Mismatch/Duplicate/Stale |
| 9 | `processLine` denial skip for allow | Empty `permission_denials:[]` not a denial witness |
| 10 | `postToolUseAllowlist` (+`tool_response`,`duration_ms`) | PostToolUse body has different fields than PreToolUse |
| 11 | `drainWaiter` injectable + natural-exit drain | `defer rt.terminate()` killed process before stdout fully drained |

## 4. Deterministic coverage

- 22 coordinator adversarial tests (`TestR4_*`)
- 3 bridge HTTP tests (`TestR4Bridge_*`)
- 1 graceful shutdown test
- 5 hook body boundary tests (`TestR5_*`)
- 4 drain lifecycle tests (`TestDrain_*`) — channel barriers, no real timers
- All ×5 `-race` PASS

## 5. Final repository gate

| Gate | Result |
|---|---|
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `gofmt -d` | clean |
| `go test -race ./...` | **0 failures** |
| `tsc --noEmit` | PASS |
| `jest --runInBand` | 34 suites / 447 tests PASS |
| `git diff --check` | PASS |
| Canonical secret scan | PASS |
| Ancestry (all prerequisite SHAs) | PASS |
| Android Kotlin gate | PASS (`:pokit-device-key:compileReleaseKotlin`, BUILD SUCCESSFUL) |
| Local == Remote | `995fbc5b680d3fc3cd4bcbd39c0ecb4e4b509db2` |
| Worktree | clean |

## 6. Stop condition

Push exact frozen HEAD and stop for independent FINAL A1.2 review.
