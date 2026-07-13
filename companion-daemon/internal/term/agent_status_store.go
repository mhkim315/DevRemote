package term

import (
	"context"
	"sort"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// S1-B/C — Internal agent-activity status resolver and bounded session-owned store.
//
// This is the provider-neutral status boundary. It consumes ONLY accepted,
// exactly-session-bound AgentEvents, delegates final precedence/resolution to the
// accepted version-specific adapter's frozen GetStatus (which delegates to
// contract.ResolveStatus), and stores exactly one immutable current result per
// canonical session ID.
//
// Authority rules enforced here:
//   - agent activity is resolved ONLY from accepted evidence via GetStatus;
//     lifecycle and connectivity health are NEVER inputs to GetStatus.
//   - an event is evidence for a session ONLY when ev.SessionID == that id exactly
//     (empty or cross-session events are discarded from BOTH evidence and the
//     adapter's RecentEvents).
//   - empty/unknown/invalid provenance NEVER creates a typed status (it is dropped,
//     never promoted to a strong tier). Provenance/confidence are never upgraded.
//   - waiting_approval requires a non-empty ApprovalID, exact session binding,
//     approval-authoritative provenance, and confidence ≥ ApprovalConfidenceFloor.
//   - on equal precedence, the LATEST validated Seq wins (not first-seen order).
//   - the accepted adapter is only queried when it advertises CapStatus.
//   - adapter error/panic isolates to a degraded result for that one session.
//   - an older stream generation can never overwrite a newer session generation.
//   - the store is size-bounded with deterministic eviction; strings are bounded.
//   - Clear() drops a session on delete; a recreated session cannot inherit status.
//   - read-time stale policy flags a prior result stale but never fabricates a status.
//
// No HTTP/mobile code (S1-D), no independent log reader/cursor (S1-C reuses the
// accepted-adapter production batch).

const (
	maxDegradedReason         = 240
	maxSessions               = 1024 // B5: bounded number of session records
	maxSessionIDLen           = 512  // B5: reject absurd session ids (fail-closed)
	maxVersionLen             = 64   // B5: bounded version string
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

func boundStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// filterSessionEvents keeps only events bound to sessionID EXACTLY. Empty and
// cross-session ids are discarded. The result is used for BOTH evidence and the
// adapter's RecentEvents so the adapter never sees foreign/empty-session events.
func filterSessionEvents(events []agent.AgentEvent, sessionID string) []agent.AgentEvent {
	out := make([]agent.AgentEvent, 0, len(events))
	for _, ev := range events {
		if ev.SessionID == sessionID {
			out = append(out, ev)
		}
	}
	return out
}

// statusForEvent maps one accepted event to a status in the closed set, applying
// the approval-authority gate. It returns ok=false for events that must not
// create a typed status.
func statusForEvent(ev agent.AgentEvent) (agent.AgentStatus, bool) {
	switch ev.Type {
	case agent.EventThinking:
		return agent.StatusThinking, true
	case agent.EventToolCallStarted:
		return agent.StatusWorking, true
	case agent.EventWaitingInput:
		return agent.StatusWaitingInput, true
	case agent.EventCompleted:
		return agent.StatusCompleted, true
	case agent.EventFailed:
		return agent.StatusFailed, true
	case agent.EventInterrupted:
		return agent.StatusInterrupted, true
	case agent.EventApprovalRequested:
		// B3: an approval activity requires an identified, exactly-bound,
		// approval-authoritative, confident event. Heuristic/prompt-hint or
		// id-less approval events never create waiting_approval (even display-only).
		if ev.ApprovalID != "" &&
			contract.ApprovalAuthoritative(contract.Provenance(ev.Provenance)) &&
			ev.Confidence >= contract.ApprovalConfidenceFloor {
			return agent.StatusWaitingApproval, true
		}
		return "", false
	default:
		// agent_started/user_message/assistant_message/tool_call_finished/
		// approval_resolved/unknown → no evidence.
		return "", false
	}
}

// buildStatusEvidence turns session-filtered events into closed StatusEvidence,
// ordered so that on equal precedence the LATEST validated Seq wins (ResolveStatus
// keeps first-seen on exact ties, so the latest event is placed first). Empty,
// unknown, or invalid provenance is dropped (never upgraded); confidence must be
// positive and is taken verbatim from the event.
func buildStatusEvidence(filtered []agent.AgentEvent) []contract.StatusEvidence {
	idx := make([]int, len(filtered))
	for i := range idx {
		idx[i] = i
	}
	// Latest-first: higher Seq first; on equal Seq, later batch position first.
	sort.SliceStable(idx, func(a, b int) bool {
		ia, ib := idx[a], idx[b]
		if filtered[ia].Seq != filtered[ib].Seq {
			return filtered[ia].Seq > filtered[ib].Seq
		}
		return ia > ib
	})
	out := make([]contract.StatusEvidence, 0, len(filtered))
	for _, i := range idx {
		ev := filtered[i]
		status, ok := statusForEvent(ev)
		if !ok {
			continue
		}
		prov := contract.Provenance(ev.Provenance)
		// B1: empty/unknown/invalid provenance never creates a typed status.
		if prov == "" || prov == contract.ProvenanceUnknown || !contract.IsKnownProvenance(prov) {
			continue
		}
		if ev.Confidence <= 0 {
			continue
		}
		out = append(out, contract.StatusEvidence{
			Status:     status,
			Provenance: prov,
			Confidence: ev.Confidence, // never upgraded; ResolveStatus clamps/ceilings
		})
	}
	return out
}

// hasStatusCap reports whether the adapter advertises the frozen CapStatus
// capability. GetStatus is only called on adapters that do.
func hasStatusCap(a contract.AgentAdapter) bool {
	if a == nil {
		return false
	}
	for _, c := range a.Descriptor().Capabilities {
		if c == contract.CapStatus {
			return true
		}
	}
	return false
}

func unknownDegraded(reason string) contract.StatusResult {
	return contract.StatusResult{
		Status:     agent.StatusUnknown,
		Provenance: contract.ProvenanceUnknown,
		Degraded:   contract.Degrade(reason),
	}
}

// safeGetStatus calls the accepted adapter's frozen GetStatus with capability,
// panic, and error isolation. An adapter without CapStatus is NOT called. A
// panicking or erroring adapter degrades ONLY this session's status; it never
// propagates to telemetry, Recorder, Terminal, lifecycle, or other sessions.
func safeGetStatus(adapter contract.AgentAdapter, in contract.StatusInput) (res contract.StatusResult) {
	defer func() {
		if r := recover(); r != nil {
			res = unknownDegraded("adapter status panic")
		}
	}()
	if !hasStatusCap(adapter) {
		return unknownDegraded("adapter lacks status capability")
	}
	r, err := adapter.GetStatus(context.Background(), in)
	if err != nil {
		return unknownDegraded("adapter status error")
	}
	return r
}

// Update resolves and stores the current activity result for one session. It
// filters events to the exact session, and — when the resulting evidence is
// non-empty — delegates resolution to the accepted adapter's GetStatus and stores
// the result under the generation rule. A poll that yields no status evidence
// leaves any valid prior result untouched (no fabrication, no downgrade); authority
// loss (version conflict, overflow, correlation loss) is expressed via Revoke.
func (s *AgentStatusStore) Update(in AgentStatusUpdate) AgentActivityRecord {
	if len(in.SessionID) == 0 || len(in.SessionID) > maxSessionIDLen {
		return AgentActivityRecord{} // fail-closed on absent/absurd id
	}
	filtered := filterSessionEvents(in.Events, in.SessionID)
	evidence := buildStatusEvidence(filtered)
	if len(evidence) == 0 {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.records[in.SessionID] // leave prior (zero value if none)
	}
	res := safeGetStatus(in.Adapter, contract.StatusInput{
		Session:      contract.SessionContext{SessionID: in.SessionID},
		RecentEvents: filtered, // B2: same filtered set the evidence came from
		Evidence:     evidence,
	})
	return s.store(in.SessionID, in.Generation, in.Version, res)
}

// Revoke forces a session's activity to unknown+degraded when authority is lost
// (accepted-version conflict or ingestion overflow). It obeys the generation rule.
func (s *AgentStatusStore) Revoke(sessionID string, generation int, version, reason string) AgentActivityRecord {
	if len(sessionID) == 0 || len(sessionID) > maxSessionIDLen {
		return AgentActivityRecord{}
	}
	return s.store(sessionID, generation, version, unknownDegraded(reason))
}

// Invalidate marks a session non-current at a NEW stream generation without
// deleting its record. Unlike Clear (used for lifecycle/history delete), it
// stores an unknown+degraded result AT the new generation, which raises the
// generation high-water mark: a late update/revoke from an OLDER generation is
// then rejected by the generation rule and cannot resurrect the prior positive
// status. Used on a stream-generation change so a gen-N `working` never survives
// into gen N+1, and a delayed gen-N write cannot re-store it.
func (s *AgentStatusStore) Invalidate(sessionID string, generation int, reason string) AgentActivityRecord {
	if len(sessionID) == 0 || len(sessionID) > maxSessionIDLen {
		return AgentActivityRecord{}
	}
	return s.store(sessionID, generation, "", unknownDegraded(reason))
}

// RevokeIfPresent downgrades an EXISTING record to unknown+degraded (used when a
// previously-managed correlation becomes unavailable). A session with no prior
// record is left absent, so an initially-uncorrelated session never gains a
// phantom record. Returns (record, true) if a record was present.
func (s *AgentStatusStore) RevokeIfPresent(sessionID string, generation int, version, reason string) (AgentActivityRecord, bool) {
	rec := s.mkRecord(sessionID, generation, version, unknownDegraded(reason))
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.records[sessionID]
	if !ok {
		return AgentActivityRecord{}, false
	}
	if generation < prev.Generation {
		return prev, true // a stale revoke cannot overwrite a newer generation
	}
	s.records[sessionID] = rec
	return rec, true
}

func (s *AgentStatusStore) mkRecord(sessionID string, generation int, version string, res contract.StatusResult) AgentActivityRecord {
	return AgentActivityRecord{
		SessionID:      sessionID,
		Status:         res.Status,
		Provenance:     res.Provenance,
		Confidence:     res.Confidence,
		Degraded:       res.Degraded.Degraded,
		DegradedReason: boundStr(res.Degraded.Reason, maxDegradedReason),
		ObservedAt:     s.now(),
		Generation:     generation,
		Version:        boundStr(version, maxVersionLen),
	}
}

// store applies the generation rule and the size bound, then records atomically.
func (s *AgentStatusStore) store(sessionID string, generation int, version string, res contract.StatusResult) AgentActivityRecord {
	rec := s.mkRecord(sessionID, generation, version, res)
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.records[sessionID]; ok {
		if generation < prev.Generation {
			return prev // an older generation cannot overwrite the current session
		}
		s.records[sessionID] = rec
		return rec
	}
	if len(s.records) >= maxSessions {
		s.evictOldestLocked() // deterministic: oldest ObservedAt, tie by id
	}
	s.records[sessionID] = rec
	return rec
}

// evictOldestLocked removes the record with the smallest ObservedAt (ties broken
// by lexically-smallest SessionID) so eviction is deterministic. Caller holds mu.
func (s *AgentStatusStore) evictOldestLocked() {
	var victim string
	var vt time.Time
	first := true
	for id, r := range s.records {
		if first || r.ObservedAt.Before(vt) || (r.ObservedAt.Equal(vt) && id < victim) {
			victim, vt, first = id, r.ObservedAt, false
		}
	}
	if victim != "" {
		delete(s.records, victim)
	}
}

// Current returns the stored record and whether it is stale (read-time policy).
// Staleness NEVER mutates the stored status.
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

// Len returns the number of stored session records (test/introspection).
func (s *AgentStatusStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

// Clear removes a session's record on explicit session/history delete so a
// recreated session with the same ID cannot inherit the old activity status.
func (s *AgentStatusStore) Clear(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, sessionID)
}
