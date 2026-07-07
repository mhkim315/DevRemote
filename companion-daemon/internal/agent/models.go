// Package agent defines the common Agent Adapter Layer models.
// These types are shared across all agent adapters and must not
// expose agent-specific field names or assumptions.
//
// Raw JSONL field names (e.g. "type", "sessionId", "payload") appear
// only in fixture metadata and mapping documentation, never in these
// common Go types.
package agent

import "time"

// AgentIdentity identifies which agent is running in a session.
type AgentIdentity struct {
	Kind        string  `json:"kind"`              // stable key: claude, codex, antigravity, unknown
	DisplayName string  `json:"displayName"`       // UI label: Claude Code, Codex, Antigravity
	Version     string  `json:"version,omitempty"` // agent version string
	Confidence  float64 `json:"confidence"`        // 0.0-1.0 detection confidence
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

// AgentCapability defines what actions are available for an agent session.
// These gate approval CTA visibility and action execution.
type AgentCapability string

const (
	CapObserve AgentCapability = "observe" // session is observable
	CapControl AgentCapability = "control" // terminal is controllable (open terminal)
	CapApprove AgentCapability = "approve" // remote approve/reject supported
	CapInput   AgentCapability = "input"   // send_text/send_key supported
)

// AgentEventType categorizes the semantic meaning of an event.
type AgentEventType string

const (
	EventAgentStarted      AgentEventType = "agent_started"
	EventUserMessage       AgentEventType = "user_message"
	EventAssistantMessage  AgentEventType = "assistant_message"
	EventThinking          AgentEventType = "thinking"
	EventToolCallStarted   AgentEventType = "tool_call_started"
	EventToolCallFinished  AgentEventType = "tool_call_finished"
	EventApprovalRequested AgentEventType = "approval_requested"
	EventApprovalResolved  AgentEventType = "approval_resolved"
	EventWaitingInput      AgentEventType = "waiting_input"
	EventCompleted         AgentEventType = "completed"
	EventFailed            AgentEventType = "failed"
	EventInterrupted       AgentEventType = "interrupted"
	EventUnknown           AgentEventType = "unknown"
)

// AgentEventSource describes how an event was detected.
type AgentEventSource string

const (
	SourceJSONL      AgentEventSource = "jsonl"       // structured log (JSONL file)
	SourceLogFile    AgentEventSource = "log_file"    // unstructured log file
	SourceScreen     AgentEventSource = "screen"      // terminal screen analysis
	SourceProcess    AgentEventSource = "process"     // process name/cmdline
	SourceManualLink AgentEventSource = "manual_link" // user manually linked
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
	RawRef     string            `json:"rawRef,omitempty"` // trace reference, not raw content
	Confidence float64           `json:"confidence"`
	Source     AgentEventSource  `json:"source"`
	Metadata   map[string]string `json:"metadata,omitempty"` // agent-specific, must not drive UX
}

// InputSchema describes the input contract for an interaction option.
type InputSchema struct {
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder,omitempty"`
	Multiline   bool   `json:"multiline,omitempty"`
	Placement   string `json:"placement,omitempty"` // "after_payload" | "as_payload" | "metadata_only"
}

// InteractionOption represents one choice in an interaction request.
// Kind carries semantic meaning (approve/reject/neutral/open/cancel);
// mobile uses Kind for styling, never infers semantics from ID.
type InteractionOption struct {
	ID      string       `json:"id"`              // stable key
	Label   string       `json:"label"`           // display label
	Kind    string       `json:"kind"`            // semantic: approve, reject, neutral, open, cancel
	Payload string       `json:"payload,omitempty"` // terminal fallback payload
	Input   *InputSchema `json:"input,omitempty"` // input contract
}

// AgentApproval represents a pending or resolved interaction request.
// Replaces the old approval-only model; supports N heterogeneous options
// with semantic kinds and optional input contracts.
type AgentApproval struct {
	ID         string              `json:"id"`
	SessionID  string              `json:"sessionId"`
	AgentKind  string              `json:"agentKind"`
	Kind       string              `json:"kind"`              // "approval" | "interaction" | "info"
	Status     string              `json:"status"`            // pending, approved, rejected
	Prompt     string              `json:"prompt"`            // what the agent is asking
	Options    []InteractionOption `json:"options"`           // available choices
	Default    string              `json:"default,omitempty"` // stable InteractionOption.ID of default choice
	Source     AgentEventSource    `json:"source"`
	Confidence float64             `json:"confidence"`
	Metadata   map[string]string   `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	ResolvedAt *time.Time        `json:"resolvedAt,omitempty"`
}
