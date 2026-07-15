// Package term — SP1-P2B: atomic approval-capability activation for the
// managed Codex runtime.
//
// InstallApprovalExecution is the ONE production-owned transition that
// enables actionable ingestion and returns the certified delivery endpoint.
// It either commits completely (store wired + actionable ingest enabled +
// boundary and current-runtime resolver returned) or fails with NOTHING
// changed: ingestion stays non-actionable (P1 observation) and the caller
// keeps the capacity-0 gate. Activation must precede the FIRST runtime, is
// copied per-runtime at create time, and is never toggled mid-life — no
// record ingested before activation can ever be upgraded.
package term

import (
	"fmt"

	"devremote/companion-daemon/internal/agent"
)

// codexCertifiedOptions returns the exact certified action set (SP1 contract
// §2: allow_once → accept and deny → decline; amendment and cancel remain
// non-actionable).
func codexCertifiedOptions() []agent.InteractionOption {
	return []agent.InteractionOption{
		{ID: "allow_once", Label: "Allow once", Kind: "approve"},
		{ID: "deny", Label: "Deny", Kind: "reject"},
	}
}

// codexDecisionResponse builds the EXACT daemon-generated provider response
// bytes for one certified decision, echoing the preserved native id token
// with the same JSON type and value (CP0-proven response shape).
func codexDecisionResponse(idToken, decision string) []byte {
	return []byte(`{"jsonrpc":"2.0","id":` + idToken + `,"result":{"decision":"` + decision + `"}}`)
}

// codexDeliveryMaterialFor generates the immutable per-option delivery
// material for one observed request under the certified schema identity.
func codexDeliveryMaterialFor(idToken string) []ApprovalDeliveryMaterial {
	return []ApprovalDeliveryMaterial{
		{OptionID: "allow_once", SchemaVersion: codexDecisionSchemaV1, ResponseBytes: codexDecisionResponse(idToken, "accept")},
		{OptionID: "deny", SchemaVersion: codexDecisionSchemaV1, ResponseBytes: codexDecisionResponse(idToken, "decline")},
	}
}

// InstallApprovalExecution is the ONE production-owned activation transition
// (SP1 P2B). Under a single critical section it verifies every precondition
// and enables actionable ingestion for every SUBSEQUENTLY created runtime,
// returning the certified delivery boundary and the current-runtime
// resolver. On ANY precondition failure nothing is changed and both
// capabilities stay off:
//
//   - the approval store must be present;
//   - the service must not be shutting down;
//   - the pinned AuthorityVersion must be exactly the certified version and
//     grammar-valid;
//   - NO runtime may exist yet (activation precedes the first epoch, so no
//     record can predate activation);
//   - a second installation is an error, never an idempotent success.
func (s *ManagedCodexService) InstallApprovalExecution(store *AuthoritativeApprovalStore) (ApprovalDelivery, func(string) (RuntimeRef, bool), error) {
	if store == nil {
		return nil, nil, fmt.Errorf("approval execution install: no approval store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return nil, nil, fmt.Errorf("approval execution install: service is shutting down")
	}
	if s.cfg.AuthorityVersion != certifiedCodexAuthorityVersion || !validVersion(s.cfg.AuthorityVersion) {
		return nil, nil, fmt.Errorf("approval execution install: authority version %q is not the certified %q",
			s.cfg.AuthorityVersion, certifiedCodexAuthorityVersion)
	}
	if s.gen != 0 || len(s.runtimes) != 0 {
		return nil, nil, fmt.Errorf("approval execution install: activation must precede the first managed runtime")
	}
	if s.actionable {
		return nil, nil, fmt.Errorf("approval execution install: already installed")
	}
	// P2B-R1: the configured observation store and the installed execution
	// store must be the SAME canonical store — a diverging handler/ingest
	// store pair can never exist.
	if s.approvals != nil && s.approvals != store {
		return nil, nil, fmt.Errorf("approval execution install: a different approval store is already configured")
	}
	s.approvals = store
	s.actionable = true
	return NewCodexManagedApprovalDelivery(s), s.RuntimeOf, nil
}

// RuntimeOf resolves the CURRENT runtime identity of one managed session for
// the approval action handler. It succeeds ONLY for an installed service and
// a live, current-epoch, non-exited managed runtime; everything else fails
// closed. Non-managed sessions are never resolved (their records are never
// actionable).
func (s *ManagedCodexService) RuntimeOf(sessionID string) (RuntimeRef, bool) {
	s.mu.Lock()
	rt := s.runtimes[sessionID]
	actionable := s.actionable
	s.mu.Unlock()
	if !actionable || rt == nil {
		return RuntimeRef{}, false
	}
	rec, ok := s.reg.Get(sessionID)
	if !ok || rec.Exited || rec.Epoch != rt.epoch {
		return RuntimeRef{}, false
	}
	return RuntimeRef{Adapter: codexAppServerAdapter, Version: rt.authorityVersion, LaunchGen: rt.epoch, StreamGen: 0}, true
}

// dispatchingApprovalDelivery routes a binding whose runtime adapter is the
// managed codex namespace to the certified P2A boundary; every other binding
// goes to the frozen capacity-0 gate (unavailable). There is no queue on the
// certified path, so queue admission can never be reported as success.
type dispatchingApprovalDelivery struct {
	codex    ApprovalDelivery
	fallback ApprovalDelivery
}

// NewDispatchingApprovalDelivery builds the production delivery dispatcher.
func NewDispatchingApprovalDelivery(codex, fallback ApprovalDelivery) ApprovalDelivery {
	return dispatchingApprovalDelivery{codex: codex, fallback: fallback}
}

func (d dispatchingApprovalDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	if req.Binding.Runtime.Adapter == codexAppServerAdapter && d.codex != nil {
		return d.codex.Deliver(req)
	}
	if d.fallback == nil {
		return DeliveryReceipt{Outcome: DeliveryUnavailable, ClaimToken: req.ClaimToken, Binding: req.Binding}
	}
	return d.fallback.Deliver(req)
}
