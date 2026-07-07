# Agent Phase A5 Implementation Review 10 — Evidence Interface Attempt

Reviewed commit: `e47f2d50c`

Verdict: **REJECT**

This revision improves the A5 interface shape, but it still does not prove the required product-boundary flow:

> actual agent evidence → production `TermAgentDetector` → `/api/sessions` telemetry fields → client-visible state

The current true-positive proof is still test-assisted. Production telemetry only passes the terminal adapter name to the detector, so the production detector has no process/CWD evidence and cannot identify Claude.

## Verification performed

Commands run:

```bash
git diff --check 0e91859..HEAD
gofmt -l companion-daemon/internal/agent/bridge.go companion-daemon/internal/term/runtime.go companion-daemon/internal/term/telemetry.go companion-daemon/internal/term/telemetry_service.go companion-daemon/internal/term/claude_boundary_test.go
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/agent -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./internal/term -run 'TestClaudeOutput_(TelemetryPath|TruePositive|FalsePositivePrevention)' -count=1 -v
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5-go-cache go vet ./...
cd companion-daemon && GOCACHE=/tmp/devremote-agent-a5-go-cache go test ./...
```

Notes:

- `go test ./...` requires local `httptest` port binding. The sandboxed run failed with `bind: operation not permitted`; the authorized run passed.
- Formatting and diff checks passed.
- Targeted agent and term tests passed.
- Passing tests do not satisfy A5 because the main true-positive test injects evidence through a test-only detector.

## What improved

- `agent.ProdDetectionEvidence` now has production-shaped fields:
  - `ProcessName`
  - `CWD`
  - `TermAdapter`
- `term.AgentDetector` now accepts evidence instead of only session identifiers.
- `cmd/devremote` wires `agent.NewTermAgentDetector()` when agent detection is enabled.
- A no-evidence false-positive guard exists and correctly verifies that production detection does not identify Claude without evidence.

These are useful prerequisites, but they are not sufficient for A5 acceptance.

## Blocking issue 1 — production telemetry still passes only adapter evidence

`TelemetryService.Snapshot()` calls the detector like this:

```go
kind, status, confidence := s.detector.DetectAgent(st.ID, ref.Adapter, ref.LocalID, ProdDetectionEvidence{
    TermAdapter: ref.Adapter,
})
```

The fallback simple telemetry path does the same:

```go
kind, status, confidence := detector.DetectAgent(st.ID, ref.Adapter, ref.LocalID, ProdDetectionEvidence{
    TermAdapter: ref.Adapter,
})
```

That means the production detector receives:

```text
ProcessName = ""
CWD         = ""
TermAdapter = "tmux" | "cmux" | "localpty" | ...
```

`agent.TermAgentDetector` then forwards those empty values to `ClaudeDetector`. With no process or CWD evidence, the detector should return unknown. This is correct false-positive prevention, but it is not a Claude true-positive product-boundary proof.

## Blocking issue 2 — the product-boundary true-positive test uses a test-only detector

The test named `claude_evidence_true_positive` does not use the production detector:

```go
detector := &claudeEvidenceDetector{}
```

That detector injects Claude evidence when production evidence is empty:

```go
if evidence.ProcessName == "" && adapterName == "tmux" {
    ev.ProcessName = "claude"
    ev.CWD = "/Users/test/project"
}
```

This proves that `/api/sessions` can carry scalar agent fields if a test double fabricates detection evidence. It does not prove that the production path can obtain evidence and detect Claude.

The no-evidence subtest is closer to the real production state:

```go
detector := agent.NewTermAgentDetector()
```

That subtest expects no high-confidence Claude detection. Given the current production evidence path, this is exactly what would happen in production.

## Blocking issue 3 — direct Claude detector true-positive is not product-boundary proof

`TestClaudeOutput_TruePositive` directly calls:

```go
agent.NewClaudeDetector().Detect(agent.DetectionEvidence{
    ProcessName: "claude",
    CWD:         "/Users/test/project",
    TermAdapter: "tmux",
})
```

This is a valid detector unit test, but A5 is about product boundary propagation. It does not exercise:

- `TelemetryService`
- production `agent.NewTermAgentDetector()`
- session/process evidence collection
- `/api/sessions`
- mobile/client-visible payloads

## Blocking issue 4 — A5 parser/event semantics still do not reach the product boundary

The API fields added in the telemetry payload are still scalar detector output:

- `agentKind`
- `agentStatus`
- `agentConfidence`

There is still no product-boundary proof for:

- parser-derived agent events
- approval events
- degraded parser state
- resolver status
- cursor/resume semantics
- unknown-agent safety across API/client boundaries

If A5 scope is intentionally only “process evidence based detection fields,” then the A5 acceptance criteria must be narrowed explicitly. Under the current Agent Adapter Layer plan, this remains below the required boundary.

## Blocking issue 5 — mobile/client boundary remains incomplete

The backend telemetry struct contains A5 fields, but there is still no verified mobile/client consumption path for these values. A5 acceptance should not stop at Go JSON struct fields unless the documented scope is reduced to backend-only.

At minimum, the implementation needs either:

- mobile schema/component tests proving unknown and detected agent states render safely, or
- a documented decision that A5 excludes mobile and will be completed in a later phase.

## Required changes for acceptance

The next revision should not add another test double that supplies evidence. It should make the production path supply evidence.

Required:

1. Populate `ProdDetectionEvidence` in `TelemetryService.Snapshot()` from real session/process metadata.
   - Use existing `ProcessInfo` / process snapshot data where available.
   - If the telemetry loop already observes process information, retain enough per-session evidence for `Snapshot()`.
   - If a session adapter does not support process info, pass empty evidence and keep unknown/low-confidence behavior.

2. Add a product-boundary true-positive test using `agent.NewTermAgentDetector()`.
   - The session/test adapter should expose process metadata through the same interface production uses.
   - The test must not use a detector test double that injects Claude evidence.
   - The assertion should verify `/api/sessions` contains `agentKind=claude`, `agentStatus=working`, and confidence above the accepted threshold.

3. Keep the false-positive tests.
   - No process evidence must remain unknown/low-confidence.
   - Non-agent shells such as `bash`, `zsh`, `sh`, or `fish` must remain unknown/low-confidence.

4. Decide and document A5 boundary precisely.
   - If A5 means “agent detection scalar fields only,” state that and move parser/event/approval UX to A6.
   - If A5 still means “Claude agent output reaches product boundary,” parser/resolver/event state must also flow to API/client surfaces.

5. If mobile/client is in A5 scope, add client-side schema/rendering tests.
   - Unknown agent must render safely.
   - Detected Claude must render without backend-name allowlists.
   - Missing A5 fields from older daemons must remain backward compatible.

## Acceptance target

The minimal acceptable proof is:

```text
production-like session process metadata
→ TelemetryService evidence construction
→ agent.NewTermAgentDetector()
→ /api/sessions
→ agentKind=claude, agentStatus=working, agentConfidence>=threshold
```

and the same path must produce unknown/low-confidence when process evidence is absent or non-agent.

Until that path exists, Phase A5 remains rejected.
