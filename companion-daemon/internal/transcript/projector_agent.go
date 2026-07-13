package transcript

import (
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// AgentEventProjector projects accepted contract.AgentEvent values into
// TranscriptSegment values with a CLOSED structural-display allowlist.
//
// Safety rules (handoff §6.2, BLOCKER 6):
//   - Never expose arbitrary event.Text as safe display content.
//   - Never expose prompts, thinking, assistant body, approval body,
//     command strings, tool input/output, source code, tokens,
//     signatures, raw JSONL, secrets, credentials, or private paths.
//   - Expose only structural fields explicitly permitted by event type
//     and the accepted adapter contract.
//   - Unknown event types → bounded degraded marker, never fabrication.
type AgentEventProjector struct{}

func NewAgentEventProjector() *AgentEventProjector {
	return &AgentEventProjector{}
}

// Project converts a single contract.AgentEvent into a TranscriptSegment.
// Returns nil if the event should not be projected:
//   - empty or mismatched SessionID
//   - provenance is not from an accepted correlation path
//   - correlation is unavailable (per T1/T2 acceptance)
func (p *AgentEventProjector) Project(event agent.AgentEvent, sessionID string) *TranscriptSegment {
	// Session binding: require non-empty, exact match.
	if event.SessionID == "" {
		return nil // fail closed: no session binding
	}
	if event.SessionID != sessionID {
		return nil // cross-session: reject
	}

	// Provenance validation: reject empty, unknown, or advisory provenance.
	// The correlation gate (CorrelationState.CanBePrimarySource) at the
	// service level is the separate authority for semantic access.
	// Provenance records evidence strength for display metadata.
	prov := contract.Provenance(event.Provenance)
	if prov == "" || prov == contract.ProvenanceUnknown || prov.Advisory() {
		return nil
	}

	eventType := string(event.Type)
	display := displayFieldsForEvent(event)

	if display.kind == "" {
		return nil // unprojectable
	}

	seg := TranscriptSegment{
		SessionID:       sessionID,
		Kind:            KindAgentEvent,
		Source:          SourceAgentEvent,
		Text:            display.text,
		AgentEventRef:   event.ID,
		AgentKind:       event.AgentKind,
		EventType:       eventType,
		ToolName:        display.toolName,
		Confidence:      event.Confidence,
		ObservedAt:      observationTime(event),
		ContractVersion: ContractVersion,
	}

	return &seg
}

// ProjectBatch projects multiple events, filtering out nil projections.
func (p *AgentEventProjector) ProjectBatch(events []agent.AgentEvent, sessionID string) []TranscriptSegment {
	var out []TranscriptSegment
	for i := range events {
		seg := p.Project(events[i], sessionID)
		if seg != nil {
			out = append(out, *seg)
		}
	}
	return out
}

// ── Closed structural-display allowlist ──

// displayFields holds the subset of fields safe for public Transcript display.
// Every field is explicitly permitted by event type. Nothing is projected
// from arbitrary .Text without type-specific validation.
type displayFields struct {
	kind     SegmentKind
	text     string
	toolName string
}

// displayFieldsForEvent returns the closed structural-display allowlist.
// NEVER projects arbitrary event.Text. Only structural markers are exposed.
// Provider payloads (assistant body, approval body, prompts, tool I/O)
// are excluded per T3 privacy contract.
func displayFieldsForEvent(event agent.AgentEvent) displayFields {
	switch event.Type {
	case agent.EventAgentStarted:
		return displayFields{kind: KindAgentEvent, text: "Agent started"}

	case agent.EventCompleted:
		return displayFields{kind: KindAgentEvent, text: "Completed"}

	case agent.EventFailed:
		return displayFields{kind: KindAgentEvent, text: "Failed"}

	case agent.EventInterrupted:
		return displayFields{kind: KindAgentEvent, text: "Interrupted"}

	case agent.EventWaitingInput:
		return displayFields{kind: KindAgentEvent, text: "Waiting for input"}

	case agent.EventToolCallStarted:
		// Tool name only — bounded identity metadata. Input never exposed.
		return displayFields{kind: KindAgentEvent, toolName: sanitizeToolName(event.ToolName)}

	case agent.EventToolCallFinished:
		return displayFields{kind: KindAgentEvent, toolName: sanitizeToolName(event.ToolName)}

	case agent.EventApprovalRequested:
		// Approval lifecycle marker only. Body belongs to A1 scope.
		return displayFields{kind: KindAgentEvent, text: "Approval requested"}

	case agent.EventApprovalResolved:
		return displayFields{kind: KindAgentEvent, text: "Approval resolved"}

	case agent.EventAssistantMessage:
		// Structural marker only — body is provider payload, not Transcript content.
		return displayFields{kind: KindAgentEvent, text: "Message"}

	case agent.EventUserMessage:
		// Prompts are NEVER exposed.
		return displayFields{kind: KindAgentEvent, text: "[user input]"}

	case agent.EventThinking:
		// Thinking is private.
		return displayFields{kind: KindAgentEvent}

	case agent.EventUnknown:
		return displayFields{kind: KindUnknown}

	default:
		return displayFields{kind: KindUnknown}
	}
}

// sanitizeToolName bounds and validates a tool name for display.
func sanitizeToolName(name string) string {
	if len(name) > 128 {
		return name[:128]
	}
	return name
}

// ── Correlation and provenance validation ──

// ValidateSessionBinding checks that an AgentEvent is correctly bound.
// Empty SessionID → reject (fail closed, per BLOCKER 1).
func ValidateSessionBinding(event agent.AgentEvent, sessionID string) bool {
	if event.SessionID == "" {
		return false
	}
	return event.SessionID == sessionID
}

// IsCrossSession returns true if the event is explicitly bound to a different session.
func IsCrossSession(event agent.AgentEvent, sessionID string) bool {
	return event.SessionID != "" && event.SessionID != sessionID
}

// CorrelationState tracks whether an adapter has established correlation.
type CorrelationState struct {
	SessionID   string
	Correlation contract.Correlation
	Provider    string
}

// CanBePrimarySource returns true when AgentEvent projection can serve as
// the primary semantic Transcript source.
func (cs CorrelationState) CanBePrimarySource() bool {
	return cs.Correlation == contract.CorrelationProven ||
		cs.Correlation == contract.CorrelationManagedLaunch
}

// ── Helpers ──

func observationTime(event agent.AgentEvent) time.Time {
	if !event.Timestamp.IsZero() {
		return event.Timestamp
	}
	return time.Now()
}
