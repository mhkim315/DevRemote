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
	StatePending                         ApprovalState = "pending"
	StateApproved                        ApprovalState = "approved"
	StateRejected                        ApprovalState = "rejected"
	StateExpired                         ApprovalState = "expired"
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
	requestID            string
	adapterName          string
	provider             string
	targetVersion        string
	baselineSHA          string
	evidenceDigest       string
	patchDigest          string
	changedFiles         []string
	suiteCommandManifest []string
	suiteResultDigest    string
	suiteResult          ObservatoryResult
	createdAt            time.Time
	expiresAt            time.Time
}

// Accessors return deep copies of slice fields only.
func (rb *ReviewBundle) RequestID() string             { return rb.requestID }
func (rb *ReviewBundle) AdapterName() string           { return rb.adapterName }
func (rb *ReviewBundle) Provider() string              { return rb.provider }
func (rb *ReviewBundle) TargetVersion() string         { return rb.targetVersion }
func (rb *ReviewBundle) BaselineSHA() string           { return rb.baselineSHA }
func (rb *ReviewBundle) EvidenceDigest() string        { return rb.evidenceDigest }
func (rb *ReviewBundle) PatchDigest() string           { return rb.patchDigest }
func (rb *ReviewBundle) ChangedFiles() []string        { return append([]string{}, rb.changedFiles...) }
func (rb *ReviewBundle) SuiteCommandManifest() []string { return append([]string{}, rb.suiteCommandManifest...) }
func (rb *ReviewBundle) SuiteResultDigest() string     { return rb.suiteResultDigest }
func (rb *ReviewBundle) SuiteResult() ObservatoryResult { return rb.suiteResult }
func (rb *ReviewBundle) CreatedAt() time.Time          { return rb.createdAt }
func (rb *ReviewBundle) ExpiresAt() time.Time          { return rb.expiresAt }
func (rb *ReviewBundle) IsExpired() bool               { return time.Now().After(rb.expiresAt) }

// BundleDigest computes the deterministic SHA-256 of ALL bundle fields,
// including timestamps. It recomputes on every call — no caching.
func (rb *ReviewBundle) BundleDigest() string {
	payload := bundleDigestPayload{
		RequestID:            rb.requestID,
		AdapterName:          rb.adapterName,
		Provider:             rb.provider,
		TargetVersion:        rb.targetVersion,
		BaselineSHA:          rb.baselineSHA,
		EvidenceDigest:       rb.evidenceDigest,
		PatchDigest:          rb.patchDigest,
		ChangedFiles:         sortedStrings(rb.changedFiles),
		SuiteCommandManifest: sortedStrings(rb.suiteCommandManifest),
		SuiteResultDigest:    rb.suiteResultDigest,
		CreatedAt:            rb.createdAt.Format(time.RFC3339Nano),
		ExpiresAt:            rb.expiresAt.Format(time.RFC3339Nano),
	}
	b, _ := json.Marshal(payload)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type bundleDigestPayload struct {
	RequestID            string   `json:"request_id"`
	AdapterName          string   `json:"adapter_name"`
	Provider             string   `json:"provider"`
	TargetVersion        string   `json:"target_version"`
	BaselineSHA          string   `json:"baseline_sha"`
	EvidenceDigest       string   `json:"evidence_digest"`
	PatchDigest          string   `json:"patch_digest"`
	ChangedFiles         []string `json:"changed_files"`
	SuiteCommandManifest []string `json:"suite_command_manifest"`
	SuiteResultDigest    string   `json:"suite_result_digest"`
	CreatedAt            string   `json:"created_at"`
	ExpiresAt            string   `json:"expires_at"`
}

// NewReviewBundle creates a ReviewBundle. Returns error if any required
// field (digest, baseline, changed files) is empty.
func NewReviewBundle(
	requestID, adapterName, provider, targetVersion, baselineSHA string,
	evidenceDigest, patchDigest string,
	changedFiles []string,
	suiteCommandManifest []string,
	suiteResult ObservatoryResult,
) (*ReviewBundle, error) {
	if requestID == "" || baselineSHA == "" || evidenceDigest == "" || patchDigest == "" {
		return nil, fmt.Errorf("%w: requestID, baselineSHA, evidenceDigest, patchDigest required", ErrEmptyField)
	}
	if len(changedFiles) == 0 {
		return nil, fmt.Errorf("%w: changedFiles must be non-empty", ErrEmptyField)
	}
	if suiteResult.TotalTests == 0 {
		return nil, fmt.Errorf("%w: suite result has zero tests", ErrEmptyField)
	}
	cf := make([]string, len(changedFiles))
	copy(cf, changedFiles)
	scm := make([]string, len(suiteCommandManifest))
	copy(scm, suiteCommandManifest)
	return &ReviewBundle{
		requestID:            requestID,
		adapterName:          adapterName,
		provider:             provider,
		targetVersion:        targetVersion,
		baselineSHA:          baselineSHA,
		evidenceDigest:       evidenceDigest,
		patchDigest:          patchDigest,
		changedFiles:         cf,
		suiteCommandManifest: scm,
		suiteResultDigest:    HashJSON(suiteResult),
		suiteResult:          suiteResult,
		createdAt:            time.Now(),
		expiresAt:            time.Now().Add(24 * time.Hour),
	}, nil
}

// ── Deep copy ──

func (rb *ReviewBundle) deepCopy() *ReviewBundle {
	return &ReviewBundle{
		requestID:            rb.requestID,
		adapterName:          rb.adapterName,
		provider:             rb.provider,
		targetVersion:        rb.targetVersion,
		baselineSHA:          rb.baselineSHA,
		evidenceDigest:       rb.evidenceDigest,
		patchDigest:          rb.patchDigest,
		changedFiles:         append([]string{}, rb.changedFiles...),
		suiteCommandManifest: append([]string{}, rb.suiteCommandManifest...),
		suiteResultDigest:    rb.suiteResultDigest,
		suiteResult:          rb.suiteResult,
		createdAt:            rb.createdAt,
		expiresAt:            rb.expiresAt,
	}
}

// ── ApprovalRequest ──

type ApprovalRequest struct {
	bundle ReviewBundle
	state  ApprovalState
}

func (ar *ApprovalRequest) Bundle() ReviewBundle { return ar.bundle } // value copy
func (ar *ApprovalRequest) State() ApprovalState  { return ar.state }

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

	// Validate non-empty fields.
	if bundle.baselineSHA == "" || bundle.evidenceDigest == "" || bundle.patchDigest == "" {
		return nil, fmt.Errorf("%w: baseline/digest must be non-empty", ErrEmptyField)
	}
	if len(bundle.changedFiles) == 0 {
		return nil, fmt.Errorf("%w: changed files must be non-empty", ErrEmptyField)
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
		if len(userShort) > 16 { userShort = userShort[:16] }
		if len(digestShort) > 16 { digestShort = digestShort[:16] }
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

func sortedStrings(ss []string) []string {
	if ss == nil {
		return nil
	}
	out := make([]string, len(ss))
	copy(out, ss)
	sort.Strings(out)
	return out
}
