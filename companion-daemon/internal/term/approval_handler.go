package term

import (
	"encoding/json"
	"log"
	"net/http"
	"unicode/utf8"

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
// A1 R3: the STORE is the sole authority. The handler strictly decodes, resolves the
// server-derived requester + runtime, and hands the store the selected option ID +
// raw input; the store recomputes the digest AND the canonical payload, binds the
// requester authorization context, and returns the claim + payload. The handler uses
// the STORE's payload for delivery (never a snapshot rebuild); commit succeeds only
// on an accepted receipt whose token, full binding, and delivered-payload digest match.
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
	if h.RuntimeOf == nil {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "unavailable", http.StatusServiceUnavailable)
		return
	}
	runtime, ok := h.RuntimeOf(sessionID)
	if !ok {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "stale_runtime", http.StatusConflict)
		return
	}
	// The bearer may have been revoked/replaced after authentication. Acquire
	// an epoch reservation before the first approval mutation. The reservation
	// holds the registry lock so revoke cannot interleave during the entire
	// claim→delivery→commit window.
	tok, err := h.reserveEpoch(r)
	if err != nil {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "stale_epoch", http.StatusConflict)
		return
	}

	claim := h.Approvals.ClaimForExecution(ClaimRequest{
		SessionID:      sessionID,
		ApprovalID:     approvalID,
		OptionID:       req.Action,
		Input:          req.Input,
		Runtime:        runtime,
		Requester:      requester,
		IdempotencyKey: req.IdempotencyKey,
		EpochRecheck:   func() error { return h.commitEpoch(tok) },
	})
	switch claim.Outcome {
	case ClaimAlreadyAccepted:
		h.writeApprovalSuccess(w, sessionID, approvalID, req.Action, "", "already_accepted")
		return
	case ClaimGranted:
		// proceed to delivery
	default:
		code, oc := claimOutcomeHTTP(claim.Outcome)
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, oc, code)
		return
	}

	// Revalidate the runtime immediately before delivery.
	cur, ok := h.RuntimeOf(sessionID)
	if !ok || !cur.equal(claim.Binding.Runtime) {
		h.Approvals.RecordDelivery(DeliveryReceipt{Outcome: DeliveryStaleRuntime, ClaimToken: claim.Token, Binding: claim.Binding})
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "stale_runtime", http.StatusConflict)
		return
	}
	// Reservation held: epoch cannot change during delivery.

	delivery := h.ApprovalDelivery
	if delivery == nil {
		delivery = NewUnavailableApprovalDelivery()
	}
	receipt := delivery.Deliver(ApprovalDeliveryRequest{
		ClaimToken:   claim.Token,
		Binding:      claim.Binding,
		Payload:      claim.Payload, // the STORE's canonical payload, never a snapshot rebuild
		EpochRecheck: func() error { return nil },
	})
	// Reservation held: epoch cannot change during commit.
	commit := h.Approvals.RecordDelivery(receipt, func() error { return h.commitEpoch(tok) })
	if commit.Committed {
		h.writeApprovalSuccess(w, sessionID, approvalID, req.Action, commit.Kind, string(commit.Outcome))
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
	case ClaimRetryExhausted:
		return http.StatusConflict, "retry_exhausted"
	case ClaimStaleRuntime:
		return http.StatusConflict, "stale_runtime"
	case ClaimStaleEpoch:
		return http.StatusConflict, "stale_epoch"
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

// sanitizeLogID makes an attacker-influenced identifier log-safe: it strips control/
// newline bytes, then applies the repository's conservative diagnostic redaction
// (home paths, API-key and bearer-token patterns, Authorization headers, key=value
// secrets), and bounds the result. Identifiers are never logged verbatim.
func sanitizeLogID(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			b = append(b, ' ')
		} else {
			b = append(b, c)
		}
	}
	out := redactStr(string(b))
	if len(out) > maxLogIDLen {
		out = out[:maxLogIDLen]
	}
	return out
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
