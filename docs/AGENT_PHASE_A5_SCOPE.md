# Phase A5 — Claude Agent Adapter Vertical Slice

Date: 2026-07-07
Baseline: Phase A4 in review

## Goal

가장 확실한 agent 하나(Claude)를 end-to-end로 구현하여 Agent Adapter pipeline을 검증.
목적은 Claude 완성도가 아니라 A2(A3+A4) infrastructure의 vertical slice proof.

## Files

| File | Purpose |
|------|---------|
| `internal/agent/claude_adapter.go` | Claude detector + log resolver + parser |
| `internal/agent/claude_adapter_test.go` | Contract harness tests |
| `cmd/devremote/app.go` | Production registration (behind feature flag, optional) | |

## Design

### ClaudeParser → implements AgentParser
- Input: Claude JSONL lines (`~/.claude/projects/<project>/<uuid>.jsonl`)
- Maps: `type=user` → user_message, `type=assistant` → (thinking|tool_use|text) → thinking|tool_call_started|assistant_message
- `type=permission-mode` → approval_requested
- `type=attachment` → unknown (informational)
- `type=file-history-snapshot` → unknown (skip)

### ClaudeLogResolver → implements LogResolver
- Given CWD + process name, resolves:
  - `~/.claude/projects/<project>/*.jsonl`
  - `~/.claude/history.jsonl`
- Returns LogRef with DisplayPath (redacted)

### ClaudeDetector → implements AgentDetector
- process name = "claude" → high confidence
- CWD contains `.claude/` → medium confidence
- Manual link overrides all

## Contract Integration

```go
func TestClaudeAdapter_Contract(t *testing.T) {
    RunParserContract(t, "claude", claudeParserFactory)
    RunDetectorContract(t, "claude", claudeDetectorFactory, claudeResolverFactory)
}
```

## Acceptance

- Claude A1 fixtures pass parser contract (expectedEvents match)
- Claude detector passes detector contract (Claude+Codex known, manual, false-positive)
- Claude resolver: DisplayPath non-empty, no raw paths
- 0 production registration changes
- go vet, go test, go test -race, gofmt clean
