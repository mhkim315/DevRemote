// Package term — C2D-B: private one-shot resume coordinator for Claude
// PreToolUse decision delivery.
//
// C1D drops four critical identity fields (Claude session_id, tool_use_id,
// tool_name, input_digest) after joinDeferred matches a pending observation.
// C2D-B preserves them in a private identity record so C2D-C can later
// deliver a decision through an exact Claude resume.
//
// The coordinator is owned by ManagedClaudeService so identities and entries
// survive individual Claude process exit. All state transitions happen under
// a single mutex. No lock is held across process spawn, hook IPC, or
// provider I/O.
//
// This is controlled-composition work: the coordinator exists in production
// but no delivery path is activated. C2D-C wires it into ApprovalDelivery.
package term

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"sync"
	"time"
)

const (
	maxCoordinatorIdentities = 16
	maxCoordinatorEntries    = 16
	coordinatorEntryTimeout  = 120 * time.Second
)

// ── Private identity record ──

// claudePrivateIdentity preserves the C0D-certified four-field binding that
// C1D's joinDeferred currently drops. Created before Store admission and
// rolled back on failure. Keyed by ApprovalID.
type claudePrivateIdentity struct {
	sessionID   string
	toolUseID   string
	toolName    string
	inputDigest string
}

// ── Resume state machine ──

// resumeState is the closed state vocabulary for a coordinator entry.
type resumeState int

const (
	stateDecisionReserved resumeState = iota
	stateTerminal
)

// resumeOutcome is the closed outcome vocabulary for ValidateAndWrite.
type resumeOutcome int

const (
	outcomeWritten    resumeOutcome = iota + 1
	outcomeCancelled
	outcomeTimeout
	outcomeMismatch
	outcomeDuplicate
	outcomeStale
)

// resumeEntry is one reserved claim-to-delivery lifecycle. It is created by
// ReserveEntry when an authenticated mobile decision arrives, and consumed
// by ValidateAndWrite when the resume hook delivers the decision.
type resumeEntry struct {
	claimToken  string
	resumeNonce string
	approvalID  string
	sessionID   string // Claude session_id (from identity)
	toolUseID   string
	toolName    string
	inputDigest string
	decision    string // "allow" or "deny" (provider-native)
	state       resumeState
	createdAt   time.Time
	ch          chan resumeOutcome // buffered 1
}

// ── Coordinator ──

// claudeResumeCoordinator is the C2D-B private one-shot resume coordinator.
// It owns two bounded maps: identities (approvalID → claudePrivateIdentity)
// and entries (claimToken → resumeEntry). All state transitions happen under
// a single mutex. The coordinator is owned by ManagedClaudeService so
// identities survive individual Claude process exit.
type claudeResumeCoordinator struct {
	mu         sync.Mutex
	identities map[string]*claudePrivateIdentity
	entries    map[string]*resumeEntry
	closed     bool
}

// NewClaudeResumeCoordinator creates an empty coordinator.
func NewClaudeResumeCoordinator() *claudeResumeCoordinator {
	return &claudeResumeCoordinator{
		identities: make(map[string]*claudePrivateIdentity),
		entries:    make(map[string]*resumeEntry),
	}
}

// ── Identity lifecycle ──

// ReserveIdentity creates a private identity record under the coordinator
// mutex. Returns false if the approvalID already exists, the coordinator
// is closed, or identity capacity is exhausted.
func (c *claudeResumeCoordinator) ReserveIdentity(approvalID, sessionID, toolUseID, toolName, inputDigest string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return false
	}
	if _, dup := c.identities[approvalID]; dup {
		return false
	}
	if len(c.identities) >= maxCoordinatorIdentities {
		return false
	}

	c.identities[approvalID] = &claudePrivateIdentity{
		sessionID:   sessionID,
		toolUseID:   toolUseID,
		toolName:    toolName,
		inputDigest: inputDigest,
	}
	return true
}

// RemoveIdentity removes a private identity record. It is idempotent
// (no-op for an unknown approvalID). Used for rollback on Store admission
// failure and for expiry cleanup.
func (c *claudeResumeCoordinator) RemoveIdentity(approvalID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.identities, approvalID)
}

// LookupIdentity returns a copy of the identity record, or false if not found.
func (c *claudeResumeCoordinator) LookupIdentity(approvalID string) (*claudePrivateIdentity, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id, ok := c.identities[approvalID]
	if !ok {
		return nil, false
	}
	return &claudePrivateIdentity{
		sessionID:   id.sessionID,
		toolUseID:   id.toolUseID,
		toolName:    id.toolName,
		inputDigest: id.inputDigest,
	}, true
}

// ── Entry lifecycle ──

// generateNonce creates an opaque one-shot resume nonce, or returns an
// empty string on entropy failure.
func generateNonce() string {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// ReserveEntry creates a coordinator entry from a stored identity.
// Returns the entry and true on success, or nil and false if the identity
// is missing, the claim token is already reserved, the coordinator is
// closed, or entry capacity is exhausted.
//
// The returned entry carries an opaque resumeNonce that the caller must
// embed in the resume hook script. The nonce is never exposed to the
// mobile client.
func (c *claudeResumeCoordinator) ReserveEntry(claimToken, approvalID, decision string) (*resumeEntry, bool) {
	nonce := generateNonce()
	if nonce == "" {
		return nil, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, false
	}
	id, ok := c.identities[approvalID]
	if !ok {
		return nil, false
	}
	if _, dup := c.entries[claimToken]; dup {
		return nil, false
	}
	if len(c.entries) >= maxCoordinatorEntries {
		return nil, false
	}

	entry := &resumeEntry{
		claimToken:  claimToken,
		resumeNonce: nonce,
		approvalID:  approvalID,
		sessionID:   id.sessionID,
		toolUseID:   id.toolUseID,
		toolName:    id.toolName,
		inputDigest: id.inputDigest,
		decision:    decision,
		state:       stateDecisionReserved,
		createdAt:   clockNow(),
		ch:          make(chan resumeOutcome, 1),
	}
	c.entries[claimToken] = entry
	return entry, true
}

// ValidateAndWrite is the core hook validation. It receives the repeated
// PreToolUse fields, validates them against the stored identity, and writes
// the decision exactly once. It is called from the resume hook handler.
//
// Returns:
//   - ("<decision>", outcomeWritten) — success, exactly one write
//   - ("", outcomeMismatch) — any field doesn't match
//   - ("", outcomeDuplicate) — decision was already written
//   - ("", outcomeCancelled) — entry was cancelled
//   - ("", outcomeStale) — entry doesn't exist or coordinator closed
func (c *claudeResumeCoordinator) ValidateAndWrite(claimToken, resumeNonce, sessionID, toolUseID, toolName, inputDigest string) (string, resumeOutcome) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return "", outcomeStale
	}
	entry, ok := c.entries[claimToken]
	if !ok {
		return "", outcomeStale
	}
	if entry.resumeNonce != resumeNonce {
		return "", outcomeMismatch
	}
	if entry.state != stateDecisionReserved {
		// Already written or already terminal.
		return "", outcomeDuplicate
	}
	if entry.sessionID != sessionID || entry.toolUseID != toolUseID ||
		entry.toolName != toolName || entry.inputDigest != inputDigest {
		return "", outcomeMismatch
	}

	entry.state = stateTerminal
	entry.ch <- outcomeWritten
	return entry.decision, outcomeWritten
}

// CancelEntry cancels an active entry. If the entry is still in the
// reserved state, it transitions to terminal and signals the waiter.
// If already written or terminal, it is a no-op (idempotent).
func (c *claudeResumeCoordinator) CancelEntry(claimToken string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[claimToken]
	if !ok {
		return
	}
	if entry.state != stateDecisionReserved {
		return // already terminal
	}
	entry.state = stateTerminal
	entry.ch <- outcomeCancelled
}

// ClearForApproval removes the identity record and cancels any active entry
// for the given approvalID. Used on terminate, stop, delete, and epoch
// replacement.
func (c *claudeResumeCoordinator) ClearForApproval(approvalID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.identities, approvalID)
	for claimToken, entry := range c.entries {
		if entry.approvalID == approvalID && entry.state == stateDecisionReserved {
			entry.state = stateTerminal
			entry.ch <- outcomeCancelled
		}
		// Clean up terminal entries for this approval.
		if entry.approvalID == approvalID {
			delete(c.entries, claimToken)
		}
	}
}

// Close cancels all active entries and marks the coordinator closed.
// No new identities or entries can be created after close.
func (c *claudeResumeCoordinator) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	for _, entry := range c.entries {
		if entry.state == stateDecisionReserved {
			entry.state = stateTerminal
			entry.ch <- outcomeCancelled
		}
	}
	// Do not clear entries — drained waiters need to observe the outcome.
	// The coordinator is gone when the service is destroyed.
}

// clearStaleEntries removes entries that have exceeded the entry timeout.
// Called periodically (piggybacks on the runtime's clearStaleObservations).
func (c *claudeResumeCoordinator) clearStaleEntries(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := now.Add(-coordinatorEntryTimeout)
	for claimToken, entry := range c.entries {
		if entry.state == stateDecisionReserved && entry.createdAt.Before(cutoff) {
			entry.state = stateTerminal
			entry.ch <- outcomeTimeout
			delete(c.entries, claimToken)
		}
	}
}

// pendingCount returns the number of active (non-terminal) entries.
// Exposed for test assertions.
func (c *claudeResumeCoordinator) pendingCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.entries {
		if e.state != stateTerminal {
			n++
		}
	}
	return n
}

// identityCount returns the number of stored identities.
// Exposed for test assertions.
func (c *claudeResumeCoordinator) identityCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.identities)
}
