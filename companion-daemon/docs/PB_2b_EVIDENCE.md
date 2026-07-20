# PB.2b Evidence — Discovery, Resolver, and External Observer Removal

**PB.2b IMPL SHA:** `3d0ea0aa7` (R2: collectProcessSnapshots deleted, comments cleaned)
**PA4 ACCEPT SHA:** `74560edd`

## Consumer Inventory

| Component | Status | Files |
|-----------|--------|-------|
| GeminiResolver | DELETED | gemini_resolver.go |
| CodexResolver | DELETED | codex_resolver.go |
| ClaudeResolver | DELETED | claude_resolver.go |
| ResolveAgentLog + findAgentProcess | DELETED | tracker.go |
| Antigravity detector+resolver | DELETED | antigravity_adapter.go |
| TermAgentDetector (bridge) | DELETED | bridge.go |
| TelemetryService legacy observer | GUTTED | telemetry_service.go |
| buildSimpleSnapshotWithDetector | DELETED | telemetry.go |
| AgentDetector wiring | REMOVED | app.go, runtime.go |

## Retained Managed Codex/Claude Evidence

| Component | Status | Justification |
|-----------|--------|---------------|
| ClaudeParser | RETAINED | Provider-native JSONL parsing |
| CodexParser | RETAINED | Provider-native JSONL parsing |
| ingestApprovals | RETAINED | Managed approval ingestion |
| Claude.Adapter (contract) | RETAINED | Accepted adapter interface |
| Codex.Adapter (contract) | RETAINED | Accepted adapter interface |
| TerminalTransport | RETAINED | Managed transport boundary |

## Zero Consumers Proof

### Control
```
$ printf "ResolveAgentLog" | grep -E "ResolveAgentLog|GeminiResolver|CodexResolver|ClaudeResolver|TermAgentDetector"
ResolveAgentLog
(exit 0 — pattern matches)
```

### Production
```
$ grep -rnE "ResolveAgentLog|GeminiResolver|CodexResolver|ClaudeResolver|TermAgentDetector" --include='*.go' . | grep -v "_test.go"
(empty — zero)
```

### Tests
```
$ grep -rnE "ResolveAgentLog|GeminiResolver|CodexResolver|ClaudeResolver|TermAgentDetector" --include='*_test.go' .
(empty — zero)
```

### Mobile
```
$ grep -rnE "ResolveAgentLog|GeminiResolver|CodexResolver|TermAgentDetector" ../mobile/src/
(empty — zero)
```

### Scripts
```
$ grep -rnE "ResolveAgentLog|GeminiResolver|CodexResolver|TermAgentDetector" ../scripts/ scripts/
(empty — zero)
```

## Gates
```
go build ./...                     exit 0
go vet ./...                       exit 0
gofmt -d .                         clean
go test -race ./... -count=1       ALL PASS (12 packages)
go test -race -run "TestPA4_" -count=20  PASS
npx tsc --noEmit                    clean
npx jest                            451/451 pass, 34 suites
```
