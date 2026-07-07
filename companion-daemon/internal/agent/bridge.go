package agent

// TermAgentDetector adapts agent-layer detectors to the term.AgentDetector
// interface. This is the production bridge between Agent Adapter layer and
// Terminal telemetry pipeline.
type TermAgentDetector struct {
	detector *ClaudeDetector
}

// NewTermAgentDetector creates a production agent detector wired with Claude.
// Additional detectors (Codex, Antigravity) can be added as they are built.
func NewTermAgentDetector() *TermAgentDetector {
	return &TermAgentDetector{
		detector: NewClaudeDetector(),
	}
}

// DetectAgent implements the term.AgentDetector interface.
// Uses process-based detection evidence. In production, the caller supplies
// real process name, CWD, and log paths from the terminal session.
func (d *TermAgentDetector) DetectAgent(sessionID, adapterName, localID string) (agentKind, agentStatus string, agentConfidence float64) {
	// Use adapter-gnostic evidence: process name hints from session context.
	// For now, use a simple heuristic. Phase A6+ will add real process evidence.
	ev := DetectionEvidence{
		ProcessName: "claude", // default assumption; real detection needs process info
		TermAdapter: adapterName,
	}
	id := d.detector.Detect(ev)
	status := StatusWorking
	if id.Confidence < 0.5 {
		status = StatusUnknown
	}
	return id.Kind, string(status), id.Confidence
}
