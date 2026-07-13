package term

// A1-A — Approval-safety contract freeze.
//
// This file freezes the CLOSED internal state machine and outcome vocabulary for
// the approval-safety boundary. It deliberately does NOT reuse the agent-activity
// runtime vocabulary (thinking/working/waiting_*): an approval is an authoritative,
// at-most-once user DECISION request, not advisory activity. Runtime status
// (including waiting_approval) can display attention only and MUST never create an
// approval record, option, CTA, or action authority — that authority begins solely
// with accepted-adapter DetectApproval (A1-C) and is enforced by the store (A1-B)
// and the action boundary (A1-D).
//
// Nothing here fabricates authority. The types are the shared vocabulary the later
// packets bind to; the store, ingestion, and action boundary enforce it.

// ApprovalState is the closed lifecycle state of one authoritative approval
// request. It is INTERNAL: the public AgentApproval.Status DTO is a bounded
// projection of these states (see projectPublicStatus). The set is total and
// closed — an unrecognized value is not a valid state and must fail closed.
type ApprovalState string

const (
	// ApprovalPending — created from accepted evidence, awaiting a user decision.
	ApprovalPending ApprovalState = "pending"
	// ApprovalExecuting — a user decision has been accepted and its exact action is
	// being delivered to the owned delivery boundary. This is the at-most-once
	// in-flight reservation: it is entered by an atomic CAS from Pending BEFORE any
	// delivery, so a concurrent second submission observes a non-Pending state and
	// is rejected. A decision is NEVER recorded as successfully resolved before its
	// action is durably accepted — the request commits from Executing only after
	// the delivery boundary result is known.
	ApprovalExecuting ApprovalState = "executing"
	// ApprovalApproved — decision delivered and durably accepted, approve semantics.
	ApprovalApproved ApprovalState = "approved"
	// ApprovalRejected — decision delivered (or no delivery required), reject/cancel
	// semantics.
	ApprovalRejected ApprovalState = "rejected"
	// ApprovalResolved — decision delivered and durably accepted, neutral semantics.
	ApprovalResolved ApprovalState = "resolved"
	// ApprovalDeliveryFailed — a user decision was made but its required action could
	// NOT be durably accepted by the owned delivery boundary. Terminal and
	// fail-closed: the decision is consumed (at most once) but must never be reported
	// as successfully executed. Distinct from the resolved states so the product can
	// tell the user their decision did not take effect.
	ApprovalDeliveryFailed ApprovalState = "delivery_failed"
	// ApprovalExpired — the request TTL elapsed before any decision was accepted.
	ApprovalExpired ApprovalState = "expired"
	// ApprovalInvalidated — the request was superseded before a decision by a
	// runtime/launch generation change, stream-generation change, correlation loss,
	// or session delete/unlink/relink. It carries no residual action authority.
	ApprovalInvalidated ApprovalState = "invalidated"
)

// approvalStateValid is the closed membership set. A value outside it is not a
// state and callers must treat it as fail-closed (no authority).
var approvalStateValid = map[ApprovalState]bool{
	ApprovalPending:        true,
	ApprovalExecuting:      true,
	ApprovalApproved:       true,
	ApprovalRejected:       true,
	ApprovalResolved:       true,
	ApprovalDeliveryFailed: true,
	ApprovalExpired:        true,
	ApprovalInvalidated:    true,
}

// IsValidApprovalState reports whether s is a member of the closed state set.
func IsValidApprovalState(s ApprovalState) bool { return approvalStateValid[s] }

// approvalTerminal is the closed set of states from which NO transition is
// allowed. Terminality is what makes a user decision at-most-once: once a request
// is terminal it can never be re-decided, re-delivered, or restored.
var approvalTerminal = map[ApprovalState]bool{
	ApprovalApproved:       true,
	ApprovalRejected:       true,
	ApprovalResolved:       true,
	ApprovalDeliveryFailed: true,
	ApprovalExpired:        true,
	ApprovalInvalidated:    true,
}

// IsTerminalApprovalState reports whether s admits no further transition.
func IsTerminalApprovalState(s ApprovalState) bool { return approvalTerminal[s] }

// approvalTransitions is the frozen, total transition table. A transition absent
// from this table is forbidden. The only legal paths are:
//
//	pending   → executing     (user decision accepted; reserve before delivery)
//	pending   → expired       (TTL elapsed with no decision)
//	pending   → invalidated   (generation/correlation loss / session delete)
//	executing → approved      (delivery durably accepted, approve kind)
//	executing → rejected      (delivery durably accepted / not required, reject kind)
//	executing → resolved      (delivery durably accepted, neutral kind)
//	executing → delivery_failed (delivery could not be confirmed — fail closed)
//
// Note there is intentionally NO executing→invalidated and NO executing→pending:
// once a decision is committed to delivery it resolves to exactly one terminal
// outcome (a runtime replacement mid-delivery surfaces as delivery_failed, never a
// silent drop or a re-offer).
var approvalTransitions = map[ApprovalState]map[ApprovalState]bool{
	ApprovalPending: {
		ApprovalExecuting:   true,
		ApprovalExpired:     true,
		ApprovalInvalidated: true,
	},
	ApprovalExecuting: {
		ApprovalApproved:       true,
		ApprovalRejected:       true,
		ApprovalResolved:       true,
		ApprovalDeliveryFailed: true,
	},
}

// CanTransitionApproval reports whether from→to is a permitted transition. It is
// false for any unknown state and for every terminal `from` (fail closed).
func CanTransitionApproval(from, to ApprovalState) bool {
	if !IsValidApprovalState(from) || !IsValidApprovalState(to) {
		return false
	}
	return approvalTransitions[from][to]
}

// terminalStateForKind maps a validated option Kind to the terminal state a
// confirmed delivery commits to. It is the ONLY authority for resolution status —
// never the action ID. Unknown kinds resolve to the neutral ApprovalResolved.
func terminalStateForKind(kind string) ApprovalState {
	switch kind {
	case "approve":
		return ApprovalApproved
	case "reject", "cancel":
		return ApprovalRejected
	default:
		return ApprovalResolved
	}
}

// projectPublicStatus maps an internal ApprovalState to the bounded public
// AgentApproval.Status value the mobile client decodes. The public vocabulary is
// the closed set {pending, approved, rejected, resolved, delivery_failed, expired,
// invalidated}. The transient in-flight reservation `executing` is internal only
// and projects to `pending` (the request is not yet resolved and must not read as
// a terminal outcome); mobile disables re-submission from its own local in-flight
// state, not from this field. An unknown internal state fails closed to `expired`
// (a safe non-actionable terminal value) rather than leaking an unmodeled string.
func projectPublicStatus(s ApprovalState) string {
	switch s {
	case ApprovalPending, ApprovalExecuting:
		return string(ApprovalPending)
	case ApprovalApproved:
		return string(ApprovalApproved)
	case ApprovalRejected:
		return string(ApprovalRejected)
	case ApprovalResolved:
		return string(ApprovalResolved)
	case ApprovalDeliveryFailed:
		return string(ApprovalDeliveryFailed)
	case ApprovalExpired:
		return string(ApprovalExpired)
	case ApprovalInvalidated:
		return string(ApprovalInvalidated)
	default:
		return string(ApprovalExpired)
	}
}

// publicApprovalStatusValid is the closed public-DTO status vocabulary. The A1-E
// mobile strict decoder mirrors this exact set.
var publicApprovalStatusValid = map[string]bool{
	string(ApprovalPending):        true,
	string(ApprovalApproved):       true,
	string(ApprovalRejected):       true,
	string(ApprovalResolved):       true,
	string(ApprovalDeliveryFailed): true,
	string(ApprovalExpired):        true,
	string(ApprovalInvalidated):    true,
}

// IsPublicApprovalStatus reports membership in the closed public status set.
func IsPublicApprovalStatus(s string) bool { return publicApprovalStatusValid[s] }

// ActionOutcome is the closed vocabulary of results the authenticated action
// boundary (A1-D) may report. It is a fail-closed set: every non-ok value denies
// execution and none of them ever implies the requested action ran unless it is
// exactly OutcomeOK. Outcomes carry no raw prompt, input, path, or token — only an
// ID-level classification suitable for audit logs.
type ActionOutcome string

const (
	OutcomeOK              ActionOutcome = "ok"               // action durably accepted
	OutcomeNotFound        ActionOutcome = "not_found"        // no such session/approval
	OutcomeSessionMismatch ActionOutcome = "session_mismatch" // approval not owned by session
	OutcomeExpired         ActionOutcome = "expired"          // TTL elapsed
	OutcomeAlreadyTerminal ActionOutcome = "already_terminal" // already resolved or in flight
	OutcomeStaleGeneration ActionOutcome = "stale_generation" // launch/stream generation moved
	OutcomeUnknownAction   ActionOutcome = "unknown_action"   // action ID not an allowed option
	OutcomeInputRejected   ActionOutcome = "input_rejected"   // unknown field/oversize/bad utf8/missing/not accepted
	OutcomeDeliveryFailed  ActionOutcome = "delivery_failed"  // decision made, delivery unconfirmable
)

// actionOutcomeValid is the closed membership set for outcomes.
var actionOutcomeValid = map[ActionOutcome]bool{
	OutcomeOK:              true,
	OutcomeNotFound:        true,
	OutcomeSessionMismatch: true,
	OutcomeExpired:         true,
	OutcomeAlreadyTerminal: true,
	OutcomeStaleGeneration: true,
	OutcomeUnknownAction:   true,
	OutcomeInputRejected:   true,
	OutcomeDeliveryFailed:  true,
}

// IsValidActionOutcome reports membership in the closed outcome set.
func IsValidActionOutcome(o ActionOutcome) bool { return actionOutcomeValid[o] }

// outcomeHTTPStatus maps each closed outcome to its HTTP status. OK→200; input
// problems→400; auth is enforced by middleware before the handler, so the handler
// never returns 401/403 itself. A stale generation, expiry, already-terminal, or
// mismatch are all "the thing you're acting on is no longer the current authority"
// and map to 409/410 so the client re-reads rather than retrying blindly. A
// delivery failure is a server-side inability to complete and fails closed as 502.
func outcomeHTTPStatus(o ActionOutcome) int {
	switch o {
	case OutcomeOK:
		return 200
	case OutcomeUnknownAction, OutcomeInputRejected:
		return 400
	case OutcomeNotFound, OutcomeSessionMismatch:
		return 404
	case OutcomeAlreadyTerminal, OutcomeStaleGeneration:
		return 409
	case OutcomeExpired:
		return 410
	case OutcomeDeliveryFailed:
		return 502
	default:
		return 500 // unknown outcome must never read as success
	}
}
