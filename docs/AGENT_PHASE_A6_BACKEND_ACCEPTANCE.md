# Agent Phase A6 Backend Acceptance — Codex Parser Expansion

Accepted implementation commits:

- `e79a111` — `feat: Phase A6 — CodexAdapter (parser + detector + resolver)`
- `c112518` — `feat: Phase A6 — term CodexParser normalization + boundary test`
- `e2c2252` — `fix: Phase A6 — composite detector, Codex assertions`
- `7206980` — `fix: Phase A6 — Codex confidence assertion`

Verifier commits leading to acceptance:

- `f42664e` — first A6 review, rejected completion
- `d8551db` — composite detector review, rejected completion pending confidence assertion
- this document — final A6 backend acceptance

Verdict: **ACCEPT — Phase A6 Backend Slice**

Phase A6 is accepted as a backend parser/detector expansion from Claude to Codex. It proves that the A5 Agent Backend Foundation can support a second real agent family through the common event/status/detection bridge and the existing `/api/sessions` product boundary.

This is not a UX release acceptance. The A5 UX release gates still remain mandatory before product release.

## What A6 proves

### 1. Codex has a contract parser and detector

Codex now has agent-layer coverage for:

- parser contract;
- detector contract;
- resolver contract;
- malformed/unknown input behavior;
- approval detection;
- common status derivation.

The common contract suite passes for Codex alongside Claude and mock adapters.

### 2. Codex reaches the production telemetry path

The Codex production boundary test exercises:

```text
TelemetryService.Run
→ processSession
→ SetLogResolver / LogRef{Agent: "codex"}
→ ReadNewEvents
→ term.CodexParser
→ EventStore.Append
→ /api/sessions.Events
```

The API boundary now asserts the important product fields:

- `Events` is non-empty;
- common event types include:
  - `agent_started`;
  - `user_message`;
  - `approval_requested`;
- `AgentKind == "codex"`;
- `AgentStatus == "waiting_approval"`;
- `AgentConfidence >= 0.5`.

This closes the remaining A6 completion gap from the previous reviews.

### 3. Production detection bridge is composite

`TermAgentDetector` is now a composite detector over known agent detectors, including Claude and Codex. It selects the highest-confidence detection result.

This means Codex product detection is no longer only an accidental behavior of `ClaudeDetector`.

### 4. A5 Claude backend regressions still pass

The A6 verification kept the A5 backend proof alive:

- Claude production log path still emits common events.
- Claude parser-derived `waiting_approval` status still reaches `/api/sessions`.
- Claude malformed log survival still passes.
- Bash/non-agent false-positive prevention still passes.

## Verification performed

Commands run:

```bash
git diff --check d8551db..HEAD
gofmt -l companion-daemon/internal/term/codex_boundary_test.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-3-go-cache go test ./internal/term -run 'TestCodexLog_ProductionEventsPath|TestCodexParser|TestClaudeLog_ProductionEventsPath|TestClaudeDegraded_MalformedLog|TestClaudeDetection_FalsePositive' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-3-go-cache go test ./internal/agent -run 'TestCodex|TestClaude|TestMock' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-3-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a6-3-go-cache go test ./...
cd mobile && ./node_modules/.bin/tsc --noEmit
rg -n "if .*agent|agentKind|agentStatus|claude|codex|gpt|antigravity|gemini" mobile --glob '!node_modules' --glob '!dist' --glob '!build'
```

Results:

- `git diff --check`: passed.
- `gofmt -l`: returned no files.
- Targeted term boundary tests: passed.
- Agent contract tests: passed.
- `go vet ./...`: passed.
- Sandboxed `go test ./...`: failed only because `httptest` could not bind a local port in the sandbox.
- Authorized external `go test ./...`: passed.
- Mobile TypeScript compile: passed.
- Mobile name-branch grep: no mobile source matches. The only match was an incidental `gpt` substring inside `mobile/package-lock.json` integrity data.

Relevant targeted output:

```text
=== RUN   TestCodexLog_ProductionEventsPath
    codex_boundary_test.go:55: codex: events=4, agentKind=codex, state=waiting
--- PASS: TestCodexLog_ProductionEventsPath
```

The test itself now asserts `AgentStatus == "waiting_approval"` and `AgentConfidence >= 0.5`; the log line's legacy `state=waiting` field is not the accepted AgentStatus contract.

## Remaining non-A6 release gates

These are still mandatory before product release, but they do not block A6 backend acceptance:

- degraded diagnostics UX;
- degraded status visualization;
- mobile rendering for agent fields;
- mobile schema proof;
- no backend-name branching in mobile UX;
- safe behavior for unknown future agent kinds.

These remain tracked by:

- `docs/AGENT_UX_RELEASE_GATE_FOLLOWUPS.md`
- `docs/NEXT_SESSION_AGENT_ADAPTER_HANDOFF.md`
- `docs/AGENT_ADAPTER_LAYER_PLAN.md`

## Next recommended phase

Proceed to the next backend expansion only if it follows the same acceptance shape:

1. real redacted fixture first;
2. common parser/detector contract;
3. production telemetry boundary;
4. `/api/sessions` assertions for events, `AgentKind`, `AgentStatus`, and `AgentConfidence`;
5. Claude/Codex regression preservation;
6. no mobile name-branching.

For an additional agent family, the next candidate should be selected based on availability of real logs and stable redaction fixtures, not on speculative parser design.
