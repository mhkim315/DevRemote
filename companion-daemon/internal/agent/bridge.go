package agent

// TermAgentDetector adapts agent-layer detectors to the term.AgentDetector
// interface. Uses evidence-based detection — no hardcoded agent names.
type TermAgentDetector struct {
	detector *ClaudeDetector
}

func NewTermAgentDetector() *TermAgentDetector {
	return &TermAgentDetector{detector: NewClaudeDetector()}
}

// DetectAgent implements term.AgentDetector using evidence-based detection.
// Returns unknown when evidence is insufficient, preventing false positives.
func (d *TermAgentDetector) DetectAgent(sessionID, adapterName, localID string) (agentKind, agentStatus string, agentConfidence float64) {
	// Build evidence from available signals. In production, process name
	// and CWD would come from terminal session metadata (Phase A6+).
	// For now, use adapter name as weak signal — "tmux" hints at terminal usage,
	// but does not identify the agent. Result: unknown with low confidence.
	ev := DetectionEvidence{
		TermAdapter: adapterName,
	}
	id := d.detector.Detect(ev)

	status := StatusUnknown
	if id.Confidence >= 0.5 && id.Kind != "unknown" {
		status = StatusWorking
	}
	return id.Kind, string(status), id.Confidence
}
