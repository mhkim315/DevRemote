# CT-P0 Evidence — Canonical Timeline Source Freeze

**IMPL SHA:** `9688cc687`
**EVID SHA:** (this commit)
**Freeze baseline:** `ab1884662`
**Current HEAD:** `316008304`
**Date:** 2026-07-22

## 1. Source Tree Verification

```
$ git rev-parse HEAD
316008304b74331fed0eb99f70a6768c7e9ea836

$ git status --porcelain --untracked-files=all
(0 files — clean worktree)

$ git merge-base --is-ancestor ab1884662 HEAD && echo "ANCESTOR OK"
ANCESTOR OK
```

## 2. Zero Non-Documentation Changes

```
$ git diff --name-status ab1884662..HEAD
A    companion-daemon/docs/ARTIFACT_ID1_EVIDENCE.md
M    companion-daemon/docs/PB_7_EVIDENCE.md
A    docs/CANONICAL_TIMELINE_CT_P0_SOURCE_FREEZE.md
A    docs/CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md
A    docs/COORDINATOR_HANDOFF.md
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
Expected: 5c1470a0d580... ✅ MATCH

$ shasum -a 256 /tmp/pokit-pb-device-artifacts/pokit-app-release.apk
934febb17b8de175816413a1aaa1a17b8d1e8f43c2cbfcb4fe26e20fce433c7d
Expected: 934febb17b... ✅ MATCH
```

## 4. Build Gate

```
$ cd companion-daemon
$ go build ./...                 PASS
$ go vet ./...                   PASS
$ gofmt -l .                     CLEAN (0 files)
$ git diff --check               CLEAN
$ go test -race ./... -count=1   ALL PASS

ok  cmd/devremote                33.6s
ok  internal/agent                1.5s
ok  internal/agent/...claude     2.9s
ok  internal/agent/...codex      6.2s
ok  internal/agent/contract      3.5s
ok  internal/agent/doctor       101.3s
ok  internal/devicetrust          6.4s
ok  internal/sessionid            2.5s
ok  internal/term                20.9s
ok  internal/transcript           4.7s
ok  internal/watcher              4.0s
?   cmd/signald                  [no test files]
?   internal/models              [no test files]
?   scripts                      [no test files]

$ cd ../mobile
$ npx tsc --noEmit               PASS (clean)
```

## 5. Static Scans

### Timeline

```
$ grep -rn "timeline" companion-daemon/cmd companion-daemon/internal/term mobile/src \
  | grep -v "_test.go|.md|testdata"
(empty — ZERO runtime references)
```

### Legacy Symbols

```
$ grep -rn "ResolveAgentLog|NewActivityBuffer|NewMemoryEventStore|manual_link" companion-daemon \
  --include="*.go" | grep -v "_test.go|testdata"

companion-daemon/internal/agent/models.go:93:
  Source AgentEventSource — "manual_link" enum value only (not a production caller)

companion-daemon/internal/agent/contract/validate.go:173:
  SourceScreen fallback — contract test default, not a production path

companion-daemon/internal/agent/contract/contract.go:68:
  Documentation comment — legacy source classification reference

Classification: SYMBOLS EXIST AS ENUM/CONTRACT DEFINITIONS ONLY.
Zero production callers from term/ or cmd/.
```

### Forbidden Symbols

```
$ grep -rn "mux\.Registry|mux\.Adapter[^a-zA-Z]|mux\.Session[^I]" companion-daemon --include="*.go" | grep -v "_test.go"
(empty)

$ grep -rn "tmux|Tmux|TMUX|cmux|Cmux|CMUX|localpty|LocalPTY" companion-daemon --include="*.go" | grep -v "_test.go"
(empty)

$ grep -rn "GetRecorder|EnsureRecorder|RegistryFromContext|WithRegistry" companion-daemon --include="*.go" | grep -v "_test.go"
(empty)

$ grep -rn "v1Bridge|NewV1FromOld|v1Handle" companion-daemon --include="*.go" | grep -v "_test.go"
(empty)

$ grep -rn "snapshotEndMarker|deltaMarker|drainSnapshot" companion-daemon --include="*.go" | grep -v "_test.go"
(empty)
```

### Mobile

```
$ grep -rn "agentKind.*===" mobile/src/
(empty — no vendor branching)

$ grep -rn "opt\.id === 'approve'|opt\.id === 'reject'" mobile/src/
(empty — no ID inference)
```

### Security

```
Secret scan (excl. testdata/redaction patterns): CLEAN
```

## 6. Agent Adapter Import Map

The ONLY production path from `term/` into `agent/adapters/`:

```
internal/term/telemetry_service.go:
  import claude v2_1_202
  import codex v0_144_1
```

Read-only telemetry consumer. No session creation, process spawning, or
input routing through agent adapters. All managed-native lifecycle is
owned by `ManagedCodexService` / `ManagedClaudeService` in `term/`.

## 7. Managed-Native Producer Summary

| Producer | Sessions | Session Prefix | Live |
|----------|----------|---------------|------|
| ManagedCodexService | Codex app-server | `codex_app_server:` | ✅ |
| ManagedClaudeService | Claude headless | `claude_headless:` | ✅ |
| OwnedPTYRuntime | Shell/custom | `controlled_pty:` | ✅ |
| Legacy mux adapter | — | — | ❌ DELETED |
| Legacy localpty | — | — | ❌ DELETED |
| Legacy tmux/cmux | — | — | ❌ DELETED |
