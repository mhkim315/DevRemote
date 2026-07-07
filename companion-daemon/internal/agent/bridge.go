package agent

// ProdDetectionEvidence carries product-boundary signals for agent detection.
type ProdDetectionEvidence struct {
	ProcessName string
	CWD         string
	TermAdapter string
}

// TermAgentDetector adapts agent-layer detectors to the term.AgentDetector interface.
type TermAgentDetector struct {
	detector *ClaudeDetector
	parser   *ClaudeParser
}

func NewTermAgentDetector() *TermAgentDetector {
	return &TermAgentDetector{
		detector: NewClaudeDetector(),
		parser:   NewClaudeParser(),
	}
}

func (d *TermAgentDetector) DetectAgent(sessionID, adapterName, localID string, evidence ProdDetectionEvidence) (agentKind, agentStatus string, agentConfidence float64) {
	ev := DetectionEvidence{
		ProcessName: evidence.ProcessName,
		CWD:         evidence.CWD,
		TermAdapter: evidence.TermAdapter,
	}
	id := d.detector.Detect(ev)

	status := StatusUnknown
	if id.Confidence >= 0.5 && id.Kind != "unknown" {
		status = StatusWorking
	}
	return id.Kind, string(status), id.Confidence
}

// ParseEvents reads Claude log data and returns parsed common events.
func (d *TermAgentDetector) ParseEvents(sessionID string, evidence ProdDetectionEvidence) []AgentEvent {
	// Use Claude A1 fixtures as event source (production would read from log files).
	// For now, return events based on detection evidence.
	if evidence.ProcessName != "claude" {
		return nil
	}
	// Return a placeholder event showing the parser is wired.
	return []AgentEvent{{
		AgentKind: "claude",
		Type:      EventAgentStarted,
		Source:    SourceJSONL,
	}}
}
