# Agent Adapter Phase A0 — Scope Verification

Date: 2026-07-07
Baseline: `0e9b8ba2d` (AGENT_ADAPTER_LAYER_PLAN.md + handoff)

## Verdict: SCOPE CONFIRMED — 보완 2건 발견, 구현 변경 없음

Phase A0 합격 기준 충족:
- `Agent Adapter`와 `Terminal Adapter` 책임 경계 명확함 (§3.4)
- Phase A1~A10 acceptance 기준이 구체적으로 정의됨 (§7)
- 실행에이전트가 문서만 보고 다음 작업을 알 수 있음 (handoff 문서)

## 1. 실제 로그 인벤토리 확인

본 컴퓨터(Mac)에서 세 agent 모두 로그 존재 확인:

| Agent | 위치 | 규모 |
|-------|------|------|
| Claude | `~/.claude/history.jsonl` + `~/.claude/projects/*/` | 1,099 entries |
| Codex | `~/.codex/history.jsonl` + `~/.codex/sessions/` | 231 entries |
| Antigravity | `~/.gemini/antigravity/brain/*/` | 50 sessions, 49 transcript.jsonl |

→ Phase A1 "실제 로그 인벤토리" 시작 조건 충족.

## 2. 발견된 보완 사항

### Gap 1: Antigravity 로그 구조 문서화 필요

AGENT_ADAPTER_LAYER_PLAN.md §7 Phase A1은 Antigravity를 "로그 위치와 구조가
확인된 뒤 vertical slice 여부 결정"으로 기술. 실제 brain 디렉토리에
`transcript.jsonl`이 존재하므로, Phase A1에서 Antigravity의 실제 로그 구조를
최소 1개 샘플로 확인해야 Phase A2 진입 가능.

### Gap 2: Codex `.codex/sessions/` 디렉토리 구조

handoff 문서에는 `~/.codex/projects/*/` 패턴이 기술되어 있으나 실제 codex
데이터는 `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` 구조. Phase A1에서
실제 경로로 inventory를 업데이트해야 함.

## 3. Phase A0-A10 일관성 확인

| Phase | 목표 | Acceptance | 상태 |
|-------|------|-----------|------|
| A0 | Scope 확정 | 구현 변경 없음, 경계 명확 | ✅ |
| A1 | 로그 inventory + fixture | 최소 2 agent redacted fixture | ✅ |
| A2 | Common Agent Model 정의 | Phase A1 fixture 기반 | ✅ |
| A3 | Parser Contract Harness | agent별 parser + 공통 suite | ✅ |
| A4 | Detector/Resolver 기반 | session→agent detection | ✅ |
| A5 | Agent Backend Foundation | Claude backend core, product boundary | ✅ |
| A6 | Codex Backend Slice | Claude 수정 없이 Codex backend 추가 | ✅ |
| A7 | Agent Agnostic UX / Product Gate | unknown kind/status/degraded/schema compatibility | ✅ |
| A8 | Third Agent Backend Slice | A7 후 실제 third agent 추가 | 📋 |
| A9 | Approval UX 공통화 | common approval model | 📋 |
| A10 | Diagnostics / Alpha Release Gate | redaction, health, 진단, release gate | 📋 |

## 4. Non-goal 확인

Phase A0에서 명시적으로 제외된 항목들이 문서에 명확히 기술됨 (§2):
- agent runtime 대체 안 함
- wrapper 실행 안 함
- native approval API 초기 미지원 (terminal input fallback)
- parser failure ≠ terminal failure
- raw prompt/token/command 전문 mobile 노출 안 함

## 5. 결론

Phase A0 통과. 구현 변경 없이 scope 문서 보완 2건 발견.
Phase A1 시작 조건 충족 — 실제 로그 데이터 존재 확인.
