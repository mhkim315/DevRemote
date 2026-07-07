# Phase A5 — Claude Agent Adapter Vertical Slice

Date: 2026-07-07
Baseline: Phase A4 accepted

## Product-Boundary Bridge

`SessionTelemetry` (internal/term/telemetry.go)에 3개 agent field 추가:
- `AgentKind` (omitempty) — detected agent: claude, codex, unknown
- `AgentStatus` (omitempty) — agent activity state
- `AgentConfidence` (omitempty) — detection confidence 0.0-1.0

Backward-compatible: agent field가 없는 legacy session은 omitempty로 JSON에서 생략.

## Files Changed

| File | Change |
|------|--------|
| `internal/term/telemetry.go` | +3 agent fields to SessionTelemetry |
| `internal/agent/claude_adapter.go` | ClaudeParser, ClaudeDetector, ClaudeLogResolver |
| `internal/agent/claude_adapter_test.go` | Contract tests + E2E pipeline |
| `internal/term/claude_boundary_test.go` | Product bridge: Claude output → SessionTelemetry JSON |

## Acceptance

- Claude A1 fixtures pass parser contract (12/12)
- Claude detector passes detector contract (10/10)
- E2E pipeline: detect → parse → status → approval (agent package)
- Product bridge: Claude output populates SessionTelemetry JSON with agent fields
- Backward compat: agent fields omitted when absent
- Agent failure isolation: /api/sessions unaffected by missing agent layer
- go vet, go test, go test -race, gofmt clean
