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
	maxIdempotencyKeyLen  = 128
)

// HandleApprovalAction handles POST /api/sessions/<id>/approvals/<approvalId>.
// Body: {"action":"<option-id>", "input":"<optional>", "idempotencyKey":"<key>"}.
//
// A1 remediation: an approval action is an AUTHORITATIVE, once-only execution.
// The flow is strictly: strict-decode → display-only lookup → actionability gate
// (B5) → server-derived requester (never client identity) → canonical action digest
// (B2) → one atomic ClaimForExecution (B1) → runtime revalidation immediately
// before delivery (B4) → dedicated approval delivery boundary + receipt (B3) →
// commit ONLY on an accepted/already_accepted receipt bound to the exact claim
// token and digest. There is no lookup→validate→reserve→deliver path and no generic
// CommandBroker delivery. Every non-success fails closed and never returns HTTP 200.
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
	dec.DisallowUnknownFields() // reject unknown / client-supplied authority fields
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
	if len(req.IdempotencyKey) > maxIdempotencyKeyLen || (req.IdempotencyKey != "" && !utf8.ValidString(req.IdempotencyKey)) {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "input_rejected", http.StatusBadRequest)
		return
	}

	// Display-only lookup (no execution authority).
	snap, ok := h.Approvals.LookupRecord(sessionID, approvalID)
	if !ok {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "not_found", http.StatusNotFound)
		return
	}

	// Server-derived requester. An authoritative action ALWAYS requires an
	// authenticated device principal; there is no insecure-local bypass and no
	// client-asserted identity. (The remote route also enforces PermTerminalInput via
	// RequirePrincipal.)
	principal := devicetrust.PrincipalFromContext(r.Context())
	if principal == nil {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "unauthorized", http.StatusForbidden)
		return
	}
	requester := requesterFromPrincipal(principal)

	// Actionability gate (B5): a non-actionable approval (no proven action mapping)
	// exposes no execution path — no claim, no delivery, no terminal bytes.
	if !snap.Actionable {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "not_actionable", http.StatusConflict)
		return
	}

	selected := findOption(req.Action, snap.Options)
	if selected == nil {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "unknown_action", http.StatusBadRequest)
		return
	}
	if req.Input != "" && selected.Input == nil {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "input_rejected", http.StatusBadRequest)
		return
	}
	if selected.Input != nil && selected.Input.Required && req.Input == "" {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "input_rejected", http.StatusBadRequest)
		return
	}

	// Canonical action digest (B2) — the execution-integrity value bound through the
	// whole flow.
	ca := canonicalActionFor(selected, req.Input)
	digest := ca.Digest()

	// Current server-derived runtime. Required to bind the claim to the live runtime.
	if h.RuntimeOf == nil {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "unavailable", http.StatusServiceUnavailable)
		return
	}
	runtime, ok := h.RuntimeOf(sessionID)
	if !ok {
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "stale_runtime", http.StatusConflict)
		return
	}

	// One atomic full-binding claim (B1).
	claim := h.Approvals.ClaimForExecution(ClaimRequest{
		SessionID:      sessionID,
		ApprovalID:     approvalID,
		OptionID:       selected.ID,
		Runtime:        runtime,
		ActionDigest:   digest,
		Requester:      requester,
		RequiredPerm:   snap.RequiredPerm,
		IdempotencyKey: req.IdempotencyKey,
	})
	switch claim.Outcome {
	case ClaimAlreadyAccepted:
		// Idempotent replay of an accepted key+digest: success, no re-delivery.
		h.writeApprovalSuccess(w, sessionID, approvalID, req.Action, selected.Kind, "already_accepted")
		return
	case ClaimGranted:
		// proceed to delivery
	default:
		code, oc := claimOutcomeHTTP(claim.Outcome)
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, oc, code)
		return
	}

	// B4 — revalidate the runtime immediately before delivery. A launch replacement,
	// stream change, delete, or unlink between claim and delivery must NOT deliver to
	// a wrong/absent runtime nor return success.
	cur, ok := h.RuntimeOf(sessionID)
	if !ok || !cur.equal(runtime) {
		h.Approvals.RecordDelivery(sessionID, approvalID, claim.Token, DeliveryReceipt{
			Outcome: DeliveryStaleRuntime, ApprovalID: approvalID, SessionID: sessionID, ActionDigest: digest, IdempotencyKey: req.IdempotencyKey,
		})
		h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, "stale_runtime", http.StatusConflict)
		return
	}

	// Dedicated approval delivery boundary (B3) — never CommandBroker.
	delivery := h.ApprovalDelivery
	if delivery == nil {
		delivery = NewUnavailableApprovalDelivery()
	}
	receipt := delivery.Deliver(ApprovalDeliveryRequest{
		ClaimToken:     claim.Token,
		ApprovalID:     approvalID,
		SessionID:      sessionID,
		Runtime:        runtime,
		ActionDigest:   digest,
		IdempotencyKey: req.IdempotencyKey,
		Payload:        serverPayloadFor(ca),
	})
	commit := h.Approvals.RecordDelivery(sessionID, approvalID, claim.Token, receipt)
	if commit.Committed {
		h.writeApprovalSuccess(w, sessionID, approvalID, req.Action, selected.Kind, string(commit.Outcome))
		return
	}
	code, oc := deliveryOutcomeHTTP(commit.Outcome)
	h.writeApprovalOutcome(w, sessionID, approvalID, req.Action, oc, code)
}

// requesterFromPrincipal derives the server-authenticated requester. Only
// server-verified fields are used; nothing comes from the client body.
func requesterFromPrincipal(p *devicetrust.Principal) RequesterContext {
	return RequesterContext{
		DeviceID:        p.DeviceID,
		HostID:          p.HostID,
		BearerSessionID: p.BearerSessionID,
		BootID:          p.DeviceBootID,
		Permissions:     append([]string(nil), p.Permissions...),
	}
}

// canonicalActionFor builds the delivery-semantic canonical action from the stored
// option and the user input. Display label/prompt and raw payload are never inputs.
func canonicalActionFor(opt *agent.InteractionOption, input string) CanonicalAction {
	inputType, placement := "", ""
	if opt.Input != nil {
		inputType = "text"
		placement = opt.Input.Placement
	}
	return CanonicalAction{
		OptionID:        opt.ID,
		Kind:            opt.Kind,
		SchemaVersion:   ActionSchemaVersion,
		InputType:       inputType,
		InputPlacement:  placement,
		NormalizedInput: input,
	}
}

// serverPayloadFor returns the exact server-side delivery bytes for a canonical
// action. It NEVER synthesizes a decision keystroke (no blind y/n). Only the user's
// literal input for an explicit input-bearing action is forwarded; decision-only
// actions carry no payload (there is no accepted decision-delivery channel).
func serverPayloadFor(ca CanonicalAction) []byte {
	if ca.InputType == "text" && (ca.InputPlacement == "as_payload" || ca.InputPlacement == "after_payload") && ca.NormalizedInput != "" {
		return []byte(ca.NormalizedInput + "\n")
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

// claimOutcomeHTTP maps a non-granted claim outcome to an HTTP status + code. None
// map to 2xx — a denied claim is never a success.
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
	default:
		return http.StatusInternalServerError, "error"
	}
}

// deliveryOutcomeHTTP maps a non-success delivery outcome to an HTTP status + code.
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

func (h *Handlers) writeApprovalSuccess(w http.ResponseWriter, sessionID, approvalID, action, kind, outcome string) {
	log.Printf("APPROVAL ACTION: session=%s approval=%s action=%s kind=%s outcome=%s http=200",
		sessionID, approvalID, action, kind, outcome)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "action": action, "outcome": outcome})
}

func (h *Handlers) writeApprovalOutcome(w http.ResponseWriter, sessionID, approvalID, action, outcome string, code int) {
	log.Printf("APPROVAL ACTION: session=%s approval=%s action=%s outcome=%s http=%d",
		sessionID, approvalID, action, outcome, code)
	http.Error(w, outcome, code)
}
