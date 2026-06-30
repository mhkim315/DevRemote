package detector

import (
	"sync"
	"time"

	"devremote/companion-daemon/internal/watcher"
)

// Alert is emitted when a tool_use needs user approval.
type Alert struct {
	SessionID   string `json:"sessionId"`
	ToolUseID   string `json:"toolUseId"`
	ToolName    string `json:"toolName"`
	Description string `json:"description"`
	Question    string `json:"question"`
	Timestamp   string `json:"timestamp"`
}

// State tracks pending tool approvals for a session.
type State struct {
	mu      sync.Mutex
	pending map[string]*PendingToolUse
	OnAlert func(Alert)
}

// PendingToolUse is a tool_use that hasn't been resolved yet.
type PendingToolUse struct {
	ToolUse watcher.ToolUse
	Event   watcher.RawEvent
	Timer   *time.Timer
}

// NewState creates a detector state machine.
func NewState(onAlert func(Alert)) *State {
	return &State{
		pending: make(map[string]*PendingToolUse),
		OnAlert: onAlert,
	}
}

const approvalTimeout = 3 * time.Second

// Feed processes a raw event from the watcher.
func (s *State) Feed(ev watcher.RawEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if this is a tool_result for a pending tool.
	if ev.Type == "user" {
		if pu, ok := s.pending[ev.ParentUUID]; ok {
			pu.Timer.Stop()
			delete(s.pending, ev.ParentUUID)
		}
		return
	}

	// Check if this is an assistant tool_use.
	tu := watcher.ExtractToolUse(ev)
	if tu == nil {
		return
	}

	// AskUserQuestion: alert immediately.
	if tu.Name == "AskUserQuestion" {
		question := ""
		desc := ""
		if input, ok := tu.Input["questions"].([]interface{}); ok && len(input) > 0 {
			if q, ok := input[0].(map[string]interface{}); ok {
				if txt, ok := q["question"].(string); ok {
					question = txt
				}
			}
		}
		if input, ok := tu.Input["question"]; ok {
			if q, ok := input.(string); ok {
				question = q
			} else {
				desc = "질문이 있습니다"
			}
		}
		if desc == "" && question == "" {
			desc = "Claude가 질문을 보냈습니다"
		}

		s.OnAlert(Alert{
			SessionID:   ev.SessionID,
			ToolUseID:   tu.ID,
			ToolName:    "AskUserQuestion",
			Description: desc,
			Question:    question,
			Timestamp:   ev.Timestamp,
		})
		return
	}

	// Other tool_use: start timeout timer.
	desc := getToolDescription(tu)
	pu := &PendingToolUse{
		ToolUse: *tu,
		Event:   ev,
	}
	pu.Timer = time.AfterFunc(approvalTimeout, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.pending[tu.ID]; ok {
			delete(s.pending, tu.ID)
			s.OnAlert(Alert{
				SessionID:   ev.SessionID,
				ToolUseID:   tu.ID,
				ToolName:    tu.Name,
				Description: desc,
				Timestamp:   ev.Timestamp,
			})
		}
	})
	s.pending[tu.ID] = pu
}

func getToolDescription(tu *watcher.ToolUse) string {
	if cmd, ok := tu.Input["command"].(string); ok {
		if len(cmd) > 120 {
			cmd = cmd[:120] + "..."
		}
		return cmd
	}
	if desc, ok := tu.Input["description"].(string); ok {
		return desc
	}
	if path, ok := tu.Input["file_path"].(string); ok {
		return "Edit: " + path
	}
	return tu.Name
}
