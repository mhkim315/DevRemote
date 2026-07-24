package term

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/devicetrust"
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
	// DeliveryMaterial (SP1 P2A, optional) supplies daemon-generated immutable
	// delivery material per certified option. Allowed only on an actionable
	// item; any invalid entry rejects the WHOLE item. Production managed
	// ingestion supplies none (non-actionable observation).
	DeliveryMaterial []ApprovalDeliveryMaterial
	// CatalogActionID (C2D P2B) is the optional catalog action identifier
	// set by the Claude provider boundary. Empty for non-catalog observations.
	// Internal only — never projected directly into a DTO; used server-side
	// to select the Pokit-owned static summary label.
	CatalogActionID string
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
	approval        agent.AgentApproval
	state           ApprovalState
	provider        string
	version         string
	launchGen       int64
	streamGen       int
	provenance      contract.Provenance
	requiredPerm    string
	actionable      bool
	optionFprint    string
	catalogActionID string // C2D P2B: empty for non-catalog observations
	// delivery holds the defensively-copied per-option delivery material
	// (SP1 P2A). Internal only — never projected into any DTO.
	delivery   map[string]storedDeliveryMaterial
	createdAt  time.Time
	expiresAt  time.Time
	resolvedAt *time.Time

	claimToken    string
	claimOptionID string
	binding       ApprovalExecutionBinding
	auth          RequesterAuthContext
	retries       int
	superseded    bool
}

// storedDeliveryMaterial is the store-owned immutable copy of one option's
// delivery material.
type storedDeliveryMaterial struct {
	schemaVersion string
	bytes         []byte
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
	mu         sync.Mutex
	sessions   map[string]*sessionApprovals
	now        func() time.Time
	authorizer devicetrust.MutationAuthorizer
}

func NewAuthoritativeApprovalStore(authorizers ...devicetrust.MutationAuthorizer) *AuthoritativeApprovalStore {
	authorizer := devicetrust.MutationAuthorizer(localMutationAuthorizer{})
	if len(authorizers) > 0 {
		if authorizers[0] == nil {
			return nil
		}
		authorizer = authorizers[0]
	}
	return &AuthoritativeApprovalStore{sessions: make(map[string]*sessionApprovals), now: time.Now, authorizer: authorizer}
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

// boundCatalogID returns the catalogActionID only when it is valid for the
// given provider and version. Otherwise returns empty string (non-catalog).
// An empty input is passed through unchanged.
func boundCatalogID(catalogActionID, provider, version string) string {
	if !validCatalogBinding(catalogActionID, provider, version) {
		return ""
	}
	return boundStr(catalogActionID, maxCoordinatorToolName)
}

func newClaimToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// validateDeliveryMaterial validates and defensively copies the OPTIONAL
// per-option delivery material of one ingest item (SP1 P2A). ok=false rejects
// the whole item: material on a non-actionable item, an unknown or duplicate
// option ID, an empty/oversized schema version, or empty/oversized response
// bytes all fail closed.
func validateDeliveryMaterial(item ApprovalIngestItem, opts []agent.InteractionOption) (map[string]storedDeliveryMaterial, bool) {
	if len(item.DeliveryMaterial) == 0 {
		return nil, true
	}
	if !item.Actionable {
		return nil, false
	}
	known := make(map[string]bool, len(opts))
	for _, o := range opts {
		known[o.ID] = true
	}
	out := make(map[string]storedDeliveryMaterial, len(item.DeliveryMaterial))
	for _, m := range item.DeliveryMaterial {
		if m.OptionID == "" || !known[m.OptionID] {
			return nil, false
		}
		if _, dup := out[m.OptionID]; dup {
			return nil, false
		}
		if m.SchemaVersion == "" || len(m.SchemaVersion) > maxDeliverySchemaVerBytes {
			return nil, false
		}
		if len(m.ResponseBytes) == 0 || len(m.ResponseBytes) > maxDeliveryMaterialBytes {
			return nil, false
		}
		out[m.OptionID] = storedDeliveryMaterial{
			schemaVersion: m.SchemaVersion,
			bytes:         append([]byte(nil), m.ResponseBytes...),
		}
	}
	return out, true
}

// materialFingerprint folds the stored delivery material into the record's
// option-set fingerprint: a same-ID re-offer with different material is not
// the same request. Empty material contributes the empty string, so records
// without material keep their frozen fingerprints.
func materialFingerprint(mats map[string]storedDeliveryMaterial) string {
	if len(mats) == 0 {
		return ""
	}
	ids := make([]string, 0, len(mats))
	for id := range mats {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	h := sha256.New()
	h.Write([]byte("a1.material.v1\x00"))
	for _, id := range ids {
		m := mats[id]
		h.Write([]byte("id:" + id + "\x1fsv:" + m.schemaVersion + "\x1fpd:" + payloadDigest(m.bytes) + "\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))
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
// generation rule. Returns the EXACT approval IDs newly admitted by this locked
// transaction. Idempotent re-offers are excluded from the returned set.
// An empty slice means zero new records were admitted.
func (s *AuthoritativeApprovalStore) Ingest(in ApprovalIngest) []string {
	return s.ingest(in)
}

// IngestObserved is a NARROW SP1-P1 additive entry for single-item
// NON-ACTIONABLE observation ingest: it applies EXACTLY the same admission
// rules as Ingest and reports whether the item was admitted as a NEW record,
// so an observer can keep its private state and the store record as one
// outcome. It grants no claim, delivery, or actionability semantics.
func (s *AuthoritativeApprovalStore) IngestObserved(in ApprovalIngest) bool {
	if len(in.Items) != 1 {
		return false
	}
	return len(s.ingest(in)) == 1
}

// ingest applies the generation rule and admission checks, returning the
// number of items admitted as NEW records (an idempotent re-offer of an
// existing record is NOT counted).
//
// SAFETY (R11-F1): session creation and generation high-water advance are
// DEFERRED until after at least one item passes ALL validation gates
// (authoritative provenance, structural validity, delivery material).
// An invalid ingest must not create a session, advance generation, or
// supersede records.
func (s *AuthoritativeApprovalStore) ingest(in ApprovalIngest) (admitted []string) {
	if in.SessionID == "" || len(in.SessionID) > maxSessionIDLen {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	sess := s.sessions[in.SessionID]

	// Pre-check: reject older generation (read-only, no mutation).
	if sess != nil && sess.hwInitialized {
		if genNewer(sess.hwLaunch, sess.hwStream, in.LaunchGen, in.StreamGen) {
			return nil
		}
	}

	now := s.now()

	// First pass: validate items WITHOUT mutating session state.
	// Track both (a) items that can be admitted as new records and
	// (b) whether ANY item is structurally valid (authoritative provenance).
	// A structurally valid newer generation must supersede old records even
	// when specific items fail duplicate-ID or capacity checks (R11-F1 fix 3).
	type pendingRec struct {
		rec    *approvalRecord
		fprint string
	}
	var pending []pendingRec
	existingCount := 0
	if sess != nil {
		existingCount = len(sess.records)
	}
	hasAuthoritativeItem := false

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

		copied := copyOptions(a.Options)
		delivery, mok := validateDeliveryMaterial(item, copied)
		if !mok {
			continue // invalid delivery material rejects the whole item
		}
		// C3D §4: provider-keyed actionable-admission policy. The deepest
		// Store admission boundary re-verifies the exact certified catalog
		// tuple and daemon-recomputed options/delivery material for every
		// actionable claude_headless item — a forged Actionable, catalog ID,
		// option, schema or material is rejected here even when a trusted
		// internal caller supplies it. Rejection happens at the same stage as
		// invalid delivery material: the item neither creates a session nor
		// advances the generation high-water.
		if !providerActionableAdmission(in, item, copied, delivery) {
			continue
		}
		// Item is structurally valid (provenance + delivery passed).
		// A newer generation must supersede old authority even if
		// this specific item cannot be admitted.
		hasAuthoritativeItem = true

		fprint := optionSetFingerprint(a.Kind, a.Default, a.Options) + materialFingerprint(delivery)
		if sess != nil {
			if existing, ok := sess.records[a.ID]; ok {
				if existing.state != ApprovalPending {
					continue
				}
				if existing.optionFprint != fprint {
					continue
				}
				continue // idempotent re-offer of existing pending record
			}
		}
		// Capacity check (no mutation yet — count pending items).
		if existingCount+len(pending) >= authMaxApprovalsPerSession {
			continue
		}
		rec := &approvalRecord{
			approval: agent.AgentApproval{
				ID:         a.ID,
				SessionID:  in.SessionID,
				AgentKind:  boundStr(a.AgentKind, authMaxOptionField),
				Kind:       boundStr(a.Kind, authMaxOptionField),
				Prompt:     boundStr(a.Prompt, authMaxPromptLen),
				Options:    copied,
				Default:    boundStr(a.Default, authMaxOptionField),
				Source:     a.Source,
				Confidence: a.Confidence,
			},
			state:           ApprovalPending,
			provider:        boundStr(in.Provider, maxVersionLen),
			version:         boundStr(in.Version, maxVersionLen),
			launchGen:       in.LaunchGen,
			streamGen:       in.StreamGen,
			provenance:      item.Provenance,
			requiredPerm:    boundStr(item.RequiredPerm, authMaxOptionField),
			actionable:      item.Actionable,
			optionFprint:    fprint,
			catalogActionID: boundCatalogID(item.CatalogActionID, in.Provider, in.Version),
			delivery:        delivery,
			createdAt:       now,
			expiresAt:       now.Add(authApprovalExpiry),
		}
		pending = append(pending, pendingRec{rec: rec, fprint: fprint})
	}

	// Determine whether the generation should advance. A structurally valid
	// item (authoritative provenance) gates the generation change; the
	// generation advances even when admission fails for duplicate-ID or
	// capacity reasons (R11-F1 fix 3). Without any authoritative item the
	// entire ingest is a no-op (the original F1 fix).
	if !hasAuthoritativeItem && len(pending) == 0 {
		return nil // no structural validity, no new records → no state change
	}

	// Second pass: mutate session state.
	if sess == nil {
		if len(s.sessions) >= authMaxApprovalSessions {
			if !s.evictOldestSessionLocked() {
				return nil // capacity exhausted, no safe victim
			}
		}
		sess = &sessionApprovals{records: make(map[string]*approvalRecord), idempotency: make(map[string]idempotencyEntry)}
		s.sessions[in.SessionID] = sess
	}

	// Advance high-water + supersede if this is a structurally newer generation.
	if hasAuthoritativeItem {
		if sess.hwInitialized {
			if genNewer(in.LaunchGen, in.StreamGen, sess.hwLaunch, sess.hwStream) {
				s.supersedeLocked(sess, "generation advanced")
				sess.hwLaunch, sess.hwStream = in.LaunchGen, in.StreamGen
			}
		} else {
			sess.hwLaunch, sess.hwStream = in.LaunchGen, in.StreamGen
			sess.hwInitialized = true
		}
	}

	// Insert records (re-check duplicate under lock — safe since we hold mu).
	for _, p := range pending {
		if _, exists := sess.records[p.rec.approval.ID]; !exists {
			sess.records[p.rec.approval.ID] = p.rec
			admitted = append(admitted, p.rec.approval.ID)
		}
	}
	return admitted
}

// InvalidateRecord is a NARROW SP1-P1 additive display-safety transition: it
// marks ONE pending record invalidated (provider-side resolution observed, or
// its turn completed) so an already-resolved intervention is never presented
// as a current request. It uses the frozen legal pending→invalidated
// transition, never records a success, never touches any other record, and
// never advances the generation high-water. A missing or non-pending record
// is left unchanged.
func (s *AuthoritativeApprovalStore) InvalidateRecord(sessionID, approvalID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[sessionID]
	if sess == nil {
		return false
	}
	rec := sess.records[approvalID]
	if rec == nil || rec.state != ApprovalPending {
		return false
	}
	now := s.now()
	rec.state = ApprovalInvalidated
	rec.resolvedAt = &now
	return true
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

// InstallRuntimeGeneration is the metadata-only runtime-generation transition
// (R11-F1). It creates a session if one does not exist, atomically binds
// SessionID + LaunchGeneration + StreamGeneration, supersedes records at
// older generations, and installs the high-water used by IngestObserved.
// It creates NO Approval record — it is the single Store-owned path for
// termination, replacement and cleanup to install generation authority
// without an ingest side effect.
//
// SAFETY (R11-F1 review): supersedeLocked is ONLY called when the incoming
// generation is actually newer than the current high-water. An older or
// equal generation that does not advance the high-water is a complete
// no-op — it must not supersede current-generation authority.
func (s *AuthoritativeApprovalStore) InstallRuntimeGeneration(sessionID string, launchGen int64, streamGen int, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[sessionID]
	if sess == nil {
		if len(s.sessions) >= authMaxApprovalSessions {
			if !s.evictOldestSessionLocked() {
				return fmt.Errorf("approval session capacity exhausted")
			}
		}
		sess = &sessionApprovals{records: make(map[string]*approvalRecord), idempotency: make(map[string]idempotencyEntry)}
		s.sessions[sessionID] = sess
	}
	if !sess.hwInitialized || genNewer(launchGen, streamGen, sess.hwLaunch, sess.hwStream) {
		sess.hwLaunch, sess.hwStream = launchGen, streamGen
		sess.hwInitialized = true
		s.supersedeLocked(sess, reason)
	}
	return nil
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
	// SP1 P2A: an option with stored delivery material yields the EXACT stored
	// bytes as the claim payload, and the material's delivery-semantic fields
	// are folded into the canonical action digest. Options without material
	// keep the frozen legacy payload behavior byte-for-byte.
	action := canonicalActionFromOption(&view, req.Input)
	var payload []byte
	if mat, ok := rec.delivery[opt.ID]; ok {
		action.DeliverySchemaVersion = mat.schemaVersion
		action.DeliveryPayloadDigest = payloadDigest(mat.bytes)
		payload = append([]byte(nil), mat.bytes...)
	} else {
		payload = canonicalPayload(&view, req.Input)
	}
	digest := action.Digest()
	pdigest := payloadDigest(payload)
	if req.AssertDigest != "" && req.AssertDigest != digest {
		return ClaimResult{Outcome: ClaimDigestMismatch}
	}
	bound := RuntimeRef{Adapter: rec.provider, Version: rec.version, LaunchGen: rec.launchGen, StreamGen: rec.streamGen}
	binding := ApprovalExecutionBinding{
		ApprovalID: req.ApprovalID, SessionID: req.SessionID, Runtime: bound,
		ActionDigest: digest, PayloadDigest: pdigest, IdempotencyKey: req.IdempotencyKey,
		// SP1 P2A-R1: the store binds the SELECTED option identity and the
		// material's schema identity so the certified boundary can enforce
		// exact action↔response semantics and commit compares them.
		OptionID:       opt.ID,
		DeliverySchema: action.DeliverySchemaVersion,
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
	// Approval claim authorization is evaluated by the store while its
	// mutation lock is held, immediately before any claim transition.
	if err := s.authorizer.AuthorizeCommit(req.Requester.DeviceID, req.Requester.DeviceEpoch, devicetrust.IntentApprovalClaim); err != nil {
		return ClaimResult{Outcome: ClaimStaleEpoch}
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
	// Approval commit authorization is evaluated by the store while its
	// mutation lock is held, immediately before changing the accepted state.
	if err := s.authorizer.AuthorizeCommit(rec.auth.DeviceID, rec.auth.DeviceEpoch, devicetrust.IntentApprovalCommit); err != nil {
		return DeliveryCommit{Outcome: DeliveryStaleRuntime, State: rec.state}
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
		// Never evict pending or executing records.
		if rec.state == ApprovalPending || rec.state == ApprovalExecuting {
			continue
		}
		// Never evict delivery_failed records that still have retries.
		if rec.state == ApprovalDeliveryFailed && rec.retries < maxManualRetries {
			continue
		}
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

// evictOldestSessionLocked removes the oldest session with no live authority.
// Returns false when no safe victim exists (all sessions hold live authority).
func (s *AuthoritativeApprovalStore) evictOldestSessionLocked() bool {
	var victim string
	var vt time.Time
	first := true
	for id, sess := range s.sessions {
		// Never evict a session that holds live authority.
		if sessionHasLiveAuthority(sess) {
			continue
		}
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
		return true
	}
	return false
}

// sessionHasLiveAuthority reports whether a session must not be evicted by
// capacity pressure. Every hwInitialized session is protected — the high-water
// is always meaningful and blocks stale replay for its session ID. Only
// explicit lifecycle cleanup (Clear/delete) may remove a session.
func sessionHasLiveAuthority(sess *sessionApprovals) bool {
	return sess.hwInitialized
}

// providerActionableAdmission is the C3D §4 provider-keyed actionable-
// admission policy applied at the single Store admission site. It is a
// data-driven policy lookup, not a branch inside claim/receipt authority.
// Non-actionable items keep the frozen admission rules of every provider;
// codex_app_server actionable admission is byte-for-byte the accepted SP1
// behavior (no additional policy).
func providerActionableAdmission(in ApprovalIngest, item ApprovalIngestItem, opts []agent.InteractionOption, delivery map[string]storedDeliveryMaterial) bool {
	if !item.Actionable {
		return true
	}
	switch in.Provider {
	case claudeHeadlessAdapter:
		return claudeActionableAdmission(in, item, opts, delivery)
	default:
		return true
	}
}

// claudeActionableAdmission re-verifies the exact certified catalog tuple
// and the daemon-recomputed options/delivery material for one actionable
// claude_headless item (C3D contract §4). ALL of the following must hold —
// otherwise the item is dropped fail-closed:
//
//  1. StreamGen is zero;
//  2. the CatalogActionID resolves to a compiled catalog entry whose
//     Provider/Version match the ingest exactly (this transitively pins the
//     tool and the exact catalog command — the Store never needs raw bytes);
//  3. the option set is EXACTLY the two certified options (IDs, labels,
//     kinds; no input schema, no payload; order-insensitive);
//  4. the delivery material is EXACTLY the two daemon-recomputed responses
//     under the certified decision schema, byte-equal per option;
//  5. the stored permission is exactly the terminal-input permission;
//  6. the provenance is the provider hook.
func claudeActionableAdmission(in ApprovalIngest, item ApprovalIngestItem, opts []agent.InteractionOption, delivery map[string]storedDeliveryMaterial) bool {
	if in.StreamGen != 0 {
		return false
	}
	entry, found := lookupCatalogEntry(item.CatalogActionID)
	if !found || entry.Provider != in.Provider || entry.Version != in.Version {
		return false
	}
	if item.RequiredPerm != claudeRequiredPerm {
		return false
	}
	if item.Provenance != contract.ProvenanceProviderHook {
		return false
	}
	if len(opts) != 2 {
		return false
	}
	seen := make(map[string]bool, 2)
	for i := range opts {
		o := &opts[i]
		if o.Input != nil || o.Payload != "" || seen[o.ID] {
			return false
		}
		switch {
		case o.ID == "allow_once" && o.Label == "Allow once" && o.Kind == "approve":
		case o.ID == "deny" && o.Label == "Deny" && o.Kind == "reject":
		default:
			return false
		}
		seen[o.ID] = true
	}
	if len(delivery) != 2 {
		return false
	}
	for optionID, decision := range certifiedClaudeDecision {
		mat, ok := delivery[optionID]
		if !ok || mat.schemaVersion != claudeDecisionSchemaV1 {
			return false
		}
		if !bytesEqual(mat.bytes, claudeHookResponseBytes(decision)) {
			return false
		}
	}
	return true
}

func (s *AuthoritativeApprovalStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}
