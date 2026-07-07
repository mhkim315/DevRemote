package term

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"devremote/companion-daemon/internal/agent"
)

// HandleApprovalAction handles POST /api/sessions/<id>/approvals/<approvalId>.
// Body: {"action":"<option-id>"}. The action must match an option in the approval.
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Action == "" {
		http.Error(w, "Invalid body: {\"action\":\"...\"} required", http.StatusBadRequest)
		return
	}

	// Look up approval (includes expired for diagnostic purposes).
	target := h.Approvals.Lookup(sessionID, approvalID)
	if target == nil {
		http.Error(w, "Approval not found", http.StatusNotFound)
		return
	}

	// Validate action is in the approval's options.
	if !optionExists(req.Action, target.Options) {
		http.Error(w, "Action not available for this approval", http.StatusBadRequest)
		return
	}

	// Check expiry before resolving.
	if time.Since(target.CreatedAt) > approvalExpiry {
		http.Error(w, "Approval expired", http.StatusGone)
		return
	}

	// Resolve via ApprovalStore.
	resolved := h.Approvals.Resolve(sessionID, approvalID, mapActionToStatus(req.Action))
	if !resolved {
		http.Error(w, "Approval already resolved", http.StatusConflict)
		return
	}

	// Execute terminal fallback action.
	actionPayload := getActionPayload(req.Action, *target)
	if actionPayload != "" {
		h.Cmds.Put(sessionID, []byte(actionPayload))
	}

	// Audit log (no raw prompt exposure).
	log.Printf("APPROVAL ACTION: session=%s approval=%s action=%s agent=%s",
		sessionID, approvalID, req.Action, target.AgentKind)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"action": req.Action,
	})
}

// optionExists checks if an action ID exists in the approval options.
func optionExists(action string, options []agent.InteractionOption) bool {
	for _, opt := range options {
		if opt.ID == action {
			return true
		}
	}
	return false
}

// mapActionToStatus maps an action string to approval status.
func mapActionToStatus(action string) string {
	switch action {
	case "approve", "send_text", "send_key":
		return "approved"
	case "reject":
		return "rejected"
	default:
		return "resolved"
	}
}

// getActionPayload returns the terminal input payload for an action.
func getActionPayload(action string, approval agent.AgentApproval) string {
	// Use the option's payload if available, otherwise fall back to defaults.
	for _, opt := range approval.Options {
		if opt.ID == action && opt.Payload != "" {
			switch action {
			case "send_key":
				return opt.Payload
			default:
				return opt.Payload + "\n"
			}
		}
	}
	// Default terminal input fallback for approve/reject.
	switch action {
	case "approve":
		return "y\n"
	case "reject":
		return "n\n"
	case "open_terminal", "view_only":
		return ""
	default:
		return ""
	}
}
