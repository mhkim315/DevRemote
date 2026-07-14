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

// A1-B (+ R2/R3 remediation) — generation-bound authoritative ApprovalStore.
//
// The store is the complete execution-authority boundary. ClaimForExecution
// recomputes the canonical ActionDigest AND canonical delivery payload from the
// stored option, binds the immutable server-derived requester authorization context,
// runs the FULL current-authority checks (stored permission, runtime, supersession,
// expiry) BEFORE any idempotent-replay decision, and issues a claim token owning one
// immutable ApprovalExecutionBinding. RecordDelivery commits a success ONLY for an
// accepted/already_accepted receipt whose claim token, every binding field, and the
// delivered-payload digest match, and only if the runtime was not superseded. A
// non-accepting delivery leaves the record re-claimable under the frozen bounded
// manual retry.

const (
	authMaxApprovalsPerSession = 50
	authMaxApprovalSessions    = 1024
	authApprovalExpiry         = 5 * time.Minute
	authMaxPromptLen           = 4096
	authMaxOptionField         = 512
	authMaxOptions             = 32
	authMaxApprovalIDLen       = 256
	authMaxIdempotencyKeys     = 256
	maxManualRetries           = 2
)

// ApprovalIngestItem is one authoritative approval request offered for ingestion.
type ApprovalIngestItem struct {
	Approval     agent.AgentApproval
	Provenance   contract.Provenance
	Actionable   bool
	RequiredPerm string
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

// ApprovalSnapshot is an immutable value copy for DISPLAY / pre-validation only.
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
	optionFprint string
	createdAt    time.Time
	expiresAt    time.Time
	resolvedAt   *time.Time

	claimToken    string
	claimOptionID string
	binding       ApprovalExecutionBinding
	auth          RequesterAuthContext
	retries       int
	superseded    bool
}

// idempotencyEntry binds one idempotency key to the exact approval execution and the
// immutable requester authorization context.
type idempotencyEntry struct {
	binding  ApprovalExecutionBinding
	auth     RequesterAuthContext
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

func NewAuthoritativeApprovalStore() *AuthoritativeApprovalStore {
	return &AuthoritativeApprovalStore{sessions: make(map[string]*sessionApprovals), now: time.Now}
}

func genNewer(launchA int64, streamA int, launchB int64, streamB int) bool {
	if launchA != launchB {
		return launchA > launchB
	}
	return streamA > streamB
}

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

func newClaimToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

func optionView(opt *agent.InteractionOption) interactionOptionView {
	v := interactionOptionView{id: opt.ID, kind: opt.Kind}
	if opt.Input != nil {
		v.hasInput = true
		v.required = opt.Input.Required
		v.placement = opt.Input.Placement
	}
	return v
}

func findStoredOption(rec *approvalRecord, optionID string) *agent.InteractionOption {
	for i := range rec.approval.Options {
		if rec.approval.Options[i].ID == optionID {
			return &rec.approval.Options[i]
		}
	}
	return nil
}

// Ingest records the session's current-generation approval requests under the
// generation rule; a newer generation supersedes prior pending AND executing authority.
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
			s.supersedeLocked(sess, "generation advanced")
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
				continue
			}
			continue
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

func (s *AuthoritativeApprovalStore) supersedeLocked(sess *sessionApprovals, reason string) {
	now := s.now()
	for _, rec := range sess.records {
		switch rec.state {
		case ApprovalPending:
			rec.state = ApprovalInvalidated
			rec.resolvedAt = &now
		case ApprovalExecuting, ApprovalDeliveryFailed:
			rec.superseded = true
		}
	}
}

// InvalidateSession supersedes pending + executing/retryable authority (correlation
// loss / version conflict) without advancing generation.
func (s *AuthoritativeApprovalStore) InvalidateSession(sessionID, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.sessions[sessionID]; sess != nil {
		s.supersedeLocked(sess, reason)
	}
}

// SupersedeRuntime advances the runtime high-water and supersedes prior authority
// (launch replacement / stream-generation change).
func (s *AuthoritativeApprovalStore) SupersedeRuntime(sessionID string, launchGen int64, streamGen int, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[sessionID]
	if sess == nil {
		return
	}
	if !sess.hwInitialized || genNewer(launchGen, streamGen, sess.hwLaunch, sess.hwStream) {
		sess.hwLaunch, sess.hwStream = launchGen, streamGen
		sess.hwInitialized = true
	}
	s.supersedeLocked(sess, reason)
}

// Clear drops all records for a session on delete/unlink.
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

// ClaimForExecution is the ONE atomic execution-authority transition. The full
// current authority (stored permission, runtime, supersession, expiry) is validated
// BEFORE any idempotent-replay decision; `already_accepted` is returned only for the
// exact binding + exact current requester authorization context under a current,
// non-superseded runtime. A non-accepting prior delivery is re-claimable under the
// bounded manual retry.
func (s *AuthoritativeApprovalStore) ClaimForExecution(req ClaimRequest) ClaimResult {
	if req.SessionID == "" || req.ApprovalID == "" {
		return ClaimResult{Outcome: ClaimNotFound}
	}
	if !validCanonicalKey(req.IdempotencyKey) {
		return ClaimResult{Outcome: ClaimInvalidKey}
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

	if !rec.actionable {
		return ClaimResult{Outcome: ClaimNotActionable}
	}
	opt := findStoredOption(rec, req.OptionID)
	if opt == nil {
		return ClaimResult{Outcome: ClaimUnknownAction}
	}
	view := optionView(opt)
	if !validPlacement(view.placement) || !validInput(&view, req.Input) {
		return ClaimResult{Outcome: ClaimInvalidInput}
	}
	digest := canonicalActionFromOption(&view, req.Input).Digest()
	payload := canonicalPayload(&view, req.Input)
	pdigest := payloadDigest(payload)
	if req.AssertDigest != "" && req.AssertDigest != digest {
		return ClaimResult{Outcome: ClaimDigestMismatch}
	}
	bound := RuntimeRef{Adapter: rec.provider, Version: rec.version, LaunchGen: rec.launchGen, StreamGen: rec.streamGen}
	binding := ApprovalExecutionBinding{
		ApprovalID: req.ApprovalID, SessionID: req.SessionID, Runtime: bound,
		ActionDigest: digest, PayloadDigest: pdigest, IdempotencyKey: req.IdempotencyKey,
	}
	auth := canonicalRequesterAuth(req.Requester)

	// R3-A/R4-A: the SINGLE requester validator gates EVERY path — this runs before
	// the idempotent-replay branch, so an initial claim, a bounded manual retry, and
	// an already_accepted replay all require a complete server-derived requester
	// context AND the stored permission.
	if !requesterAuthorized(req.Requester, rec.requiredPerm) {
		return ClaimResult{Outcome: ClaimUnauthorized}
	}
	if req.Runtime.Adapter != bound.Adapter || req.Runtime.Version != bound.Version {
		return ClaimResult{Outcome: ClaimRuntimeMismatch}
	}
	if req.Runtime.LaunchGen != bound.LaunchGen || req.Runtime.StreamGen != bound.StreamGen {
		return ClaimResult{Outcome: ClaimStaleRuntime}
	}
	if rec.superseded {
		return ClaimResult{Outcome: ClaimStaleRuntime}
	}

	// Idempotent replay — only after the authority checks above passed.
	if led, ok := sess.idempotency[req.IdempotencyKey]; ok {
		if !led.binding.equal(binding) || !led.auth.equal(auth) {
			return ClaimResult{Outcome: ClaimConflict}
		}
		if led.accepted {
			return ClaimResult{Outcome: ClaimAlreadyAccepted, Binding: binding, Payload: payload}
		}
		switch rec.state {
		case ApprovalExecuting:
			return ClaimResult{Outcome: ClaimAlreadyOwned}
		case ApprovalDeliveryFailed:
			// R3-D: bounded manual retry with the SAME key/binding/auth.
			if s.now().After(rec.expiresAt) {
				return ClaimResult{Outcome: ClaimExpired}
			}
			if rec.retries >= maxManualRetries {
				return ClaimResult{Outcome: ClaimRetryExhausted}
			}
			token := newClaimToken()
			if token == "" {
				return ClaimResult{Outcome: ClaimUnauthorized}
			}
			rec.retries++
			rec.state = ApprovalExecuting
			rec.claimToken = token
			rec.claimOptionID = opt.ID
			rec.binding = binding
			rec.auth = auth
			return ClaimResult{Outcome: ClaimGranted, Token: token, Binding: binding, Payload: payload}
		default:
			return ClaimResult{Outcome: ClaimAlreadyOwned}
		}
	}

	if rec.state == ApprovalExpired {
		return ClaimResult{Outcome: ClaimExpired}
	}
	if rec.state != ApprovalPending {
		return ClaimResult{Outcome: ClaimAlreadyOwned}
	}
	if len(sess.idempotency) >= authMaxIdempotencyKeys {
		return ClaimResult{Outcome: ClaimLedgerFull}
	}
	token := newClaimToken()
	if token == "" {
		return ClaimResult{Outcome: ClaimUnauthorized}
	}
	rec.state = ApprovalExecuting
	rec.claimToken = token
	rec.claimOptionID = opt.ID
	rec.binding = binding
	rec.auth = auth
	rec.retries = 0
	sess.idempotency[req.IdempotencyKey] = idempotencyEntry{binding: binding, auth: auth, accepted: false}
	return ClaimResult{Outcome: ClaimGranted, Token: token, Binding: binding, Payload: payload}
}

// DeliveryCommit is the result of RecordDelivery.
type DeliveryCommit struct {
	Outcome   DeliveryOutcome
	Committed bool
	State     ApprovalState
	Kind      string
}

// RecordDelivery consumes a fully-bound receipt. It commits a success ONLY when the
// claim token, EVERY binding field, and the delivered-payload digest match the
// executing record, the runtime is not superseded, and the outcome is
// accepted/already_accepted with an opaque ReceiptID. A non-accepting outcome leaves
// the record delivery_failed and retryable (bounded); an ambiguous or superseded
// case is non-retryable. Substituted bytes (payload-digest mismatch) never commit.
func (s *AuthoritativeApprovalStore) RecordDelivery(receipt DeliveryReceipt) DeliveryCommit {
	b := receipt.Binding
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[b.SessionID]
	if sess == nil {
		return DeliveryCommit{Outcome: DeliveryUnavailable}
	}
	rec := sess.records[b.ApprovalID]
	if rec == nil {
		return DeliveryCommit{Outcome: DeliveryUnavailable}
	}
	if rec.state != ApprovalExecuting || rec.claimToken == "" {
		return DeliveryCommit{Outcome: DeliveryRejected, State: rec.state}
	}
	if receipt.ClaimToken != rec.claimToken || !b.equal(rec.binding) {
		return DeliveryCommit{Outcome: DeliveryRejected, State: rec.state}
	}
	now := s.now()
	if rec.superseded {
		rec.state = ApprovalDeliveryFailed
		rec.resolvedAt = &now
		rec.retries = maxManualRetries // superseded runtime is not retryable
		return DeliveryCommit{Outcome: DeliveryStaleRuntime, State: rec.state}
	}
	if !IsValidDeliveryOutcome(receipt.Outcome) {
		receipt.Outcome = DeliveryRejected
	}
	if deliverySucceeded(receipt.Outcome) {
		// R3-B: the EXACT delivered bytes must match the canonical payload digest.
		if receipt.ReceiptID == "" || receipt.DeliveredPayloadDigest != rec.binding.PayloadDigest {
			rec.state = ApprovalDeliveryFailed
			rec.resolvedAt = &now
			rec.retries = maxManualRetries // integrity failure is not retryable
			return DeliveryCommit{Outcome: DeliveryRejected, State: rec.state}
		}
		opt := findStoredOption(rec, rec.claimOptionID)
		kind := ""
		if opt != nil {
			kind = opt.Kind
		}
		rec.state = terminalStateForKind(kind)
		rec.resolvedAt = &now
		if e, ok := sess.idempotency[rec.binding.IdempotencyKey]; ok {
			e.accepted = true
			sess.idempotency[rec.binding.IdempotencyKey] = e
		}
		return DeliveryCommit{Outcome: receipt.Outcome, Committed: true, State: rec.state, Kind: kind}
	}
	// Non-success. Retryable only when the outcome proves non-acceptance.
	rec.state = ApprovalDeliveryFailed
	rec.resolvedAt = &now
	if !deliveryProvesNonAcceptance(receipt.Outcome) {
		rec.retries = maxManualRetries // ambiguous → non-retryable
	}
	return DeliveryCommit{Outcome: receipt.Outcome, Committed: false, State: rec.state}
}

func (s *AuthoritativeApprovalStore) List(sessionID string) []agent.AgentApproval {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[sessionID]
	if sess == nil {
		return nil
	}
	recs := s.liveRecordsLocked(sess)
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

func (s *AuthoritativeApprovalStore) ListSafe(sessionID string) []SafeApprovalDTO {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[sessionID]
	if sess == nil {
		return nil
	}
	recs := s.liveRecordsLocked(sess)
	out := make([]SafeApprovalDTO, 0, len(recs))
	for _, rec := range recs {
		out = append(out, projectSafeApproval(rec))
	}
	return out
}

func (s *AuthoritativeApprovalStore) liveRecordsLocked(sess *sessionApprovals) []*approvalRecord {
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
	return recs
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

func (s *AuthoritativeApprovalStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}
