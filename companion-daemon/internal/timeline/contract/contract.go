// Package contract defines the minimal, pre-persistence canonical timeline
// envelope. It wraps the existing T0 agent.AgentEvent; it neither replaces nor
// forks that model, and it does not append to or project any read model.
package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"devremote/companion-daemon/internal/agent"
)

const (
	SchemaV1  uint16 = 1
	PayloadV1 uint16 = 1

	// Fixture-derived limits are intentionally tighter than the T0 safety caps.
	MaxEventsPerRead  = 1000
	MaxCursorBytes    = 4096
	MaxRawRecordBytes = 1 << 20

	MaxPayloadBytes   = 16 << 10
	MaxReferenceBytes = 512
)

var (
	ErrUnsupportedSchema  = errors.New("timeline: unsupported schema version")
	ErrUnsupportedPayload = errors.New("timeline: unsupported payload version")
	ErrEventIDMismatch    = errors.New("timeline: event id does not match semantic identity")
	ErrEventIDCollision   = errors.New("timeline: same event id has a different canonical digest")
)

// EventKind is a closed event vocabulary. Unknown values are rejected.
type EventKind string

const (
	EventProviderInvocationStarted  EventKind = "provider_invocation_started"
	EventProviderInvocationFinished EventKind = "provider_invocation_finished"
	EventThreadObserved             EventKind = "thread_observed"
	EventTurnObserved               EventKind = "turn_observed"
	EventToolCallStarted            EventKind = "tool_call_started"
	EventToolCallFinished           EventKind = "tool_call_finished"
	EventApprovalRequested          EventKind = "approval_requested"
	EventApprovalResolved           EventKind = "approval_resolved"
	EventCorrelationEstablished     EventKind = "correlation_established"
	EventStreamObserved             EventKind = "stream_observed"
	EventDegraded                   EventKind = "degraded"
	EventEvidenceObserved           EventKind = "evidence_observed"
)

// ReferenceKind is a closed conditional-reference vocabulary.
type ReferenceKind string

const (
	ReferenceProviderInvocation ReferenceKind = "provider_invocation"
	ReferenceThread             ReferenceKind = "thread"
	ReferenceTurn               ReferenceKind = "turn"
	ReferenceToolCall           ReferenceKind = "tool_call"
	ReferenceApprovalRequest    ReferenceKind = "approval_request"
	ReferenceCorrelation        ReferenceKind = "correlation"
	ReferenceCausation          ReferenceKind = "causation"
	ReferenceTransport          ReferenceKind = "transport"
	ReferenceDegraded           ReferenceKind = "degraded"
	ReferenceProvenance         ReferenceKind = "provenance"
)

// SourceIdentity is the provider-side identity associated with a source record.
type SourceIdentity struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Scope binds a reference to exactly one managed runtime generation.
type Scope struct {
	SessionID        string `json:"sessionId"`
	RuntimeID        string `json:"runtimeId"`
	LaunchGeneration int64  `json:"launchGeneration"`
}

// TypedReference is deliberately conditional: References exposes one pointer
// per semantic domain and validation rejects a pointer on an unrelated kind.
type TypedReference struct {
	Kind  ReferenceKind `json:"kind"`
	ID    string        `json:"id"`
	Scope Scope         `json:"scope"`
}

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

// RedactedPayload is display-safe text after the indicated redaction policy.
type RedactedPayload struct {
	Summary string `json:"summary"`
}

// DigestReferencePayload carries only a digest and bounded byte count.
type DigestReferencePayload struct {
	Digest string `json:"digest"`
	Bytes  int    `json:"bytes"`
}

// OpaqueReferencePayload points to an external protected record without
// copying raw terminal bytes or secret-bearing provider input into the envelope.
type OpaqueReferencePayload struct {
	Reference string `json:"reference"`
}

// Payload is a closed one-of union. Exactly one variant is required.
type Payload struct {
	Redacted *RedactedPayload        `json:"redacted,omitempty"`
	Digest   *DigestReferencePayload `json:"digest,omitempty"`
	Opaque   *OpaqueReferencePayload `json:"opaque,omitempty"`
}

// Envelope is immutable pre-persistence evidence. Append sequence is absent by
// design; timestamps are evidence only and do not participate in identity,
// digest, idempotency, or ordering.
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

// NewEnvelope assigns the stable semantic EventID after validating all required
// pre-persistence fields. It never assigns an append position.
func NewEnvelope(e Envelope) (Envelope, error) {
	e.EventID = ""
	if err := e.validate(false); err != nil {
		return Envelope{}, err
	}
	id, err := e.ComputedEventID()
	if err != nil {
		return Envelope{}, err
	}
	e.EventID = id
	return e, nil
}

// Validate checks closed versions, bounds, scope isolation, and conditional
// reference placement. It intentionally does not infer order from timestamps.
func (e Envelope) Validate() error {
	return e.validate(true)
}

func (e Envelope) validate(requireEventID bool) error {
	if e.SchemaVersion != SchemaV1 {
		return ErrUnsupportedSchema
	}
	if e.PayloadVersion != PayloadV1 {
		return ErrUnsupportedPayload
	}
	if !knownEventKind(e.EventKind) {
		return fmt.Errorf("timeline: unknown event kind %q", e.EventKind)
	}
	for _, pair := range []struct{ name, value string }{
		{"session id", e.SessionID}, {"runtime id", e.RuntimeID}, {"provider", e.Provider},
		{"source incarnation", e.SourceIncarnation}, {"source identity kind", e.SourceIdentity.Kind},
		{"source identity id", e.SourceIdentity.ID}, {"source position", e.SourcePosition},
		{"redaction policy version", e.RedactionPolicyVersion},
	} {
		if pair.value == "" || len(pair.value) > MaxReferenceBytes {
			return fmt.Errorf("timeline: invalid %s", pair.name)
		}
	}
	if e.LaunchGeneration < 0 || e.OccurredAt.IsZero() || e.ObservedAt.IsZero() {
		return errors.New("timeline: invalid generation or evidence timestamp")
	}
	if e.T0Event.SessionID != e.SessionID || e.T0Event.AgentKind != e.Provider {
		return errors.New("timeline: wrapped T0 event scope does not match envelope")
	}
	if err := e.Payload.validate(); err != nil {
		return err
	}
	if err := e.References.validate(e); err != nil {
		return err
	}
	if requireEventID {
		if e.EventID == "" {
			return errors.New("timeline: event id is required")
		}
		id, err := e.ComputedEventID()
		if err != nil {
			return err
		}
		if e.EventID != id {
			return ErrEventIDMismatch
		}
	}
	return nil
}

// CanonicalDigest returns the semantic digest used for collision detection. It
// excludes EventID, OccurredAt, and ObservedAt, so evidence timestamps cannot
// drive deduplication.
func (e Envelope) CanonicalDigest() (string, error) {
	if err := e.validateWithoutEventID(); err != nil {
		return "", err
	}
	return digestHex("timeline/canonical-digest/v1", e.canonicalFields(false)), nil
}

// ComputedEventID derives a stable, domain-separated identity from the provider
// and source identity tuple plus canonical evidence. It excludes timestamps and
// intentionally has no append-sequence argument.
func (e Envelope) ComputedEventID() (string, error) {
	digest, err := e.CanonicalDigest()
	if err != nil {
		return "", err
	}
	return digestHex("timeline/event-id/v1", []string{
		e.Provider, e.SourceIncarnation, e.SourceIdentity.Kind, e.SourceIdentity.ID,
		e.SourcePosition, string(e.EventKind), e.SessionID, e.RuntimeID,
		fmt.Sprintf("%d", e.LaunchGeneration), digest,
	}), nil
}

// SameEvidence reports idempotency for an already-stored envelope. A matching
// EventID with a distinct canonical digest is a collision, never a replacement.
func SameEvidence(existing, candidate Envelope) (bool, error) {
	if existing.EventID == "" || candidate.EventID == "" || existing.EventID != candidate.EventID {
		return false, nil
	}
	a, err := existing.CanonicalDigest()
	if err != nil {
		return false, err
	}
	b, err := candidate.CanonicalDigest()
	if err != nil {
		return false, err
	}
	if a != b {
		return false, ErrEventIDCollision
	}
	return true, nil
}

// ValidateReadBounds validates fixture-derived adapter input limits.
func ValidateReadBounds(eventCount int, cursor, rawRecord []byte) error {
	if eventCount < 0 || eventCount > MaxEventsPerRead {
		return errors.New("timeline: event count exceeds bound")
	}
	if len(cursor) > MaxCursorBytes {
		return errors.New("timeline: cursor exceeds bound")
	}
	if len(rawRecord) > MaxRawRecordBytes {
		return errors.New("timeline: raw record exceeds bound")
	}
	return nil
}

func (e Envelope) validateWithoutEventID() error {
	copy := e
	copy.EventID = ""
	return copy.validate(false)
}

func (p Payload) validate() error {
	n := 0
	if p.Redacted != nil {
		n++
	}
	if p.Digest != nil {
		n++
	}
	if p.Opaque != nil {
		n++
	}
	if n != 1 {
		return errors.New("timeline: exactly one payload variant is required")
	}
	if p.Redacted != nil {
		if p.Redacted.Summary == "" || len(p.Redacted.Summary) > MaxPayloadBytes || containsSecretMarker(p.Redacted.Summary) {
			return errors.New("timeline: invalid redacted payload")
		}
	}
	if p.Digest != nil {
		if len(p.Digest.Digest) != sha256.Size*2 || p.Digest.Bytes < 0 || p.Digest.Bytes > MaxRawRecordBytes {
			return errors.New("timeline: invalid digest payload")
		}
		if _, err := hex.DecodeString(p.Digest.Digest); err != nil {
			return errors.New("timeline: invalid digest payload")
		}
	}
	if p.Opaque != nil && (p.Opaque.Reference == "" || len(p.Opaque.Reference) > MaxReferenceBytes || containsSecretMarker(p.Opaque.Reference)) {
		return errors.New("timeline: invalid opaque payload")
	}
	return nil
}

func (r References) validate(e Envelope) error {
	for _, item := range []struct {
		ref  *TypedReference
		kind ReferenceKind
	}{
		{r.ProviderInvocation, ReferenceProviderInvocation}, {r.Thread, ReferenceThread}, {r.Turn, ReferenceTurn},
		{r.ToolCall, ReferenceToolCall}, {r.ApprovalRequest, ReferenceApprovalRequest}, {r.Correlation, ReferenceCorrelation},
		{r.Causation, ReferenceCausation}, {r.Transport, ReferenceTransport}, {r.Degraded, ReferenceDegraded}, {r.Provenance, ReferenceProvenance},
	} {
		if item.ref == nil {
			continue
		}
		if item.ref.Kind != item.kind || !referenceAllowed(e.EventKind, item.kind) {
			return fmt.Errorf("timeline: %s reference forbidden on %s", item.kind, e.EventKind)
		}
		if item.ref.ID == "" || len(item.ref.ID) > MaxReferenceBytes || item.ref.Scope.SessionID != e.SessionID || item.ref.Scope.RuntimeID != e.RuntimeID || item.ref.Scope.LaunchGeneration != e.LaunchGeneration {
			return errors.New("timeline: reference scope mismatch")
		}
	}
	if required := requiredReference(e.EventKind); required != "" && !r.has(required) {
		return fmt.Errorf("timeline: %s reference is required on %s", required, e.EventKind)
	}
	return nil
}

func (r References) has(kind ReferenceKind) bool {
	switch kind {
	case ReferenceProviderInvocation:
		return r.ProviderInvocation != nil
	case ReferenceThread:
		return r.Thread != nil
	case ReferenceTurn:
		return r.Turn != nil
	case ReferenceToolCall:
		return r.ToolCall != nil
	case ReferenceApprovalRequest:
		return r.ApprovalRequest != nil
	case ReferenceCorrelation:
		return r.Correlation != nil
	case ReferenceTransport:
		return r.Transport != nil
	case ReferenceDegraded:
		return r.Degraded != nil
	case ReferenceProvenance:
		return r.Provenance != nil
	}
	return false
}

func requiredReference(event EventKind) ReferenceKind {
	switch event {
	case EventProviderInvocationStarted, EventProviderInvocationFinished:
		return ReferenceProviderInvocation
	case EventThreadObserved:
		return ReferenceThread
	case EventTurnObserved:
		return ReferenceTurn
	case EventToolCallStarted, EventToolCallFinished:
		return ReferenceToolCall
	case EventApprovalRequested, EventApprovalResolved:
		return ReferenceApprovalRequest
	case EventCorrelationEstablished:
		return ReferenceCorrelation
	case EventStreamObserved:
		return ReferenceTransport
	case EventDegraded:
		return ReferenceDegraded
	case EventEvidenceObserved:
		return ReferenceProvenance
	}
	return ""
}

func knownEventKind(k EventKind) bool {
	switch k {
	case EventProviderInvocationStarted, EventProviderInvocationFinished, EventThreadObserved, EventTurnObserved, EventToolCallStarted, EventToolCallFinished, EventApprovalRequested, EventApprovalResolved, EventCorrelationEstablished, EventStreamObserved, EventDegraded, EventEvidenceObserved:
		return true
	}
	return false
}

func referenceAllowed(event EventKind, ref ReferenceKind) bool {
	switch ref {
	case ReferenceProviderInvocation:
		return event == EventProviderInvocationStarted || event == EventProviderInvocationFinished
	case ReferenceThread:
		return event == EventThreadObserved
	case ReferenceTurn:
		return event == EventTurnObserved
	case ReferenceToolCall:
		return event == EventToolCallStarted || event == EventToolCallFinished
	case ReferenceApprovalRequest:
		return event == EventApprovalRequested || event == EventApprovalResolved
	case ReferenceCorrelation:
		return event == EventCorrelationEstablished
	case ReferenceCausation:
		return event == EventToolCallFinished || event == EventApprovalResolved || event == EventCorrelationEstablished
	case ReferenceTransport:
		return event == EventStreamObserved
	case ReferenceDegraded:
		return event == EventDegraded
	case ReferenceProvenance:
		return event == EventEvidenceObserved
	}
	return false
}

func (e Envelope) canonicalFields(includeEventID bool) []string {
	refs := e.References
	fields := []string{fmt.Sprintf("%d", e.SchemaVersion), fmt.Sprintf("%d", e.PayloadVersion), string(e.EventKind), e.SessionID, e.RuntimeID, fmt.Sprintf("%d", e.LaunchGeneration), e.Provider, e.SourceIncarnation, e.SourceIdentity.Kind, e.SourceIdentity.ID, e.SourcePosition, e.RedactionPolicyVersion, e.T0Event.ID, string(e.T0Event.Type), e.T0Event.RawRef}
	if includeEventID {
		fields = append(fields, e.EventID)
	}
	fields = append(fields, payloadFields(e.Payload)...)
	for _, r := range []*TypedReference{refs.ProviderInvocation, refs.Thread, refs.Turn, refs.ToolCall, refs.ApprovalRequest, refs.Correlation, refs.Causation, refs.Transport, refs.Degraded, refs.Provenance} {
		if r == nil {
			fields = append(fields, "")
		} else {
			fields = append(fields, string(r.Kind), r.ID, r.Scope.SessionID, r.Scope.RuntimeID, fmt.Sprintf("%d", r.Scope.LaunchGeneration))
		}
	}
	return fields
}

func payloadFields(p Payload) []string {
	if p.Redacted != nil {
		return []string{"redacted", p.Redacted.Summary}
	}
	if p.Digest != nil {
		return []string{"digest", p.Digest.Digest, fmt.Sprintf("%d", p.Digest.Bytes)}
	}
	return []string{"opaque", p.Opaque.Reference}
}

func digestHex(domain string, fields []string) string {
	h := sha256.New()
	writeFrame(h, domain)
	for _, f := range fields {
		writeFrame(h, f)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeFrame(h interface{ Write([]byte) (int, error) }, s string) {
	fmt.Fprintf(h, "%d:", len(s))
	h.Write([]byte(s))
}

func containsSecretMarker(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "sk-") || strings.Contains(lower, "ghp_") || strings.Contains(lower, "xox") || strings.Contains(lower, "bearer ")
}
