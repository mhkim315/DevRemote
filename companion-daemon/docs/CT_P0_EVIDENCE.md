# CT-P0 Evidence — Canonical Timeline Source Freeze (R3)

**IMPL SHA:** `7b7f23d0a`
**EVID SHA:** `9885caf1f`
**Freeze baseline:** `ab1884662`
**Date:** 2026-07-22

This R3 correction records only observations reproduced from the current tree.
Every fenced block below is unedited stdout from the command named immediately
before it; commands, prompts, and explanatory annotations are deliberately not
inside fenced blocks.

## 1. Adapter producer/consumer inventory

The exact stdout of `rg -n --glob='*.go' 'callAcceptedAdapter\(' companion-daemon/internal companion-daemon/cmd | LC_ALL=C sort` is:

```
companion-daemon/internal/term/production_bridge_test.go:104:	ev2, _, nc2, _ := callAcceptedAdapter("codex", recs, "s", ac)
companion-daemon/internal/term/production_bridge_test.go:114:	ev3, _, nc3, _ := callAcceptedAdapter("codex", recs, "s", ac)
companion-daemon/internal/term/production_bridge_test.go:152:	_, version, _, _ := callAcceptedAdapter("codex", recs, "s", ac)
companion-daemon/internal/term/production_bridge_test.go:283:	events, version, nc, _ := callAcceptedAdapter("codex", recs, sid, ac)
companion-daemon/internal/term/production_bridge_test.go:321:	ev, _, _, _ := callAcceptedAdapter("codex", recs, "s", ac)
companion-daemon/internal/term/production_bridge_test.go:44:	events, version, nextCursor, _ := callAcceptedAdapter("codex", records, sid, acursor)
companion-daemon/internal/term/production_bridge_test.go:94:	ev1, _, nc1, _ := callAcceptedAdapter("codex", recs, "s", ac)
companion-daemon/internal/term/telemetry_service.go:168:func callAcceptedAdapter(kind string, rawLines [][]byte, sessionID string, prevCursor string) ([]agent.AgentEvent, string, string, bool) {
```

The exact stdout of `rg -n --glob='*.go' 'ingestApprovals\(' companion-daemon/internal companion-daemon/cmd` is:

```
companion-daemon/internal/term/approval_ingest.go:74:func (s *TelemetryService) ingestApprovals(sessionID string, launchGen int64, streamGen int, provider, version string, events []agent.AgentEvent) {
```

`telemetry_service.go` imports the version-pinned adapter packages to implement
`acceptedAdapterFor`, but import and compilation do not establish a live
producer. The first trace shows that all calls to `callAcceptedAdapter` are in
`production_bridge_test.go`; the second shows that `ingestApprovals` has no
callers. Classification: **no live adapter producer or consumer**. T1 and T2
are fixture-only parser assets, not production-live session or telemetry paths.

## 2. T0–T3 contract classification

| Layer | Classification |
|---|---|
| T0 | `AgentEvent` and `contract` model/harness layer; not a session producer. |
| T1 | Codex version-pinned fixtures; no live caller. |
| T2 | Claude version-pinned fixtures; no live caller. |
| T3 | Transcript capture/ingest/replay authority. |

Managed Codex, Managed Claude, and owned PTY lifecycle remain direct managed
service paths; they do not route through T1 or T2.

## 3. Exact frozen-candidate diff

The exact stdout of `git diff --name-status ab1884662..HEAD` is:

```
A	companion-daemon/docs/ARTIFACT_ID1_EVIDENCE.md
A	companion-daemon/docs/CT_P0_EVIDENCE.md
M	companion-daemon/docs/PB_7_EVIDENCE.md
A	docs/CANONICAL_TIMELINE_CT_P0_SOURCE_FREEZE.md
A	docs/CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md
A	docs/COORDINATOR_HANDOFF.md
M	docs/LOCAL_E2E_EVIDENCE.md
M	docs/PB_DEVICE_GATE_TERMINAL_REGRESSION_REMEDIATION_PLAN.md
M	docs/PB_EXECUTION_PLAN.md
M	docs/PB_LEGACY_REMOVAL_CONTRACT.md
M	docs/POST_PA3_AUTHORITATIVE_ROADMAP.md
M	docs/PRE_DEVICE_QR_INPUT_REMEDIATION_PLAN.md
```

All paths are under `docs/` or `companion-daemon/docs/`.

## 4. Legacy-symbol literal scan

The exact stdout of `grep -rnE "ResolveAgentLog|NewActivityBuffer|NewMemoryEventStore|manual_link" companion-daemon --include="*.go" | grep -v "_test.go\|testdata"` is:

```
companion-daemon/internal/agent/models.go:93:	Source     AgentEventSource `json:"source"` // mechanical origin (jsonl/log_file/screen/process/manual_link)
companion-daemon/internal/agent/contract/validate.go:173:		e.Source = agent.SourceScreen // weakest concrete source; never manual_link by default
companion-daemon/internal/agent/contract/contract.go:68:// jsonl/log_file/screen/process/manual_link). Precedence between conflicting
```

Those three matches are comments. The exact `grep -rnE 'SourceManualLink'
companion-daemon/cmd companion-daemon/internal --include='*.go'` command produced
no stdout: `SourceManualLink` is physically deleted, while `manual_link` remains
only in the three comments shown above.

## 5. Security scan across internal code and documentation

`gsecrets` is not installed in this workspace. The available, reproducible
extended-regex scan was run across both required roots with `grep -E`; no scanner
result is inferred from an unavailable executable. The exact stdout of
`grep -rnE "sk-[A-Za-z0-9]|ghp_|xox[baprs]-|Bearer [A-Za-z0-9]" companion-daemon/internal docs` is:

```
companion-daemon/internal/transcript/api_test.go:231:	req.Header.Set("Authorization", "Bearer deadbeef")
companion-daemon/internal/term/auth_test.go:512:	req.Header.Set("Authorization", "Bearer test-token")
companion-daemon/internal/term/diagnostic.go:120:	redactGhpRE    = regexp.MustCompile(`\bghp_[A-Za-z0-9]{20,}\b`)
companion-daemon/internal/term/diagnostic.go:143:	s = redactGhpRE.ReplaceAllString(s, "ghp_<REDACTED>")
companion-daemon/internal/term/devices_ipc_test.go:119:	// Bearer session gone.
companion-daemon/internal/devicetrust/session.go:189:// isActiveBearer reports whether a bearer session ID is still the current
companion-daemon/internal/devicetrust/auth_middleware_test.go:29:	req.Header.Set("Authorization", "Bearer deadbeef")
companion-daemon/internal/devicetrust/auth_middleware_test.go:121:	req.Header.Set("Authorization", "Bearer xyz")
companion-daemon/internal/devicetrust/auth_middleware.go:51:// bearerToken extracts the raw token from an Authorization: Bearer header.
companion-daemon/internal/agent/contract/validate.go:365:var redactPrefixes = []string{"sk-", "ghp_", "xoxb-", "xoxp-", "Bearer ", "AKIA"}
companion-daemon/internal/agent/doctor/orchestrator.go:639:		"sk-", "ghp_", "xoxb-", "xoxp-", "Bearer ", "AKIA",
companion-daemon/internal/agent/doctor/doctor_test.go:915:	secretPatch := []byte("diff --git a/internal/agent/adapters/claude/v3_0_0/adapter.go b/internal/agent/adapters/claude/v3_0_0/adapter.go\nnew file mode 100644\n--- /dev/null\n+++ b/internal/agent/adapters/claude/v3_0_0/adapter.go\n@@ -0,0 +1,3 @@\n+package v3_0_0\n+// sk-supersecretkey\n+var x = 1\n")
companion-daemon/internal/agent/doctor/doctor_test.go:1106:	secretDiff := []byte("sk-mysecretkey\ndiff --git a/internal/agent/adapters/claude/v3_0_0/adapter.go b/internal/agent/adapters/claude/v3_0_0/adapter.go\nnew file mode 100644\n--- /dev/null\n+++ b/internal/agent/adapters/claude/v3_0_0/adapter.go\n@@ -0,0 +1,2 @@\n+package v3_0_0\n+var x = 1\n")
companion-daemon/internal/agent/doctor/doctor_test.go:1117:	secretDiff := []byte("diff --git a/internal/agent/adapters/claude/v3_0_0/adapter.go b/internal/agent/adapters/claude/v3_0_0/adapter.go\nnew file mode 100644\n--- /dev/null\n+++ b/internal/agent/adapters/claude/v3_0_0/adapter.go\n@@ -0,0 +1,2 @@ ghp_secretinhunk\n+package v3_0_0\n+var x = 1\n")
companion-daemon/internal/agent/doctor/doctor_test.go:1126:	secretDiff := []byte("diff --git a/internal/agent/adapters/claude/v3_0_0/Bearer token.go b/internal/agent/adapters/claude/v3_0_0/adapter.go\nnew file mode 100644\n--- /dev/null\n+++ b/internal/agent/adapters/claude/v3_0_0/Bearer token.go\n@@ -0,0 +1,1 @@\n+package v3_0_0\n")
companion-daemon/internal/agent/doctor/doctor_test.go:1687:	os.WriteFile(filepath.Join(nestedDir, "events.jsonl"), []byte(`sk-mysecretkey`), 0644)
docs/E6_NO_LOGIN_REPORT.md:8:- **Backend**: `InsecureLocalOnly` flag on Handlers. AuthMiddleware accepts `Authorization: Bearer dev-token` when insecure mode is active.
docs/AGENT_PHASE_A10_ALPHA_CHECKLIST.md:50:- [x] Bearer tokens redacted (tested: TestRedactStr_HomePath)
docs/A1_APPROVAL_SAFETY_REMEDIATION_3_REPORT.md:34:| `sanitizeLogID` leaves paths/tokens verbatim | strip control bytes then apply `redactStr` (home paths, sk-/ghp_/xox/Bearer, Authorization, key=value secrets), bounded | `TestHandler_LogRedaction` (actual log capture; Unix+Windows paths, secret/token patterns) — production-wired |
docs/M3_AUTH_2B_IMPLEMENTATION_REPORT.md:72:| ticket is exactly lowercase 64-hex, session-bound | New: "getWSTicket 64-hex ticket, session query, Bearer header"; `wsTicket.test.ts` |
docs/M3B_IMPLEMENTATION_REPORT.md:158:| paired-device bearer only in Authorization | `m3bLifecycleClient`: `Bearer DEVICE_BEARER_A`, header never contains `SUPABASE_JWT` |
docs/PA3_CONTRACT.md:903:  Transcript does not regex-scan for `sk-*`, `ghp_REDACTED`, bearer tokens,
docs/PA3_CONTRACT.md:1282:SECRETS=$(grep -rn "sk-[REDACTED]\|ghp_REDACTED\|xox_REDACTED-\|Bearer_REDACTED" \
docs/AGENT_ADAPTER_LAYER_PLAN.md:598:| API key / token | `<TOKEN>` | `sk-ant-abc123...` → `<TOKEN>` |
```

The matches classify as follows: test fixtures are the transcript, terminal,
device-trust, and doctor-test entries; `diagnostic.go`, `validate.go`, and
`orchestrator.go` contain redaction patterns; `session.go` and
`auth_middleware.go` name the HTTP Bearer scheme; and every `docs/` match is
documentation, a redaction description, or an explicitly synthetic token.
No match is an active credential. The doctor-test values are deliberately used
to verify secret detection and redaction, so they are not excluded from the
literal scan.

## 6. Gate result

The full mobile gate completed with 35 suites and 537/537 tests. The TypeScript
check completed successfully. This document makes no claim about a live adapter
path because the caller inventory above proves none exists.
