package term

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// A1-B (+ R-A/R-B remediation) — generation-bound authoritative ApprovalStore.
//
// This is the authoritative record + execution-authority boundary for agent
// approvals. Each pending request is immutably bound to its runtime, provider,
// generation, provenance, and allowed actions at ingest. Execution authority is
// acquired ONLY through one atomic ClaimForExecution transition that validates the
// entire binding in a single critical section and returns an unforgeable claim
// token; delivery is recorded through RecordDelivery, and only an `accepted` /
// `already_accepted` receipt bound to the exact claim token and ActionDigest may
// commit a successful terminal state. There is no separate lookup→validate→mutate
// authority path, no generic overwrite-queue delivery, and no second execution
// owner under any race.

const (
	authMaxApprovalsPerSession = 50
	authMaxApprovalSessions    = 1024
	authApprovalExpiry         = 5 * time.Minute
	authMaxPromptLen           = 4096
	authMaxOptionField         = 512
	authMaxOptions             = 32
	authMaxApprovalIDLen       = 256
	authMaxIdempotencyKeys     = 256
)

// ApprovalIngestItem is one authoritative approval request offered for ingestion.
type ApprovalIngestItem struct {
	Approval   agent.AgentApproval // must carry a non-empty ID and match the ingest session
	Provenance contract.Provenance // authoritative provenance of the source event
	// Actionable is true ONLY when a controlled fixture has proven this provider's
	// exact evidence→action→delivery mapping (B5). No such mapping exists in
	// production today, so production ingestion sets it false: the approval is
	// non-actionable intervention information (no buttons, no claim, no delivery).
	Actionable   bool
	RequiredPerm string // authorization context the action route must satisfy
}

// ApprovalIngest is one generation-scoped ingestion for a single session.
type ApprovalIngest struct {
	SessionID string
	LaunchGen int64
	StreamGen int
	Provider  string
	Version   string
	Items     []ApprovalIngestItem
}

// ApprovalSnapshot is an immutable value copy of a stored record for revalidation.
type ApprovalSnapshot struct {
	SessionID    string
	ApprovalID   string
	Provider     string
	Version      string
	LaunchGen    int64
	StreamGen    int
	Provenance   contract.Provenance
	State        ApprovalState
	Actionable   bool
	RequiredPerm string
	Options      []agent.InteractionOption
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// BoundRuntime returns the runtime identity this record is bound to.
func (s ApprovalSnapshot) BoundRuntime() RuntimeRef {
	return RuntimeRef{Adapter: s.Provider, Version: s.Version, LaunchGen: s.LaunchGen, StreamGen: s.StreamGen}
}

type approvalRecord struct {
	approval     agent.AgentApproval
	state        ApprovalState
	provider     string
	version      string
	launchGen    int64
	streamGen    int
	provenance   contract.Provenance
	requiredPerm string
	actionable   bool
	optionFprint string // ingest-dedup fingerprint of the option set (NOT execution authority)
	createdAt    time.Time
	expiresAt    time.Time
	resolvedAt   *time.Time

	// execution-claim binding (set atomically by ClaimForExecution)
	claimToken    string
	claimDigest   string
	claimKey      string
	claimOptionID string
	requester     RequesterContext
}

// idempotencyEntry records the digest and acceptance state for one idempotency key.
type idempotencyEntry struct {
	digest   string
	accepted bool
}

type sessionApprovals struct {
	records       map[string]*approvalRecord
	idempotency   map[string]idempotencyEntry
	hwLaunch      int64
	hwStream      int
	hwInitialized bool
}

// AuthoritativeApprovalStore is the generation-bound approval record + claim store.
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

func genNewer(launchA int64, streamA int, launchB int64, streamB int) bool {
	if launchA != launchB {
		return launchA > launchB
	}
	return streamA > streamB
}

// optionSetFingerprint is an order-independent fingerprint of the option set used
// ONLY to detect a same-generation duplicate approval ID whose contract changed. It
// is NOT the execution ActionDigest (that is the per-selected-action CanonicalAction
// digest computed at claim time).
func optionSetFingerprint(kind, def string, options []agent.InteractionOption) string {
	idx := make([]int, len(options))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return options[idx[a]].ID < options[idx[b]].ID })
	h := sha256.New()
	h.Write([]byte("k:" + kind + "\x00d:" + def + "\x00"))
	for _, i := range idx {
		o := options[i]
		h.Write([]byte("id:" + o.ID + "\x1fkd:" + o.Kind + "\x1fpl:" + o.Payload + "\x1f"))
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

func copyInputSchema(in *agent.InputSchema) *agent.InputSchema {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

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

// newClaimToken returns an opaque, unforgeable 128-bit token. It is not derivable
// from the ApprovalID and never appears in a public DTO.
func newClaimToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// Ingest records the session's current-generation approval requests under the
// generation rule (unchanged from A1-B), now also carrying the per-item
// actionability decision.
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
		sess = &sessionApprovals{records: make(map[string]*approvalRecord), idempotency: make(map[string]idempotencyEntry)}
		s.sessions[in.SessionID] = sess
	}

	if sess.hwInitialized {
		if genNewer(sess.hwLaunch, sess.hwStream, in.LaunchGen, in.StreamGen) {
			return
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
			continue
		}
		if a.SessionID != "" && a.SessionID != in.SessionID {
			continue
		}
		if !contract.ApprovalAuthoritative(item.Provenance) {
			continue
		}
		fprint := optionSetFingerprint(a.Kind, a.Default, a.Options)
		if existing, ok := sess.records[a.ID]; ok {
			if existing.state != ApprovalPending {
				continue
			}
			if existing.optionFprint != fprint {
				continue // changed action set for a live id: refuse the mutation
			}
			continue // idempotent
		}
		if len(sess.records) >= authMaxApprovalsPerSession {
			s.evictOneLocked(sess)
			if len(sess.records) >= authMaxApprovalsPerSession {
				continue
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
			actionable:   item.Actionable,
			optionFprint: fprint,
			createdAt:    now,
			expiresAt:    now.Add(authApprovalExpiry),
		}
		sess.records[a.ID] = rec
	}
}

func (s *AuthoritativeApprovalStore) invalidatePendingLocked(sess *sessionApprovals, reason string) {
	now := s.now()
	for _, rec := range sess.records {
		if rec.state == ApprovalPending {
			rec.state = ApprovalInvalidated
			rec.resolvedAt = &now
		}
	}
}

// InvalidateSession marks all pending requests invalidated (correlation loss).
// Executing records are NOT force-committed here; the delivery boundary's runtime
// revalidation (B4) rejects a stale in-flight delivery.
func (s *AuthoritativeApprovalStore) InvalidateSession(sessionID, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.sessions[sessionID]; sess != nil {
		s.invalidatePendingLocked(sess, reason)
	}
}

// Clear drops all records for a session on delete/unlink so a recreated session
// cannot inherit prior authority; in-flight claim tokens for it become void (a
// subsequent RecordDelivery finds no record → non-success).
func (s *AuthoritativeApprovalStore) Clear(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

func (s *AuthoritativeApprovalStore) expireLocked(rec *approvalRecord) {
	if rec.state == ApprovalPending && s.now().After(rec.expiresAt) {
		now := s.now()
		rec.state = ApprovalExpired
		rec.resolvedAt = &now
	}
}

func findStoredOption(rec *approvalRecord, optionID string) *agent.InteractionOption {
	for i := range rec.approval.Options {
		if rec.approval.Options[i].ID == optionID {
			return &rec.approval.Options[i]
		}
	}
	return nil
}

// LookupRecord returns an immutable snapshot for DISPLAY/pre-validation only. It
// carries NO execution authority — execution is acquired solely via
// ClaimForExecution.
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
		Actionable:   rec.actionable,
		RequiredPerm: rec.requiredPerm,
		Options:      copyOptions(rec.approval.Options),
		CreatedAt:    rec.createdAt,
		ExpiresAt:    rec.expiresAt,
	}
}

// ClaimForExecution is the ONE atomic execution-authority transition. In a single
// critical section it validates the entire binding — existence, exact session,
// actionability, expiry, current state, idempotency, server-derived requester and
// permission, current runtime (adapter/provider/version + launch/stream generation),
// the selected option, and the caller-supplied canonical ActionDigest — and, only
// on full success, transitions pending→executing and issues an opaque claim token.
// It never leaves a check-before-claim window and never creates a second owner.
func (s *AuthoritativeApprovalStore) ClaimForExecution(req ClaimRequest) ClaimResult {
	if req.SessionID == "" || req.ApprovalID == "" {
		return ClaimResult{Outcome: ClaimNotFound}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	sess := s.sessions[req.SessionID]
	if sess == nil {
		return ClaimResult{Outcome: ClaimNotFound}
	}
	rec := sess.records[req.ApprovalID]
	if rec == nil {
		return ClaimResult{Outcome: ClaimNotFound}
	}
	s.expireLocked(rec)

	// Idempotency is evaluated first so a replay of an accepted or in-flight key is
	// resolved without creating a second owner.
	if req.IdempotencyKey != "" {
		if led, ok := sess.idempotency[req.IdempotencyKey]; ok {
			if led.digest != req.ActionDigest {
				return ClaimResult{Outcome: ClaimConflict}
			}
			if led.accepted {
				return ClaimResult{Outcome: ClaimAlreadyAccepted, Digest: req.ActionDigest}
			}
			// same key+digest, not yet accepted: the original claim owns it.
			return ClaimResult{Outcome: ClaimAlreadyOwned}
		}
	}

	if !rec.actionable {
		return ClaimResult{Outcome: ClaimNotActionable}
	}
	if rec.state == ApprovalExpired {
		return ClaimResult{Outcome: ClaimExpired}
	}
	if rec.state != ApprovalPending {
		return ClaimResult{Outcome: ClaimAlreadyOwned}
	}
	// Server-derived requester + permission (client identity is never trusted).
	need := req.RequiredPerm
	if need == "" {
		need = rec.requiredPerm
	}
	if !req.Requester.present() || (need != "" && !req.Requester.hasPermission(need)) {
		return ClaimResult{Outcome: ClaimUnauthorized}
	}
	// Runtime binding: adapter/provider/version first, then generation.
	bound := RuntimeRef{Adapter: rec.provider, Version: rec.version, LaunchGen: rec.launchGen, StreamGen: rec.streamGen}
	if req.Runtime.Adapter != bound.Adapter || req.Runtime.Version != bound.Version {
		return ClaimResult{Outcome: ClaimRuntimeMismatch}
	}
	if req.Runtime.LaunchGen != bound.LaunchGen || req.Runtime.StreamGen != bound.StreamGen {
		return ClaimResult{Outcome: ClaimStaleRuntime}
	}
	// The selected option must exist; its digest is the handler-computed canonical
	// action digest bound through the whole flow.
	if findStoredOption(rec, req.OptionID) == nil {
		return ClaimResult{Outcome: ClaimUnknownAction}
	}
	if req.ActionDigest == "" {
		return ClaimResult{Outcome: ClaimUnknownAction}
	}

	token := newClaimToken()
	if token == "" {
		return ClaimResult{Outcome: ClaimUnauthorized} // entropy failure fails closed
	}
	rec.state = ApprovalExecuting
	rec.claimToken = token
	rec.claimDigest = req.ActionDigest
	rec.claimKey = req.IdempotencyKey
	rec.claimOptionID = req.OptionID
	rec.requester = req.Requester
	if req.IdempotencyKey != "" {
		if len(sess.idempotency) < authMaxIdempotencyKeys {
			sess.idempotency[req.IdempotencyKey] = idempotencyEntry{digest: req.ActionDigest, accepted: false}
		}
	}
	return ClaimResult{Outcome: ClaimGranted, Token: token, Digest: req.ActionDigest}
}

// DeliveryCommit is the result of RecordDelivery.
type DeliveryCommit struct {
	Outcome   DeliveryOutcome
	Committed bool          // true iff the approval transitioned to a successful terminal state
	State     ApprovalState // resulting record state
	Kind      string        // committed option kind (approve/reject/...) when Committed
}

// RecordDelivery consumes a delivery receipt against the EXACT claim token and
// ActionDigest and transitions the record. Only `accepted`/`already_accepted`
// commit a successful terminal state (and mark the idempotency key accepted); every
// other receipt, a missing record, a token/digest mismatch, or a non-executing
// state yields a non-success (delivery_failed) and never a committed success.
func (s *AuthoritativeApprovalStore) RecordDelivery(sessionID, approvalID, token string, receipt DeliveryReceipt) DeliveryCommit {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[sessionID]
	if sess == nil {
		return DeliveryCommit{Outcome: DeliveryUnavailable}
	}
	rec := sess.records[approvalID]
	if rec == nil {
		return DeliveryCommit{Outcome: DeliveryUnavailable}
	}
	if rec.state != ApprovalExecuting || rec.claimToken == "" || rec.claimToken != token {
		return DeliveryCommit{Outcome: DeliveryRejected, State: rec.state}
	}
	if rec.claimDigest != receipt.ActionDigest {
		return DeliveryCommit{Outcome: DeliveryConflict, State: rec.state}
	}
	if !IsValidDeliveryOutcome(receipt.Outcome) {
		receipt.Outcome = DeliveryRejected
	}
	now := s.now()
	if deliverySucceeded(receipt.Outcome) {
		opt := findStoredOption(rec, rec.claimOptionID)
		kind := ""
		if opt != nil {
			kind = opt.Kind
		}
		rec.state = terminalStateForKind(kind)
		rec.resolvedAt = &now
		if rec.claimKey != "" {
			sess.idempotency[rec.claimKey] = idempotencyEntry{digest: rec.claimDigest, accepted: true}
		}
		return DeliveryCommit{Outcome: receipt.Outcome, Committed: true, State: rec.state, Kind: kind}
	}
	rec.state = ApprovalDeliveryFailed
	rec.resolvedAt = &now
	return DeliveryCommit{Outcome: receipt.Outcome, Committed: false, State: rec.state}
}

// List returns the public projection (unchanged internal type; see safe DTO for the
// external boundary). Pending/executing always kept; terminal within the window.
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
		pub := rec.approval
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

// ListSafe returns the bounded, redacted public DTO projection (B6).
func (s *AuthoritativeApprovalStore) ListSafe(sessionID string) []SafeApprovalDTO {
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
	out := make([]SafeApprovalDTO, 0, len(recs))
	for _, rec := range recs {
		out = append(out, projectSafeApproval(rec))
	}
	return out
}

func (s *AuthoritativeApprovalStore) evictOneLocked(sess *sessionApprovals) {
	var victim string
	var vt time.Time
	var victimTerminal bool
	first := true
	for id, rec := range sess.records {
		terminal := IsTerminalApprovalState(rec.state)
		better := first ||
			(terminal && !victimTerminal) ||
			(terminal == victimTerminal && (rec.createdAt.Before(vt) || (rec.createdAt.Equal(vt) && id < victim)))
		if better {
			victim, vt, victimTerminal, first = id, rec.createdAt, terminal, false
		}
	}
	if victim != "" {
		delete(sess.records, victim)
	}
}

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
