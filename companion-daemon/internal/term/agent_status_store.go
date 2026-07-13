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

	// S1.1-B — LaunchGen is the monotonic launch-instance identity (transcript
	// LaunchBinding.Generation) this result belongs to. It is a SEPARATE axis from
	// Generation (streamGen): LaunchGen identifies the process/runtime launch,
	// streamGen identifies the accepted event-stream path/inode/truncation epoch.
	// The two are combined lexicographically by the store's generation rule
	// (LaunchGen dominates; streamGen breaks ties within one launch) so a write
	// bound to an OLDER launch can never overwrite a newer launch's record. It is
	// internal only — NOT part of the public agentActivity DTO.
	LaunchGen int64

	// S1.1-A — bounded winning-evidence reference. WinningSeq is the stable
	// per-session AgentEvent.Seq of the exact accepted event that produced this
	// resolved status. It is an INTERNAL trace/anti-replay reference only: it is
	// NOT part of the public agentActivity DTO and carries no text/metadata.
	// HasWinningSeq is false whenever there is no accepted positive winner — an
	// unknown/degraded/revoked result never fabricates a winning identity.
	// S1.1-C uses (LaunchGen, Generation, WinningSeq) to reject same-generation
	// replay of an older winning revision.
	WinningSeq    int64
	HasWinningSeq bool

	// S1.1-C — WinnerHighWater is the highest winning Seq ACCEPTED within the
	// current (LaunchGen, Generation) epoch, whether or not the current record
	// still shows a positive status. It survives a downgrade to unknown/degraded
	// so that a same-epoch REPLAY of an older Seq — or a repeated/rewound cursor
	// batch — cannot restore an already-superseded positive status. It resets only
	// when the epoch advances (a newer LaunchGen or streamGen), which legitimately
	// starts a fresh event stream. HasWinnerHighWater is false until the first
	// positive winner in the epoch. Internal only; never in the public DTO.
	WinnerHighWater    int64
	HasWinnerHighWater bool
}

// AgentStatusUpdate is one resolution request for a single session. It carries
// only accepted, session-bound, correlation/version-gated inputs; the caller
// (S1-C) sources these from the accepted-adapter hook in processSession.
type AgentStatusUpdate struct {
	SessionID  string
	LaunchGen  int64 // S1.1-B: monotonic launch-instance identity (0 = unmanaged/legacy)
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
//
// S1.1-A: it returns a PARALLEL seqs slice — seqs[i] is the stable AgentEvent.Seq
// of the accepted event that produced evidence[i]. The two slices share ordering
// and length so the winning candidate located after resolution maps back to an
// exact source Seq without re-deriving precedence.
func buildStatusEvidence(filtered []agent.AgentEvent) (evidence []contract.StatusEvidence, seqs []int64) {
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
	outSeqs := make([]int64, 0, len(filtered))
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
		outSeqs = append(outSeqs, ev.Seq)
	}
	return out, outSeqs
}

// locateWinningSeq maps the RESOLVED status back to the Seq of the exact accepted
// candidate that produced it, WITHOUT re-deriving precedence. It scans the
// already-ordered candidate list (latest-Seq first) and returns the first
// candidate whose (Status, Provenance, normalized Confidence) equals the
// resolver's winning output. This is a lookup of the resolver's own decision,
// never a second resolver.
//
// Confidence matters: the frozen ResolveStatus selects by provenance rank and
// THEN by normalized (clamp01) confidence, so two same-(status,provenance)
// candidates with different confidence resolve to the HIGHER-confidence one — not
// necessarily the latest-Seq one. Matching on status+provenance alone could bind
// the anti-replay reference to a candidate that did not win; matching normalized
// confidence too binds the exact winner. normConfidence applies the SAME clamp the
// contract applied before selection, so the comparison is exact float equality
// against the resolver's own clamped output.
//
// It fails closed: a degraded result, a StatusUnknown result, or any resolved
// status/confidence that does not correspond to a positive accepted candidate
// (e.g. an advisory-terminal claim ResolveStatus downgraded to unknown, or an
// advisory winner whose confidence was ceiling-capped below its clamped value)
// yields ok=false and NO fabricated winning identity. The accepted production path
// is native_log (non-advisory), so the resolver never transforms a winning
// candidate's status/provenance/confidence and the exact winner is always found.
func locateWinningSeq(evidence []contract.StatusEvidence, seqs []int64, res contract.StatusResult) (int64, bool) {
	if res.Degraded.Degraded || res.Status == agent.StatusUnknown {
		return 0, false
	}
	for i := range evidence {
		if evidence[i].Status == res.Status &&
			evidence[i].Provenance == res.Provenance &&
			normConfidence(evidence[i].Confidence) == res.Confidence {
			return seqs[i], true
		}
	}
	return 0, false
}

// normConfidence clamps a raw event confidence to [0,1] exactly as the frozen
// contract's ResolveStatus does before it selects the winner. It is arithmetic-free
// (clamp only) so a matched candidate's value equals the resolver's clamped output
// bit-for-bit; it never reimplements status precedence or the advisory ceiling.
func normConfidence(c float64) float64 {
	if c < 0 {
		return 0
	}
	if c > 1 {
		return 1
	}
	return c
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
	evidence, seqs := buildStatusEvidence(filtered)
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
	// S1.1-A: bind the resolved status to the exact accepted event Seq that won,
	// located from the resolver's own output over the accepted ordering. A
	// degraded/unknown resolution binds no winner (fail closed).
	winSeq, hasWin := locateWinningSeq(evidence, seqs, res)
	return s.store(in.SessionID, in.LaunchGen, in.Generation, in.Version, res, winSeq, hasWin)
}

// Revoke forces a session's activity to unknown+degraded when authority is lost
// (accepted-version conflict or ingestion overflow). It obeys the generation rule.
func (s *AgentStatusStore) Revoke(sessionID string, launchGen int64, generation int, version, reason string) AgentActivityRecord {
	if len(sessionID) == 0 || len(sessionID) > maxSessionIDLen {
		return AgentActivityRecord{}
	}
	// Authority loss carries NO winning-evidence reference (fail closed).
	return s.store(sessionID, launchGen, generation, version, unknownDegraded(reason), 0, false)
}

// Invalidate marks a session non-current at a NEW generation (stream or launch)
// without deleting its record. Unlike Clear (used for lifecycle/history delete),
// it stores an unknown+degraded result AT the new generation, which raises the
// generation high-water mark: a late update/revoke from an OLDER launch or stream
// generation is then rejected by the generation rule and cannot resurrect the
// prior positive status. Used on a stream-generation change AND on a launch
// replacement (S1.1-B) so a prior-launch/prior-stream positive status never
// survives, and a delayed prior-generation write cannot re-store it.
func (s *AgentStatusStore) Invalidate(sessionID string, launchGen int64, generation int, reason string) AgentActivityRecord {
	if len(sessionID) == 0 || len(sessionID) > maxSessionIDLen {
		return AgentActivityRecord{}
	}
	// A non-current invalidation carries NO winning-evidence reference.
	return s.store(sessionID, launchGen, generation, "", unknownDegraded(reason), 0, false)
}

// RevokeIfPresent downgrades an EXISTING record to unknown+degraded (used when a
// previously-managed correlation becomes unavailable). A session with no prior
// record is left absent, so an initially-uncorrelated session never gains a
// phantom record. Returns (record, true) if a record was present.
func (s *AgentStatusStore) RevokeIfPresent(sessionID string, launchGen int64, generation int, version, reason string) (AgentActivityRecord, bool) {
	// Authority loss carries NO winning-evidence reference (fail closed).
	rec := s.mkRecord(sessionID, launchGen, generation, version, unknownDegraded(reason), 0, false)
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.records[sessionID]
	if !ok {
		return AgentActivityRecord{}, false
	}
	if staleWrite(launchGen, generation, prev) {
		return prev, true // a stale revoke cannot overwrite a newer generation
	}
	// A downgrade never carries a winner but must preserve the epoch's launch and
	// winner high-water so a later same-epoch replay stays rejected.
	merged, _ := reconcileEpoch(prev, rec)
	s.records[sessionID] = merged
	return merged, true
}

func (s *AgentStatusStore) mkRecord(sessionID string, launchGen int64, generation int, version string, res contract.StatusResult, winSeq int64, hasWin bool) AgentActivityRecord {
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
		LaunchGen:      launchGen,
		WinningSeq:     winSeq,
		HasWinningSeq:  hasWin,
	}
}

// staleWrite reports whether a write bound to (launchGen, streamGen) is OLDER than
// the stored record and must be rejected. The two generation axes are combined
// lexicographically: LaunchGen dominates (a newer launch instance always wins,
// even at a lower streamGen, because a new launch legitimately restarts the
// event-stream epoch), and streamGen breaks ties WITHIN one launch.
//
// A launchGen of 0 is "unspecified/unmanaged": it does NOT compete on the launch
// axis (in production every launchGen==0 write is a safe downgrade — correlation
// unavailable, version conflict, or overflow — never a positive status), so it
// falls through to the stream-generation rule and, per store(), never lowers the
// stored launch high-water. This lets a current observer downgrade a session
// whose binding just disappeared while still rejecting a delayed OLDER nonzero
// launch write. This is the single generation rule for update, revoke,
// invalidate, and revoke-if-present.
func staleWrite(launchGen int64, streamGen int, prev AgentActivityRecord) bool {
	if launchGen != 0 && launchGen != prev.LaunchGen {
		return launchGen < prev.LaunchGen
	}
	return streamGen < prev.Generation
}

// sameEpoch reports whether rec falls in the SAME (launch, stream) epoch as prev.
// A launchGen of 0 inherits prev's launch (unspecified downgrade), so it stays in
// the current launch epoch; a higher launch or stream generation is a NEW epoch
// that legitimately restarts the event stream and resets the winner high-water.
// Caller has already established rec is not a staleWrite.
func sameEpoch(rec, prev AgentActivityRecord) bool {
	effLaunch := rec.LaunchGen
	if effLaunch == 0 {
		effLaunch = prev.LaunchGen
	}
	return effLaunch == prev.LaunchGen && rec.Generation == prev.Generation
}

// reconcileEpoch merges a non-stale write rec against the existing prev record,
// enforcing S1.1-B launch-generation carry-forward and the S1.1-C per-epoch
// winner high-water. It returns the record to store and whether the write was
// REJECTED as a same-epoch regression (in which case prev is returned unchanged).
//
// S1.1-C rules:
//   - a positive update whose winning Seq is NOT STRICTLY ABOVE the epoch's
//     winner high-water is a replay / cursor rewind / repeated or re-read batch
//     and is rejected — it carries no new information, so it cannot move the
//     winner backward NOR restore a status that was since downgraded to
//     unknown/degraded. A repeated identical batch is therefore idempotent;
//   - the winner high-water survives a downgrade within the epoch, so recovery
//     requires FRESH evidence (a strictly higher Seq) or a NEW epoch;
//   - advancing the launch or stream generation resets the high-water (a new
//     event stream), and the launch high-water is carried forward monotonically
//     so a later delayed older-launch write is still rejected by staleWrite.
func reconcileEpoch(prev, rec AgentActivityRecord) (AgentActivityRecord, bool) {
	inEpoch := sameEpoch(rec, prev)
	// Reject a same-epoch positive write that is not strictly newer than the
	// epoch's winner high-water (replay / rewind / repeated / re-read batch).
	if inEpoch && rec.HasWinningSeq && prev.HasWinnerHighWater && rec.WinningSeq <= prev.WinnerHighWater {
		return prev, true
	}
	// Carry the launch high-water forward: an unspecified (0) downgrade must not
	// lower it, so a later delayed older nonzero-launch write stays rejected.
	if prev.LaunchGen > rec.LaunchGen {
		rec.LaunchGen = prev.LaunchGen
	}
	// Carry the winner high-water within the same epoch (survives a downgrade);
	// a new epoch starts fresh.
	if inEpoch && prev.HasWinnerHighWater {
		rec.WinnerHighWater = prev.WinnerHighWater
		rec.HasWinnerHighWater = true
	}
	// A positive winner at or above the current high-water raises it.
	if rec.HasWinningSeq && (!rec.HasWinnerHighWater || rec.WinningSeq >= rec.WinnerHighWater) {
		rec.WinnerHighWater = rec.WinningSeq
		rec.HasWinnerHighWater = true
	}
	return rec, false
}

// store applies the generation rule, the S1.1-C epoch reconciliation, and the
// size bound, then records atomically.
func (s *AgentStatusStore) store(sessionID string, launchGen int64, generation int, version string, res contract.StatusResult, winSeq int64, hasWin bool) AgentActivityRecord {
	rec := s.mkRecord(sessionID, launchGen, generation, version, res, winSeq, hasWin)
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.records[sessionID]; ok {
		if staleWrite(launchGen, generation, prev) {
			return prev // an older launch/stream generation cannot overwrite the current session
		}
		merged, rejected := reconcileEpoch(prev, rec)
		if rejected {
			return prev // same-epoch replay/regression: leave the prior record intact
		}
		s.records[sessionID] = merged
		return merged
	}
	// First record for this session: seed the winner high-water from the winner.
	if rec.HasWinningSeq {
		rec.WinnerHighWater = rec.WinningSeq
		rec.HasWinnerHighWater = true
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
