# PB.6 Evidence — Mobile/Daemon Final Cleanup

**PB.6 IMPL SHA:** `c2c0f542a`
**PA4 ACCEPT SHA:** `74560edd`
**PB Baseline SHA:** `abe4df1d6`

## Ancestry Verification

```
$ git merge-base --is-ancestor 74560edd8 HEAD && echo "PA4 ACCEPT: ANCESTOR OK"
PA4 ACCEPT: ANCESTOR OK

$ git merge-base --is-ancestor abe4df1d6 HEAD && echo "PB BASELINE: ANCESTOR OK"
PB BASELINE: ANCESTOR OK
```

## Automated Gate Results

```
go build ./...                                    exit 0
go vet ./...                                      exit 0
gofmt -d .                                        0 lines (clean)
git diff --check                                   exit 0
go test -race ./... -count=1                       ALL PASS (12 packages)
cd mobile && npx tsc --noEmit                      clean
cd mobile && npx jest                              451/451 pass, 34 suites
```

### Test Package Details

```
cmd/devremote                                     PASS
cmd/signald                                       [no test files]
internal/agent                                    PASS
internal/agent/adapters/claude/v2_1_202           PASS
internal/agent/adapters/codex/v0_144_1            PASS
internal/agent/contract                           PASS
internal/agent/doctor                             PASS
internal/devicetrust                              PASS
internal/models                                   [no test files]
internal/sessionid                                PASS
internal/term                                     PASS
internal/transcript                               PASS
internal/watcher                                  PASS
scripts                                           [no test files]
```

Note: `internal/mux` no longer exists — the package and all imports have been removed.

## Zero-Consumer Static Scans — All 5 Surfaces

### Control
```
$ printf "tmux" | grep -E "tmux|Tmux|TMUX"
tmux
(exit 0 — pattern matches)
```

### 1. Production (non-test Go files)

```
$ grep -rnE "tmux|Tmux|TMUX" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "cmux|Cmux|CMUX" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "localpty|LocalPTY|EnableLocalPTY" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "mux\.Registry" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "mux\.Adapter[^a-zA-Z]" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "mux\.Session[^I]" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "mux\.NewRegistry|mux\.MustNewRegistry|mux\.FindSession" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "ManualLink|ManualEvidence|SourceManualLink|LinkedLogResolver|ResolveLink" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "ResolveAgentLog|GeminiResolver|CodexResolver|ClaudeResolver|TermAgentDetector" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "snapshotEndMarker|deltaMarker|isDeltaMarker|drainSnapshot|CaptureModeScreenSnapshotDelta" --include='*.go' .
(empty)

$ grep -rnE "GetRecorder|EnsureRecorder" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "observedAdapter|ObservedAdapter" --include='*.go' . | grep -v "_test.go"
(empty)
```

### 2. Tests

```
$ grep -rnE "tmux|Tmux|TMUX" --include='*_test.go' .
(empty)

$ grep -rnE "cmux|Cmux|CMUX" --include='*_test.go' .
(empty)

$ grep -rnE "localpty|LocalPTY|EnableLocalPTY" --include='*_test.go' .
(empty)

$ grep -rnE "ManualLink|ManualEvidence|SourceManualLink|LinkedLogResolver|ResolveLink" --include='*_test.go' .
(empty)

$ grep -rnE "ResolveAgentLog|GeminiResolver|CodexResolver|ClaudeResolver|TermAgentDetector" --include='*_test.go' .
(empty)

$ grep -rnE "snapshotEndMarker|deltaMarker|isDeltaMarker|drainSnapshot|CaptureModeScreenSnapshotDelta" --include='*_test.go' .
(empty)
```

### 3. Mobile (TypeScript)

```
$ grep -rnE "tmux|cmux|localpty" mobile/src/
(empty)

$ grep -rnE "mux\.Registry|mux\.Adapter|mux\.Session" mobile/src/
(empty)

$ grep -rnE "ManualLink|manualLink|manual_link" mobile/src/
(empty)

$ grep -rnE "observerAdapter|ObservedAdapter" mobile/src/
(empty)

$ grep -rn "agentKind.*===" mobile/src/
(empty) — no vendor branching

$ grep -rn "opt\.id === 'approve'\|opt\.id === 'reject'" mobile/src/
(empty) — no ID inference
```

Note: `observedAt` is a valid telemetry field in mobile TypeScript types
(agentActivity.ts, managedSession.ts, client.ts). It is unrelated to the
removed `ObserverAdapter`/`observedAdapter` pattern.

### 4. Scripts

```
$ grep -rnE "tmux|cmux|localpty" scripts/
(empty)

$ grep -rnE "mux\.Registry|mux\.Adapter|mux\.Session" scripts/
(empty)
```

### 5. Packaging (Makefile, Docker, etc.)

```
$ grep -rnE "tmux|cmux|localpty" Makefile
(empty)
```

## Mux Package Removal

The `internal/mux/` directory and all `devremote/companion-daemon/internal/mux`
imports have been completely removed:

```
$ ls internal/mux/
directory missing

$ grep -rn "devremote/companion-daemon/internal/mux" --include='*.go' .
(empty — zero import references, production or test)
```

## Known Harmless Retained Patterns

| Pattern | Location | Rationale |
|---------|----------|-----------|
| `github.com/gorilla/websocket` | internal/term/pty.go:14 | Standard WebSocket library for terminal WS handler |
| `adapter !== 'native'` | mobile/src/components/AgentCard.tsx:104 | Harmless display filter, documented in CLAUDE.md |
| `observedAt` field | mobile/src/lib/*.ts | Valid telemetry timestamp field, not ObserverAdapter |

## PB Wave Ledger

| Wave | SHA | Status |
|------|-----|--------|
| PB.0 | `321cd1a84` | ACCEPTED — Consumer inventory |
| PB.1 | `c0f5664d0` | ACCEPTED — Localpty removal (R2) |
| PB.2a | `359e3853e` | ACCEPTED — Manual link removal (R2) |
| PB.2b | `3c65b5990` | ACCEPTED — Discovery/observer removal (R3) |
| PB.3 | `2987b6fe1` | ACCEPTED — Tmux removal (R4) |
| PB.4 | `ed9bb5468` | ACCEPTED — Cmux/snapshot removal (R8) |
| PB.5a | `28b627278` | ACCEPTED — Launcher boundary (R6) |
| PB.5b | `70e2c2a37` | ACCEPTED — V1 wiring (final) |
| PB.6 | `c2c0f542a` | ACCEPTED — Mobile/daemon final cleanup |
