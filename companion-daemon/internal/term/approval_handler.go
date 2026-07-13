package term

import (
	"encoding/json"
	"log"
	"net/http"

	"devremote/companion-daemon/internal/agent"
)

// HandleApprovalAction handles POST /api/sessions/<id>/approvals/<approvalId>.
// Body: {"action":"<option-id>", "input":"<optional user input>"}.
//
// A1-C: the handler drives the generation-bound AuthoritativeApprovalStore through
// the frozen lifecycle — validate the exact allowed option, atomically reserve the
// pending request (at-most-once), deliver only the exact action, and commit the
// resolution ONLY after delivery is durably accepted; a decision whose delivery
// cannot be confirmed fails closed as delivery_failed and is never reported as a
// success. Resolution status is derived from the option Kind, never the action ID.
// (A1-D hardens this further: strict body bounds, and revalidation against live
// runtime identity immediately before delivery.)
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

	// Look up the current authoritative request to validate the action against its
	// exact allowed options BEFORE reserving. The reservation below re-checks state
	// atomically, so a concurrent resolution between here and Reserve is caught.
	snap, ok := h.Approvals.LookupRecord(sessionID, approvalID)
	if !ok {
		h.finishApproval(w, r, sessionID, approvalID, req.Action, "", OutcomeNotFound)
		return
	}

	selected := findOption(req.Action, snap.Options)
	if selected == nil {
		h.finishApproval(w, r, sessionID, approvalID, req.Action, "", OutcomeUnknownAction)
		return
	}
	// Input contract validation.
	if req.Input != "" && selected.Input == nil {
		h.finishApproval(w, r, sessionID, approvalID, req.Action, selected.Kind, OutcomeInputRejected)
		return
	}
	if selected.Input != nil && selected.Input.Required && req.Input == "" {
		h.finishApproval(w, r, sessionID, approvalID, req.Action, selected.Kind, OutcomeInputRejected)
		return
	}

	// Atomic at-most-once reservation (pending → executing). A concurrent second
	// submit, an expired request, or an already-terminal/invalidated request is
	// refused here without any delivery.
	_, oc := h.Approvals.Reserve(sessionID, approvalID)
	if oc != OutcomeOK {
		h.finishApproval(w, r, sessionID, approvalID, req.Action, selected.Kind, oc)
		return
	}

	// Delivery. The resolution is committed ONLY after the required action is
	// durably accepted by the owned delivery boundary.
	needsCmd, requiresConfirmation := deliveryPlan(selected)
	switch {
	case requiresConfirmation:
		// A decision that requires the agent to durably receive it. The terminal
		// fallback cannot confirm the agent accepted it, so fail closed — never a
		// false success. No command is synthesized.
		h.Approvals.Fail(sessionID, approvalID)
		h.finishApproval(w, r, sessionID, approvalID, req.Action, selected.Kind, OutcomeDeliveryFailed)
		return
	case needsCmd:
		// Fire-and-forget raw input the user explicitly chose to send. Enqueue into
		// the owned delivery boundary; enqueue IS the accepted semantics for raw
		// input (not a confirmation-required decision).
		payload := inputPayload(selected, req.Input)
		if payload != "" {
			h.Cmds.Put(sessionID, []byte(payload))
		}
		h.Approvals.Commit(sessionID, approvalID, selected.Kind)
	default:
		// A denial or a no-delivery action (reject/cancel/open/neutral-no-input):
		// no terminal command is emitted (no-command-on-rejection) and the user's
		// decision is recorded.
		h.Approvals.Commit(sessionID, approvalID, selected.Kind)
	}

	h.finishApproval(w, r, sessionID, approvalID, req.Action, selected.Kind, OutcomeOK)
}

// finishApproval writes the response for an outcome and emits a privacy-safe audit
// log (IDs + outcome codes only, never raw input or prompt).
func (h *Handlers) finishApproval(w http.ResponseWriter, _ *http.Request, sessionID, approvalID, action, kind string, oc ActionOutcome) {
	status := outcomeHTTPStatus(oc)
	log.Printf("APPROVAL ACTION: session=%s approval=%s action=%s kind=%s outcome=%s http=%d",
		sessionID, approvalID, action, kind, oc, status)
	if oc == OutcomeOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"action":  action,
			"outcome": string(oc),
		})
		return
	}
	http.Error(w, string(oc), status)
}

// deliveryPlan classifies how an option is delivered. requiresConfirmation is true
// for decision actions (approve) that need durable agent receipt the terminal
// fallback cannot confirm — those fail closed. reject/cancel are denials that emit
// no command. A neutral option carrying an explicit input contract is fire-and-
// forget raw input (needsCmd). open and neutral-no-input resolve with no delivery.
func deliveryPlan(opt *agent.InteractionOption) (needsCmd, requiresConfirmation bool) {
	switch opt.Kind {
	case "approve":
		return false, true
	case "reject", "cancel", "open":
		return false, false
	default: // neutral and any other kind
		if opt.Input != nil {
			return true, false
		}
		return false, false
	}
}

// inputPayload builds the terminal payload for a fire-and-forget input option from
// the user's explicit input. It NEVER synthesizes a decision keystroke; it only
// forwards the literal input the user chose to send, per the option's placement.
func inputPayload(opt *agent.InteractionOption, input string) string {
	if opt.Input == nil || input == "" {
		return ""
	}
	switch opt.Input.Placement {
	case "as_payload", "after_payload":
		return input + "\n"
	default: // metadata_only or unset: input is not injected into the terminal
		return ""
	}
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
