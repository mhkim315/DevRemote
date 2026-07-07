# Phase A5 — Claude Agent Adapter Vertical Slice

Date: 2026-07-07
Baseline: Phase A4 accepted

## Goal

Claude agent adapter를 end-to-end vertical slice로 구현하여 Agent Adapter pipeline을 검증.
Claude 완성도가 목적이 아니라 A2-A4 infrastructure가 실제 agent 데이터로 동작함을 증명.

## Files

| File | Purpose |
|------|---------|
| `internal/agent/claude_adapter.go` | Claude detector + log resolver + parser |
| `internal/agent/claude_adapter_test.go` | Contract harness tests + E2E integration test |
| (no app.go changes — Agent adapter layer is separate from Terminal adapter registration) | |

## Design

### ClaudeParser → implements AgentParser
- Input: Claude JSONL lines (`~/.claude/projects/<project>/<uuid>.jsonl` per A1)
- Maps: `type=user` → user_message, `type=assistant` → (thinking|tool_use|text)
- `type=permission-mode` → approval_requested

### ClaudeDetector → implements AgentDetector
- process name = "claude" → high confidence (0.7)
- CWD contains `.claude/` → boosted confidence

### ClaudeLogResolver → implements LogResolver
- References A1-confirmed paths: `~/.claude/projects/*/<uuid>.jsonl`, `~/.claude/history.jsonl`
- DisplayPath with `<PROJECT>` / `<HOME>` redaction

## Vertical Slice Proof

`TestClaudeAdapter_E2E_Pipeline` in claude_adapter_test.go:
1. ClaudeParser.ParseBatch(A1 fixtures) → ParseResult with correct Event types + Status + Approvals
2. ClaudeDetector.Detect(claude process evidence) → AgentIdentity{Kind:"claude", Confidence≥0.7}
3. Full pipeline: detect → parse → status inference → approval detection
4. Output matches A1 metadata expectedEvents

## Acceptance

- Claude A1 fixtures pass parser contract (12/12)
- Claude detector passes detector contract (10/10)
- E2E pipeline test: detect→parse→status→approval chain verified
- go vet, go test, go test -race, gofmt clean
