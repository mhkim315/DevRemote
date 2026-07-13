package term

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// A1-B — generation-bound authoritative ApprovalStore.
//
// This is the authoritative record boundary for agent approvals. It stores at
// most a bounded number of immutable request records per canonical session, each
// permanently bound to the launch + stream generation, accepted provider/version,
// authoritative provenance, and the exact allowed-action contract that were true
// when the request was ingested. It exists so that:
//
//   - a request is authority ONLY for the runtime instance that created it: an
//     older launch or stream generation can never create, update, resolve, or
//     restore a record, and a newer generation invalidates all prior pending
//     authority (runtime replacement / stream-generation change);
//   - a duplicate provider approval ID whose allowed-action contract CHANGED within
//     the same generation cannot mutate a live request (fail closed);
//   - correlation loss and session delete/unlink/relink remove residual authority;
//   - every value handed in or out is copied, so a caller can never mutate stored
//     authority through an aliased slice/map/pointer;
//   - the lifecycle obeys the frozen A1-A state machine: pending → executing (an
//     atomic at-most-once reservation) → exactly one terminal state, and a decision
//     is NEVER committed as resolved before its delivery result is known.
//
// It does NOT ingest evidence itself (A1-C wires accepted DetectApproval into
// Ingest) and does NOT authenticate the action route (A1-D revalidates identity
// and drives Reserve/Commit/Fail behind device auth). Nothing here can manufacture
// an approval from runtime status, prompt text, or terminal capability.

const (
	authMaxApprovalsPerSession = 50
	authMaxApprovalSessions    = 1024
	authApprovalExpiry         = 5 * time.Minute
	authMaxPromptLen           = 4096
	authMaxOptionField         = 512
	authMaxOptions             = 32
	authMaxApprovalIDLen       = 256
)

// ApprovalIngestItem is one authoritative approval request offered for ingestion.
// A1-C populates it from an accepted-adapter DetectApproval result plus the exact
// source event's provenance; the store copies and bounds every field.
type ApprovalIngestItem struct {
	Approval     agent.AgentApproval // must carry a non-empty ID and match the ingest session
	Provenance   contract.Provenance // authoritative provenance of the source event
	RequiredPerm string              // authorization context the action route must satisfy
}

// ApprovalIngest is one generation-scoped ingestion for a single session. All
// items share the session's current launch/stream generation and provider/version.
type ApprovalIngest struct {
	SessionID string
	LaunchGen int64
	StreamGen int
	Provider  string
	Version   string
	Items     []ApprovalIngestItem
}

// ApprovalSnapshot is an immutable value copy of a stored record, returned to the
// action boundary (A1-D) so it can revalidate identity WITHOUT holding a reference
// to mutable store state. Its Options slice is a deep copy.
type ApprovalSnapshot struct {
	SessionID    string
	ApprovalID   string
	Provider     string
	Version      string
	LaunchGen    int64
	StreamGen    int
	Provenance   contract.Provenance
	State        ApprovalState
	RequiredPerm string
	ActionDigest string
	Options      []agent.InteractionOption
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// approvalRecord is the internal, mutable-only-by-store record. Its Options are
// owned by the store and never shared with callers.
type approvalRecord struct {
	approval     agent.AgentApproval // deep copied; Status is derived from state on projection
	state        ApprovalState
	provider     string
	version      string
	launchGen    int64
	streamGen    int
	provenance   contract.Provenance
	requiredPerm string
	actionDigest string
	createdAt    time.Time
	expiresAt    time.Time
	resolvedAt   *time.Time
}

// sessionApprovals holds one session's records plus its generation high-water. The
// high-water is monotonic: it only advances, so a delayed older-generation ingest
// is rejected even after the newer generation has been observed.
type sessionApprovals struct {
	records       map[string]*approvalRecord
	hwLaunch      int64
	hwStream      int
	hwInitialized bool
}

// AuthoritativeApprovalStore is the generation-bound approval record store.
type AuthoritativeApprovalStore struct {
	mu       sync.Mutex
	sessions map[string]*sessionApprovals
	now      func() time.Time
}

// NewAuthoritativeApprovalStore builds an empty store with the production clock.
func NewAuthoritativeApprovalStore() *AuthoritativeApprovalStore {
	return &AuthoritativeApprovalStore{
		sessions: make(map[string]*sessionApprovals),
		now:      time.Now,
	}
}

// genNewerOrEqual reports whether (launch,stream) is >= (hwLaunch,hwStream)
// lexicographically with launch dominating. Used to decide whether an ingest is a
// legitimate current/advancing generation vs a rejected stale one.
func genNewer(launchA int64, streamA int, launchB int64, streamB int) bool {
	if launchA != launchB {
		return launchA > launchB
	}
	return streamA > streamB
}

// digestOptions computes a stable digest of the allowed-action contract so a
// duplicate approval ID whose option set/kinds/payloads/input-contract changed is
// detected and refused within the same generation. Order-independent: options are
// sorted by ID first so a reordering is NOT treated as a contract change.
func digestOptions(kind, def string, options []agent.InteractionOption) string {
	idx := make([]int, len(options))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return options[idx[a]].ID < options[idx[b]].ID })
	h := sha256.New()
	h.Write([]byte("k:" + kind + "\x00d:" + def + "\x00"))
	for _, i := range idx {
		o := options[i]
		h.Write([]byte("id:" + o.ID + "\x1f"))
		h.Write([]byte("kd:" + o.Kind + "\x1f"))
		h.Write([]byte("pl:" + o.Payload + "\x1f"))
		if o.Input != nil {
			req := "0"
			if o.Input.Required {
				req = "1"
			}
			h.Write([]byte("in:1\x1freq:" + req + "\x1fpm:" + o.Input.Placement + "\x1f"))
		} else {
			h.Write([]byte("in:0\x1f"))
		}
		h.Write([]byte("\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// copyInputSchema deep-copies an option's input contract.
func copyInputSchema(in *agent.InputSchema) *agent.InputSchema {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

// copyOptions deep-copies and bounds an option slice so stored authority can never
// be mutated through an aliased slice/pointer, and an oversized set is truncated.
func copyOptions(options []agent.InteractionOption) []agent.InteractionOption {
	n := len(options)
	if n > authMaxOptions {
		n = authMaxOptions
	}
	out := make([]agent.InteractionOption, 0, n)
	for i := 0; i < n; i++ {
		o := options[i]
		out = append(out, agent.InteractionOption{
			ID:      boundStr(o.ID, authMaxOptionField),
			Label:   boundStr(o.Label, authMaxOptionField),
			Kind:    boundStr(o.Kind, authMaxOptionField),
			Payload: boundStr(o.Payload, authMaxOptionField),
			Input:   copyInputSchema(o.Input),
		})
	}
	return out
}

// Ingest records the session's current-generation approval requests under the
// generation rule. An older-generation ingest is rejected wholesale. A newer
// generation invalidates all prior pending requests before adding the new ones. A
// same-generation duplicate ID is idempotent when its action contract is unchanged
// and refused (leaving the live request intact) when the contract changed. Items
// with an empty/oversized ID, a cross-session binding, an empty options set, or a
// non-authoritative provenance are dropped (fail closed).
func (s *AuthoritativeApprovalStore) Ingest(in ApprovalIngest) {
	if in.SessionID == "" || len(in.SessionID) > maxSessionIDLen {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	sess := s.sessions[in.SessionID]
	if sess == nil {
		if len(s.sessions) >= authMaxApprovalSessions {
			s.evictOldestSessionLocked()
		}
		sess = &sessionApprovals{records: make(map[string]*approvalRecord)}
		s.sessions[in.SessionID] = sess
	}

	// Generation rule: reject a stale ingest; advance + invalidate on a newer one.
	if sess.hwInitialized {
		if genNewer(sess.hwLaunch, sess.hwStream, in.LaunchGen, in.StreamGen) {
			return // older generation cannot create/update/resolve/restore anything
		}
		if genNewer(in.LaunchGen, in.StreamGen, sess.hwLaunch, sess.hwStream) {
			s.invalidatePendingLocked(sess, "generation advanced")
			sess.hwLaunch, sess.hwStream = in.LaunchGen, in.StreamGen
		}
	} else {
		sess.hwLaunch, sess.hwStream = in.LaunchGen, in.StreamGen
		sess.hwInitialized = true
	}

	now := s.now()
	for _, item := range in.Items {
		a := item.Approval
		if a.ID == "" || len(a.ID) > authMaxApprovalIDLen {
			continue // id-less/oversized: no actionable request
		}
		if a.SessionID != "" && a.SessionID != in.SessionID {
			continue // cross-session binding: fail closed
		}
		if len(a.Options) == 0 {
			continue // no allowed action → not actionable
		}
		if !contract.ApprovalAuthoritative(item.Provenance) {
			continue // spoofable/advisory provenance can never establish a request
		}
		digest := digestOptions(a.Kind, a.Default, a.Options)
		if existing, ok := sess.records[a.ID]; ok {
			// Same generation (older/newer already handled). A terminal record is
			// never resurrected; a live record is only kept if the contract matches.
			if existing.state != ApprovalPending {
				continue
			}
			if existing.actionDigest != digest {
				continue // changed action set for a live id: refuse the mutation
			}
			continue // idempotent: identical live request, no change
		}
		if len(sess.records) >= authMaxApprovalsPerSession {
			s.evictOneLocked(sess)
			if len(sess.records) >= authMaxApprovalsPerSession {
				continue // still full of live records: drop rather than exceed the bound
			}
		}
		rec := &approvalRecord{
			approval: agent.AgentApproval{
				ID:         a.ID,
				SessionID:  in.SessionID,
				AgentKind:  boundStr(a.AgentKind, authMaxOptionField),
				Kind:       boundStr(a.Kind, authMaxOptionField),
				Prompt:     boundStr(a.Prompt, authMaxPromptLen),
				Options:    copyOptions(a.Options),
				Default:    boundStr(a.Default, authMaxOptionField),
				Source:     a.Source,
				Confidence: a.Confidence,
			},
			state:        ApprovalPending,
			provider:     boundStr(in.Provider, maxVersionLen),
			version:      boundStr(in.Version, maxVersionLen),
			launchGen:    in.LaunchGen,
			streamGen:    in.StreamGen,
			provenance:   item.Provenance,
			requiredPerm: boundStr(item.RequiredPerm, authMaxOptionField),
			actionDigest: digest,
			createdAt:    now,
			expiresAt:    now.Add(authApprovalExpiry),
		}
		sess.records[a.ID] = rec
	}
}

// invalidatePendingLocked marks every still-pending record in a session as
// invalidated. Executing/terminal records are untouched (a committed decision
// resolves on its own terminal path). Caller holds mu.
func (s *AuthoritativeApprovalStore) invalidatePendingLocked(sess *sessionApprovals, reason string) {
	now := s.now()
	for _, rec := range sess.records {
		if rec.state == ApprovalPending {
			rec.state = ApprovalInvalidated
			rec.resolvedAt = &now
		}
	}
}

// InvalidateSession marks all pending requests for a session invalidated, used on
// correlation loss where the session record persists but its prior approval
// authority must not survive. Generation-driven invalidation is handled inside
// Ingest.
func (s *AuthoritativeApprovalStore) InvalidateSession(sessionID, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.sessions[sessionID]; sess != nil {
		s.invalidatePendingLocked(sess, reason)
	}
}

// Clear drops all records for a session on explicit session/history delete or
// unlink, so a recreated session with the same canonical ID cannot inherit prior
// approval authority. The generation high-water is dropped with it; a fresh
// session legitimately restarts generations.
func (s *AuthoritativeApprovalStore) Clear(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

// expireLocked transitions a pending record past its TTL to expired in place.
// Caller holds mu.
func (s *AuthoritativeApprovalStore) expireLocked(rec *approvalRecord) {
	if rec.state == ApprovalPending && s.now().After(rec.expiresAt) {
		now := s.now()
		rec.state = ApprovalExpired
		rec.resolvedAt = &now
	}
}

// LookupRecord returns an immutable snapshot of a record for identity
// revalidation, applying lazy expiry first. ok is false if there is no such
// record for the exact session.
func (s *AuthoritativeApprovalStore) LookupRecord(sessionID, approvalID string) (ApprovalSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[sessionID]
	if sess == nil {
		return ApprovalSnapshot{}, false
	}
	rec := sess.records[approvalID]
	if rec == nil {
		return ApprovalSnapshot{}, false
	}
	s.expireLocked(rec)
	return s.snapshotLocked(rec), true
}

func (s *AuthoritativeApprovalStore) snapshotLocked(rec *approvalRecord) ApprovalSnapshot {
	return ApprovalSnapshot{
		SessionID:    rec.approval.SessionID,
		ApprovalID:   rec.approval.ID,
		Provider:     rec.provider,
		Version:      rec.version,
		LaunchGen:    rec.launchGen,
		StreamGen:    rec.streamGen,
		Provenance:   rec.provenance,
		State:        rec.state,
		RequiredPerm: rec.requiredPerm,
		ActionDigest: rec.actionDigest,
		Options:      copyOptions(rec.approval.Options),
		CreatedAt:    rec.createdAt,
		ExpiresAt:    rec.expiresAt,
	}
}

// Reserve performs the atomic at-most-once pending→executing transition. It is the
// ONLY way a decision begins delivery: a concurrent second caller observes a
// non-pending state and is refused. It applies lazy expiry and returns a closed
// outcome (never a false OK). It does NOT itself authenticate or compare
// generation — A1-D revalidates the returned snapshot against current runtime
// identity before calling this, and Commit/Fail follow the delivery result.
func (s *AuthoritativeApprovalStore) Reserve(sessionID, approvalID string) (ApprovalSnapshot, ActionOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[sessionID]
	if sess == nil {
		return ApprovalSnapshot{}, OutcomeNotFound
	}
	rec := sess.records[approvalID]
	if rec == nil {
		return ApprovalSnapshot{}, OutcomeNotFound
	}
	s.expireLocked(rec)
	switch rec.state {
	case ApprovalPending:
		rec.state = ApprovalExecuting
		return s.snapshotLocked(rec), OutcomeOK
	case ApprovalExpired:
		return s.snapshotLocked(rec), OutcomeExpired
	default:
		// executing (in flight) or any terminal state: at most one decision.
		return s.snapshotLocked(rec), OutcomeAlreadyTerminal
	}
}

// Commit transitions a reserved (executing) record to its terminal resolution
// state, chosen by option Kind (never the action ID). It returns false if the
// record is not currently executing. Delivery must be durably accepted BEFORE the
// caller commits.
func (s *AuthoritativeApprovalStore) Commit(sessionID, approvalID, kind string) bool {
	return s.finishExecuting(sessionID, approvalID, terminalStateForKind(kind))
}

// Fail transitions a reserved (executing) record to delivery_failed when the
// decision could not be durably delivered. The decision stays consumed (at most
// once) but is never reported as a success.
func (s *AuthoritativeApprovalStore) Fail(sessionID, approvalID string) bool {
	return s.finishExecuting(sessionID, approvalID, ApprovalDeliveryFailed)
}

func (s *AuthoritativeApprovalStore) finishExecuting(sessionID, approvalID string, to ApprovalState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[sessionID]
	if sess == nil {
		return false
	}
	rec := sess.records[approvalID]
	if rec == nil || rec.state != ApprovalExecuting {
		return false
	}
	if !CanTransitionApproval(ApprovalExecuting, to) {
		return false // guarded by the frozen transition table
	}
	now := s.now()
	rec.state = to
	rec.resolvedAt = &now
	return true
}

// List returns the public projection of a session's requests: pending/executing
// and recently-terminal records within the retention window, newest first. Every
// returned AgentApproval is a deep copy; the Status field is the bounded public
// projection of the internal state. Expired pending records are transitioned lazily
// and pruned.
func (s *AuthoritativeApprovalStore) List(sessionID string) []agent.AgentApproval {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[sessionID]
	if sess == nil {
		return nil
	}
	now := s.now()
	recs := make([]*approvalRecord, 0, len(sess.records))
	for _, rec := range sess.records {
		s.expireLocked(rec)
		// Retention: keep pending/executing always; keep a terminal record only
		// within the window after resolution.
		if rec.state == ApprovalPending || rec.state == ApprovalExecuting {
			recs = append(recs, rec)
			continue
		}
		if rec.resolvedAt != nil && now.Sub(*rec.resolvedAt) < authApprovalExpiry {
			recs = append(recs, rec)
		}
	}
	sort.SliceStable(recs, func(a, b int) bool {
		if !recs[a].createdAt.Equal(recs[b].createdAt) {
			return recs[a].createdAt.After(recs[b].createdAt)
		}
		return recs[a].approval.ID < recs[b].approval.ID
	})
	out := make([]agent.AgentApproval, 0, len(recs))
	for _, rec := range recs {
		pub := rec.approval // struct copy
		pub.Status = projectPublicStatus(rec.state)
		pub.Options = copyOptions(rec.approval.Options)
		if rec.resolvedAt != nil {
			t := *rec.resolvedAt
			pub.ResolvedAt = &t
		}
		pub.CreatedAt = rec.createdAt
		out = append(out, pub)
	}
	return out
}

// evictOneLocked removes one record to stay within the per-session bound. It
// prefers the oldest terminal record; if all are live (pending/executing) it
// removes the oldest by creation time, ties broken by lexically-smallest ID. This
// keeps eviction deterministic. Caller holds mu.
func (s *AuthoritativeApprovalStore) evictOneLocked(sess *sessionApprovals) {
	var victim string
	var vt time.Time
	var victimTerminal bool
	first := true
	for id, rec := range sess.records {
		terminal := IsTerminalApprovalState(rec.state)
		better := first ||
			(terminal && !victimTerminal) || // a terminal victim beats a live one
			(terminal == victimTerminal && (rec.createdAt.Before(vt) || (rec.createdAt.Equal(vt) && id < victim)))
		if better {
			victim, vt, victimTerminal, first = id, rec.createdAt, terminal, false
		}
	}
	if victim != "" {
		delete(sess.records, victim)
	}
}

// evictOldestSessionLocked removes the session whose newest record is oldest, to
// bound the number of tracked sessions. Deterministic: ties broken by
// lexically-smallest session ID. Caller holds mu.
func (s *AuthoritativeApprovalStore) evictOldestSessionLocked() {
	var victim string
	var vt time.Time
	first := true
	for id, sess := range s.sessions {
		var newest time.Time
		for _, rec := range sess.records {
			if rec.createdAt.After(newest) {
				newest = rec.createdAt
			}
		}
		if first || newest.Before(vt) || (newest.Equal(vt) && id < victim) {
			victim, vt, first = id, newest, false
		}
	}
	if victim != "" {
		delete(s.sessions, victim)
	}
}

// Len reports the number of tracked sessions (test/introspection).
func (s *AuthoritativeApprovalStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}
