# Phase A10 — Alpha Release Gate Checklist

- Date: 2026-07-07
- Branch: feature/phase10-multi-adapter
- Base: A9 ACCEPT (3b476da91)

## Backend

- [x] All agent parsers (Claude/Codex/Antigravity) produce common events in /api/sessions
- [x] Agent detection works for all three agents (Claude: 0.7+, Codex: 0.6+, Antigravity: 0.6+)
- [x] Unknown agent → unknown fallback, terminal survives
- [x] Malformed log → degraded diagnostics, terminal survives
- [x] Approval pipeline: pending → approve/reject → resolved + audit log
- [x] Capability-aware: observe-only → no fake actions
- [x] Interaction options preserved with Kind/Input schema/Placement
- [x] /debug/diag returns redacted, exportable diagnostics (tested: TestHandleDiagnostic_*)
- [x] go test -race ./... passes
- [x] go vet ./... passes
- [x] git diff --check clean
- [x] No raw secrets in repo (redaction tokens only)

## Mobile

- [ ] Mobile TypeScript compile passes
- [ ] No vendor-specific branching (rg check for agentKind === "claude"/"codex")
- [ ] Unknown agent kind renders gracefully
- [ ] Unknown agent status renders gracefully
- [ ] Degraded/low-confidence states show visual distinction
- [ ] ApprovalCard uses kind-based styling, not ID inference
- [ ] Per-option input UI renders correctly
- [ ] Empty options shows "No remote actions available"

## Architecture Invariants

- [x] Vendor-specific behavior limited to agent parser/detector/resolver internal code
- [x] Common/mobile UX never branches on agent name
- [x] Unknown/future agent/event/status fallback intact
- [x] agentKind/agentStatus preserved across activity paths
- [x] Parser failure never kills terminal session
- [x] Capability determines executability, not event existence

## Diagnostics (`/debug/diag`)

- [x] Daemon health (uptime, Go version, adapter count)
- [x] Adapter status per-adapter (healthy/unhealthy, last error)
- [x] Session diagnostics (agent kind/status/confidence, state, parser health)
- [x] Approval count per session
- [x] Sampling failures tracked
- [x] Paths redacted: /Users/<user> → <HOME> (tested: TestRedactStr_HomePath)
- [x] Bearer tokens redacted (tested: TestRedactStr_HomePath)
- [x] No raw prompt/token/command/secret in output

## Known Gaps for Beta/GA

- Mobile connection health is binary (URL saved/not), not live health probe
- EventStore is in-memory only; events lost on daemon restart
- Term-layer and agent-layer parsers are parallel systems; not unified
- Antigravity ERROR_MESSAGE normalization differs between layers (tool_result vs failed)
- No automated mobile build/typecheck in CI
- No actual device UX smoke tests

## Verification Commands

```sh
cd companion-daemon
go build ./... && go vet ./...
go test -race ./... -count=1
git diff --check
rg -n "sk-[A-Za-z0-9]|Bearer [A-Za-z0-9]" internal/ docs/
rg -n "/Users/mhk|/home/mhk" internal/ --include="*.go"
```
