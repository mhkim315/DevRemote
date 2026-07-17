// Package term — C3D-A: atomic approval-capability activation for the
// managed Claude runtime.
//
// InstallApprovalExecution is the ONE production-owned transition that
// enables actionable catalog ingestion and returns the certified Claude
// delivery boundary plus the current-runtime resolver. It either commits
// completely (store owned + actionable ingest enabled for SUBSEQUENTLY
// created runtimes + boundary and resolver returned) or fails with ZERO
// activation change. Precisely because the composition root configures the
// observation store BEFORE installing, a store binding created earlier by
// SetApprovalStore is NOT unwound by a failed install — that is the accepted
// C1D observation-only state, not a partial activation.
//
// Activation is limited to the exact one-entry closed catalog
// (claude.bash.approval_probe.v1). The provider-only provenActionMapping
// switch is untouched; the deepest Store admission boundary independently
// re-verifies the certified tuple (approval_store_gen.go).
package term

import (
	"fmt"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/mux"
)

// certifiedClaudeAuthorityVersion is the exact C0D-certified provider
// version. Provider name/version alone never certifies an action; this
// constant only gates the install transition and the launch tuple.
const certifiedClaudeAuthorityVersion = "2.1.209"

// claudeCertifiedOptions returns the exact certified action set for the
// single catalog entry (C3D contract §3): allow_once → approve and
// deny → reject. Nothing else is certified.
func claudeCertifiedOptions() []agent.InteractionOption {
	return []agent.InteractionOption{
		{ID: "allow_once", Label: "Allow once", Kind: "approve"},
		{ID: "deny", Label: "Deny", Kind: "reject"},
	}
}

// claudeCertifiedDeliveryMaterial generates the immutable per-option delivery
// material: the exact C0D-certified hook response bytes under the certified
// decision schema. Daemon-generated at the ingest site; the Store admission
// boundary recomputes and compares these exact bytes.
func claudeCertifiedDeliveryMaterial() []ApprovalDeliveryMaterial {
	return []ApprovalDeliveryMaterial{
		{OptionID: "allow_once", SchemaVersion: claudeDecisionSchemaV1, ResponseBytes: claudeHookResponseBytes("allow")},
		{OptionID: "deny", SchemaVersion: claudeDecisionSchemaV1, ResponseBytes: claudeHookResponseBytes("deny")},
	}
}

// InstallApprovalExecution is the ONE production-owned Claude activation
// transition (C3D contract §1). Under a single critical section it verifies
// every precondition and enables actionable catalog ingestion for every
// SUBSEQUENTLY created runtime, returning the certified delivery boundary
// and the current-runtime resolver. On ANY precondition failure nothing is
// mutated and every capability stays off:
//
//  1. the approval store must be present;
//  2. the service must not be shutting down;
//  3. the configured observation store and the installed execution store
//     must be the SAME canonical store;
//  4. NO runtime may exist yet (activation precedes the first epoch, so no
//     record can predate activation and no upgrade path can exist);
//  5. a second installation is an error, never an idempotent success;
//  6. the pinned Version and AuthorityVersion must be exactly the certified
//     version and grammar-valid;
//  7. the launch-certification seam must be complete: pinned path present
//     and pinned digest exactly 64 lowercase hex (per-spawn Certify stays
//     fail-closed on every spawn and resume);
//  8. {OS, arch} must be a member of the compiled closed
//     certifiedClaudePlatforms set (the exact C0D-certified tuple).
func (s *ManagedClaudeService) InstallApprovalExecution(store *AuthoritativeApprovalStore) (ApprovalDelivery, func(string) (RuntimeRef, bool), error) {
	if store == nil {
		return nil, nil, fmt.Errorf("claude approval execution install: no approval store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return nil, nil, fmt.Errorf("claude approval execution install: service is shutting down")
	}
	if s.approvals != nil && s.approvals != store {
		return nil, nil, fmt.Errorf("claude approval execution install: a different approval store is already configured")
	}
	if s.gen != 0 || len(s.runtimes) != 0 {
		return nil, nil, fmt.Errorf("claude approval execution install: activation must precede the first managed runtime")
	}
	if s.actionable {
		return nil, nil, fmt.Errorf("claude approval execution install: already installed")
	}
	if s.cfg.AuthorityVersion != certifiedClaudeAuthorityVersion || !validVersion(s.cfg.AuthorityVersion) {
		return nil, nil, fmt.Errorf("claude approval execution install: authority version %q is not the certified %q",
			s.cfg.AuthorityVersion, certifiedClaudeAuthorityVersion)
	}
	if s.cfg.Version != certifiedClaudeAuthorityVersion {
		return nil, nil, fmt.Errorf("claude approval execution install: pinned version %q is not the certified %q",
			s.cfg.Version, certifiedClaudeAuthorityVersion)
	}
	if s.cfg.PinnedPath == "" {
		return nil, nil, fmt.Errorf("claude approval execution install: pinned path not configured")
	}
	if len(s.cfg.PinnedDigest) != 64 || !allHex(s.cfg.PinnedDigest) {
		return nil, nil, fmt.Errorf("claude approval execution install: pinned digest not configured")
	}
	if !claudePlatformCertified(launchOS, launchArch) {
		return nil, nil, fmt.Errorf("claude approval execution install: platform %s/%s is not certified", launchOS, launchArch)
	}
	// Linearization point: both mutations commit together under s.mu.
	s.approvals = store
	s.actionable = true
	return NewClaudeManagedApprovalDelivery(s), s.RuntimeOf, nil
}

// ApprovalExecutionInstalled reports whether the atomic activation
// transition has committed. Used by the composition root's tolerance rule.
func (s *ManagedClaudeService) ApprovalExecutionInstalled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.actionable
}

// RuntimeOf resolves the CURRENT runtime identity of one managed Claude
// session for the approval action handler (C3D contract §6). It succeeds
// ONLY for an installed service and a certified, current-epoch runtime that
// is either live (pre-defer) or in the expected joined-deferred exit window
// (B4 semantics: the initial process exited after tool_deferred and its
// coordinator identity is still alive). Everything else fails closed:
// exit without join, timeout, stop/kill/delete, replacement and daemon
// restart all leave no resolvable authority.
func (s *ManagedClaudeService) RuntimeOf(sessionID string) (RuntimeRef, bool) {
	s.mu.Lock()
	rt := s.runtimes[sessionID]
	actionable := s.actionable
	coord := s.coordinator
	s.mu.Unlock()
	if !actionable || rt == nil {
		return RuntimeRef{}, false
	}
	rec, ok := s.reg.Get(sessionID)
	if !ok || rec.Epoch != rt.epoch {
		return RuntimeRef{}, false
	}
	// C3D §6 condition 5: only a certified launch incarnation resolves.
	if rec.CertResult != claudeCertCertified {
		return RuntimeRef{}, false
	}
	if rec.Exited {
		if !rt.deferredExit.Load() {
			return RuntimeRef{}, false
		}
		if coord == nil || !coord.HasIdentityForRuntime(sessionID, rt.epoch) {
			return RuntimeRef{}, false
		}
	}
	return RuntimeRef{Adapter: claudeHeadlessAdapter, Version: rt.authorityVersion, LaunchGen: rt.epoch, StreamGen: 0}, true
}

// claudeRequiredPerm is the stored permission every actionable catalog
// record requires (same permission the mobile approval route enforces).
const claudeRequiredPerm = devicetrust.PermTerminalInput

// ── Composition-boundary dispatch ──

// claudeDispatchingApprovalDelivery routes a binding whose runtime adapter
// is the managed Claude namespace to the certified Claude boundary; every
// other binding continues to the frozen fallback (the capacity-0 gate in
// production). It is composed into the FALLBACK slot of the accepted Codex
// dispatcher, so Codex routing is byte-for-byte unchanged and unknown
// adapters still terminate at the capacity-zero gate.
type claudeDispatchingApprovalDelivery struct {
	claude   ApprovalDelivery
	fallback ApprovalDelivery
}

// NewClaudeDispatchingApprovalDelivery builds the Claude fallback dispatcher.
func NewClaudeDispatchingApprovalDelivery(claude, fallback ApprovalDelivery) ApprovalDelivery {
	return claudeDispatchingApprovalDelivery{claude: claude, fallback: fallback}
}

func (d claudeDispatchingApprovalDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	if req.Binding.Runtime.Adapter == claudeHeadlessAdapter && d.claude != nil {
		return d.claude.Deliver(req)
	}
	if d.fallback == nil {
		return DeliveryReceipt{Outcome: DeliveryUnavailable, ClaimToken: req.ClaimToken, Binding: req.Binding}
	}
	return d.fallback.Deliver(req)
}

// NewCombinedRuntimeResolver dispatches current-runtime resolution by the
// canonical session-ID adapter prefix at the composition boundary. Sessions
// of an unknown adapter — or of a provider whose resolver is not installed —
// never resolve. No generic authority gains a provider branch: this is the
// one boundary-owned dispatch point.
func NewCombinedRuntimeResolver(codex, claude func(string) (RuntimeRef, bool)) func(string) (RuntimeRef, bool) {
	return func(sessionID string) (RuntimeRef, bool) {
		switch mux.ParseSessionID(sessionID).Adapter {
		case codexAppServerAdapter:
			if codex == nil {
				return RuntimeRef{}, false
			}
			return codex(sessionID)
		case claudeHeadlessAdapter:
			if claude == nil {
				return RuntimeRef{}, false
			}
			return claude(sessionID)
		default:
			return RuntimeRef{}, false
		}
	}
}
