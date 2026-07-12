package doctor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent/contract"
)

// ── Approval states ──

type ApprovalState string

const (
	StatePending                          ApprovalState = "pending"
	StateApproved                         ApprovalState = "approved"
	StateRejected                         ApprovalState = "rejected"
	StateExpired                          ApprovalState = "expired"
	StateApprovedButActivationUnsupported ApprovalState = "approved_but_activation_unsupported"
)

// ── Errors ──

var (
	ErrRequestIDExists       = errors.New("request ID already used")
	ErrRequestNotFound       = errors.New("request not found")
	ErrDigestMismatch        = errors.New("digest mismatch — bundle may have been modified")
	ErrNotPending            = errors.New("request is not in pending state")
	ErrExpired               = errors.New("request has expired")
	ErrActivationUnsupported = errors.New("safe activation not implemented — fail-closed")
	ErrEmptyField            = errors.New("required field is empty")
)

// ── ReviewBundle (immutable, fields unexported) ──

type ReviewBundle struct {
	requestID               string
	adapterName             string
	provider                string
	targetVersion           string
	baselineSHA             string
	rollbackSHA             string // previous accepted adapter version SHA
	evidenceDigest          string
	evidenceProvenance      string // provenance tier of collected evidence
	patchDigest             string
	unifiedDiff             []byte          // exact unified diff bytes, immutable
	changedFiles            []string        // allowed-file manifest
	adapterSourceManifest   []string        // runner-provided adapter source files
	pokitTestManifest       []string        // Pokit-owned conformance test files
	providerFixtureManifest []string        // runner-provided provider fixtures
	fixtureRedaction        RedactionResult // typed redaction scan result
	suiteCommands           []FixedCommand  // Pokit-owned authoritative command specs
	suiteCommandsDigest     string          // sha256 of canonical command specs
	suiteResultDigest       string
	suiteResult             ObservatoryResult
	driftEvidence           DriftEvidence // bounded drift evidence
	workspaceDigest         string        // digest of workspace after patch
	remainingUnknowns       []string      // bounded, redacted remaining unknowns
	createdAt               time.Time
	expiresAt               time.Time
}

// Accessors return deep copies of slice fields only.
func (rb *ReviewBundle) RequestID() string          { return rb.requestID }
func (rb *ReviewBundle) AdapterName() string        { return rb.adapterName }
func (rb *ReviewBundle) Provider() string           { return rb.provider }
func (rb *ReviewBundle) TargetVersion() string      { return rb.targetVersion }
func (rb *ReviewBundle) BaselineSHA() string        { return rb.baselineSHA }
func (rb *ReviewBundle) RollbackSHA() string        { return rb.rollbackSHA }
func (rb *ReviewBundle) EvidenceDigest() string     { return rb.evidenceDigest }
func (rb *ReviewBundle) EvidenceProvenance() string { return rb.evidenceProvenance }
func (rb *ReviewBundle) PatchDigest() string        { return rb.patchDigest }
func (rb *ReviewBundle) UnifiedDiff() []byte {
	d := make([]byte, len(rb.unifiedDiff))
	copy(d, rb.unifiedDiff)
	return d
}
func (rb *ReviewBundle) ChangedFiles() []string { return append([]string{}, rb.changedFiles...) }
func (rb *ReviewBundle) AdapterSourceManifest() []string {
	return append([]string{}, rb.adapterSourceManifest...)
}
func (rb *ReviewBundle) PokitTestManifest() []string {
	return append([]string{}, rb.pokitTestManifest...)
}
func (rb *ReviewBundle) ProviderFixtureManifest() []string {
	return append([]string{}, rb.providerFixtureManifest...)
}
func (rb *ReviewBundle) FixtureRedaction() RedactionResult { return rb.fixtureRedaction }
func (rb *ReviewBundle) SuiteCommands() []FixedCommand {
	cp := make([]FixedCommand, len(rb.suiteCommands))
	copy(cp, rb.suiteCommands)
	return cp
}

func (rb *ReviewBundle) SuiteCommandLabels() []string {
	labels := make([]string, len(rb.suiteCommands))
	for i, fc := range rb.suiteCommands {
		labels[i] = fc.Label
	}
	return labels
}
func (rb *ReviewBundle) SuiteResultDigest() string      { return rb.suiteResultDigest }
func (rb *ReviewBundle) SuiteResult() ObservatoryResult { return rb.suiteResult.deepCopy() }
func (rb *ReviewBundle) DriftEvidence() DriftEvidence   { return rb.driftEvidence }
func (rb *ReviewBundle) WorkspaceDigest() string        { return rb.workspaceDigest }
func (rb *ReviewBundle) RemainingUnknowns() []string {
	return append([]string{}, rb.remainingUnknowns...)
}
func (rb *ReviewBundle) CreatedAt() time.Time { return rb.createdAt }
func (rb *ReviewBundle) ExpiresAt() time.Time { return rb.expiresAt }
func (rb *ReviewBundle) IsExpired() bool      { return time.Now().After(rb.expiresAt) }

// BundleDigest computes the deterministic SHA-256 of ALL bundle fields,
// including timestamps. It recomputes on every call — no caching.
func (rb *ReviewBundle) BundleDigest() string {
	payload := bundleDigestPayload{
		RequestID:               rb.requestID,
		AdapterName:             rb.adapterName,
		Provider:                rb.provider,
		TargetVersion:           rb.targetVersion,
		BaselineSHA:             rb.baselineSHA,
		RollbackSHA:             rb.rollbackSHA,
		EvidenceDigest:          rb.evidenceDigest,
		EvidenceProvenance:      rb.evidenceProvenance,
		PatchDigest:             rb.patchDigest,
		UnifiedDiffDigest:       HashBytes(rb.unifiedDiff),
		ChangedFiles:            sortedStrings(rb.changedFiles),
		AdapterSourceManifest:   sortedStrings(rb.adapterSourceManifest),
		PokitTestManifest:       sortedStrings(rb.pokitTestManifest),
		ProviderFixtureManifest: sortedStrings(rb.providerFixtureManifest),
		FixtureRedaction:        HashJSON(rb.fixtureRedaction),
		SuiteCommandsDigest:     rb.suiteCommandsDigest,
		SuiteResultDigest:       rb.suiteResultDigest,
		DriftEvidenceDigest:     HashJSON(rb.driftEvidence),
		WorkspaceDigest:         rb.workspaceDigest,
		RemainingUnknowns:       sortedStrings(rb.remainingUnknowns),
		CreatedAt:               rb.createdAt.Format(time.RFC3339Nano),
		ExpiresAt:               rb.expiresAt.Format(time.RFC3339Nano),
	}
	b, _ := json.Marshal(payload)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type bundleDigestPayload struct {
	RequestID               string   `json:"request_id"`
	AdapterName             string   `json:"adapter_name"`
	Provider                string   `json:"provider"`
	TargetVersion           string   `json:"target_version"`
	BaselineSHA             string   `json:"baseline_sha"`
	RollbackSHA             string   `json:"rollback_sha"`
	EvidenceDigest          string   `json:"evidence_digest"`
	EvidenceProvenance      string   `json:"evidence_provenance"`
	PatchDigest             string   `json:"patch_digest"`
	UnifiedDiffDigest       string   `json:"unified_diff_digest"`
	ChangedFiles            []string `json:"changed_files"`
	AdapterSourceManifest   []string `json:"adapter_source_manifest"`
	PokitTestManifest       []string `json:"pokit_test_manifest"`
	ProviderFixtureManifest []string `json:"provider_fixture_manifest"`
	FixtureRedaction        string   `json:"fixture_redaction"`
	SuiteCommandsDigest     string   `json:"suite_commands_digest"`
	SuiteResultDigest       string   `json:"suite_result_digest"`
	DriftEvidenceDigest     string   `json:"drift_evidence_digest"`
	WorkspaceDigest         string   `json:"workspace_digest"`
	RemainingUnknowns       []string `json:"remaining_unknowns"`
	CreatedAt               string   `json:"created_at"`
	ExpiresAt               string   `json:"expires_at"`
}

// NewReviewBundle creates a ReviewBundle. Returns error if any required
// field (digest, baseline, changed files, manifests) is empty.
func NewReviewBundle(
	requestID, adapterName, provider, targetVersion, baselineSHA string,
	evidenceDigest, patchDigest string,
	unifiedDiff []byte,
	changedFiles, adapterSourceManifest, pokitTestManifest, providerFixtureManifest []string,
	fixtureRedaction RedactionResult,
	candidatePkg string,
	suiteResult ObservatoryResult,
	driftEvidence DriftEvidence,
	evidenceProvenance, workspaceDigest string,
	remainingUnknowns []string,
) (*ReviewBundle, error) {
	if requestID == "" || baselineSHA == "" || evidenceDigest == "" || patchDigest == "" {
		return nil, fmt.Errorf("%w: requestID, baselineSHA, evidenceDigest, patchDigest required", ErrEmptyField)
	}
	if len(changedFiles) == 0 {
		return nil, fmt.Errorf("%w: changedFiles must be non-empty", ErrEmptyField)
	}
	// Derive authoritative command specs from Pokit-owned FixedSuite.
	// Caller cannot inject arbitrary FixedCommands — only the canonical
	// suite specification is used as authority.
	authoritativeCmds := NewFixedSuite().Commands(candidatePkg)
	if err := ValidateSuiteEvidence(authoritativeCmds, suiteResult.CommandResults,
		suiteResult.TotalTests, suiteResult.Passed, suiteResult.Failed); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmptyField, err)
	}
	if fixtureRedaction.Scanned == 0 {
		return nil, fmt.Errorf("%w: fixture redaction has zero scanned files", ErrEmptyField)
	}
	if !fixtureRedaction.Clean {
		return nil, fmt.Errorf("%w: fixture redaction is not clean", ErrEmptyField)
	}
	if fixtureRedaction.Clean && len(fixtureRedaction.Findings) > 0 {
		return nil, fmt.Errorf("%w: clean redaction must have empty findings", ErrEmptyField)
	}
	if len(adapterSourceManifest) == 0 && len(providerFixtureManifest) == 0 {
		return nil, fmt.Errorf("%w: at least one of adapter or fixture manifest must be non-empty", ErrEmptyField)
	}
	if workspaceDigest == "" {
		return nil, fmt.Errorf("%w: workspace digest must be non-empty", ErrEmptyField)
	}
	cf := make([]string, len(changedFiles))
	copy(cf, changedFiles)
	am := make([]string, len(adapterSourceManifest))
	copy(am, adapterSourceManifest)
	pm := make([]string, len(pokitTestManifest))
	copy(pm, pokitTestManifest)
	pfm := make([]string, len(providerFixtureManifest))
	copy(pfm, providerFixtureManifest)
	ru := make([]string, len(remainingUnknowns))
	copy(ru, remainingUnknowns)
	ud := make([]byte, len(unifiedDiff))
	copy(ud, unifiedDiff)
	return &ReviewBundle{
		requestID:               requestID,
		adapterName:             adapterName,
		provider:                provider,
		targetVersion:           targetVersion,
		baselineSHA:             baselineSHA,
		rollbackSHA:             baselineSHA,
		evidenceDigest:          evidenceDigest,
		evidenceProvenance:      evidenceProvenance,
		patchDigest:             patchDigest,
		unifiedDiff:             ud,
		changedFiles:            cf,
		adapterSourceManifest:   am,
		pokitTestManifest:       pm,
		providerFixtureManifest: pfm,
		fixtureRedaction:        fixtureRedaction,
		suiteCommands:           copyFixedCommands(authoritativeCmds),
		suiteCommandsDigest:     HashJSON(authoritativeCmds),
		suiteResultDigest:       HashJSON(suiteResult),
		suiteResult:             suiteResult.deepCopy(),
		driftEvidence:           driftEvidence,
		workspaceDigest:         workspaceDigest,
		remainingUnknowns:       ru,
		createdAt:               time.Now(),
		expiresAt:               time.Now().Add(24 * time.Hour),
	}, nil
}

// ── Deep copy ──

func (rb *ReviewBundle) deepCopy() *ReviewBundle {
	ud := make([]byte, len(rb.unifiedDiff))
	copy(ud, rb.unifiedDiff)
	return &ReviewBundle{
		requestID:               rb.requestID,
		adapterName:             rb.adapterName,
		provider:                rb.provider,
		targetVersion:           rb.targetVersion,
		baselineSHA:             rb.baselineSHA,
		rollbackSHA:             rb.rollbackSHA,
		evidenceDigest:          rb.evidenceDigest,
		evidenceProvenance:      rb.evidenceProvenance,
		patchDigest:             rb.patchDigest,
		unifiedDiff:             ud,
		changedFiles:            append([]string{}, rb.changedFiles...),
		adapterSourceManifest:   append([]string{}, rb.adapterSourceManifest...),
		pokitTestManifest:       append([]string{}, rb.pokitTestManifest...),
		providerFixtureManifest: append([]string{}, rb.providerFixtureManifest...),
		fixtureRedaction:        copyRedactionResult(rb.fixtureRedaction),
		suiteCommands:           copyFixedCommands(rb.suiteCommands),
		suiteCommandsDigest:     rb.suiteCommandsDigest,
		suiteResultDigest:       rb.suiteResultDigest,
		suiteResult:             rb.suiteResult.deepCopy(),
		driftEvidence:           rb.driftEvidence,
		workspaceDigest:         rb.workspaceDigest,
		remainingUnknowns:       append([]string{}, rb.remainingUnknowns...),
		createdAt:               rb.createdAt,
		expiresAt:               rb.expiresAt,
	}
}

// ── ApprovalRequest ──

type ApprovalRequest struct {
	bundle ReviewBundle
	state  ApprovalState
}

func (ar *ApprovalRequest) Bundle() ReviewBundle { return *ar.bundle.deepCopy() }
func (ar *ApprovalRequest) State() ApprovalState { return ar.state }

// ── ApprovalStore ──

type ApprovalStore struct {
	mu       sync.Mutex
	requests map[string]*ApprovalRequest
}

func NewApprovalStore() *ApprovalStore {
	return &ApprovalStore{requests: make(map[string]*ApprovalRequest)}
}

func (as *ApprovalStore) Submit(bundle *ReviewBundle) (*ApprovalRequest, error) {
	as.mu.Lock()
	defer as.mu.Unlock()

	if _, exists := as.requests[bundle.requestID]; exists {
		return nil, ErrRequestIDExists
	}

	// Defensive revalidation — same invariants as NewReviewBundle.
	if bundle.baselineSHA == "" || bundle.evidenceDigest == "" || bundle.patchDigest == "" {
		return nil, fmt.Errorf("%w: baseline/digest must be non-empty", ErrEmptyField)
	}
	if len(bundle.changedFiles) == 0 {
		return nil, fmt.Errorf("%w: changed files must be non-empty", ErrEmptyField)
	}
	sr := bundle.suiteResult
	// Verify suite commands digest matches (anti-forgery).
	if HashJSON(bundle.suiteCommands) != bundle.suiteCommandsDigest {
		return nil, fmt.Errorf("%w: suite commands digest mismatch", ErrEmptyField)
	}
	// Re-validate against the authoritative commands stored in the bundle
	// (derived internally by NewReviewBundle, not caller-provided).
	if err := ValidateSuiteEvidence(bundle.suiteCommands, sr.CommandResults,
		sr.TotalTests, sr.Passed, sr.Failed); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmptyField, err)
	}
	if !bundle.fixtureRedaction.Clean || bundle.fixtureRedaction.Scanned == 0 {
		return nil, fmt.Errorf("%w: redaction not clean or zero-scanned", ErrEmptyField)
	}
	if bundle.workspaceDigest == "" {
		return nil, fmt.Errorf("%w: workspace digest empty", ErrEmptyField)
	}

	state := StatePending
	if bundle.IsExpired() {
		state = StateExpired
	}

	req := &ApprovalRequest{bundle: *bundle.deepCopy(), state: state}
	as.requests[bundle.requestID] = req

	if state == StateExpired {
		return req, ErrExpired
	}
	return req, nil
}

// Approve transitions pending→approved. The user MUST supply the exact
// digest they reviewed — the store validates it against the current
// bundle digest (recomputed, not cached).
func (as *ApprovalStore) Approve(requestID string, userSuppliedDigest string) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	req, ok := as.requests[requestID]
	if !ok {
		return ErrRequestNotFound
	}
	if req.state != StatePending {
		return fmt.Errorf("%w: current state is %s", ErrNotPending, req.state)
	}
	if req.bundle.IsExpired() {
		req.state = StateExpired
		return ErrExpired
	}

	// Recompute digest — always, no cache.
	currentDigest := req.bundle.BundleDigest()
	if currentDigest != userSuppliedDigest {
		userShort := contract.SanitizeDiagnostic(userSuppliedDigest)
		digestShort := contract.SanitizeDiagnostic(currentDigest)
		if len(userShort) > 16 {
			userShort = userShort[:16]
		}
		if len(digestShort) > 16 {
			digestShort = digestShort[:16]
		}
		return fmt.Errorf("%w: user supplied %s, bundle digest is %s",
			ErrDigestMismatch, userShort, digestShort)
	}

	req.state = StateApproved
	return nil
}

func (as *ApprovalStore) Reject(requestID string) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	req, ok := as.requests[requestID]
	if !ok {
		return ErrRequestNotFound
	}
	if req.state == StateRejected || req.state == StateExpired || req.state == StateApprovedButActivationUnsupported {
		return fmt.Errorf("request already in terminal state: %s", req.state)
	}
	req.state = StateRejected
	return nil
}

// Activate always returns ErrActivationUnsupported. Safe filesystem
// activation is not implemented — this is fail-closed by design.
// The request transitions to approved_but_activation_unsupported.
func (as *ApprovalStore) Activate(requestID string, userSuppliedDigest string) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	req, ok := as.requests[requestID]
	if !ok {
		return ErrRequestNotFound
	}
	if req.state != StateApproved {
		return fmt.Errorf("request must be approved, current state: %s", req.state)
	}
	if req.bundle.IsExpired() {
		req.state = StateExpired
		return ErrExpired
	}

	currentDigest := req.bundle.BundleDigest()
	if currentDigest != userSuppliedDigest {
		req.state = StateRejected
		return ErrDigestMismatch
	}

	req.state = StateApprovedButActivationUnsupported
	return ErrActivationUnsupported
}

// Get returns a deep copy of the approval request. The caller cannot
// mutate internal state through the returned value.
func (as *ApprovalStore) Get(requestID string) (*ApprovalRequest, bool) {
	as.mu.Lock()
	defer as.mu.Unlock()

	req, ok := as.requests[requestID]
	if !ok {
		return nil, false
	}
	cp := &ApprovalRequest{
		bundle: *req.bundle.deepCopy(),
		state:  req.state,
	}
	return cp, true
}

// ── Helpers ──

func HashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func HashJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "error:" + contract.SanitizeDiagnostic(err.Error())
	}
	return HashBytes(b)
}

func copyFixedCommands(fcs []FixedCommand) []FixedCommand {
	if fcs == nil {
		return nil
	}
	cp := make([]FixedCommand, len(fcs))
	copy(cp, fcs)
	return cp
}

func copyRedactionResult(r RedactionResult) RedactionResult {
	cp := r
	if r.Findings != nil {
		cp.Findings = make([]string, len(r.Findings))
		copy(cp.Findings, r.Findings)
	}
	return cp
}

func sortedStrings(ss []string) []string {
	if ss == nil {
		return nil
	}
	out := make([]string, len(ss))
	copy(out, ss)
	sort.Strings(out)
	return out
}
