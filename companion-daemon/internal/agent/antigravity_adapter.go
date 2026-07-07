package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// --- AntigravityParser ---

type AntigravityParser struct{}

func NewAntigravityParser() *AntigravityParser { return &AntigravityParser{} }

func (p *AntigravityParser) AgentKind() string { return "antigravity" }

func (p *AntigravityParser) ParseBatch(lines [][]byte, cursor string) ParseResult {
	lineHash := antigravityHashFirstLine(lines)
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

		// EPHEMERAL_MESSAGE is skipped — not semantically meaningful.
		if antigravityStrField(raw, "type") == "EPHEMERAL_MESSAGE" {
			continue
		}

		eType := classifyAntigravityEvent(raw)
		event := AgentEvent{
			AgentKind: "antigravity",
			Source:    SourceJSONL,
			Type:      eType,
		}
		if eType == EventUnknown {
			event.Confidence = 0.3
		} else {
			event.Confidence = 0.9
		}

		// Populate tool name for tool_call_started events.
		if eType == EventToolCallStarted {
			rawType := antigravityStrField(raw, "type")
			event.ToolName = strings.ToLower(rawType)
		}

		events = append(events, event)

		if eType == EventApprovalRequested {
			approvals = append(approvals, AgentApproval{
				ID:        lineHash,
				AgentKind: "antigravity",
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
		case EventFailed:
			status = StatusFailed
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

func classifyAntigravityEvent(raw map[string]interface{}) AgentEventType {
	rawType := antigravityStrField(raw, "type")

	switch rawType {
	case "USER_INPUT":
		return EventUserMessage
	case "PLANNER_RESPONSE":
		// If thinking field is present and non-empty, classify as thinking.
		if thinking := antigravityStrField(raw, "thinking"); thinking != "" {
			return EventThinking
		}
		return EventAssistantMessage
	case "VIEW_FILE", "SEARCH_WEB", "LIST_DIRECTORY":
		return EventToolCallStarted
	case "ERROR_MESSAGE":
		return EventFailed
	case "CHECKPOINT", "GENERIC":
		return EventUnknown
	default:
		return EventUnknown
	}
}

// --- AntigravityDetector ---

type AntigravityDetector struct{}

func NewAntigravityDetector() *AntigravityDetector { return &AntigravityDetector{} }

func (d *AntigravityDetector) Detect(ev DetectionEvidence) AgentIdentity {
	if ev.ManualLink != nil && ev.ManualLink.AgentKind != "" {
		return AgentIdentity{Kind: ev.ManualLink.AgentKind, DisplayName: ev.ManualLink.AgentKind, Confidence: 1.0}
	}

	confidence := 0.1
	kind := "unknown"

	processName := strings.ToLower(ev.ProcessName)
	switch processName {
	case "antigravity", "gemini":
		kind = "antigravity"
		confidence = 0.6
	case "claude":
		kind = "claude"
		confidence = 0.55
	case "codex":
		kind = "codex"
		confidence = 0.55
	default:
		if strings.Contains(processName, "antigravity") || strings.Contains(processName, "gemini") {
			kind = "antigravity"
			confidence = 0.5
		} else if strings.Contains(processName, "claude") {
			kind = "claude"
			confidence = 0.5
		} else if strings.Contains(processName, "codex") {
			kind = "codex"
			confidence = 0.5
		}
	}

	if ev.CWD != "" && (strings.Contains(ev.CWD, ".gemini") || strings.Contains(ev.CWD, "antigravity")) {
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

// --- AntigravityLogResolver ---

type AntigravityLogResolver struct{}

func NewAntigravityLogResolver() *AntigravityLogResolver { return &AntigravityLogResolver{} }

func (r *AntigravityLogResolver) Resolve(ev DetectionEvidence) (ResolveResult, error) {
	if ev.CWD == "" || ev.CWD == "/nonexistent/path" {
		return ResolveResult{}, nil
	}
	if strings.HasPrefix(ev.CWD, "/root") || strings.Contains(ev.CWD, "permission-denied") {
		return ResolveResult{
			Degraded:    true,
			Diagnostics: []string{"permission denied: <PATH>"},
		}, nil
	}

	// If CWD suggests an Antigravity session, return a log path hint.
	// Full resolution happens at the term layer via GeminiResolver/AntigravityResolver.
	if strings.Contains(ev.CWD, ".gemini/antigravity") {
		return ResolveResult{
			Logs: []LogRef{{
				Path:        ev.CWD + "/transcript.jsonl",
				DisplayPath: "<HOME>/.gemini/antigravity/brain/<UUID>/.system_generated/logs/transcript.jsonl",
				Type:        SourceJSONL,
				Agent:       "antigravity",
			}},
		}, nil
	}

	// CWD alone is insufficient — return empty; term layer handles full resolution.
	return ResolveResult{}, nil
}

// --- Helpers ---

func antigravityStrField(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func antigravityHashFirstLine(lines [][]byte) string {
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
