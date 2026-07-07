package term

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"devremote/companion-daemon/internal/agent"
)

// HandleApprovalAction handles POST /api/sessions/<id>/approvals/<approvalId>.
// Body: {"action":"<option-id>", "input":"<optional user input>"}.
// Semantics are derived from the selected option's Kind, never from action ID.
func (h *Handlers) HandleApprovalAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.PathValue("id")
	approvalID := r.PathValue("approvalId")
	if sessionID == "" || approvalID == "" {
		http.Error(w, "Invalid path: expected /api/sessions/<id>/approvals/<approvalId>", http.StatusBadRequest)
		return
	}

	var req struct {
		Action string `json:"action"`
		Input  string `json:"input,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Action == "" {
		http.Error(w, "Invalid body: {\"action\":\"...\", \"input\":\"...\"} required", http.StatusBadRequest)
		return
	}

	// Look up approval (includes expired for diagnostic purposes).
	target := h.Approvals.Lookup(sessionID, approvalID)
	if target == nil {
		http.Error(w, "Approval not found", http.StatusNotFound)
		return
	}

	// Find the selected option and validate.
	selected := findOption(req.Action, target.Options)
	if selected == nil {
		http.Error(w, "Action not available for this approval", http.StatusBadRequest)
		return
	}

	// Validate required input.
	if selected.Input != nil && selected.Input.Required && req.Input == "" {
		http.Error(w, "Input required for this action", http.StatusBadRequest)
		return
	}

	// Check expiry before resolving.
	if time.Since(target.CreatedAt) > approvalExpiry {
		http.Error(w, "Approval expired", http.StatusGone)
		return
	}

	// Resolve via ApprovalStore. Status derived from option Kind, not action ID.
	status := mapKindToStatus(selected.Kind)
	resolved := h.Approvals.Resolve(sessionID, approvalID, status)
	if !resolved {
		http.Error(w, "Approval already resolved", http.StatusConflict)
		return
	}

	// Execute terminal fallback action.
	actionPayload := buildPayload(selected, req.Input)
	if actionPayload != "" {
		h.Cmds.Put(sessionID, []byte(actionPayload))
	}

	// Audit log (no raw input/prompt exposure).
	log.Printf("APPROVAL ACTION: session=%s approval=%s action=%s kind=%s agent=%s",
		sessionID, approvalID, req.Action, selected.Kind, target.AgentKind)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"action": req.Action,
	})
}

// findOption returns the InteractionOption matching the given action ID.
func findOption(action string, options []agent.InteractionOption) *agent.InteractionOption {
	for i, opt := range options {
		if opt.ID == action {
			return &options[i]
		}
	}
	return nil
}

// mapKindToStatus derives resolution status from the option's semantic Kind.
// Action ID is never used to infer status — Kind is the source of truth.
func mapKindToStatus(kind string) string {
	switch kind {
	case "approve":
		return "approved"
	case "reject", "cancel":
		return "rejected"
	default:
		return "resolved"
	}
}

// buildPayload constructs the terminal fallback payload from the selected option.
func buildPayload(opt *agent.InteractionOption, input string) string {
	// If option has an explicit payload, use it and append user input.
	if opt.Payload != "" {
		if input != "" {
			return opt.Payload + "\n" + input + "\n"
		}
		return opt.Payload + "\n"
	}

	// Default terminal fallback based on option Kind.
	switch opt.Kind {
	case "approve":
		return "y\n"
	case "reject", "cancel":
		return "n\n"
	case "open":
		return ""
	case "neutral":
		if input != "" {
			return input + "\n"
		}
		return ""
	default:
		if input != "" {
			return input + "\n"
		}
		return ""
	}
}
