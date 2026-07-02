package detector

import (
	"sync"
	"time"

	"devremote/companion-daemon/internal/watcher"
)

// QuestionOption is a single choice in an AskUserQuestion.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// QuestionItem is one question in an AskUserQuestion.
type QuestionItem struct {
	Question string           `json:"question"`
	Header   string           `json:"header"`
	Options  []QuestionOption `json:"options"`
}

// Alert is emitted when a tool_use needs user approval.
type Alert struct {
	SessionID   string         `json:"sessionId"`
	ToolUseID   string         `json:"toolUseId"`
	ToolName    string         `json:"toolName"`
	Description string         `json:"description"`
	Question    string         `json:"question"`
	Questions   []QuestionItem `json:"questions,omitempty"`
	Type        string         `json:"type,omitempty"`
	Timestamp   string         `json:"timestamp"`
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

// needsApproval returns true for tools that require user approval.
// Read-only / auto-approved tools return false and are silently skipped.
func needsApproval(toolName string) bool {
	switch toolName {
	case "Bash", "Write", "Edit", "NotebookEdit",
		"AskUserQuestion", "Skill", "WebFetch":
		return true
	default:
		return false
	}
}

// Feed processes a raw event from the watcher.
func (s *State) Feed(ev watcher.RawEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ev.Type == "user" {
		if pu, ok := s.pending[ev.ParentUUID]; ok {
			pu.Timer.Stop()
			delete(s.pending, ev.ParentUUID)
		}
		return
	}

	tu := watcher.ExtractToolUse(ev)
	if tu == nil {
		return
	}

	if tu.Name == "AskUserQuestion" {
		var questions []QuestionItem
		question := ""
		desc := ""

		if input, ok := tu.Input["questions"].([]interface{}); ok {
			for _, qi := range input {
				if qm, ok := qi.(map[string]interface{}); ok {
					q := QuestionItem{}
					if txt, ok := qm["question"].(string); ok {
						q.Question = txt
						question = txt
					}
					if h, ok := qm["header"].(string); ok {
						q.Header = h
					}
					if opts, ok := qm["options"].([]interface{}); ok {
						for _, oi := range opts {
							if om, ok := oi.(map[string]interface{}); ok {
								opt := QuestionOption{}
								if l, ok := om["label"].(string); ok {
									opt.Label = l
								}
								if d, ok := om["description"].(string); ok {
									opt.Description = d
								}
								q.Options = append(q.Options, opt)
							}
						}
					}
					questions = append(questions, q)
				}
			}
		}
		if len(questions) == 0 {
			desc = "Claude가 질문을 보냈습니다"
		}

		s.OnAlert(Alert{
			SessionID:   ev.SessionID,
			ToolUseID:   tu.ID,
			ToolName:    "AskUserQuestion",
			Description: desc,
			Question:    question,
			Questions:   questions,
			Timestamp:   ev.Timestamp,
		})
		return
	}

}

func getToolDescription(tu *watcher.ToolUse) string {
	if cmd, ok := tu.Input["command"].(string); ok {
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
