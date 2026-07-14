package term

import (
	"context"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/devicetrust"
)

// A1-C — accepted-adapter approval ingestion.
//
// Approvals become authority ONLY here, from the accepted version-specific
// adapter's frozen DetectApproval, fed the SAME accepted, version/correlation-gated
// event batch that already drives Transcript and status. Nothing on the terminal /
// prompt / screen / process / legacy-parser side can manufacture an approval.

// provenActionMapping returns the frozen, controlled-fixture-proven action mapping
// for a provider, and whether an actionable mapping exists. B5: an approval may
// expose action buttons ONLY when controlled fixtures prove the exact provider
// event → option/action ID → normalized input schema → terminal bytes/keys →
// placement → supported version. NO such mapping exists today: Codex's log only
// OBSERVES its own resolution and there is no verified resolution channel, and blind
// terminal Y/N synthesis is prohibited. So every provider is non-actionable
// intervention information (no options, no claim, no delivery) until a mapping is
// separately proven with controlled redacted evidence.
func provenActionMapping(provider string) (options []agent.InteractionOption, actionable bool) {
	return nil, false
}

// hasApprovalCap reports whether an accepted adapter advertises the frozen
// CapApprovalDetection capability. DetectApproval is only called on adapters that
// do — an adapter that does not (e.g. Claude 2.1.202) produces zero approvals.
func hasApprovalCap(a contract.AgentAdapter) bool {
	if a == nil {
		return false
	}
	for _, c := range a.Descriptor().Capabilities {
		if c == contract.CapApprovalDetection {
			return true
		}
	}
	return false
}

// safeDetectApproval calls the accepted adapter's frozen DetectApproval with
// capability, panic, and error isolation. A missing capability, an error, or a
// panic yields NO approvals (and never propagates to the poll loop). This is the
// approval analogue of safeGetStatus.
func safeDetectApproval(adapter contract.AgentAdapter, events []agent.AgentEvent) (out []agent.AgentApproval) {
	defer func() {
		if r := recover(); r != nil {
			out = nil // adapter panic → no approval authority
		}
	}()
	if !hasApprovalCap(adapter) {
		return nil
	}
	res, err := adapter.DetectApproval(context.Background(), events)
	if err != nil {
		return nil
	}
	return res
}

// ingestApprovals derives authoritative approval requests from the accepted batch
// and ingests them under the current launch/stream generation. It runs only on a
// managed-launch correlated batch (the caller gates this). Each detected approval
// is re-bound to the exact source event's authoritative provenance and the frozen
// provider option mapping; an approval without a matching authoritative source
// event, without a frozen option mapping, or cross-session is dropped. The
// generation-bound store applies the remaining generation/duplicate/provenance
// rules.
func (s *TelemetryService) ingestApprovals(sessionID string, launchGen int64, streamGen int, provider, version string, events []agent.AgentEvent) {
	adapter := acceptedAdapterFor(provider)
	detected := safeDetectApproval(adapter, events)
	if len(detected) == 0 {
		return
	}

	// Index the exact source-event provenance by ApprovalID. Only an authoritative
	// approval_requested event carries an ApprovalID here (SafeApprovalGate ran in
	// DetectApproval), so this both provides provenance and re-verifies the
	// approval traces to a real event in THIS batch.
	prov := make(map[string]contract.Provenance, len(events))
	for _, e := range events {
		if e.ApprovalID != "" {
			prov[e.ApprovalID] = contract.Provenance(e.Provenance)
		}
	}

	// B5: an actionable option set exists ONLY when a controlled fixture has proven
	// this provider's exact evidence→action→delivery mapping. None exists today, so
	// the approval is ingested as NON-ACTIONABLE intervention information: no options,
	// no claim path, no delivery. It is still recorded (bound to the exact ApprovalID,
	// provenance, and generation) so the user is honestly informed.
	opts, actionable := provenActionMapping(provider)

	items := make([]ApprovalIngestItem, 0, len(detected))
	for _, a := range detected {
		if a.ID == "" {
			continue
		}
		p, ok := prov[a.ID]
		if !ok || !contract.ApprovalAuthoritative(p) {
			continue // no authoritative source event for this id → drop
		}
		req := a // copy
		req.SessionID = sessionID
		req.AgentKind = provider
		req.Kind = "approval"
		req.Options = opts
		req.Default = ""
		items = append(items, ApprovalIngestItem{
			Approval:     req,
			Provenance:   p,
			Actionable:   actionable,
			RequiredPerm: devicetrust.PermTerminalInput,
		})
	}
	if len(items) == 0 {
		return
	}
	s.approvals.Ingest(ApprovalIngest{
		SessionID: sessionID,
		LaunchGen: launchGen,
		StreamGen: streamGen,
		Provider:  provider,
		Version:   version,
		Items:     items,
	})
}
