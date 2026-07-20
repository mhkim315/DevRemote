// Package transcript implements T3 Transcript Integration: the versioned,
// bounded, session-isolated Transcript projection that separates primary
// semantic AgentEvent-sourced segments from a generic byte-stream fallback.
//
// Ownership: this package is owned by the Transcript projection layer. It
// consumes the frozen T0 contract.AgentEvent (accepted) and the existing
// Recorder byte stream. It must not modify either source.
//
// Source arbitration (§3.1):
//   - Correctly correlated accepted AgentEvents are the PRIMARY semantic source.
//   - Recorder byte-stream projection is a SEPARATE fallback/degraded channel.
//   - Never merge, correlate by timestamp/text/prompt/CWD, or duplicate.
package transcript

import (
	"time"
)

// ContractVersion identifies this Transcript contract revision.
// Bump only on a deliberate, reviewed contract change.
const ContractVersion = "t3.1"

// ── Segment kind vocabulary ──

// SegmentKind classifies the semantic source of a Transcript segment.
type SegmentKind string

const (
	// KindAgentEvent: projected from an accepted, correlated contract.AgentEvent.
	// This is the primary semantic Transcript source.
	KindAgentEvent SegmentKind = "agent_event"

	// KindTerminalOutput: bounded plain-text output from the byte-stream fallback.
	// Produced only when no correlated AgentEvent source is available.
	KindTerminalOutput SegmentKind = "terminal_output"

	// KindInputBoundary: content-free marker recording that terminal input occurred.
	// Contains NO typed command content, prompt text, or timing that could
	// reconstruct the input. Used for echo-privacy boundaries.
	KindInputBoundary SegmentKind = "input_boundary"

	// KindDegraded: a gap or degradation marker. Produced when projection fails
	// safely (overflow, parse error, unknown event, malformed input).
	KindDegraded SegmentKind = "degraded"

	// KindUI_Omitted: terminal UI region (alternate screen, TUI burst) that
	// could not be safely projected. One bounded marker, never flattened repaint.
	KindUIOmitted SegmentKind = "ui_omitted"

	// KindUnknown: safe fallback for unrecognised input. Preserves ordering
	// without inventing a semantic event.
	KindUnknown SegmentKind = "unknown"
)

// ── Source provenance for Transcript segments ──

// SegmentSource records which projection path produced a segment.
type SegmentSource string

const (
	// SourceAgentEvent: projected from a validated contract.AgentEvent.
	SourceAgentEvent SegmentSource = "agent_event"

	// SourceByteStream: projected from Recorder PTY byte chunks.
	SourceByteStream SegmentSource = "byte_stream"

	// SourceSnapshot: projected from screen snapshot (degraded — PB.4 removed).
	SourceSnapshot SegmentSource = "snapshot_delta"

	// SourceUnknown: source could not be determined.
	SourceUnknown SegmentSource = "unknown"
)

// ── TranscriptSegment ──

// TranscriptSegment is the immutable, ordered unit of a session Transcript.
// It is the public-read projection — it must never expose private agent
// metadata, prompts, thinking, tool I/O, source code, tokens, signatures,
// raw JSONL, or absolute paths.
type TranscriptSegment struct {
	// ID is a stable, unique-per-segment identifier within the session.
	// Derived from content hash + sequence; stable across re-reads.
	ID string `json:"id"`

	// Seq is the immutable per-session monotonic ordering key.
	// Seq is assigned at append time and never reused or reordered.
	Seq int64 `json:"seq"`

	// SessionID is the canonical Pokit session identifier.
	SessionID string `json:"sessionId"`

	// Kind classifies the semantic source of this segment.
	Kind SegmentKind `json:"kind"`

	// Source records which projection path produced this segment.
	Source SegmentSource `json:"source"`

	// Text is the bounded, public-safe display text. Agent-event text is
	// allowlist-projected (no prompts, thinking, commands, secrets).
	// Byte-stream text is ANSI-stripped plain output.
	Text string `json:"text,omitempty"`

	// AgentEventRef is the stable ID of the source contract.AgentEvent,
	// present only for KindAgentEvent segments. It allows correlation
	// without exposing the raw event.
	AgentEventRef string `json:"agentEventRef,omitempty"`

	// AgentKind is the detected agent kind (e.g. "claude", "codex"),
	// present only for KindAgentEvent segments.
	AgentKind string `json:"agentKind,omitempty"`

	// EventType is the original AgentEventType string, present only for
	// KindAgentEvent segments. Consumers use it for display categorization.
	EventType string `json:"eventType,omitempty"`

	// ToolName is the tool name from a tool-call event, present only for
	// KindAgentEvent segments where the original event had a tool name.
	ToolName string `json:"toolName,omitempty"`

	// Confidence is the detection confidence [0,1] from the source event.
	// Only set for KindAgentEvent segments.
	Confidence float64 `json:"confidence,omitempty"`

	// ByteCount is the original byte count of the source chunk, for
	// diagnostics. Never the raw bytes themselves.
	ByteCount int `json:"byteCount,omitempty"`

	// DegradedReason is a bounded diagnostic string set only for
	// KindDegraded segments. It must not contain content, paths, or PII.
	DegradedReason string `json:"degradedReason,omitempty"`

	// ObservedAt is the daemon-local time when the segment was appended
	// to the store. It is display metadata, not ordering authority.
	ObservedAt time.Time `json:"observedAt"`

	// ContractVersion is the Transcript contract version at append time.
	ContractVersion string `json:"contractVersion"`
}

// ── Bounds ──

const (
	// MaxTextBytes is the maximum byte length of a segment's Text field.
	// Longer text is truncated with a truncation marker.
	MaxTextBytes = 32768

	// MaxSegmentsPerSession is the maximum number of segments stored per session.
	// When exceeded, oldest segments are evicted and a gap marker is inserted.
	MaxSegmentsPerSession = 5000

	// MaxTotalBytesPerSession is a soft upper bound on total stored text bytes
	// per session (~16 MB). When exceeded, oldest segments are evicted.
	MaxTotalBytesPerSession = 16 * 1024 * 1024

	// MaxDegradedReasonBytes bounds the diagnostic field to prevent leakage.
	MaxDegradedReasonBytes = 256

	// MaxAgentEventRefBytes bounds the event reference ID.
	MaxAgentEventRefBytes = 128
)

// ── Helper constructors ──

// NewAgentEventSegment creates a KindAgentEvent segment from allowlisted fields.
// It must be called only after field-level validation/redaction.
func NewAgentEventSegment(
	eventID, sessionID, agentKind, eventType string,
	text string,
	toolName string,
	confidence float64,
	observedAt time.Time,
) TranscriptSegment {
	return TranscriptSegment{
		ID:              "", // assigned by store
		Seq:             0,  // assigned by store
		SessionID:       sessionID,
		Kind:            KindAgentEvent,
		Source:          SourceAgentEvent,
		Text:            boundedText(text, MaxTextBytes),
		AgentEventRef:   boundedString(eventID, MaxAgentEventRefBytes),
		AgentKind:       agentKind,
		EventType:       eventType,
		ToolName:        toolName,
		Confidence:      confidence,
		ObservedAt:      observedAt,
		ContractVersion: ContractVersion,
	}
}

// NewTerminalOutputSegment creates a KindTerminalOutput segment from byte-stream input.
func NewTerminalOutputSegment(sessionID, text string, byteCount int, observedAt time.Time) TranscriptSegment {
	return TranscriptSegment{
		ID:              "", // assigned by store
		Seq:             0,  // assigned by store
		SessionID:       sessionID,
		Kind:            KindTerminalOutput,
		Source:          SourceByteStream,
		Text:            boundedText(text, MaxTextBytes),
		ByteCount:       byteCount,
		ObservedAt:      observedAt,
		ContractVersion: ContractVersion,
	}
}

// NewInputBoundarySegment creates a content-free input-boundary marker.
// It contains NO typed command content, prompt text, or timing data.
func NewInputBoundarySegment(sessionID string, observedAt time.Time) TranscriptSegment {
	return TranscriptSegment{
		ID:              "", // assigned by store
		Seq:             0,  // assigned by store
		SessionID:       sessionID,
		Kind:            KindInputBoundary,
		Source:          SourceByteStream,
		ObservedAt:      observedAt,
		ContractVersion: ContractVersion,
	}
}

// NewDegradedSegment creates a KindDegraded segment with a bounded reason.
// reason must not contain content, paths, or PII.
func NewDegradedSegment(sessionID, reason string, observedAt time.Time) TranscriptSegment {
	return TranscriptSegment{
		ID:              "", // assigned by store
		Seq:             0,  // assigned by store
		SessionID:       sessionID,
		Kind:            KindDegraded,
		Source:          SourceUnknown,
		DegradedReason:  boundedString(reason, MaxDegradedReasonBytes),
		ObservedAt:      observedAt,
		ContractVersion: ContractVersion,
	}
}

// NewUIOmittedSegment creates a KindUI_Omitted marker for unprojectable TUI regions.
func NewUIOmittedSegment(sessionID string, observedAt time.Time) TranscriptSegment {
	return TranscriptSegment{
		ID:              "", // assigned by store
		Seq:             0,  // assigned by store
		SessionID:       sessionID,
		Kind:            KindUIOmitted,
		Source:          SourceByteStream,
		ObservedAt:      observedAt,
		ContractVersion: ContractVersion,
	}
}

// ── Internal helpers ──

func boundedText(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes-3] + "..."
}

func boundedString(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes]
}
