package agent

// ProdDetectionEvidence carries product-boundary signals for agent detection.
// Mirrors DetectionEvidence but with only the fields available at the API layer.
type ProdDetectionEvidence struct {
	ProcessName string
	CWD         string
	TermAdapter string
}

// TermAgentDetector adapts agent-layer detectors to the term.AgentDetector interface.
type TermAgentDetector struct {
	detector *ClaudeDetector
}

func NewTermAgentDetector() *TermAgentDetector {
	return &TermAgentDetector{detector: NewClaudeDetector()}
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
