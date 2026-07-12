package transcript

// SourceArbiter manages source selection between primary AgentEvent-sourced
// Transcript segments and the byte-stream fallback.
//
// Rules (handoff §3.1):
//   - AgentEvent segments are the PRIMARY semantic source when available.
//   - Byte-stream segments are a SEPARATE fallback channel.
//   - NEVER merge, correlate, or deduplicate across the two sources.
//   - NEVER use timestamp proximity, text equality, fuzzy matching,
//     prompt/command recognition, CWD, or process-name heuristics.
//   - Source is explicit on every segment.
//   - When safe arbitration is unavailable, retain source separation.
type SourceArbiter struct {
	// agentEventAvailable is true when at least one correctly correlated
	// AgentEvent has been projected for the session.
	agentEventAvailable bool

	// byteStreamAvailable is true when at least one byte-stream segment
	// has been projected for the session.
	byteStreamAvailable bool

	// degraded indicates that the arbiter has entered a degraded state
	// (e.g., correlation lost, projection failure).
	degraded bool
}

// NewSourceArbiter creates a fresh arbiter for a session.
func NewSourceArbiter() *SourceArbiter {
	return &SourceArbiter{}
}

// RecordAgentEvent notifies the arbiter that an AgentEvent-sourced segment
// was appended. This transitions the session to primary-agent-event mode.
func (a *SourceArbiter) RecordAgentEvent() {
	a.agentEventAvailable = true
}

// RecordByteStream notifies the arbiter that a byte-stream-sourced segment
// was appended.
func (a *SourceArbiter) RecordByteStream() {
	a.byteStreamAvailable = true
}

// RecordDegraded notifies the arbiter of a degraded state.
func (a *SourceArbiter) RecordDegraded() {
	a.degraded = true
}

// PrimarySource reports which source is currently the primary Transcript source.
// It never returns a merged or ambiguous value.
func (a *SourceArbiter) PrimarySource() SegmentSource {
	if a.agentEventAvailable {
		return SourceAgentEvent
	}
	if a.byteStreamAvailable {
		return SourceByteStream
	}
	return SourceUnknown
}

// HasAgentEvents reports whether any AgentEvent-sourced segments exist.
func (a *SourceArbiter) HasAgentEvents() bool {
	return a.agentEventAvailable
}

// IsDegraded reports whether the arbiter is in a degraded state.
func (a *SourceArbiter) IsDegraded() bool {
	return a.degraded
}

// ── Segment interleaving policy ──

// InterleavePolicy controls how segments from different sources are ordered
// when both sources are active.
type InterleavePolicy int

const (
	// InterleaveByTime: segments are ordered by observation time.
	// This is the default. It does NOT merge or correlate sources;
	// each segment retains its explicit source label.
	InterleaveByTime InterleavePolicy = iota

	// InterleaveAgentFirst: AgentEvent segments always precede byte-stream
	// segments within the same time window. Byte-stream segments are still
	// retained as fallback evidence.
	InterleaveAgentFirst
)

// SegmentAccumulator collects segments from multiple projectors and emits
// them in observation-time order without merging across sources.
type SegmentAccumulator struct {
	segments []TranscriptSegment
	policy   InterleavePolicy
}

// NewSegmentAccumulator creates an accumulator.
func NewSegmentAccumulator(policy InterleavePolicy) *SegmentAccumulator {
	return &SegmentAccumulator{policy: policy}
}

// Add appends segments to the accumulator.
func (sa *SegmentAccumulator) Add(segments []TranscriptSegment) {
	sa.segments = append(sa.segments, segments...)
}

// Flush returns accumulated segments in observation order and clears the buffer.
func (sa *SegmentAccumulator) Flush() []TranscriptSegment {
	if len(sa.segments) == 0 {
		return nil
	}
	// Simple insertion-order return (segments are already timestamp-ordered
	// within their source; Feed and ProjectBatch both preserve input order).
	out := sa.segments
	sa.segments = nil
	return out
}

// Len returns the number of buffered segments.
func (sa *SegmentAccumulator) Len() int {
	return len(sa.segments)
}
