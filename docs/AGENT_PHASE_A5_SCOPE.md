# Phase A5 — Claude Agent Adapter Vertical Slice

Date: 2026-07-07
Baseline: Phase A4 accepted

## Goal

Claude agent adapter의 detect → parse → status → approval pipeline을
agent package와 term boundary에서 검증. Phase A5는 product integration
infrastructure(telemetry wiring, Activity feed)를 구축하는 단계가 아니라,
agent adapter가 product boundary로 흘러갈 수 있는 출력을 생산함을 증명.

Product-boundary wiring (telemetry → agent event, Activity feed 연결,
mobile status badge)은 Phase A8-A9에서 수행한다. Phase A5는 그 wiring이
연결됐을 때 Claude adapter output이 올바른 형태임을 미리 증명.

## Files

| File | Purpose |
|------|---------|
| `internal/agent/claude_adapter.go` | Claude detector + log resolver + parser |
| `internal/agent/claude_adapter_test.go` | Contract tests + agent-level pipeline test |
| `internal/term/claude_boundary_test.go` | Term-boundary proof: adapter output → SessionTelemetry schema compatibility |

## Design

(ClaudeParser, ClaudeDetector, ClaudeLogResolver — unchanged from previous)

## Acceptance

- Claude passes A3 parser contract (12/12)
- Claude passes A4 detector contract (10/10)
- Agent-level pipeline: detect → parse → status → approval (E2E)
- Term-boundary: adapter output compatible with SessionTelemetry JSON schema
- Hermetic: no host filesystem dependencies in unit tests
- go vet, go test, go test -race, gofmt clean
