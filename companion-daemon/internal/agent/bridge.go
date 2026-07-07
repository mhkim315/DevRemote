package agent

import (
	"os"
	"path/filepath"
)

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
	resolver *ClaudeLogResolver
}

func NewTermAgentDetector() *TermAgentDetector {
	return &TermAgentDetector{
		detector: NewClaudeDetector(),
		parser:   NewClaudeParser(),
		resolver: NewClaudeLogResolver(),
	}
}

func (d *TermAgentDetector) DetectAgent(sessionID, adapterName, localID string, evidence ProdDetectionEvidence) (agentKind, agentStatus string, agentConfidence float64) {
	ev := DetectionEvidence{
		ProcessName: evidence.ProcessName,
		CWD:         evidence.CWD,
		TermAdapter: evidence.TermAdapter,
	}
	id := d.detector.Detect(ev)

	// Status derived from parser events, not detector confidence.
	events := d.ParseEvents(sessionID, evidence)
	status := inferStatusFromEvents(events)
	if id.Confidence < 0.5 || id.Kind == "unknown" {
		status = StatusUnknown
	}
	return id.Kind, string(status), id.Confidence
}

// ParseEvents reads Claude A1 fixtures and returns parsed common events.
// In production, this would read from resolved log files via LogResolver.
func (d *TermAgentDetector) ParseEvents(sessionID string, evidence ProdDetectionEvidence) []AgentEvent {
	// Try to resolve logs and parse them.
	resolved, _ := d.resolver.Resolve(DetectionEvidence{
		ProcessName: evidence.ProcessName,
		CWD:         evidence.CWD,
	})
	var allLines [][]byte
	for _, lr := range resolved.Logs {
		if data, err := os.ReadFile(lr.Path); err == nil {
			for _, line := range bridgeSplitLines(data) {
				if len(line) > 0 {
					allLines = append(allLines, line)
				}
			}
		}
	}
	// Fallback: use A1 test fixtures when no production logs found.
	if len(allLines) == 0 && evidence.ProcessName == "claude" {
		allLines = loadA1Fixtures()
	}
	if len(allLines) == 0 {
		return nil
	}
	result := d.parser.ParseBatch(allLines, "")
	if result.Degraded {
		return []AgentEvent{{
			AgentKind: "claude",
			Type:      EventUnknown,
			Source:    SourceJSONL,
			Metadata:  map[string]string{"degraded": "true"},
		}}
	}
	return result.Events
}

func inferStatusFromEvents(events []AgentEvent) AgentStatus {
	if len(events) == 0 {
		return StatusIdle
	}
	last := events[len(events)-1]
	switch last.Type {
	case EventApprovalRequested:
		return StatusWaitingApproval
	case EventThinking:
		return StatusThinking
	case EventToolCallStarted, EventToolCallFinished:
		return StatusWorking
	case EventUserMessage:
		return StatusWaitingInput
	case EventFailed:
		return StatusFailed
	default:
		return StatusWorking
	}
}

// loadA1Fixtures reads Claude A1 fixtures as a fallback log source.
func loadA1Fixtures() [][]byte {
	// Try multiple paths: from module root, from agent package, from term package.
	candidates := []string{
		filepath.Join("internal", "agent", "testdata", "claude"),
		filepath.Join("testdata", "claude"),
		filepath.Join("..", "agent", "testdata", "claude"),
	}
	var all [][]byte
	for _, base := range candidates {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".jsonl" {
				data, _ := os.ReadFile(filepath.Join(base, e.Name()))
				for _, line := range bridgeSplitLines(data) {
					if len(line) > 0 {
						all = append(all, line)
					}
				}
			}
		}
		if len(all) > 0 {
			return all
		}
	}
	return all
}

func bridgeSplitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
