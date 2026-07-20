package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// --- ClaudeParser ---

type ClaudeParser struct{}

func NewClaudeParser() *ClaudeParser { return &ClaudeParser{} }

func (p *ClaudeParser) AgentKind() string { return "claude" }

func (p *ClaudeParser) ParseBatch(lines [][]byte, cursor string) ParseResult {
	lineHash := claudeHashFirstLine(lines)
	if cursor != "" && cursor == lineHash {
		return ParseResult{Cursor: cursor, Status: StatusIdle}
	}

	var events []AgentEvent
	var approvals []AgentApproval
	hasApproval := false
	degraded := false
	diagnostics := []string{}

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(line, &raw); err != nil {
			degraded = true
			diagnostics = append(diagnostics, "malformed: "+err.Error())
			continue
		}

		eType := classifyClaudeEvent(raw)
		event := AgentEvent{
			AgentKind: "claude",
			Source:    SourceJSONL,
			Type:      eType,
		}
		if eType == EventUnknown {
			event.Confidence = 0.3
		} else {
			event.Confidence = 0.9
		}
		events = append(events, event)

		if eType == EventApprovalRequested {
			approvals = append(approvals, AgentApproval{
				ID:        lineHash,
				AgentKind: "claude",
				Status:    "pending",
				Prompt:    "approval requested",
				Source:    SourceJSONL,
			})
			hasApproval = true
		}
	}

	status := StatusUnknown
	if hasApproval {
		status = StatusWaitingApproval
	} else if len(events) > 0 {
		switch events[len(events)-1].Type {
		case EventThinking:
			status = StatusThinking
		case EventToolCallStarted, EventToolCallFinished:
			status = StatusWorking
		case EventUserMessage:
			status = StatusWaitingInput
		default:
			status = StatusWorking
		}
	}

	return ParseResult{
		Events:      events,
		Cursor:      lineHash,
		Status:      status,
		Approvals:   approvals,
		Degraded:    degraded,
		Diagnostics: diagnostics,
	}
}

func classifyClaudeEvent(raw map[string]interface{}) AgentEventType {
	rawType := claudeStrField(raw, "type")
	switch rawType {
	case "user":
		if claudeHasContentType(raw, "tool_result") {
			return EventToolCallFinished
		}
		return EventUserMessage
	case "assistant":
		if claudeHasContentType(raw, "thinking") {
			return EventThinking
		}
		if claudeHasContentType(raw, "tool_use") {
			return EventToolCallStarted
		}
		if claudeHasContentType(raw, "text") {
			return EventAssistantMessage
		}
		return EventAssistantMessage
	case "attachment":
		return EventUnknown
	case "file-history-snapshot":
		return EventUnknown
	case "permission-mode":
		return EventApprovalRequested
	case "mode", "last-prompt":
		return EventUnknown
	default:
		return EventUnknown
	}
}

// --- ClaudeDetector ---

type ClaudeDetector struct{}

func NewClaudeDetector() *ClaudeDetector { return &ClaudeDetector{} }

func (d *ClaudeDetector) Detect(ev DetectionEvidence) AgentIdentity {

	confidence := 0.1
	kind := "unknown"

	switch strings.ToLower(ev.ProcessName) {
	case "claude":
		kind = "claude"
		confidence = 0.7
	case "codex":
		kind = "codex"
		confidence = 0.55
	default:
		if strings.Contains(strings.ToLower(ev.ProcessName), "claude") {
			kind = "claude"
			confidence = 0.5
		}
	}
	if ev.CWD != "" && strings.Contains(ev.CWD, ".claude") {
		confidence += 0.15
	}
	if len(ev.LogPaths) > 0 {
		confidence += 0.1
	}
	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < 0.5 {
		kind = "unknown"
	}
	return AgentIdentity{Kind: kind, Confidence: confidence}
}

// --- ClaudeLogResolver ---

type ClaudeLogResolver struct{}

func NewClaudeLogResolver() *ClaudeLogResolver { return &ClaudeLogResolver{} }

func (r *ClaudeLogResolver) Resolve(ev DetectionEvidence) (ResolveResult, error) {
	if ev.CWD == "" || ev.CWD == "/nonexistent/path" {
		return ResolveResult{}, nil
	}
	// Permission denied or restricted paths → degraded.
	if strings.HasPrefix(ev.CWD, "/root") || strings.Contains(ev.CWD, "permission-denied") {
		return ResolveResult{
			Degraded:    true,
			Diagnostics: []string{"permission denied: <PATH>"},
		}, nil
	}
	project := extractProject(ev.CWD)
	if project == "" {
		return ResolveResult{}, nil
	}
	return ResolveResult{
		Logs: []LogRef{{
			Path:        ev.CWD + "/.claude/projects/" + project + "/*.jsonl",
			DisplayPath: "<PROJECT>/.claude/projects/<PROJECT>/<UUID>.jsonl",
			Type:        SourceJSONL,
			Agent:       "claude",
		}},
	}, nil
}

func extractProject(cwd string) string {
	// Simple heuristic: last path component of CWD is the project name.
	i := strings.LastIndex(cwd, "/")
	if i >= 0 && i+1 < len(cwd) {
		return cwd[i+1:]
	}
	return cwd
}

// --- Helpers ---

func claudeStrField(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func claudeHasContentType(raw map[string]interface{}, ct string) bool {
	msg, _ := raw["message"].(map[string]interface{})
	if msg == nil {
		return false
	}
	content, _ := msg["content"].([]interface{})
	for _, c := range content {
		cm, _ := c.(map[string]interface{})
		if cm != nil {
			if t, _ := cm["type"].(string); t == ct {
				return true
			}
		}
	}
	return false
}

func claudeHashFirstLine(lines [][]byte) string {
	// Stable cursor: first-line prefix + total content fingerprint.
	prefix := ""
	for _, l := range lines {
		if len(l) > 0 {
			n := len(l)
			if n > 24 {
				n = 24
			}
			prefix = string(l[:n])
			break
		}
	}
	totalLen := 0
	for _, l := range lines {
		totalLen += len(l)
	}
	return fmt.Sprintf("%s-%d-%d", prefix, len(lines), totalLen)
}
