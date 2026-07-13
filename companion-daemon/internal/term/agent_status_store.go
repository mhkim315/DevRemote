package term

import (
	"context"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// S1-B — Internal agent-activity status resolver and bounded session-owned store.
//
// This is the provider-neutral status boundary. It consumes ONLY accepted,
// session-bound AgentEvents, delegates final precedence/resolution to the
// accepted version-specific adapter's frozen GetStatus (which delegates to
// contract.ResolveStatus), and stores exactly one immutable current result per
// canonical session ID.
//
// Authority rules enforced here (handoff §208-239):
//   - agent activity is resolved ONLY from accepted evidence via GetStatus;
//     lifecycle and connectivity health are NEVER inputs to GetStatus.
//   - cross-session events are rejected (evidence is filtered to the target id).
//   - an older stream generation can never overwrite a newer session generation.
//   - adapter error/panic isolates to a degraded result for that one session.
//   - a delete clears the record; a recreated session cannot inherit old status.
//   - the stale policy is read-time only: elapsed time may mark a prior result
//     stale, but never manufactures a new activity status.
//
// It contains no HTTP/mobile code (that is S1-D) and no production polling wiring
// (that is S1-C).

const (
	// maxDegradedReason bounds the stored degraded reason; contract.Degrade already
	// sanitizes/bounds, this is a defensive second cap so total state stays bounded.
	maxDegradedReason = 240
	// defaultActivityStaleAfter is the deterministic staleness horizon for a stored
	// result. It marks a prior result stale; it never changes the stored status.
	defaultActivityStaleAfter = 45 * time.Second
)

// AgentActivityRecord is the immutable current agent-activity result for one
// session. It is advisory activity state — never lifecycle authority.
type AgentActivityRecord struct {
	SessionID      string
	Status         agent.AgentStatus
	Provenance     contract.Provenance
	Confidence     float64
	Degraded       bool
	DegradedReason string
	ObservedAt     time.Time
	Generation     int    // adapterState.streamGen binding this result belongs to
	Version        string // accepted provider version binding
}

// AgentStatusUpdate is one resolution request for a single session. It carries
// only accepted, session-bound, correlation/version-gated inputs; the caller
// (S1-C) sources these from the accepted-adapter hook in processSession.
type AgentStatusUpdate struct {
	SessionID  string
	Generation int
	Version    string
	Events     []agent.AgentEvent
	Adapter    contract.AgentAdapter // accepted T1/T2 adapter; its GetStatus resolves
}

// AgentStatusStore holds one current AgentActivityRecord per canonical session ID.
type AgentStatusStore struct {
	mu         sync.Mutex
	records    map[string]AgentActivityRecord
	now        func() time.Time
	staleAfter time.Duration
}

// NewAgentStatusStore builds an empty store with the production clock.
func NewAgentStatusStore() *AgentStatusStore {
	return &AgentStatusStore{
		records:    make(map[string]AgentActivityRecord),
		now:        time.Now,
		staleAfter: defaultActivityStaleAfter,
	}
}

// buildStatusEvidence maps accepted AgentEvents to closed, conservative
// StatusEvidence for the target session. Cross-session events are dropped. Only
// the closed set below produces evidence; every other event type contributes
// nothing (so it can never fabricate a status). Provenance and confidence come
// from the event and are NEVER upgraded — an empty provenance defaults to the
// accepted path's native_log; a known provenance is passed through verbatim; an
// unknown non-empty provenance is passed through and dropped by ResolveStatus.
func buildStatusEvidence(events []agent.AgentEvent, sessionID string) []contract.StatusEvidence {
	out := make([]contract.StatusEvidence, 0, len(events))
	for _, ev := range events {
		if ev.SessionID != "" && ev.SessionID != sessionID {
			continue // cross-session evidence is rejected
		}
		var status agent.AgentStatus
		switch ev.Type {
		case agent.EventThinking:
			status = agent.StatusThinking
		case agent.EventToolCallStarted:
			status = agent.StatusWorking
		case agent.EventWaitingInput:
			status = agent.StatusWaitingInput
		case agent.EventApprovalRequested:
			status = agent.StatusWaitingApproval // display-only activity; A1 owns approval action state
		case agent.EventCompleted:
			status = agent.StatusCompleted
		case agent.EventFailed:
			status = agent.StatusFailed
		case agent.EventInterrupted:
			status = agent.StatusInterrupted
		default:
			continue // agent_started/user_message/assistant_message/tool_call_finished/
			// approval_resolved/unknown → no evidence (never infer a state from these)
		}
		prov := contract.Provenance(ev.Provenance)
		if ev.Provenance == "" {
			prov = contract.ProvenanceNativeLog
		}
		out = append(out, contract.StatusEvidence{
			Status:     status,
			Provenance: prov,
			Confidence: ev.Confidence, // never upgraded; ResolveStatus clamps/ceilings
		})
	}
	return out
}

// safeGetStatus calls the accepted adapter's frozen GetStatus with panic and
// error isolation. A panicking or erroring adapter degrades ONLY this session's
// status; it never propagates to telemetry, Recorder, Terminal, lifecycle, or
// other sessions.
func safeGetStatus(adapter contract.AgentAdapter, in contract.StatusInput) (res contract.StatusResult) {
	defer func() {
		if r := recover(); r != nil {
			res = contract.StatusResult{
				Status:     agent.StatusUnknown,
				Provenance: contract.ProvenanceUnknown,
				Degraded:   contract.Degrade("adapter status panic"),
			}
		}
	}()
	if adapter == nil {
		return contract.StatusResult{
			Status: agent.StatusUnknown, Provenance: contract.ProvenanceUnknown,
			Degraded: contract.Degrade("no status adapter"),
		}
	}
	r, err := adapter.GetStatus(context.Background(), in)
	if err != nil {
		return contract.StatusResult{
			Status: agent.StatusUnknown, Provenance: contract.ProvenanceUnknown,
			Degraded: contract.Degrade("adapter status error"),
		}
	}
	return r
}

// Update resolves and stores the current activity result for one session. It
// builds bounded, session-filtered evidence, delegates resolution to the
// accepted adapter's GetStatus (frozen precedence/ceiling/terminal-downgrade),
// and applies the store's binding rules. An older generation than the currently
// stored one is REJECTED (returns the existing record unchanged). The returned
// record is the new (or unchanged) current record.
func (s *AgentStatusStore) Update(in AgentStatusUpdate) AgentActivityRecord {
	evidence := buildStatusEvidence(in.Events, in.SessionID)
	res := safeGetStatus(in.Adapter, contract.StatusInput{
		Session:      contract.SessionContext{SessionID: in.SessionID},
		RecentEvents: in.Events,
		Evidence:     evidence,
	})

	reason := res.Degraded.Reason
	if len(reason) > maxDegradedReason {
		reason = reason[:maxDegradedReason]
	}
	rec := AgentActivityRecord{
		SessionID:      in.SessionID,
		Status:         res.Status,
		Provenance:     res.Provenance,
		Confidence:     res.Confidence,
		Degraded:       res.Degraded.Degraded,
		DegradedReason: reason,
		ObservedAt:     s.now(),
		Generation:     in.Generation,
		Version:        in.Version,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.records[in.SessionID]; ok && in.Generation < prev.Generation {
		// A stale/older-generation result cannot overwrite the current session.
		return prev
	}
	s.records[in.SessionID] = rec // atomic replace on same/newer generation
	return rec
}

// Current returns the stored record and whether it is stale (read-time policy).
// Staleness NEVER mutates the stored status — a stale result keeps its status and
// is merely flagged; display may downgrade a stale result to unknown, but this
// store never fabricates a new active status from elapsed time.
func (s *AgentStatusStore) Current(sessionID string) (rec AgentActivityRecord, stale bool, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok = s.records[sessionID]
	if !ok {
		return AgentActivityRecord{}, false, false
	}
	stale = s.now().Sub(rec.ObservedAt) > s.staleAfter
	return rec, stale, true
}

// Clear removes a session's record on explicit session/history delete so a
// recreated session with the same ID cannot inherit the old activity status.
func (s *AgentStatusStore) Clear(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, sessionID)
}
