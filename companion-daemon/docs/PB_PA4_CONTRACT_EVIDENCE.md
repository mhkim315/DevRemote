# PB/PA4 Contract Evidence

**Contract SHA:** `34d55e950012e97ccdcb03fd9abba88088ffd9a7` (PA3 ACCEPTED)
**Contract document:** `docs/PB_PA4_CONTRACT.md`

## Gate Output (read-only discovery — no production changes)

```
=== Backend ===
(BUILD: PASS)
(VET: PASS)

=== Tests ===
ok  	devremote/companion-daemon/cmd/devremote	35.579s
?   	devremote/companion-daemon/cmd/signald	[no test files]
ok  	devremote/companion-daemon/internal/agent	3.236s
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	3.061s
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	9.032s
ok  	devremote/companion-daemon/internal/agent/contract	6.217s
ok  	devremote/companion-daemon/internal/agent/doctor	126.518s
ok  	devremote/companion-daemon/internal/devicetrust	6.686s
?   	devremote/companion-daemon/internal/models	[no test files]
ok  	devremote/companion-daemon/internal/mux	10.037s
ok  	devremote/companion-daemon/internal/sessionid	4.781s
ok  	devremote/companion-daemon/internal/term	21.628s
ok  	devremote/companion-daemon/internal/transcript	4.224s
ok  	devremote/companion-daemon/internal/watcher	4.799s

=== Format ===
(FORMAT: PASS)

=== GATE ===
HEAD == upstream: 34d55e950
Worktree: clean (only untracked PB_PA4_CONTRACT.md)
```

## Consumer Inventory Summary

| # | Symbol | File | Class | Count |
|---|--------|------|-------|-------|
| 1 | TmuxAdapter | `mux/tmux_adapter.go` | RM | 6 capabilities, 1 registration, 1 legacy session |
| 2 | CmuxAdapter | `mux/cmux_adapter.go` | RM | 8 capabilities, 1 registration, sentinel detection in recorder |
| 3 | LocalPTYAdapter | `mux/localpty_adapter.go` | AR | Feature-flagged, default-off, 0 active callers |
| 4 | ControlledPTYAdapter | `mux/controlled_pty_adapter.go` | AR | Retained; OwnedPTYRuntime is primary owner |
| 5 | ResolveAgentLog | `term/tracker.go` | AE | 1 caller (telemetry_service), deferred |
| 6 | AgentLogResolver + resolvers | `term/resolver.go` | AE | 3 impls, deferred; GeminiResolver has tmux dep |
| 7 | ReadRawLines | `term/reader.go` | AE | 1 caller (telemetry_service), deferred |
| 8 | LogCursor | `term/parser.go` | AE | Used by reader, deferred |
| 9 | mux.Registry | `mux/registry.go` | SP | 7 production callers, must remain |
| 10 | Recorder + registry | `term/recorder.go` | SP | 8 production callers, single PTY reader invariant |
| 11 | cmux sentinel detection | `term/recorder.go` | RM | 4 functions, removed with cmux in PB.3 |
| 12 | HandleWS | `term/pty.go` | SP | 1 route, retained |
| 13 | HandleSessionsAPI/V2/CRUD | `term/pty.go`, `telemetry.go` | SP | 4 routes, retained |
| 14 | processSession | `term/telemetry_service.go` | AE | Accepted-adapter ingestion, deferred |
| 15 | app.go adapter registration | `cmd/devremote/app.go:133-162` | RM | 4 adapters, 2 removed in PB |
| 16 | Mobile DTOs | `mobile/src/` | RM | Adapter union types, capability strings |
| — | SpawnPTY | `mux/session.go:86` | PD | Becomes dead after tmux removal |
| — | cmux_delta.go | `mux/cmux_delta.go` | PD | Only included by cmux_adapter.go |

## Classification Distribution

| Class | Count | Meaning |
|-------|-------|---------|
| REQUIRES MIGRATION (RM) | 7 | Needs new owner before deletion |
| ALREADY REPLACED (AR) | 2 | Safe to delete immediately |
| ACCEPTED-ADAPTER EXCEPTION (AE) | 5 | Preserved, deferred to C0/C1 |
| SHARED PLATFORM-NEUTRAL (SP) | 5 | Must remain permanently |
| PROVEN DEAD (PD) | 2 | Safe to delete immediately |

## Static Zero-Reference Verification (pre-PB baseline)

```
# At PA3 ACCEPTED SHA 34d55e950 — verify all 18 symbols have production callers
# (none are PROVEN DEAD yet — PB deletes will make them dead)

# tmux adapter — 6 capability callers + 1 registration + 1 legacy session
internal/mux/tmux_adapter.go — self-contained adapter impl
cmd/devremote/app.go:144 — registration
internal/mux/session.go:84-86 — legacy NewSession
internal/mux/adapter_capability.go:14,24,30 — capability enums

# cmux adapter — 8 capability callers + 1 registration + sentinel detection
internal/mux/cmux_adapter.go + cmux_delta.go — self-contained
cmd/devremote/app.go:137-142 — registration
internal/term/recorder.go:295-331,397-469,612-624 — sentinel detection

# localpty — feature-flagged, default-off
cmd/devremote/app.go:147-149 — gated registration
cmd/devremote/main.go:63,79 — CLI flag

# Accepted-adapter chain — all preserved
internal/term/tracker.go:16 — ResolveAgentLog
internal/term/telemetry_service.go:257,441 — calls
internal/term/resolver.go + claude_resolver.go + codex_resolver.go + gemini_resolver.go

# SHARED PLATFORM-NEUTRAL — all have active callers
internal/mux/registry.go — 7 callers
internal/term/recorder.go (non-cmux) — 8 callers
internal/term/pty.go (HandleWS, HandleSessionsAPI, HandleSessionCRUD) — 4 routes
```

## Deletion Sequence Summary

| Phase | Sub-phase | Deletes | Gates |
|-------|-----------|---------|-------|
| A4 | Authority-isolation gate | 0 deletions; gates legacy paths | A4 ACCEPT |
| PB.1 | localpty | 1 file + 3 config lines | `grep -rn "localpty"` empty |
| PB.2 | tmux | 2 files + registration + capability cleanup | `grep -rn "tmux"` empty |
| PB.3 | cmux | 3 files + registration + sentinel + MigrateLegacyID | `grep -rn "cmux"` empty |
| PB.4 | Recorder cleanup | captureMode, sentinel code | Recorder tests pass |
| PB.5 | Mobile cleanup | adapter union types, capability strings | `npx tsc --noEmit` |
| PB.6 | Capability cleanup | screen_snapshot_delta mode, cmux caps | Capability contract valid |

## Prerequisites

- **Rollback SHA:** `34d55e950012e97ccdcb03fd9abba88088ffd9a7`
- **PF freeze:** Required before A4 begins (POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md §1)
- **A4 ACCEPT:** Required before PB begins
- **Each PB sub-phase:** Independent commit + gate before next sub-phase
