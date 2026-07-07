package agent

// ProdDetectionEvidence carries product-boundary signals for agent detection.
type ProdDetectionEvidence struct {
	ProcessName string
	CWD         string
	TermAdapter string
}

// TermAgentDetector is the composite production agent detector.
// Holds detectors for all known agents and returns the highest-confidence result.
type TermAgentDetector struct {
	claude      *ClaudeDetector
	codex       *CodexDetector
	antigravity *AntigravityDetector
}

func NewTermAgentDetector() *TermAgentDetector {
	return &TermAgentDetector{
		claude:      NewClaudeDetector(),
		codex:       NewCodexDetector(),
		antigravity: NewAntigravityDetector(),
	}
}

func (d *TermAgentDetector) DetectAgent(sessionID, adapterName, localID string, evidence ProdDetectionEvidence) (agentKind, agentStatus string, agentConfidence float64) {
	ev := DetectionEvidence{
		ProcessName: evidence.ProcessName,
		CWD:         evidence.CWD,
		TermAdapter: evidence.TermAdapter,
	}

	// Try all detectors, pick the highest-confidence non-unknown result.
	best := d.claude.Detect(ev)
	if id := d.codex.Detect(ev); id.Confidence > best.Confidence {
		best = id
	}
	if id := d.antigravity.Detect(ev); id.Confidence > best.Confidence {
		best = id
	}

	status := StatusUnknown
	if best.Confidence >= 0.5 && best.Kind != "unknown" {
		status = StatusWorking
	}
	return best.Kind, string(status), best.Confidence
}

// ParseEvents returns nil — agent events flow through the existing
// production telemetry path.
func (d *TermAgentDetector) ParseEvents(sessionID string, evidence ProdDetectionEvidence) []AgentEvent {
	return nil
}
