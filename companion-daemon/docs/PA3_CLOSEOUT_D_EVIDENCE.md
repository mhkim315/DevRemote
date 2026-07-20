# PA3 Closeout D — Evidence

**Implementation SHA:** `d72297026ddf69f739ae547028eaba02de14b5b9`

**Scope:** Deletions only — 4 legacy parser files, 2 dependent test files, partial removals from 3 files.

## Files Deleted
| File | Reason |
|------|--------|
| `term/claude_parser.go` | ClaudeParser — superseded by `agent/adapters/claude` |
| `term/codex_parser.go` | CodexParser — superseded by `agent/adapters/codex` |
| `term/gemini_parser.go` | GeminiParser — zero production callers |
| `term/antigravity_parser.go` | AntigravityParser — superseded by `agent/adapters/antigravity` |
| `term/parser_test.go` | Tests only deleted parser types and ReadNewEvents |
| `term/reader_test.go` | Tests only ReadNewEvents (deleted) |

## Partial Removals
| File | Removed | Preserved |
|------|---------|-----------|
| `term/parser.go` | `AgentLogParser` interface, `encoding/json` and `models` imports | `LogCursor` struct |
| `term/reader.go` | `ReadNewEvents()`, `log` and `models` imports | `ReadRawLines()`, `RawLinesResult` |
| `term/telemetry.go` | `normalizeEventType()`, `strings` import | All telemetry types and functions |

## Production Reference Verification
```
grep -rn "ClaudeParser|CodexParser|GeminiParser|AntigravityParser|
  ReadNewEvents|AgentLogParser|normalizeEventType" --include="*.go" .
  | grep -v "_test.go" | grep -v "claude_adapter|codex_adapter|antigravity_adapter"
→ Only a comment in runtime.go:79 mentioning ReadNewEvents (non-code reference)
```

## Gate Output
```
=== Backend ===
(BUILD: PASS)
(VET: PASS)

=== Tests ===
ok  	devremote/companion-daemon/cmd/devremote	33.593s
ok  	devremote/companion-daemon/internal/agent	4.488s
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	5.603s
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	9.829s
ok  	devremote/companion-daemon/internal/agent/contract	4.387s
ok  	devremote/companion-daemon/internal/devicetrust	5.025s
ok  	devremote/companion-daemon/internal/mux	9.778s
ok  	devremote/companion-daemon/internal/sessionid	5.372s
ok  	devremote/companion-daemon/internal/term	17.855s
ok  	devremote/companion-daemon/internal/transcript	2.545s
ok  	devremote/companion-daemon/internal/watcher	3.209s

=== Format ===
(FORMAT: PASS)

=== Focused ===
transcript -race -count=20: PASS
Recorder -race -count=20: PASS
```

## Preserved (Verified Unchanged)
- `tracker.go` (ResolveAgentLog, findAgentProcess)
- `resolver.go` (AgentLogResolver + implementations)
- `reader.go` (ReadRawLines only)
- `parser.go` (LogCursor only)
- `telemetry_service.go` (accepted adapter path)
- `managed_events.go` (managedEventStore — unrelated type)
