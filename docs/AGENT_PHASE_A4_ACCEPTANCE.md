# Agent Adapter Phase A4 Acceptance

Date: 2026-07-07

Executor commit under review: `208049972`

Verifier decision: **ACCEPT**

## Scope verification

Phase A4 is accepted as the detector/log-resolver foundation.

Reviewed files:

- `companion-daemon/internal/agent/detector.go`
- `companion-daemon/internal/agent/detector_contract_test.go`

The commit changes only the agent detector/resolver interfaces and their contract
tests. It does not add production integration, mobile behavior, raw logs, or parser
vertical slices.

## Acceptance criteria

### 1. Detector evidence is separate from parser behavior

Accepted.

`DetectionEvidence` aggregates process, cwd, terminal backend, log refs, screen text,
and manual link evidence. It does not emit `AgentEvent` and does not parse raw log
content. This keeps detection/resolution separate from the parser contract accepted in
Phase A3.

### 2. Low-confidence false positives are blocked

Accepted.

The detector contract now has a shared invariant:

```go
confidence < 0.5 => Kind == "unknown"
```

The invariant is applied across detector outputs, including empty evidence, unknown
process, false-positive candidates, low-confidence process evidence, manual link, and
log-backed detection paths.

### 3. Manual link override exists

Accepted.

`ManualEvidence` is part of `DetectionEvidence`, and the contract verifies manual link
priority over conflicting weak/unknown process evidence.

### 4. Log resolver has degraded diagnostics instead of terminal failure semantics

Accepted.

`LogResolver.Resolve` now returns `ResolveResult`:

```go
type ResolveResult struct {
    Logs        []LogRef
    Diagnostics []string
    Degraded    bool
}
```

The degraded resolver test requires:

- `err == nil`
- `Degraded == true`
- non-empty diagnostics

This matches the Agent Adapter principle that permission/path/log problems should
degrade agent detection, not hide or fail the terminal session.

### 5. Raw path boundary exists

Accepted.

`LogRef` now separates:

- `Path`: internal absolute path, never for API/mobile exposure;
- `DisplayPath`: redacted display path safe for diagnostics/API.

The contract verifies non-empty `DisplayPath` and rejects raw home path leakage such as
`/Users/`.

### 6. Known-agent coverage includes Claude and Codex

Accepted.

The detector contract covers known-process detection for:

- Claude
- Codex

Antigravity remains acceptable as fixture/log/manual evidence until process evidence is
better established.

## Verification commands

```sh
git diff --check HEAD~1..HEAD
gofmt -l companion-daemon/internal/agent/detector.go companion-daemon/internal/agent/detector_contract_test.go
```

Both produced no output.

```sh
cd companion-daemon
GOCACHE=/tmp/devremote-agent-a4-go-cache go test ./internal/agent -count=1 -v
GOCACHE=/tmp/devremote-agent-a4-go-cache go vet ./internal/agent
GOCACHE=/tmp/devremote-agent-a4-go-cache go test ./...
```

All passed.

Note: the first sandboxed full `go test ./...` run failed because existing tests use
`httptest` local listeners. The same command passed when rerun with local listener
permission.

## Non-blocking follow-ups

- For confident known detections, production detectors should set a non-empty
  `DisplayName`; A4 does not block on this because A9 is the UX polish phase.
- If `ManualEvidence.LogPath` is used later, it must pass through the same redacted
  display-path boundary as resolver output.
- `Diagnostics []string` is sufficient for A4; A10 may introduce typed diagnostic
  codes/severity if needed.

## Next phase permission

**ALLOWED**

Phase A5 may begin.

Required constraints for Phase A5:

1. Implement the Claude vertical slice against the accepted A2/A3/A4 contracts.
2. Do not weaken `ParseResult`, `ResolveResult`, or low-confidence unknown fallback.
3. Keep raw Claude logs out of the repository.
4. Parser/detector failure must not hide or fail terminal sessions.
5. Mobile/common behavior must not branch on Claude-specific raw fields.
