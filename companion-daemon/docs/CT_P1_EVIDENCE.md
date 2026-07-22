# CT-P1 Evidence — Minimal Timeline Envelope

Initial implementation commit: `680a78f69b5a0cdae737eaf7c630c1c219185617`

R2 implementation commit: `ec41d196de6e5bd2fa1531703d9ced4a69f61d31`

Correction chain (all commits after the R2 implementation):

- `d9bbf013ddcb0873fe58c59b66d44d37642340a8` — added the all-payload privacy
  boundary and T0 field bounds in `contract.go`.
- `9d97f68abe293459f2776efc7469d2fb44b76f9b` — added test-only digest and T0
  bound coverage.
- `519fae7d3b6a0b33b721060b59c6c7ef17fef49a` — corrected the digest pointer
  aliasing test and its collision assertion.
- `2f54674fc973c5a4cec91fe5b72783932a5c0057` — recorded the preceding T2
  test-correction evidence.

## Scope and dependency boundary

The implementation diff from CT-P0 acceptance contains only the new contract
package, its tests, and redaction fixture:

```text
A	companion-daemon/internal/timeline/contract/contract.go
A	companion-daemon/internal/timeline/contract/contract_test.go
A	companion-daemon/internal/timeline/contract/testdata/redaction_cases.json
```

`git diff --diff-filter=M --name-only d4b4d99ba..680a78f69` produced no output:
no existing production file was modified. `rg -n 'timeline/contract'
companion-daemon/cmd companion-daemon/internal/term mobile` also produced no
output, proving no command, terminal, or mobile consumer exists.

The package depends on `internal/agent` directly and keeps `T0Event
agent.AgentEvent` as the wrapped field. It does not define a replacement T0
event model.

## Exported API snapshot

The exact stdout of `go doc ./internal/timeline/contract Envelope; go doc
./internal/timeline/contract References` is:

```text
package contract // import "devremote/companion-daemon/internal/timeline/contract"

type Envelope struct {
	SchemaVersion          uint16           `json:"schemaVersion"`
	PayloadVersion         uint16           `json:"payloadVersion"`
	EventID                string           `json:"eventId"`
	EventKind              EventKind        `json:"eventKind"`
	SessionID              string           `json:"sessionId"`
	RuntimeID              string           `json:"runtimeId"`
	LaunchGeneration       int64            `json:"launchGeneration"`
	Provider               string           `json:"provider"`
	SourceIncarnation      string           `json:"sourceIncarnation"`
	SourceIdentity         SourceIdentity   `json:"sourceIdentity"`
	SourcePosition         string           `json:"sourcePosition"`
	OccurredAt             time.Time        `json:"occurredAt"`
	ObservedAt             time.Time        `json:"observedAt"`
	RedactionPolicyVersion string           `json:"redactionPolicyVersion"`
	Payload                Payload          `json:"payload"`
	References             References       `json:"references,omitempty"`
	T0Event                agent.AgentEvent `json:"t0Event"`
}
    Envelope is immutable pre-persistence evidence. Append sequence is absent
    by design; timestamps are evidence only and do not participate in identity,
    digest, idempotency, or ordering.

func NewEnvelope(e Envelope) (Envelope, error)
func (e Envelope) CanonicalDigest() (string, error)
func (e Envelope) ComputedEventID() (string, error)
func (e Envelope) Validate() error
package contract // import "devremote/companion-daemon/internal/timeline/contract"

type References struct {
	ProviderInvocation *TypedReference `json:"providerInvocation,omitempty"`
	Thread             *TypedReference `json:"thread,omitempty"`
	Turn               *TypedReference `json:"turn,omitempty"`
	ToolCall           *TypedReference `json:"toolCall,omitempty"`
	ApprovalRequest    *TypedReference `json:"approvalRequest,omitempty"`
	Correlation        *TypedReference `json:"correlation,omitempty"`
	Causation          *TypedReference `json:"causation,omitempty"`
	Transport          *TypedReference `json:"transport,omitempty"`
	Degraded           *TypedReference `json:"degraded,omitempty"`
	Provenance         *TypedReference `json:"provenance,omitempty"`
}

```

## Contract properties covered

- `SchemaV1`/`PayloadV1` gate unknown versions.
- `ComputedEventID` uses framed, domain-separated SHA-256 over **only** the
  provider/source identity tuple: provider, source identity, source position,
  and source incarnation. It has no append sequence, digest, or timestamp
  input. Same source identity with a different canonical digest is rejected as
  `ErrEventIDCollision`, never assigned a new EventID.
- `CanonicalDigest` excludes `EventID`, `OccurredAt`, and `ObservedAt`, but
  includes every other wrapped T0 semantic field (including deterministically
  sorted metadata) so a semantic change cannot be treated as idempotent.
- `Payload` is an exact one-of redacted summary, digest reference, or opaque
  reference. Redacted and opaque values reject common secret markers; raw PTY
  bytes are not an envelope payload field. R2 additionally rejects any wrapped
  T0 raw reference, tool name, approval ID, text, or metadata alongside a
  redacted payload.
- The wrapped T0 event must have an ID and a type compatible with the envelope
  event kind; contradictory wrappers are rejected.
- Typed references are closed and scoped to session/runtime/launch generation.
  They are required only by their matching event kind and rejected everywhere
  else, including cross-session, cross-runtime, and cross-generation bindings.
- Fixture-derived read limits are 1000 events, 4096 cursor bytes, and 1 MiB raw
  record bytes. R2 has N-1/N/N+1 coverage for those limits plus payload bytes,
  envelope/reference IDs, opaque references, and digest-reference byte counts.
- The T2 correction deep-copies the digest payload before changing it, proves
  the canonical digest differs while source-derived EventID remains equal, and
  asserts `ErrEventIDCollision`. Its valid-envelope N-1/N/N+1 table covers all
  bounded envelope identity fields plus T0 ID/source/provenance. Wrapped raw
  detail and metadata are intentionally not valid-boundary candidates because
  every payload variant rejects them at the privacy boundary; dedicated tests
  prove that rejection instead.
- Metadata count and key/value-size regressions use valid payloads. At N-1 and
  N, the production metadata-bound stage accepts the value and validation then
  reaches the independent payload privacy rejection; at N+1, validation rejects
  the metadata bound before privacy. This is the only behavior compatible with
  the all-payload privacy invariant in `contract.go`.

## Verification

All passed on the R2 implementation and T2 test-correction commits:

- `go test -race ./internal/timeline/contract -count=1`
- `go test ./internal/timeline/contract -run=^$ -fuzz=FuzzEnvelopeValidator -fuzztime=2s`
- `go test ./internal/timeline/contract -run=^$ -fuzz=FuzzComputedEventIDStable -fuzztime=2s`
- `go build ./... && go vet ./... && go test -race ./... -count=1 -timeout 300s`
- `test -z "$(gofmt -l .)"`
- `cd mobile && npx tsc --noEmit && npm test -- --runInBand` — 35 suites,
  537/537 tests.
