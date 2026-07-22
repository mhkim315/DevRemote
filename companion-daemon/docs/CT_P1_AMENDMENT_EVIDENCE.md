# CT-P1 Amendment Evidence — Operational Evidence-Source Boundary

Implementation commit: `4698b19a1f338cb632b8c1fa1a7130f5949b4d6c`

This amendment is limited to `internal/timeline/contract/`. It keeps the
existing pre-persistence identity, canonical digest, redaction boundary, and
conditional reference contract while adding a typed, closed evidence-source
boundary.

## Contract boundary

`EvidenceSources` is an exact one-of of these bounded, scope-bound references:

- `ProviderEvidenceRef`;
- `RuntimeEvidenceRef`;
- `ApprovalEvidenceRef`;
- `InputEvidenceRef`;
- `WorkspaceEvidenceRef`;
- `CoordinationEvidenceRef`.

Provider evidence continues to require the wrapped `agent.AgentEvent`, including
the existing EventKind-to-T0 type compatibility validation. Runtime, approval,
input, workspace, and coordination evidence may use `evidence_observed` with a
zero T0 wrapper, so no operational fact needs a fabricated `AgentEvent`.

The new types identify evidence only. They define no runtime, approval, input,
workspace, or coordination state machine. In particular, a coordination
reference is observational: the coordination broker/store owns delivery and
delivery state, never Timeline.

## Preserved invariants

- `ComputedEventID` remains a domain-separated source-identity tuple only; it
  has no digest, timestamp, or append-sequence input.
- `CanonicalDigest` still frames every T0 field and the payload, and now frames
  the selected typed evidence source. A changed reference under the same source
  identity remains an `ErrEventIDCollision`, not a new event.
- All payload variants retain the T0-detail privacy boundary. Provider wrappers
  reject Text, ToolName, ApprovalID, RawRef, and Metadata for redacted, digest,
  and opaque payloads; non-provider evidence rejects any non-zero T0 wrapper.
- Reference IDs and source scopes retain `MaxReferenceBytes` and exact
  session/runtime/launch-generation isolation. Tests cover N-1/N/N+1 for every
  evidence-reference ID type.
- Append sequence remains absent, timestamps remain non-authoritative, and no
  production consumer was added.

## Verification

The following passed from `companion-daemon`:

```text
go build ./...
go vet ./...
go test -race ./... -count=1 -timeout 300s
go test ./internal/timeline/contract -run=^$ -fuzz=FuzzEnvelopeValidator -fuzztime=2s
go test ./internal/timeline/contract -run=^$ -fuzz=FuzzComputedEventIDStable -fuzztime=2s
test -z "$(gofmt -l .)"
cd ../mobile && npx tsc --noEmit && npm test -- --runInBand
```

`rg -n 'timeline/contract' cmd internal/term ../mobile` produced no output:
there is no command, terminal, or mobile production import.

The implementation diff contains only:

```text
M companion-daemon/internal/timeline/contract/contract.go
M companion-daemon/internal/timeline/contract/contract_test.go
```
