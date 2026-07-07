// Package agent defines the common Agent Adapter Layer models.
// These types are shared across all agent adapters and must not
// leak agent-specific field names or assumptions.
package agent

import "time"

// AgentIdentity identifies which agent is running in a session.
type AgentIdentity struct {
	Kind        string  `json:"kind"`                  // stable key: claude, codex, antigravity, unknown
	DisplayName string  `json:"displayName"`           // UI label: Claude Code, Codex, Antigravity
	Version     string  `json:"version,omitempty"`     // agent version string
	Confidence  float64 `json:"confidence"`            // 0.0-1.0 detection confidence
}

// AgentStatus is the current activity state of an agent in a session.
type AgentStatus string

const (
	StatusUnknown         AgentStatus = "unknown"
	StatusIdle            AgentStatus = "idle"
	StatusThinking        AgentStatus = "thinking"
	StatusWorking         AgentStatus = "working"
	StatusWaitingApproval AgentStatus = "waiting_approval"
	StatusWaitingInput    AgentStatus = "waiting_input"
	StatusCompleted       AgentStatus = "completed"
	StatusFailed          AgentStatus = "failed"
	StatusInterrupted     AgentStatus = "interrupted"
	StatusDegraded        AgentStatus = "degraded"
)

// AgentEventType categorizes the semantic meaning of an event.
type AgentEventType string

const (
	EventAgentStarted       AgentEventType = "agent_started"
	EventUserMessage        AgentEventType = "user_message"
	EventAssistantMessage   AgentEventType = "assistant_message"
	EventThinking           AgentEventType = "thinking"
	EventToolCallStarted    AgentEventType = "tool_call_started"
	EventToolCallFinished   AgentEventType = "tool_call_finished"
	EventApprovalRequested  AgentEventType = "approval_requested"
	EventApprovalResolved   AgentEventType = "approval_resolved"
	EventWaitingInput       AgentEventType = "waiting_input"
	EventCompleted          AgentEventType = "completed"
	EventFailed             AgentEventType = "failed"
	EventInterrupted        AgentEventType = "interrupted"
	EventUnknown            AgentEventType = "unknown"
)

// AgentEventSource describes how an event was detected.
type AgentEventSource string

const (
	SourceLog          AgentEventSource = "log"           // structured log (JSONL)
	SourceScreen       AgentEventSource = "screen"        // terminal screen analysis
	SourceProcess      AgentEventSource = "process"       // process name/cmdline
	SourceManual       AgentEventSource = "manual"        // user manually linked
	SourceUnknown      AgentEventSource = "unknown"
)

// AgentEvent is a normalized agent activity event.
type AgentEvent struct {
	ID         string            `json:"id"`
	SessionID  string            `json:"sessionId"`
	AgentKind  string            `json:"agentKind"`
	Type       AgentEventType    `json:"type"`
	Timestamp  time.Time         `json:"timestamp"`
	Text       string            `json:"text,omitempty"`
	ToolName   string            `json:"toolName,omitempty"`
	ApprovalID string            `json:"approvalId,omitempty"`
	RawRef     string            `json:"rawRef,omitempty"`     // trace reference, not raw content
	Confidence float64           `json:"confidence"`
	Source     AgentEventSource  `json:"source"`
	Metadata   map[string]string `json:"metadata,omitempty"`   // agent-specific, must not drive UX
}

// AgentApproval represents a pending or resolved approval request.
type AgentApproval struct {
	ID        string       `json:"id"`
	SessionID string       `json:"sessionId"`
	AgentKind string       `json:"agentKind"`
	Status    string       `json:"status"`    // pending, approved, rejected
	Message   string       `json:"message"`   // what the agent is asking approval for
	CreatedAt time.Time    `json:"createdAt"`
	ResolvedAt *time.Time  `json:"resolvedAt,omitempty"`
}
