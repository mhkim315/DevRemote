// Package contract defines the minimal, pre-persistence canonical timeline
// envelope. It wraps the existing T0 agent.AgentEvent; it neither replaces nor
// forks that model, and it does not append to or project any read model.
package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
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
	MaxMetadataKeys   = 64
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

// ProviderEvidenceRef identifies provider-native evidence. Provider evidence
// is normalized through the wrapped T0 AgentEvent; it is not a second event
// model.
type ProviderEvidenceRef struct {
	ID    string `json:"id"`
	Scope Scope  `json:"scope"`
}

// RuntimeEvidenceRef identifies an operational fact owned by the managed
// runtime. It deliberately does not model runtime state.
type RuntimeEvidenceRef struct {
	ID    string `json:"id"`
	Scope Scope  `json:"scope"`
}

// ApprovalEvidenceRef identifies observational approval evidence. Approval
// authority and delivery remain outside the Timeline.
type ApprovalEvidenceRef struct {
	ID    string `json:"id"`
	Scope Scope  `json:"scope"`
}

// InputEvidenceRef identifies observational input-path evidence. It neither
// authorizes nor delivers input.
type InputEvidenceRef struct {
	ID    string `json:"id"`
	Scope Scope  `json:"scope"`
}

// WorkspaceEvidenceRef identifies an operational fact owned by a workspace
// authority. It intentionally does not define workspace state or transitions.
type WorkspaceEvidenceRef struct {
	ID    string `json:"id"`
	Scope Scope  `json:"scope"`
}

// CoordinationEvidenceRef identifies an operational coordination fact. The
// coordination broker/store owns delivery and its state; Timeline records only
// referenced evidence.
type CoordinationEvidenceRef struct {
	ID    string `json:"id"`
	Scope Scope  `json:"scope"`
}

// EvidenceSources is a closed one-of source boundary. Provider evidence wraps
// T0 AgentEvent; the other source types make operational evidence possible
// without fabricating an AgentEvent or defining a future authority's state
// machine.
type EvidenceSources struct {
	Provider     *ProviderEvidenceRef     `json:"provider,omitempty"`
	Runtime      *RuntimeEvidenceRef      `json:"runtime,omitempty"`
	Approval     *ApprovalEvidenceRef     `json:"approval,omitempty"`
	Input        *InputEvidenceRef        `json:"input,omitempty"`
	Workspace    *WorkspaceEvidenceRef    `json:"workspace,omitempty"`
	Coordination *CoordinationEvidenceRef `json:"coordination,omitempty"`
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
	EvidenceSources        EvidenceSources  `json:"evidenceSources"`
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
	providerEvidence, err := e.EvidenceSources.validate(e)
	if err != nil {
		return err
	}
	if providerEvidence {
		if err := e.validateWrappedT0(); err != nil {
			return err
		}
	} else if e.EventKind != EventEvidenceObserved {
		return errors.New("timeline: non-provider evidence must use evidence_observed")
	} else if !zeroT0Event(e.T0Event) {
		return errors.New("timeline: non-provider evidence cannot carry wrapped T0 event")
	}
	if err := e.Payload.validate(); err != nil {
		return err
	}
	// CT-P1 T1: any payload variant is the privacy boundary. When Redacted,
	// Digest, or Opaque is set, T0Event raw detail (Text, ToolName, ApprovalID,
	// RawRef, Metadata) must be zero/empty — the envelope payload IS the
	// canonical evidence; embedded T0 detail contradicts it.
	if providerEvidence && (e.Payload.Redacted != nil || e.Payload.Digest != nil || e.Payload.Opaque != nil) &&
		(e.T0Event.RawRef != "" || e.T0Event.ToolName != "" || e.T0Event.ApprovalID != "" || e.T0Event.Text != "" || len(e.T0Event.Metadata) != 0) {
		return errors.New("timeline: envelope payload variant cannot carry wrapped T0 detail")
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
// source identity tuple only. CanonicalDigest is intentionally not an input: a
// different digest under the same identity is corruption, not a new event.
// Timestamps and append sequence are absent by design.
func (e Envelope) ComputedEventID() (string, error) {
	return digestHex("timeline/event-id/v1", []string{
		e.Provider, e.SourceIncarnation, e.SourceIdentity.Kind, e.SourceIdentity.ID,
		e.SourcePosition,
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

// validate reports whether the selected source is provider-native. Exactly one
// source reference is required, scoped to the same managed runtime generation
// as its envelope, and bounded like every other reference.
func (s EvidenceSources) validate(e Envelope) (bool, error) {
	type source struct {
		name     string
		id       string
		scope    Scope
		provider bool
		present  bool
	}
	sources := []source{
		{name: "provider", provider: true, present: s.Provider != nil},
		{name: "runtime", present: s.Runtime != nil},
		{name: "approval", present: s.Approval != nil},
		{name: "input", present: s.Input != nil},
		{name: "workspace", present: s.Workspace != nil},
		{name: "coordination", present: s.Coordination != nil},
	}
	if s.Provider != nil {
		sources[0].id, sources[0].scope = s.Provider.ID, s.Provider.Scope
	}
	if s.Runtime != nil {
		sources[1].id, sources[1].scope = s.Runtime.ID, s.Runtime.Scope
	}
	if s.Approval != nil {
		sources[2].id, sources[2].scope = s.Approval.ID, s.Approval.Scope
	}
	if s.Input != nil {
		sources[3].id, sources[3].scope = s.Input.ID, s.Input.Scope
	}
	if s.Workspace != nil {
		sources[4].id, sources[4].scope = s.Workspace.ID, s.Workspace.Scope
	}
	if s.Coordination != nil {
		sources[5].id, sources[5].scope = s.Coordination.ID, s.Coordination.Scope
	}
	var selected *source
	for i := range sources {
		if !sources[i].present {
			continue
		}
		if selected != nil {
			return false, errors.New("timeline: exactly one evidence source is required")
		}
		selected = &sources[i]
	}
	if selected == nil {
		return false, errors.New("timeline: exactly one evidence source is required")
	}
	if selected.id == "" || len(selected.id) > MaxReferenceBytes || selected.scope.SessionID != e.SessionID || selected.scope.RuntimeID != e.RuntimeID || selected.scope.LaunchGeneration != e.LaunchGeneration {
		return false, fmt.Errorf("timeline: %s evidence scope mismatch", selected.name)
	}
	return selected.provider, nil
}

func (e Envelope) validateWrappedT0() error {
	if e.T0Event.SessionID != e.SessionID || e.T0Event.AgentKind != e.Provider {
		return errors.New("timeline: wrapped T0 event scope does not match envelope")
	}
	if e.T0Event.ID == "" || len(e.T0Event.ID) > MaxReferenceBytes {
		return errors.New("timeline: wrapped T0 event id is required")
	}
	if !eventKindMatchesT0(e.EventKind, e.T0Event.Type) {
		return errors.New("timeline: event kind contradicts wrapped T0 event type")
	}
	// Bound T0 fields before payload validation so bounds apply to every
	// provider-native envelope regardless of payload variant.
	if len(e.T0Event.Text) > MaxPayloadBytes {
		return errors.New("timeline: T0Event.Text exceeds payload bound")
	}
	if len(e.T0Event.ToolName) > MaxReferenceBytes {
		return errors.New("timeline: T0Event.ToolName exceeds reference bound")
	}
	if len(e.T0Event.ApprovalID) > MaxReferenceBytes {
		return errors.New("timeline: T0Event.ApprovalID exceeds reference bound")
	}
	if len(e.T0Event.RawRef) > MaxReferenceBytes {
		return errors.New("timeline: T0Event.RawRef exceeds reference bound")
	}
	if len(e.T0Event.Provenance) > MaxReferenceBytes {
		return errors.New("timeline: T0Event.Provenance exceeds reference bound")
	}
	if len(string(e.T0Event.Source)) > MaxReferenceBytes {
		return errors.New("timeline: T0Event.Source exceeds reference bound")
	}
	if len(e.T0Event.Metadata) > MaxMetadataKeys {
		return errors.New("timeline: T0Event.Metadata exceeds key count bound")
	}
	for k, v := range e.T0Event.Metadata {
		if len(k) > MaxReferenceBytes || len(v) > MaxReferenceBytes {
			return errors.New("timeline: T0Event.Metadata key or value exceeds reference bound")
		}
	}
	return nil
}

func zeroT0Event(event agent.AgentEvent) bool {
	return event.ID == "" && event.SessionID == "" && event.AgentKind == "" && event.Type == "" &&
		event.Seq == 0 && event.Timestamp.IsZero() && event.Text == "" && event.ToolName == "" &&
		event.ApprovalID == "" && event.RawRef == "" && event.Confidence == 0 && event.Source == "" &&
		event.Provenance == "" && len(event.Metadata) == 0
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

func eventKindMatchesT0(kind EventKind, t agent.AgentEventType) bool {
	switch kind {
	case EventProviderInvocationStarted, EventCorrelationEstablished:
		return t == agent.EventAgentStarted
	case EventProviderInvocationFinished:
		return t == agent.EventCompleted || t == agent.EventFailed || t == agent.EventInterrupted
	case EventThreadObserved:
		return t == agent.EventUserMessage
	case EventTurnObserved:
		return t == agent.EventUserMessage || t == agent.EventAssistantMessage
	case EventToolCallStarted:
		return t == agent.EventToolCallStarted
	case EventToolCallFinished:
		return t == agent.EventToolCallFinished
	case EventApprovalRequested:
		return t == agent.EventApprovalRequested
	case EventApprovalResolved:
		return t == agent.EventApprovalResolved
	case EventStreamObserved:
		return t == agent.EventThinking
	case EventDegraded:
		return t == agent.EventFailed
	case EventEvidenceObserved:
		return t == agent.EventUnknown
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
	// T0Event.Timestamp is intentionally omitted: timestamps are evidence, never
	// identity, ordering, or deduplication authority. Every other T0 field is
	// framed below, including a sorted representation of Metadata.
	fields := []string{fmt.Sprintf("%d", e.SchemaVersion), fmt.Sprintf("%d", e.PayloadVersion), string(e.EventKind), e.SessionID, e.RuntimeID, fmt.Sprintf("%d", e.LaunchGeneration), e.Provider, e.SourceIncarnation, e.SourceIdentity.Kind, e.SourceIdentity.ID, e.SourcePosition, e.RedactionPolicyVersion, e.T0Event.ID, e.T0Event.SessionID, e.T0Event.AgentKind, string(e.T0Event.Type), fmt.Sprintf("%d", e.T0Event.Seq), e.T0Event.Text, e.T0Event.ToolName, e.T0Event.ApprovalID, e.T0Event.RawRef, strconv.FormatFloat(e.T0Event.Confidence, 'g', -1, 64), string(e.T0Event.Source), e.T0Event.Provenance}
	keys := make([]string, 0, len(e.T0Event.Metadata))
	for k := range e.T0Event.Metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fields = append(fields, k, e.T0Event.Metadata[k])
	}
	if includeEventID {
		fields = append(fields, e.EventID)
	}
	fields = append(fields, payloadFields(e.Payload)...)
	for _, source := range evidenceSourceFields(e.EvidenceSources) {
		fields = append(fields, source...)
	}
	for _, r := range []*TypedReference{refs.ProviderInvocation, refs.Thread, refs.Turn, refs.ToolCall, refs.ApprovalRequest, refs.Correlation, refs.Causation, refs.Transport, refs.Degraded, refs.Provenance} {
		if r == nil {
			fields = append(fields, "")
		} else {
			fields = append(fields, string(r.Kind), r.ID, r.Scope.SessionID, r.Scope.RuntimeID, fmt.Sprintf("%d", r.Scope.LaunchGeneration))
		}
	}
	return fields
}

func evidenceSourceFields(s EvidenceSources) [][]string {
	fields := make([][]string, 0, 6)
	if s.Provider == nil {
		fields = append(fields, emptyEvidenceSourceField())
	} else {
		fields = append(fields, evidenceSourceField("provider", s.Provider.ID, s.Provider.Scope))
	}
	if s.Runtime == nil {
		fields = append(fields, emptyEvidenceSourceField())
	} else {
		fields = append(fields, evidenceSourceField("runtime", s.Runtime.ID, s.Runtime.Scope))
	}
	if s.Approval == nil {
		fields = append(fields, emptyEvidenceSourceField())
	} else {
		fields = append(fields, evidenceSourceField("approval", s.Approval.ID, s.Approval.Scope))
	}
	if s.Input == nil {
		fields = append(fields, emptyEvidenceSourceField())
	} else {
		fields = append(fields, evidenceSourceField("input", s.Input.ID, s.Input.Scope))
	}
	if s.Workspace == nil {
		fields = append(fields, emptyEvidenceSourceField())
	} else {
		fields = append(fields, evidenceSourceField("workspace", s.Workspace.ID, s.Workspace.Scope))
	}
	if s.Coordination == nil {
		fields = append(fields, emptyEvidenceSourceField())
	} else {
		fields = append(fields, evidenceSourceField("coordination", s.Coordination.ID, s.Coordination.Scope))
	}
	return fields
}

func evidenceSourceField(kind, id string, scope Scope) []string {
	return []string{kind, id, scope.SessionID, scope.RuntimeID, fmt.Sprintf("%d", scope.LaunchGeneration)}
}

func emptyEvidenceSourceField() []string { return []string{"", "", "", "", ""} }

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
