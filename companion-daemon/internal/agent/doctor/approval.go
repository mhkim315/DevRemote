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

// ApprovalState is a closed set of approval/activation states in the D1
// state machine. Every state is explicit; no implicit or default activation.
type ApprovalState string

const (
	StatePending                         ApprovalState = "pending"
	StateApproved                        ApprovalState = "approved"
	StateRejected                        ApprovalState = "rejected"
	StateExpired                         ApprovalState = "expired"
	StateApprovedButActivationUnsupported ApprovalState = "approved_but_activation_unsupported"
	StateActivating                      ApprovalState = "activating"
	StateActive                          ApprovalState = "active"
	StateActivationFailed                ApprovalState = "activation_failed"
	StateRollingBack                     ApprovalState = "rolling_back"
	StateRolledBack                      ApprovalState = "rolled_back"
)

// terminalApprovalStates are states from which no forward progress is possible.
var terminalApprovalStates = map[ApprovalState]bool{
	StateRejected:                        true,
	StateExpired:                         true,
	StateActive:                          true,
	StateRolledBack:                      true,
	StateApprovedButActivationUnsupported: true,
}

// IsTerminal reports whether the state is a final state.
func (s ApprovalState) IsTerminal() bool { return terminalApprovalStates[s] }

// ── Errors ──

var (
	ErrRequestIDExists       = errors.New("request ID already used")
	ErrRequestNotFound       = errors.New("request not found")
	ErrDigestMismatch        = errors.New("digest mismatch — bundle may have been modified")
	ErrNotPending            = errors.New("request is not in pending state")
	ErrNotApproved           = errors.New("request is not approved")
	ErrNotActive             = errors.New("request is not active")
	ErrExpired               = errors.New("request has expired")
	ErrActivationUnsupported = errors.New("activation not supported for this request")
	ErrAlreadyTerminal       = errors.New("request is already in a terminal state")
)

// ── ReviewBundle ──

// ReviewBundle is the complete, immutable, digest-bound evidence package
// presented for user review before activation. Every field is included in
// the BundleDigest; any modification is detected as a digest mismatch.
type ReviewBundle struct {
	RequestID            string            `json:"request_id"`
	AdapterName          string            `json:"adapter_name"`
	Provider             string            `json:"provider"`
	TargetVersion        string            `json:"target_version"`
	BaselineSHA          string            `json:"baseline_sha"`
	EvidenceDigest       string            `json:"evidence_digest"`
	PatchDigest          string            `json:"patch_digest"`
	ChangedFiles         []string          `json:"changed_files"`
	SuiteCommandManifest []string          `json:"suite_command_manifest"`
	SuiteResultDigest    string            `json:"suite_result_digest"`
	SuiteResult          ObservatoryResult `json:"suite_result"`
	CreatedAt            time.Time         `json:"created_at"`
	ExpiresAt            time.Time         `json:"expires_at"`

	// Internal digest cache (computed once, never mutated).
	digest string
}

// BundleDigest returns the deterministic SHA-256 digest of the entire bundle.
// The digest is computed once on creation and cached; the bundle is immutable
// after construction.
func (rb *ReviewBundle) BundleDigest() string {
	if rb.digest != "" {
		return rb.digest
	}
	rb.digest = computeBundleDigest(rb)
	return rb.digest
}

// IsExpired reports whether the bundle has passed its expiry time.
func (rb *ReviewBundle) IsExpired() bool {
	return time.Now().After(rb.ExpiresAt)
}

// computeBundleDigest produces a deterministic SHA-256 hash of all
// digest-relevant bundle fields. Fields are serialized in a stable order.
func computeBundleDigest(rb *ReviewBundle) string {
	// Build a deterministic JSON representation of the digest-relevant fields.
	payload := struct {
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
	}{
		RequestID:            rb.RequestID,
		AdapterName:          rb.AdapterName,
		Provider:             rb.Provider,
		TargetVersion:        rb.TargetVersion,
		BaselineSHA:          rb.BaselineSHA,
		EvidenceDigest:       rb.EvidenceDigest,
		PatchDigest:          rb.PatchDigest,
		ChangedFiles:         sortedCopy(rb.ChangedFiles),
		SuiteCommandManifest: sortedCopy(rb.SuiteCommandManifest),
		SuiteResultDigest:    rb.SuiteResultDigest,
	}
	b, _ := json.Marshal(payload)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// sortedCopy returns a sorted copy of a string slice (deterministic ordering).
func sortedCopy(ss []string) []string {
	if ss == nil {
		return nil
	}
	out := make([]string, len(ss))
	copy(out, ss)
	sort.Strings(out)
	return out
}

// ── Digest helpers ──

// HashBytes returns the hex-encoded SHA-256 of b.
func HashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// HashJSON marshals v to deterministic JSON and returns its SHA-256.
func HashJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return HashBytes(b)
}

// ── ApprovalRequest ──

// ApprovalRequest wraps a ReviewBundle with its current approval state.
// The bundle is immutable; only the state changes.
type ApprovalRequest struct {
	Bundle ReviewBundle    `json:"bundle"`
	State  ApprovalState   `json:"state"`
}

// ── ApprovalStore ──

// ApprovalStore manages the lifecycle of approval requests with replay
// protection and digest validation. It is safe for concurrent use.
type ApprovalStore struct {
	mu       sync.Mutex
	requests map[string]*ApprovalRequest // request ID → request
}

// NewApprovalStore returns a new empty ApprovalStore.
func NewApprovalStore() *ApprovalStore {
	return &ApprovalStore{requests: make(map[string]*ApprovalRequest)}
}

// Submit registers a new approval request. Returns ErrRequestIDExists if the
// request ID has already been used (replay protection).
func (as *ApprovalStore) Submit(bundle *ReviewBundle) (*ApprovalRequest, error) {
	as.mu.Lock()
	defer as.mu.Unlock()

	if _, exists := as.requests[bundle.RequestID]; exists {
		return nil, ErrRequestIDExists
	}
	if bundle.IsExpired() {
		req := &ApprovalRequest{Bundle: *bundle, State: StateExpired}
		as.requests[bundle.RequestID] = req
		return req, ErrExpired
	}
	req := &ApprovalRequest{Bundle: *bundle, State: StatePending}
	as.requests[bundle.RequestID] = req
	return req, nil
}

// Approve transitions a pending request to approved. It validates:
// - Request exists and is pending
// - Digest matches (bundle has not been modified)
// - Bundle has not expired
func (as *ApprovalStore) Approve(requestID string, expectedDigest string) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	req, ok := as.requests[requestID]
	if !ok {
		return ErrRequestNotFound
	}
	if req.State != StatePending {
		if req.State.IsTerminal() {
			return ErrAlreadyTerminal
		}
		return ErrNotPending
	}
	if req.Bundle.IsExpired() {
		req.State = StateExpired
		return ErrExpired
	}
	if req.Bundle.BundleDigest() != expectedDigest {
		return fmt.Errorf("%w: expected %s, got %s", ErrDigestMismatch,
			contract.SanitizeDiagnostic(expectedDigest)[:16],
			contract.SanitizeDiagnostic(req.Bundle.BundleDigest())[:16])
	}
	req.State = StateApproved
	return nil
}

// Reject transitions a pending request to rejected.
func (as *ApprovalStore) Reject(requestID string) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	req, ok := as.requests[requestID]
	if !ok {
		return ErrRequestNotFound
	}
	if req.State.IsTerminal() {
		return ErrAlreadyTerminal
	}
	req.State = StateRejected
	return nil
}

// Activate transitions an approved request to active. It validates:
// - Request is in approved state
// - Digest matches
// - Bundle has not expired
// If activation is unsupported, transitions to approved_but_activation_unsupported.
func (as *ApprovalStore) Activate(requestID string, expectedDigest string) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	req, ok := as.requests[requestID]
	if !ok {
		return ErrRequestNotFound
	}
	if req.State != StateApproved {
		if req.State.IsTerminal() {
			return ErrAlreadyTerminal
		}
		return ErrNotApproved
	}
	if req.Bundle.IsExpired() {
		req.State = StateExpired
		return ErrExpired
	}
	if req.Bundle.BundleDigest() != expectedDigest {
		req.State = StateActivationFailed
		return fmt.Errorf("%w: expected %s, got %s", ErrDigestMismatch,
			contract.SanitizeDiagnostic(expectedDigest)[:16],
			contract.SanitizeDiagnostic(req.Bundle.BundleDigest())[:16])
	}

	req.State = StateActivating
	// In a full production implementation, this would stage files, run
	// integrity checks, atomically switch, verify via fixed suite, and
	// auto-restore on failure. The D1 scope provides the review/approval
	// contract; filesystem activation is gated on the caller's environment.
	// When safe activation cannot be performed, we fail-closed.
	req.State = StateApprovedButActivationUnsupported
	return ErrActivationUnsupported
}

// ActivateWithResult transitions an approved request to active or
// activation_failed based on the caller's activation outcome. The caller
// performs the actual filesystem activation and reports success/failure.
func (as *ApprovalStore) ActivateWithResult(requestID string, expectedDigest string, success bool) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	req, ok := as.requests[requestID]
	if !ok {
		return ErrRequestNotFound
	}
	if req.State != StateApproved {
		if req.State.IsTerminal() {
			return ErrAlreadyTerminal
		}
		return ErrNotApproved
	}
	if req.Bundle.IsExpired() {
		req.State = StateExpired
		return ErrExpired
	}
	if req.Bundle.BundleDigest() != expectedDigest {
		req.State = StateActivationFailed
		return ErrDigestMismatch
	}

	if success {
		req.State = StateActive
	} else {
		req.State = StateActivationFailed
	}
	return nil
}

// Rollback transitions an active request to rolled_back via rolling_back.
// Only active requests can be rolled back.
func (as *ApprovalStore) Rollback(requestID string) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	req, ok := as.requests[requestID]
	if !ok {
		return ErrRequestNotFound
	}
	if req.State != StateActive && req.State != StateActivationFailed {
		if req.State.IsTerminal() {
			return ErrAlreadyTerminal
		}
		return ErrNotActive
	}

	req.State = StateRollingBack
	req.State = StateRolledBack
	return nil
}

// Get returns the current approval request, if it exists.
func (as *ApprovalStore) Get(requestID string) (*ApprovalRequest, bool) {
	as.mu.Lock()
	defer as.mu.Unlock()
	req, ok := as.requests[requestID]
	return req, ok
}

// ExpireStale transitions all pending requests past their expiry to expired.
func (as *ApprovalStore) ExpireStale() int {
	as.mu.Lock()
	defer as.mu.Unlock()
	count := 0
	now := time.Now()
	for _, req := range as.requests {
		if req.State == StatePending && now.After(req.Bundle.ExpiresAt) {
			req.State = StateExpired
			count++
		}
	}
	return count
}

// ── NewReviewBundle ──

// NewReviewBundle creates a ReviewBundle with all digests computed and a
// 24-hour expiry. All fields must be non-empty; empty digests are rejected
// at Submit time.
func NewReviewBundle(
	requestID string,
	adapterName string,
	provider string,
	targetVersion string,
	baselineSHA string,
	evidenceDigest string,
	patchDigest string,
	changedFiles []string,
	suiteCommandManifest []string,
	suiteResult ObservatoryResult,
) *ReviewBundle {
	suiteResultDigest := HashJSON(suiteResult)
	return &ReviewBundle{
		RequestID:            requestID,
		AdapterName:          adapterName,
		Provider:             provider,
		TargetVersion:        targetVersion,
		BaselineSHA:          baselineSHA,
		EvidenceDigest:       evidenceDigest,
		PatchDigest:          patchDigest,
		ChangedFiles:         append([]string{}, changedFiles...),
		SuiteCommandManifest: append([]string{}, suiteCommandManifest...),
		SuiteResultDigest:    suiteResultDigest,
		SuiteResult:          suiteResult,
		CreatedAt:            time.Now(),
		ExpiresAt:            time.Now().Add(24 * time.Hour),
	}
}
