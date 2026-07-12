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
	agentEventAvailable bool
	byteStreamSuppressed int64
	degraded             bool
	correlation          CorrelationState // set via SetCorrelation
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
// suppressed from the semantic listing (routed to fallback channel instead).
func (a *SourceArbiter) SuppressByteStream() bool {
	return a.agentEventAvailable
}

// ── Transcript response envelope ──

// TranscriptResponse is the API response envelope that separates the
// primary semantic Transcript from the byte-stream fallback channel.
// When AgentEvent is primary, semantic contains AgentEvent segments
// and fallback contains suppressed byte-stream segments.
// When AgentEvent is unavailable, semantic contains byte-stream segments
// and fallback is empty.
type TranscriptResponse struct {
	// SessionID is the canonical Pokit session identifier.
	SessionID string `json:"sessionId"`

	// Semantic contains the primary semantic Transcript segments.
	// When AgentEvent is primary: AgentEvent-sourced segments.
	// When AgentEvent unavailable: byte-stream terminal output.
	Semantic []TranscriptSegment `json:"semantic"`

	// Fallback contains the separate byte-stream fallback channel.
	// Non-nil only when AgentEvent is primary and byte-stream segments exist.
	Fallback []TranscriptSegment `json:"fallback,omitempty"`

	// PrimarySource declares the current primary source.
	PrimarySource SegmentSource `json:"primarySource"`

	// ContractVersion is the Transcript contract version.
	ContractVersion string `json:"contractVersion"`
}

// NewTranscriptResponse builds the separated response from stored segments
// and the arbiter's state.
func NewTranscriptResponse(sessionID string, allSegments []TranscriptSegment, arb *SourceArbiter) TranscriptResponse {
	if arb == nil || !arb.HasAgentEvents() {
		// No AgentEvent primary: all segments are semantic (byte-stream fallback).
		if allSegments == nil {
			allSegments = []TranscriptSegment{}
		}
		return TranscriptResponse{
			SessionID:       sessionID,
			Semantic:        allSegments,
			PrimarySource:   SourceByteStream,
			ContractVersion: ContractVersion,
		}
	}

	// AgentEvent is primary: split into semantic and fallback channels.
	var semantic, fallback []TranscriptSegment
	for _, seg := range allSegments {
		switch seg.Kind {
		case KindAgentEvent, KindUnknown:
			semantic = append(semantic, seg)
		case KindTerminalOutput, KindDegraded, KindUIOmitted, KindInputBoundary:
			fallback = append(fallback, seg)
		default:
			semantic = append(semantic, seg)
		}
	}

	if semantic == nil {
		semantic = []TranscriptSegment{}
	}

	return TranscriptResponse{
		SessionID:       sessionID,
		Semantic:        semantic,
		Fallback:        fallback,
		PrimarySource:   SourceAgentEvent,
		ContractVersion: ContractVersion,
	}
}
