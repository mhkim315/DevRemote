package term

import (
	"encoding/json"
	"log"
	"net/http"
	"unicode/utf8"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/devicetrust"
)

const (
	maxApprovalBodyBytes  = 8 << 10 // 8 KiB
	maxApprovalInputBytes = 4096
	maxLogIDLen           = 128
)

// HandleApprovalAction handles POST /api/sessions/<id>/approvals/<approvalId>.
// Body: {"action":"<option-id>", "input":"<optional>", "idempotencyKey":"<key>"}.
//
// A1 remediation 2: the STORE is the authority boundary. The handler strictly
// decodes, resolves the server-derived requester + runtime, and hands the store the
// selected option ID + raw input; the store recomputes the digest, uses the stored
// permission, and issues a claim token owning one immutable binding. Delivery and
// commit carry that same binding; commit succeeds only on an accepted receipt whose
// token and every binding field match, and only if the runtime was not superseded.
func (h *Handlers) HandleApprovalAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID := r.PathValue("id")
	approvalID := r.PathValue("approvalId")
	if sessionID == "" || approvalID == "" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	var req struct {
		Action         string `json:"action"`
		Input          string `json:"input,omitempty"`
		IdempotencyKey string `json:"idempotencyKey,omitempty"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxApprovalBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req.Action == "" {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}
	if dec.More() {
		http.Error(w, "Invalid body: trailing data", http.StatusBadRequest)
		return
	}
	if len(req.Input) > maxApprovalInputBytes || !utf8.ValidString(req.Input) {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "input_rejected", http.StatusBadRequest)
		return
	}

	snap, ok := h.Approvals.LookupRecord(sessionID, approvalID)
	if !ok {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "not_found", http.StatusNotFound)
		return
	}

	// Authoritative action ALWAYS requires a server-derived device principal.
	principal := devicetrust.PrincipalFromContext(r.Context())
	if principal == nil {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "unauthorized", http.StatusForbidden)
		return
	}
	requester := requesterFromPrincipal(principal)

	if !snap.Actionable {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "not_actionable", http.StatusConflict)
		return
	}
	selected := findOption(req.Action, snap.Options)
	if selected == nil {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "unknown_action", http.StatusBadRequest)
		return
	}

	// Current server-derived runtime (required for the atomic claim binding).
	if h.RuntimeOf == nil {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "unavailable", http.StatusServiceUnavailable)
		return
	}
	runtime, ok := h.RuntimeOf(sessionID)
	if !ok {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "stale_runtime", http.StatusConflict)
		return
	}

	// One atomic full-binding claim. The store recomputes the digest and uses the
	// STORED permission; the handler passes NO digest and NO permission.
	claim := h.Approvals.ClaimForExecution(ClaimRequest{
		SessionID:      sessionID,
		ApprovalID:     approvalID,
		OptionID:       selected.ID,
		Input:          req.Input,
		Runtime:        runtime,
		Requester:      requester,
		IdempotencyKey: req.IdempotencyKey,
	})
	switch claim.Outcome {
	case ClaimAlreadyAccepted:
		h.writeApprovalSuccess(w, sessionID, approvalID, req.Action, selected.Kind, "already_accepted")
		return
	case ClaimGranted:
		// proceed to delivery
	default:
		code, oc := claimOutcomeHTTP(claim.Outcome)
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, oc, code)
		return
	}

	// Revalidate the runtime immediately before delivery; a replacement/removal
	// between claim and delivery fails closed with NO terminal bytes.
	cur, ok := h.RuntimeOf(sessionID)
	if !ok || !cur.equal(claim.Binding.Runtime) {
		h.Approvals.RecordDelivery(DeliveryReceipt{Outcome: DeliveryStaleRuntime, ClaimToken: claim.Token, Binding: claim.Binding})
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "stale_runtime", http.StatusConflict)
		return
	}

	delivery := h.ApprovalDelivery
	if delivery == nil {
		delivery = NewUnavailableApprovalDelivery()
	}
	receipt := delivery.Deliver(ApprovalDeliveryRequest{
		ClaimToken: claim.Token,
		Binding:    claim.Binding,
		Payload:    serverPayloadFor(selected, req.Input),
	})
	commit := h.Approvals.RecordDelivery(receipt)
	if commit.Committed {
		h.writeApprovalSuccess(w, sessionID, approvalID, req.Action, selected.Kind, string(commit.Outcome))
		return
	}
	code, oc := deliveryOutcomeHTTP(commit.Outcome)
	h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, oc, code)
}

func requesterFromPrincipal(p *devicetrust.Principal) RequesterContext {
	return RequesterContext{
		DeviceID:        p.DeviceID,
		HostID:          p.HostID,
		BearerSessionID: p.BearerSessionID,
		BootID:          p.DeviceBootID,
		Permissions:     append([]string(nil), p.Permissions...),
	}
}

// serverPayloadFor returns the exact server-side delivery bytes. It NEVER
// synthesizes a decision keystroke; only an explicit input-bearing action forwards
// the user's literal input.
func serverPayloadFor(opt *agent.InteractionOption, input string) []byte {
	if opt.Input != nil && input != "" &&
		(opt.Input.Placement == "as_payload" || opt.Input.Placement == "after_payload") {
		return []byte(input + "\n")
	}
	return nil
}

func findOption(action string, options []agent.InteractionOption) *agent.InteractionOption {
	for i, opt := range options {
		if opt.ID == action {
			return &options[i]
		}
	}
	return nil
}

func claimOutcomeHTTP(o ClaimOutcome) (int, string) {
	switch o {
	case ClaimConflict:
		return http.StatusConflict, "conflict"
	case ClaimNotFound:
		return http.StatusNotFound, "not_found"
	case ClaimNotActionable:
		return http.StatusConflict, "not_actionable"
	case ClaimUnknownAction:
		return http.StatusBadRequest, "unknown_action"
	case ClaimInvalidInput:
		return http.StatusBadRequest, "input_rejected"
	case ClaimInvalidKey:
		return http.StatusBadRequest, "invalid_key"
	case ClaimDigestMismatch:
		return http.StatusBadRequest, "digest_mismatch"
	case ClaimExpired:
		return http.StatusGone, "expired"
	case ClaimAlreadyOwned:
		return http.StatusConflict, "already_owned"
	case ClaimStaleRuntime:
		return http.StatusConflict, "stale_runtime"
	case ClaimRuntimeMismatch:
		return http.StatusConflict, "runtime_mismatch"
	case ClaimUnauthorized:
		return http.StatusForbidden, "unauthorized"
	case ClaimLedgerFull:
		return http.StatusServiceUnavailable, "ledger_full"
	default:
		return http.StatusInternalServerError, "error"
	}
}

func deliveryOutcomeHTTP(o DeliveryOutcome) (int, string) {
	switch o {
	case DeliveryStaleRuntime:
		return http.StatusConflict, "stale_runtime"
	case DeliveryRuntimeMismatch:
		return http.StatusConflict, "runtime_mismatch"
	case DeliveryConflict:
		return http.StatusConflict, "conflict"
	case DeliveryUnavailable:
		return http.StatusBadGateway, "unavailable"
	case DeliveryRejected:
		return http.StatusBadGateway, "delivery_failed"
	default:
		return http.StatusBadGateway, "delivery_failed"
	}
}

// sanitizeLogID bounds an identifier and strips control/newline bytes so an
// attacker-influenced session/approval/action id cannot inject or leak into logs.
func sanitizeLogID(s string) string {
	if len(s) > maxLogIDLen {
		s = s[:maxLogIDLen]
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			b = append(b, '.')
		} else {
			b = append(b, c)
		}
	}
	return string(b)
}

func (h *Handlers) writeApprovalSuccess(w http.ResponseWriter, sessionID, approvalID, action, kind, outcome string) {
	log.Printf("APPROVAL ACTION: session=%s approval=%s action=%s kind=%s outcome=%s http=200",
		sanitizeLogID(sessionID), sanitizeLogID(approvalID), sanitizeLogID(action), sanitizeLogID(kind), outcome)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "action": action, "outcome": outcome})
}

func (h *Handlers) writeApprovalOutcome(w http.ResponseWriter, sessionID, approvalID, action, outcome string, code int) {
	log.Printf("APPROVAL ACTION: session=%s approval=%s action=%s outcome=%s http=%d",
		sanitizeLogID(sessionID), sanitizeLogID(approvalID), sanitizeLogID(action), outcome, code)
	http.Error(w, outcome, code)
}
