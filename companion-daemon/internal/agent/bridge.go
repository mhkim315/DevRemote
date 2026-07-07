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
	LogPath     string // resolved agent log path (production only)
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

	events := d.ParseEvents(sessionID, evidence)
	status := inferStatusFromEvents(events)
	if id.Confidence < 0.5 || id.Kind == "unknown" {
		status = StatusUnknown
	}
	return id.Kind, string(status), id.Confidence
}

// ParseEvents reads agent log data from the resolved path and returns parsed events.
// No fixture fallback — only production log paths are used.
func (d *TermAgentDetector) ParseEvents(sessionID string, evidence ProdDetectionEvidence) []AgentEvent {
	if evidence.LogPath == "" {
		return nil
	}
	data, err := os.ReadFile(evidence.LogPath)
	if err != nil {
		// Missing/unreadable log → no events, no fixture fallback.
		return nil
	}
	var allLines [][]byte
	for _, line := range bridgeSplitLines(data) {
		if len(line) > 0 {
			allLines = append(allLines, line)
		}
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

// WriteA1Fixtures writes Claude A1 fixtures to a temp directory for testing.
// Returns the path to the directory. Caller is responsible for cleanup.
func WriteA1Fixtures(dir string) error {
	base := filepath.Join("testdata", "claude")
	entries, err := os.ReadDir(base)
	if err != nil {
		// Try from term package perspective.
		base = filepath.Join("..", "agent", "testdata", "claude")
		entries, err = os.ReadDir(base)
		if err != nil {
			return err
		}
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jsonl" {
			data, _ := os.ReadFile(filepath.Join(base, e.Name()))
			os.WriteFile(filepath.Join(dir, e.Name()), data, 0644)
		}
	}
	return nil
}
