# CT-P1 Evidence — Minimal Timeline Envelope

Implementation commit: `680a78f69b5a0cdae737eaf7c630c1c219185617`

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
- `ComputedEventID` uses framed, domain-separated SHA-256 over provider/source
  identity and semantic digest. It accepts no append sequence and excludes both
  timestamps; append order and timestamp changes cannot alter identity or dedup.
- `CanonicalDigest` excludes `EventID`, `OccurredAt`, and `ObservedAt`.
- `Payload` is an exact one-of redacted summary, digest reference, or opaque
  reference. Redacted and opaque values reject common secret markers; raw PTY
  bytes are not an envelope payload field.
- Typed references are closed and scoped to session/runtime/launch generation.
  They are required only by their matching event kind and rejected everywhere
  else, including cross-session, cross-runtime, and cross-generation bindings.
- Fixture-derived read limits are 1000 events, 4096 cursor bytes, and 1 MiB raw
  record bytes; each has N-1/N/N+1 coverage.

## Verification

All passed on the implementation commit:

- `go test -race ./internal/timeline/contract -count=1`
- `go test ./internal/timeline/contract -run=^$ -fuzz=FuzzEnvelopeValidator -fuzztime=2s`
- `go test ./internal/timeline/contract -run=^$ -fuzz=FuzzComputedEventIDStable -fuzztime=2s`
- `go build ./... && go vet ./... && go test -race ./... -count=1 -timeout 300s`
- `test -z "$(gofmt -l .)"`
- `cd mobile && npx tsc --noEmit && npm test -- --runInBand` — 35 suites,
  537/537 tests.
