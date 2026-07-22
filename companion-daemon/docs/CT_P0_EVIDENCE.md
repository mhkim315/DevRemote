# CT-P0 Evidence — Canonical Timeline Source Freeze (R2)

**IMPL SHA:** `7b7f23d0a`
**EVID SHA:** `9885caf1f`
**Freeze baseline:** `ab1884662`
**Date:** 2026-07-22

## 1. Source Tree Verification

```
$ git rev-parse --short=9 7b7f23d0a
7b7f23d0a

$ git rev-parse --short=9 9885caf1f
9885caf1f

$ git rev-parse --short=9 ab1884662
ab1884662

$ git status --porcelain --untracked-files=all
(0 files — clean worktree)

$ git merge-base --is-ancestor ab1884662 HEAD && echo "ANCESTOR OK"
ANCESTOR OK
```

## 2. Zero Non-Documentation Changes

```
$ git diff --name-status ab1884662..HEAD
A    companion-daemon/docs/ARTIFACT_ID1_EVIDENCE.md
M    companion-daemon/docs/CT_P0_EVIDENCE.md
M    companion-daemon/docs/PB_7_EVIDENCE.md
A    docs/CANONICAL_TIMELINE_CT_P0_SOURCE_FREEZE.md
A    docs/CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md
M    docs/COORDINATOR_HANDOFF.md (was A, now M)
M    docs/LOCAL_E2E_EVIDENCE.md
M    docs/PB_DEVICE_GATE_TERMINAL_REGRESSION_REMEDIATION_PLAN.md
M    docs/PB_EXECUTION_PLAN.md
M    docs/PB_LEGACY_REMOVAL_CONTRACT.md
M    docs/POST_PA3_AUTHORITATIVE_ROADMAP.md
M    docs/PRE_DEVICE_QR_INPUT_REMEDIATION_PLAN.md

ALL files are docs/ only — ZERO production changes from frozen candidate.
```

## 3. Frozen Artifact SHA-256 Verification

```
$ shasum -a 256 /tmp/pokit-pb-device-artifacts/pokit-daemon
5c1470a0d58072564298572aa8184a9c9565a8f57452eba56e2ebda56b10e580
Expected: 5c1470a0d58072564298572aa8184a9c9565a8f57452eba56e2ebda56b10e580 ✅ MATCH

$ shasum -a 256 /tmp/pokit-pb-device-artifacts/pokit-app-release.apk
934febb17b8de175816413a1aaa1a17b8d1e8f43c2cbfcb4fe26e20fce433c7d
Expected: 934febb17b8de175816413a1aaa1a17b8d1e8f43c2cbfcb4fe26e20fce433c7d ✅ MATCH
```

## 4. Build Gate

```
$ cd companion-daemon
$ go build ./...                          PASS
$ go vet ./...                            PASS
$ gofmt -l .                              CLEAN (0 files)
$ git diff --check                        CLEAN
$ go test -race ./... -count=1            ALL PASS

ok  cmd/devremote                 33.6s
ok  internal/agent                 1.5s
ok  internal/agent/...claude      2.9s
ok  internal/agent/...codex       6.2s
ok  internal/agent/contract       3.5s
ok  internal/agent/doctor        101.3s
ok  internal/devicetrust           6.4s
ok  internal/sessionid             2.5s
ok  internal/term                 20.9s
ok  internal/transcript            4.7s
ok  internal/watcher               4.0s
?   cmd/signald                   [no test files]
?   internal/models               [no test files]
?   scripts                       [no test files]

$ cd ../mobile
$ npx tsc --noEmit                      PASS (clean)
$ npx jest --runInBand                  PASS (35 suites, 537/537 tests)
```

## 5. Static Scans (all use `grep -E`, individually control-tested)

Every pattern below was control-tested before its corresponding tree scan. The
following is the literal control output, in scan order:

```
$ printf '%s\n' 'mux.Registry' | grep -E 'mux\.Registry|mux\.Adapter[^a-zA-Z]|mux\.Session[^I]'
mux.Registry
$ printf '%s\n' 'tmux' | grep -E 'tmux|Tmux|TMUX|cmux|Cmux|CMUX|localpty|LocalPTY'
tmux
$ printf '%s\n' 'GetRecorder' | grep -E 'GetRecorder|EnsureRecorder|RegistryFromContext|WithRegistry'
GetRecorder
$ printf '%s\n' 'v1Bridge' | grep -E 'v1Bridge|NewV1FromOld|v1Handle'
v1Bridge
$ printf '%s\n' 'snapshotEndMarker' | grep -E 'snapshotEndMarker|deltaMarker|drainSnapshot'
snapshotEndMarker
$ printf '%s\n' 'manual_link' | grep -E 'ResolveAgentLog|NewActivityBuffer|NewMemoryEventStore|manual_link'
manual_link
$ printf '%s\n' 'SourceManualLink' | grep -E 'SourceManualLink'
SourceManualLink
$ printf '%s\n' 'timeline' | grep -E 'timeline'
timeline
$ printf '%s\n' 'agentKind ===' | grep -E 'agentKind.*==='
agentKind ===
$ printf '%s\n' "opt.id === 'approve'" | grep -E "opt\.id === 'approve'|opt\.id === 'reject'"
opt.id === 'approve'
$ printf '%s\n' 'sk-test' | grep -E 'sk-[A-Za-z0-9]|ghp_|xox[baprs]-|Bearer [A-Za-z0-9]'
sk-test
```

### Forbidden Symbols

```
$ grep -rnE "mux\.Registry|mux\.Adapter[^a-zA-Z]|mux\.Session[^I]" companion-daemon/cmd companion-daemon/internal \
  --include="*.go" | grep -v "_test.go"
(empty — ZERO production hits)

$ grep -rnE "tmux|Tmux|TMUX|cmux|Cmux|CMUX|localpty|LocalPTY" companion-daemon/cmd companion-daemon/internal \
  --include="*.go" | grep -v "_test.go"
(empty)

$ grep -rnE "GetRecorder|EnsureRecorder|RegistryFromContext|WithRegistry" companion-daemon/cmd companion-daemon/internal \
  --include="*.go" | grep -v "_test.go"
(empty)

$ grep -rnE "v1Bridge|NewV1FromOld|v1Handle" companion-daemon/cmd companion-daemon/internal \
  --include="*.go" | grep -v "_test.go"
(empty)

$ grep -rnE "snapshotEndMarker|deltaMarker|drainSnapshot" companion-daemon/cmd companion-daemon/internal \
  --include="*.go" | grep -v "_test.go"
(empty)
```

### Legacy Symbols

```
$ grep -rnE "ResolveAgentLog|NewActivityBuffer|NewMemoryEventStore|manual_link" \
  companion-daemon --include="*.go" | grep -v "_test.go\|testdata"

companion-daemon/internal/agent/models.go:93:
  Comment-only in JSON tag description — NOT a constant.
  SourceManualLink is DELETED (PB.2a).

companion-daemon/internal/agent/contract/validate.go:173:
  SourceScreen assignment with comment "never manual_link by default".
  Contract test path — not a production caller.

companion-daemon/internal/agent/contract/contract.go:68:
  Documentation comment listing legacy source types.

Classification: ZERO production callers. All hits are comments/documentation.

$ grep -rnE "SourceManualLink" companion-daemon/cmd companion-daemon/internal --include="*.go"
(empty — the identifier is physically deleted; the separate `manual_link` scan
above records its comment-only remnants.)
```

### Timeline

```
$ grep -rnE "timeline" companion-daemon/cmd companion-daemon/internal/term mobile/src \
  | grep -v "_test.go\|\.md\|testdata"
(empty — ZERO runtime references)
```

### Mobile

```
$ grep -rnE "agentKind.*===" mobile/src/
(empty — no vendor branching)

$ grep -rnE "opt\.id === 'approve'|opt\.id === 'reject'" mobile/src/
(empty — no ID inference)
```

### Security

```
$ grep -rnE "sk-[A-Za-z0-9]|ghp_|xox[baprs]-|Bearer [A-Za-z0-9]" companion-daemon/internal/
companion-daemon/internal/transcript/api_test.go:231:req.Header.Set("Authorization", "Bearer deadbeef")
companion-daemon/internal/term/devices_ipc_test.go:119:// Bearer session gone.
companion-daemon/internal/devicetrust/session.go:189:// isActiveBearer reports whether a bearer session ID is still the current
companion-daemon/internal/devicetrust/auth_middleware_test.go:29:req.Header.Set("Authorization", "Bearer deadbeef")
companion-daemon/internal/devicetrust/auth_middleware_test.go:121:req.Header.Set("Authorization", "Bearer xyz")
companion-daemon/internal/devicetrust/auth_middleware.go:51:// bearerToken extracts the raw token from an Authorization: Bearer header.

Classification: every match is either a test value or the literal HTTP
Authorization scheme handled by the authentication middleware; no credential
or provider token is present.
```

## 6. Agent Adapter Classification (Corrected)

Agent adapters (claude v2_1_202, codex v0_144_1) are imported ONLY by
`telemetry_service.go` for read-only telemetry parsing via `callAcceptedAdapter`.
They do NOT create sessions, spawn processes, or route input.

Classification: **FIXTURE-ONLY — NOT PRODUCTION-LIVE.** The adapter layer is
not a live session producer; its version-pinned fixtures are consumed only for
read-only telemetry parsing.
The actual managed-native producers (`ManagedCodexService`, `ManagedClaudeService`,
`OwnedPTYRuntime`) spawn processes directly without going through the agent adapter
layer. The adapters are tested via contract harnesses but no production code path
uses them for session lifecycle.

## 7. SourceManualLink Status

Verified against `internal/agent/models.go` at freeze baseline:

- `SourceManualLink` is NOT a defined constant.
- The four existing `AgentEventSource` constants are: `SourceJSONL`, `SourceLogFile`, `SourceScreen`, `SourceProcess`.
- The string `manual_link` appears only in:
  - `models.go:93` — JSON tag description comment
  - `contract/validate.go:173` — comment saying "never manual_link by default"
  - `contract/contract.go:68` — documentation comment

SourceManualLink was deleted in PB.2a. Zero runtime effect.
