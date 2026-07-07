package term

import (
	"encoding/json"
	"log"
	"net/http"

	"devremote/companion-daemon/internal/agent"
)

// HandleApprovalAction handles POST /api/sessions/<id>/approvals/<approvalId>.
// Body: {"action":"approve"|"reject"|"send_text"|"send_key"|"open_terminal"}
func (h *Handlers) HandleApprovalAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse session ID and approval ID from URL path.
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

	// Validate action.
	validActions := map[string]bool{
		"approve":       true,
		"reject":        true,
		"send_text":     true,
		"send_key":      true,
		"open_terminal": true,
	}
	if !validActions[req.Action] {
		http.Error(w, "Invalid action: must be one of approve, reject, send_text, send_key, open_terminal", http.StatusBadRequest)
		return
	}

	// Look up approval.
	approvals := h.Approvals.List(sessionID)
	var target agent.AgentApproval
	found := false
	for _, a := range approvals {
		if a.ID == approvalID {
			target = a
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "Approval not found", http.StatusNotFound)
		return
	}

	// Resolve via ApprovalStore.
	resolved := h.Approvals.Resolve(sessionID, approvalID, mapActionToStatus(req.Action))
	if !resolved {
		http.Error(w, "Approval already resolved or expired", http.StatusConflict)
		return
	}

	// Execute terminal fallback action.
	actionPayload := getActionPayload(req.Action, target)
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
	switch action {
	case "approve":
		// Check if approval has a default approve payload.
		for _, opt := range approval.Options {
			if opt.ID == "approve" && opt.Payload != "" {
				return opt.Payload + "\n"
			}
		}
		return "y\n"
	case "reject":
		for _, opt := range approval.Options {
			if opt.ID == "reject" && opt.Payload != "" {
				return opt.Payload + "\n"
			}
		}
		return "n\n"
	case "send_text", "send_key":
		// Look up payload from approval options.
		for _, opt := range approval.Options {
			if opt.ID == action && opt.Payload != "" {
				if action == "send_key" {
					return opt.Payload
				}
				return opt.Payload + "\n"
			}
		}
		return ""
	case "open_terminal":
		return "" // No backend action; mobile navigates.
	default:
		return ""
	}
}

