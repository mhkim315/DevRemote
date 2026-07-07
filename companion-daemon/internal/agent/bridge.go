package agent

// ProdDetectionEvidence carries product-boundary signals for agent detection.
type ProdDetectionEvidence struct {
	ProcessName string
	CWD         string
	TermAdapter string
}

// TermAgentDetector bridges agent-layer detectors to the product telemetry path.
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

	// Status inference from detection confidence.
	status := StatusUnknown
	if id.Confidence >= 0.5 && id.Kind != "unknown" {
		status = StatusWorking
	}
	return id.Kind, string(status), id.Confidence
}

// ParseEvents returns nil — agent events flow through the existing
// production telemetry path (TelemetryService.processSession →
// ResolveAgentLog → ReadNewEvents → EventStore). The agent-layer
// ClaudeParser is connected to that path separately.
func (d *TermAgentDetector) ParseEvents(sessionID string, evidence ProdDetectionEvidence) []AgentEvent {
	return nil
}
