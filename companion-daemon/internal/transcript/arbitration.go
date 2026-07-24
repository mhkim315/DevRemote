package transcript

// SourceArbiter enforces source separation at the storage boundary.
//
// Rules (handoff §3.1, BLOCKER 2):
//   - Correlated AgentEvents are the PRIMARY semantic Transcript source.
//   - Byte-stream projection is a SEPARATE fallback/degraded channel.
//   - When AgentEvent is primary, byte-stream segments are suppressed
//     from the semantic Transcript listing and routed to a separate
//     fallback channel.
//   - NEVER merge, correlate, or deduplicate across sources.
//   - NEVER use timestamp proximity, text equality, fuzzy matching,
//     prompt/command recognition, CWD, or process-name heuristics.
//   - Source is explicit on every segment.
type SourceArbiter struct {
	agentEventAvailable  bool
	byteStreamSuppressed int64
	degraded             bool
	permanentSuppress    bool // true after input causes permanent byte-stream suppression
	correlation          CorrelationState
}

func NewSourceArbiter() *SourceArbiter {
	return &SourceArbiter{}
}

// SetCorrelation records the session correlation state. Only CorrelationProven
// or CorrelationManagedLaunch can enable AgentEvent as primary source.
func (a *SourceArbiter) SetCorrelation(cs CorrelationState) {
	a.correlation = cs
}

// CanBePrimarySource returns true only when correlation is explicitly proven.
func (a *SourceArbiter) CanBePrimarySource() bool {
	return a.correlation.CanBePrimarySource()
}

// RecordAgentEvent marks that a correlated AgentEvent has been projected.
func (a *SourceArbiter) RecordAgentEvent() {
	a.agentEventAvailable = true
}

// RecordByteStream marks a byte-stream segment appended before AgentEvent primary.
func (a *SourceArbiter) RecordByteStream() {
	// When AgentEvent is not yet primary, byte-stream segments are the only content.
	// After AgentEvent becomes primary, this is a no-op (segments get suppressed).
}

// RecordByteStreamSuppressed increments the suppressed byte-stream counter.
func (a *SourceArbiter) RecordByteStreamSuppressed() {
	a.byteStreamSuppressed++
}

// RecordDegraded marks degraded state.
func (a *SourceArbiter) RecordDegraded() {
	a.degraded = true
}

// PrimarySource reports the current primary semantic source.
func (a *SourceArbiter) PrimarySource() SegmentSource {
	if a.agentEventAvailable {
		return SourceAgentEvent
	}
	return SourceByteStream
}

// HasAgentEvents reports whether AgentEvent-sourced segments exist.
func (a *SourceArbiter) HasAgentEvents() bool {
	return a.agentEventAvailable
}

// IsDegraded reports degraded state.
func (a *SourceArbiter) IsDegraded() bool {
	return a.degraded
}

// SuppressByteStream reports whether new byte-stream segments should be
// suppressed from the semantic listing.
func (a *SourceArbiter) SuppressByteStream() bool {
	return a.agentEventAvailable
}

// MarkByteStreamSuppressed records that byte-stream projection is permanently
// suppressed (e.g. after terminal input for echo privacy).
func (a *SourceArbiter) MarkByteStreamSuppressed() {
	a.permanentSuppress = true
}

// IsByteStreamSuppressed reports permanent suppression state.
func (a *SourceArbiter) IsByteStreamSuppressed() bool {
	return a.permanentSuppress
}

// ── Transcript response envelope ──

// TranscriptResponse is the API response envelope.
type TranscriptResponse struct {
	SessionID     string              `json:"sessionId"`
	Generation    int64               `json:"generation,omitempty"` // PA3 Step 6: per-session Transcript generation
	Semantic      []TranscriptSegment `json:"semantic"`
	Fallback      []TranscriptSegment `json:"fallback,omitempty"`
	PrimarySource SegmentSource       `json:"primarySource"`
	// ByteStreamSuppressed is true when byte-stream projection has been
	// permanently suppressed (e.g. after terminal input for echo privacy).
	// The UI should indicate that live terminal output is not available
	// in the Transcript.
	ByteStreamSuppressed bool `json:"byteStreamSuppressed,omitempty"`
	// R3: server-authoritative transcript availability state. The client
	// must display truthful messaging per state and must not infer state
	// from segment count, adapter label, or connection status.
	Availability    TranscriptAvailability `json:"availability"`
	ContractVersion string                 `json:"contractVersion"`
}

// NewTranscriptResponse builds the separated response.
// Rules:
//   - SourceAgentEvent segments → always semantic
//   - SourceByteStream segments → semantic when primary, fallback when AgentEvent primary
//   - SourceSnapshot segments → always fallback (degraded channel)
//   - primarySource reflects actual dominant source
//   - availability is the R3 server-authoritative surface state
func NewTranscriptResponse(sessionID string, allSegments []TranscriptSegment, arb *SourceArbiter, availability TranscriptAvailability) TranscriptResponse {
	if allSegments == nil {
		allSegments = []TranscriptSegment{}
	}

	hasAgentEvent := arb != nil && arb.HasAgentEvents()
	hasSnapshot := false
	hasByteStream := false

	// Separate by source.
	var semantic, fallback []TranscriptSegment
	for _, seg := range allSegments {
		switch seg.Source {
		case SourceAgentEvent:
			semantic = append(semantic, seg)
		case SourceSnapshot:
			fallback = append(fallback, seg)
			hasSnapshot = true
		case SourceByteStream:
			hasByteStream = true
			if hasAgentEvent {
				fallback = append(fallback, seg)
			} else {
				semantic = append(semantic, seg)
			}
		default:
			if hasAgentEvent {
				fallback = append(fallback, seg)
			} else {
				semantic = append(semantic, seg)
			}
		}
	}

	if semantic == nil {
		semantic = []TranscriptSegment{}
	}

	// Determine primary source.
	primary := SourceUnknown
	switch {
	case hasAgentEvent:
		primary = SourceAgentEvent
	case hasByteStream:
		primary = SourceByteStream
	case hasSnapshot:
		primary = SourceSnapshot
	}

	suppressed := arb != nil && arb.IsByteStreamSuppressed()

	return TranscriptResponse{
		SessionID:            sessionID,
		Semantic:             semantic,
		Fallback:             fallback,
		PrimarySource:        primary,
		ByteStreamSuppressed: suppressed,
		Availability:         availability,
		ContractVersion:      ContractVersion,
	}
}
