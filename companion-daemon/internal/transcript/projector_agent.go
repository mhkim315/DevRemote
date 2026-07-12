package transcript

import (
	"strings"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// AgentEventProjector projects accepted contract.AgentEvent values into
// TranscriptSegment values with strict field allowlisting.
//
// Safety rules:
//   - Never expose prompts, thinking, command strings, tool input/output,
//     source code, tokens, signatures, raw JSONL, or private absolute paths.
//   - Text is bounded after projection.
//   - Only allowlisted fields are copied.
//   - Unknown event types produce KindUnknown, not a fabricated event.
type AgentEventProjector struct {
	// allowAgentKinds is the set of agent kinds accepted for projection.
	// Empty means all kinds are accepted.
	allowAgentKinds map[string]bool
}

// NewAgentEventProjector creates a projector with default safety settings.
func NewAgentEventProjector() *AgentEventProjector {
	return &AgentEventProjector{}
}

// Project converts a single contract.AgentEvent into a TranscriptSegment.
// Returns nil if the event should not be projected (e.g., private-only content).
func (p *AgentEventProjector) Project(event agent.AgentEvent, sessionID string) *TranscriptSegment {
	// Correlation guard: only project events bound to the requested session.
	if event.SessionID != "" && event.SessionID != sessionID {
		return nil
	}

	// Allowlist check.
	if len(p.allowAgentKinds) > 0 && !p.allowAgentKinds[event.AgentKind] {
		return nil
	}

	// Field allowlisting: extract only safe display fields.
	text := p.safeText(event)
	eventType := string(event.Type)

	seg := NewAgentEventSegment(
		event.ID,
		sessionID,
		event.AgentKind,
		eventType,
		text,
		p.safeToolName(event),
		event.Confidence,
		p.observationTime(event),
	)

	// Source arbitration: provenance-based gating.
	// Advisory provenance events get lower confidence display but are still projected.
	prov := contract.Provenance(event.Provenance)
	if prov.Advisory() {
		// Advisory events are projected but at reduced confidence.
		if seg.Confidence > 0.5 {
			seg.Confidence = 0.49
		}
	}

	return &seg
}

// ProjectBatch projects multiple events, filtering out nil projections.
// Order is preserved.
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

// safeText extracts bounded, safe display text from an AgentEvent.
// Redaction rules:
//   - assistant_message: Text is allowed (already redacted by adapter)
//   - user_message: Text is allowed only if it's a summary, not raw input
//   - thinking: Text is dropped (private)
//   - tool_call_started/finished: ToolName only, no input/output
//   - approval_requested/resolved: Text carries the prompt only
//   - agent_started/completed/failed/interrupted: metadata only
//   - unknown: empty text
func (p *AgentEventProjector) safeText(event agent.AgentEvent) string {
	switch event.Type {
	case agent.EventAssistantMessage:
		// Assistant text is already redacted by the adapter (no private content).
		return strings.TrimSpace(event.Text)

	case agent.EventUserMessage:
		// User message text may contain prompts. We redact aggressively:
		// only allow if it's clearly a summary (short, no code-like content).
		text := strings.TrimSpace(event.Text)
		if looksLikeCode(text) || len(text) > 500 {
			return "[user input]"
		}
		return text

	case agent.EventThinking:
		// Thinking is private — never expose.
		return ""

	case agent.EventToolCallStarted, agent.EventToolCallFinished:
		// Tool calls: name only, never input/output.
		return ""

	case agent.EventApprovalRequested:
		// Approval prompt is public (shown to mobile user for decision).
		return strings.TrimSpace(event.Text)

	case agent.EventApprovalResolved:
		// Resolution carries no new public text.
		return ""

	case agent.EventAgentStarted:
		return "Agent started"

	case agent.EventCompleted:
		return "Completed"

	case agent.EventFailed:
		return "Failed"

	case agent.EventInterrupted:
		return "Interrupted"

	case agent.EventWaitingInput:
		return "Waiting for input"

	case agent.EventUnknown:
		return ""

	default:
		return ""
	}
}

// safeToolName returns the tool name only if it is safe for display.
// Tool names are identity metadata, not content.
func (p *AgentEventProjector) safeToolName(event agent.AgentEvent) string {
	switch event.Type {
	case agent.EventToolCallStarted, agent.EventToolCallFinished:
		return event.ToolName
	}
	return ""
}

// observationTime returns the event timestamp or current time as fallback.
func (p *AgentEventProjector) observationTime(event agent.AgentEvent) time.Time {
	if !event.Timestamp.IsZero() {
		return event.Timestamp
	}
	return time.Now()
}

// ── Content safety heuristics ──

// looksLikeCode returns true if text appears to contain code (source code, shell
// commands, JSON). Used to redact potentially sensitive user input that may
// contain API keys, tokens, or private paths.
func looksLikeCode(text string) bool {
	// Heuristic: code-like content has certain structural markers.
	indicators := []string{
		"func ", "def ", "class ", "import ", "package ",
		"#!/", "```", "curl ", "wget ",
		"export ", "sudo ", "apt-get", "npm ", "yarn ",
		"git clone", "git push", "ssh ", "scp ",
		`"`, "{", "}", "[", "]", "()", "=>",
		"SELECT ", "INSERT ", "UPDATE ", "DELETE FROM",
	}
	count := 0
	for _, ind := range indicators {
		if strings.Contains(text, ind) {
			count++
		}
	}
	// If 3+ code indicators present, treat as code.
	return count >= 3
}

// ── Correlation and provenance validation ──

// ValidateSessionBinding checks that an AgentEvent is correctly bound to the
// requested session. Returns true if the event can be projected.
//
// Rules:
//   - Empty SessionID: accept (legacy events without explicit binding)
//   - Matching SessionID: accept
//   - Mismatched SessionID: reject (cross-session event)
func ValidateSessionBinding(event agent.AgentEvent, sessionID string) bool {
	if event.SessionID == "" {
		return true // legacy tolerance
	}
	return event.SessionID == sessionID
}

// IsCrossSession returns true if the event is explicitly bound to a different session.
func IsCrossSession(event agent.AgentEvent, sessionID string) bool {
	return event.SessionID != "" && event.SessionID != sessionID
}

// ── Correlation-based source selection ──

// CorrelationState tracks whether an adapter has established correlation
// for a session. Only Proven or ManagedLaunch correlation allows AgentEvent
// as the primary source.
type CorrelationState struct {
	SessionID   string
	Correlation contract.Correlation
	Provider    string
}

// CanBePrimarySource returns true when AgentEvent projection can serve as
// the primary semantic Transcript source for this session.
func (cs CorrelationState) CanBePrimarySource() bool {
	return cs.Correlation == contract.CorrelationProven ||
		cs.Correlation == contract.CorrelationManagedLaunch
}
