// Package term — C2D-B: private one-shot resume coordinator for Claude
// PreToolUse decision delivery.
//
// C1D drops four critical identity fields (Claude session_id, tool_use_id,
// tool_name, input_digest) after joinDeferred matches a pending observation.
// C2D-B preserves them in a private identity record bound to the exact
// POKIT RuntimeRef so C2D-C can later deliver a decision through an exact
// Claude resume.
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
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const (
	maxCoordinatorIdentities  = 16
	maxCoordinatorEntries     = 16
	coordinatorEntryTimeout   = 120 * time.Second

	// Input bounds.
	maxCoordinatorSessionID   = 256
	maxCoordinatorToolID      = 256
	maxCoordinatorToolName    = 256
	maxCoordinatorClaimToken  = 256
)

// ── Certified decision mapping ──

// certifiedClaudeDecision maps the A1 store's OptionID to the Claude-native
// decision string. Only allow_once/deny are certified; everything else is
// rejected.
var certifiedClaudeDecision = map[string]string{
	"allow_once": "allow",
	"deny":       "deny",
}

const claudeDecisionSchemaV1 = "claude.pretooluse.decision.v1"

// ClaudeDecisionSchemaV1 is the public accessor for the certified schema identity.
func ClaudeDecisionSchemaV1() string { return claudeDecisionSchemaV1 }

// claudeHookResponseBytes encodes the C0D-certified hook response for a decision.
// This is the ONE canonical encoding; both the handler and the delivery must use it.
func claudeHookResponseBytes(decision string) []byte {
	return []byte(fmt.Sprintf(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"%s"}}`, decision))
}

// ClaudeHookResponseBytes is the exported accessor for composition tests.
func ClaudeHookResponseBytes(decision string) []byte { return claudeHookResponseBytes(decision) }

// deriveDecision validates the binding and returns the Claude-native
// decision. Returns ("", false) for unknown options, wrong schema, wrong
// adapter, or non-zero StreamGen.
func deriveDecision(binding ApprovalExecutionBinding) (string, bool) {
	if binding.Runtime.Adapter != claudeHeadlessAdapter || binding.Runtime.StreamGen != 0 {
		return "", false
	}
	if binding.DeliverySchema != claudeDecisionSchemaV1 {
		return "", false
	}
	dec, ok := certifiedClaudeDecision[binding.OptionID]
	return dec, ok
}

// ── Input validation ──

// validCoordinatorToken validates a variable-length identity token:
// non-empty, ≤ maxLen, only printable non-space ASCII (0x21–0x7e).
func validCoordinatorToken(s string, maxLen int) bool {
	if len(s) == 0 || len(s) > maxLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// validCoordinatorDigest validates a SHA-256 hex digest: exactly 64
// lowercase hex characters.
func validCoordinatorDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// validCoordinatorDecision validates a decision token against the closed
// Claude vocabulary.
func validCoordinatorDecision(s string) bool { return s == "allow" || s == "deny" }

// validCoordinatorClaimToken validates a claim token (reuses the existing
// store-level validator: 32 hex chars).
func validCoordinatorClaimToken(s string) bool { return validClaimToken(s) }

// validCoordinatorBinding validates the full binding metadata using the
// same canonical validators as ApprovalDelivery. This is the deepest
// boundary — no field escapes validation.
func validCoordinatorBinding(b ApprovalExecutionBinding) bool {
	return b.ApprovalID != "" && b.SessionID != "" && b.ActionDigest != "" && b.PayloadDigest != "" &&
		validCanonicalKey(b.IdempotencyKey) &&
		validSessionID(b.SessionID) && validApprovalID(b.ApprovalID) &&
		validAdapterID(b.Runtime.Adapter) && validVersion(b.Runtime.Version) &&
		isHex64(b.ActionDigest) && isHex64(b.PayloadDigest) &&
		len(b.OptionID) <= authMaxOptionField && len(b.DeliverySchema) <= maxDeliverySchemaVerBytes
}

// ── Witness identity ──

// WitnessKind identifies the type of consumption witness observed by C2D-C.
type WitnessKind int

const (
	WitnessPostToolUse       WitnessKind = iota + 1 // allow path
	WitnessPermissionDenials                         // deny path
)

// ── Private identity record ──

// claudePrivateIdentity preserves the C0D-certified four-field binding plus
// the exact POKIT RuntimeRef and POKIT session identifier. Created before
// Store admission and rolled back on failure. Keyed by ApprovalID.
type claudePrivateIdentity struct {
	sessionID       string // Claude's internal session_id
	toolUseID       string
	toolName        string
	inputDigest     string
	catalogActionID string // P2A: empty for non-catalog observations
	runtime         RuntimeRef // {Adapter, Version, LaunchGen, StreamGen}
	pokitSessionID  string     // POKIT compound session ID (e.g. "claude_headless:claude-abc123")
}

// ── Resume state machine ──

// resumeState is the closed state vocabulary for a coordinator entry.
type resumeState int

const (
	stateDecisionReserved resumeState = iota
	stateWriteClaimed
	stateDecisionWritten
	stateTerminal
)

// ── Claim-owned terminal result (R3-A) ──

// TerminalOutcome is the closed result vocabulary for a single claim.
type TerminalOutcome int

const (
	TerminalWitnessed         TerminalOutcome = iota + 1 // MarkWitnessed succeeded
	TerminalCancelled                                     // CancelEntry or ClearForApproval
	TerminalStaleRuntime                                  // ClearRuntime invalidated
	TerminalAmbiguous                                     // response write failed or ambiguous
	TerminalTimeout                                       // deadline exceeded
	TerminalRejected                                      // claim/identity mismatch
)

// TerminalResult is the claim-owned completion signal published exactly once.
type TerminalResult struct {
	Outcome             TerminalOutcome
	Binding             ApprovalExecutionBinding // set only for TerminalWitnessed
	ExactResponseDigest string                   // digest of exact written bytes
}

// ResumeHandle is returned by ReserveEntry. It carries the opaque tokens
// and a Completion channel that resolves to exactly one TerminalResult.
type ResumeHandle struct {
	ClaimToken  string
	ResumeNonce string
	Completion  <-chan TerminalResult
}

// resumeOutcome is the closed outcome vocabulary for ClaimWrite / ConfirmWrite.
type resumeOutcome int

const (
	outcomeWritten    resumeOutcome = iota + 1
	outcomeCancelled
	outcomeTimeout
	outcomeMismatch
	outcomeDuplicate
	outcomeStale
	outcomeAmbiguous
)

type WriteHandle struct {
	claimToken string
	decision   string
}

// Decision returns the Claude-native decision string to write.
func (h WriteHandle) Decision() string { return h.decision }

// ── Internal entry ──

// resumeEntry is one reserved claim-to-delivery lifecycle. It is NOT
// exposed to callers — only opaque handles are returned.
type resumeEntry struct {
	claimToken     string
	resumeNonce    string
	approvalID     string
	sessionID      string // Claude session_id
	toolUseID      string
	toolName       string
	inputDigest    string
	decision       string
	runtime        RuntimeRef
	pokitSessionID string                    // POKIT compound session ID from binding
	binding           ApprovalExecutionBinding  // full defensive copy for witness→receipt binding
	state             resumeState
	createdAt         time.Time // reservation time
	writeClaimedAt    time.Time // ClaimWrite called
	decisionWrittenAt time.Time // ConfirmWrite(true) called
	completion        chan TerminalResult // buffered 1; claim-owned terminal signal
}

// ── Coordinator ──

// claudeResumeCoordinator is the C2D-B private one-shot resume coordinator.
type claudeResumeCoordinator struct {
	mu         sync.Mutex
	identities map[string]*claudePrivateIdentity // ApprovalID → identity
	entries    map[string]*resumeEntry           // claimToken → entry
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

// ReserveIdentity creates a private identity record bound to the exact
// POKIT RuntimeRef. All string fields are validated; empty, over-size, or
// non-printable values are rejected. Returns false if validation fails,
// the approvalID already exists, the coordinator is closed, or capacity
// is exhausted.
//
// catalogActionID is empty for non-catalog observations. A non-empty
// value is validated against the compiled catalog: the entry must exist
// and its Provider/Version/ToolName must match rt.Adapter/rt.Version/toolName.
func (c *claudeResumeCoordinator) ReserveIdentity(approvalID, sessionID, toolUseID, toolName, inputDigest, catalogActionID, pokitSessionID string, rt RuntimeRef) bool {
	if !validCoordinatorToken(sessionID, maxCoordinatorSessionID) ||
		!validCoordinatorToken(toolUseID, maxCoordinatorToolID) ||
		!validCoordinatorToken(toolName, maxCoordinatorToolName) ||
		!validCoordinatorDigest(inputDigest) {
		return false
	}
	if !validAdapterID(rt.Adapter) || !validVersion(rt.Version) {
		return false
	}
	// Validate non-empty catalog ID against the compiled catalog.
	if catalogActionID != "" {
		if !validCoordinatorToken(catalogActionID, maxCoordinatorToolName) {
			return false
		}
		entry := lookupCatalogEntry(catalogActionID)
		if entry == nil {
			return false
		}
		if entry.Provider != rt.Adapter || entry.Version != rt.Version || entry.ToolName != toolName {
			return false
		}
	}

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
		sessionID:       sessionID,
		toolUseID:       toolUseID,
		toolName:        toolName,
		inputDigest:     inputDigest,
		catalogActionID: catalogActionID,
		runtime:         rt,
		pokitSessionID:  pokitSessionID,
	}
	return true
}

// RemoveIdentity removes a private identity record. It is idempotent.
func (c *claudeResumeCoordinator) RemoveIdentity(approvalID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.identities, approvalID)
}

// LookupIdentity returns a defensive copy of the identity record, or false.
func (c *claudeResumeCoordinator) LookupIdentity(approvalID string) (*claudePrivateIdentity, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id, ok := c.identities[approvalID]
	if !ok {
		return nil, false
	}
	return &claudePrivateIdentity{
		sessionID:       id.sessionID,
		toolUseID:       id.toolUseID,
		toolName:        id.toolName,
		inputDigest:     id.inputDigest,
		catalogActionID: id.catalogActionID,
		runtime:         id.runtime,
		pokitSessionID:  id.pokitSessionID,
	}, true
}

// ── Entry lifecycle ──

// entropyReader is injectable for tests (package-level, same pattern as
// managed_claude.go).
var coordEntropy io.Reader = rand.Reader

// generateCoordNonce creates an opaque one-shot resume nonce.
func generateCoordNonce() string {
	var b [16]byte
	if _, err := io.ReadFull(coordEntropy, b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// cloneBindingCopy returns a defensive copy of an ApprovalExecutionBinding
// with all string fields cloned via strings.Clone so the coordinator does
// not pin caller memory. Every field gets an independent bounded backing
// array; a short substring cannot pin a multi-MB caller buffer.
func cloneBindingCopy(b ApprovalExecutionBinding) ApprovalExecutionBinding {
	return ApprovalExecutionBinding{
		ApprovalID:     strings.Clone(b.ApprovalID),
		SessionID:      strings.Clone(b.SessionID),
		Runtime:        cloneRuntimeRef(b.Runtime),
		ActionDigest:   strings.Clone(b.ActionDigest),
		PayloadDigest:  strings.Clone(b.PayloadDigest),
		IdempotencyKey: strings.Clone(b.IdempotencyKey),
		OptionID:       strings.Clone(b.OptionID),
		DeliverySchema: strings.Clone(b.DeliverySchema),
	}
}

// ReserveEntry creates a coordinator entry from an identity and the
// store-issued ApprovalExecutionBinding. It validates the binding,
// derives the Claude-native decision from OptionID + DeliverySchema,
// and returns an opaque ResumeHandle.
//
// The runtime MUST exactly match the stored identity's runtime and
// POKIT session ID. A stale epoch, wrong adapter, or cross-session
// binding is rejected. The full binding is preserved defensively so
// claim→write→witness→receipt carries the same identity.
func (c *claudeResumeCoordinator) ReserveEntry(claimToken string, binding ApprovalExecutionBinding) (ResumeHandle, bool) {
	if !validCoordinatorClaimToken(claimToken) {
		return ResumeHandle{}, false
	}
	if !validCoordinatorBinding(binding) {
		return ResumeHandle{}, false
	}

	decision, ok := deriveDecision(binding)
	if !ok {
		return ResumeHandle{}, false
	}

	nonce := generateCoordNonce()
	if nonce == "" {
		return ResumeHandle{}, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return ResumeHandle{}, false
	}
	id, ok := c.identities[binding.ApprovalID]
	if !ok {
		return ResumeHandle{}, false
	}
	// B1: full identity comparison — runtime AND POKIT session must match.
	if !id.runtime.equal(binding.Runtime) || id.pokitSessionID != binding.SessionID {
		return ResumeHandle{}, false
	}
	if _, dup := c.entries[claimToken]; dup {
		return ResumeHandle{}, false
	}
	if len(c.entries) >= maxCoordinatorEntries {
		return ResumeHandle{}, false
	}

	ch := make(chan TerminalResult, 1)
	entry := &resumeEntry{
		claimToken:     claimToken,
		resumeNonce:    nonce,
		approvalID:     binding.ApprovalID,
		sessionID:      id.sessionID,
		toolUseID:      id.toolUseID,
		toolName:       id.toolName,
		inputDigest:    id.inputDigest,
		decision:       decision,
		runtime:        id.runtime,
		pokitSessionID: binding.SessionID,
		binding:        cloneBindingCopy(binding),
		state:          stateDecisionReserved,
		createdAt:      clockNow(),
		completion:     ch,
	}
	c.entries[claimToken] = entry
	return ResumeHandle{ClaimToken: claimToken, ResumeNonce: nonce, Completion: ch}, true
}

// ClaimWrite transitions a reserved entry to write-claimed and returns a
// WriteHandle with the decision string. The caller must write the HTTP
// response OUTSIDE the coordinator lock, then call ConfirmWrite.
//
// Returns (WriteHandle, outcomeWritten) on success.
// Returns (WriteHandle{}, outcomeMismatch) on field mismatch.
// Returns (WriteHandle{}, outcomeDuplicate) if already claimed.
// Returns (WriteHandle{}, outcomeStale) if not found or cancelled.
func (c *claudeResumeCoordinator) ClaimWrite(claimToken, resumeNonce, sessionID, toolUseID, toolName, inputDigest string) (WriteHandle, resumeOutcome) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return WriteHandle{}, outcomeStale
	}
	entry, ok := c.entries[claimToken]
	if !ok {
		return WriteHandle{}, outcomeStale
	}
	if entry.resumeNonce != resumeNonce {
		return WriteHandle{}, outcomeMismatch
	}
	if entry.state != stateDecisionReserved {
		return WriteHandle{}, outcomeDuplicate
	}
	if entry.sessionID != sessionID || entry.toolUseID != toolUseID ||
		entry.toolName != toolName || entry.inputDigest != inputDigest {
		return WriteHandle{}, outcomeMismatch
	}
	// B2: reject late hooks — the entry must not be expired.
	if clockNow().After(entry.createdAt.Add(coordinatorEntryTimeout)) {
		entry.state = stateTerminal
		entry.completion <- TerminalResult{Outcome: TerminalTimeout}
		delete(c.entries, claimToken)
		return WriteHandle{}, outcomeStale
	}

	entry.state = stateWriteClaimed
	entry.writeClaimedAt = clockNow()
	return WriteHandle{claimToken: claimToken, decision: entry.decision}, outcomeWritten
}

// ConfirmWrite confirms the outcome of an external HTTP write. It MUST be
// called after ClaimWrite's returned decision has been written (or the
// write has failed).
//
// If writeOK is true: transitions writeClaimed → decisionWritten and
// signals the waiter.
// If writeOK is false: transitions writeClaimed → terminal with
// outcomeAmbiguous. The write was claimed but not confirmed — partial
// bytes may have reached the provider. The caller must NOT retry.
//
// Returns outcomeWritten on successful confirmation, outcomeAmbiguous
// if the entry was invalidated concurrently or the write was reported
// as failed (non-retryable in both cases).
func (c *claudeResumeCoordinator) ConfirmWrite(claimToken string, writeOK bool) resumeOutcome {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[claimToken]
	if !ok || entry.state != stateWriteClaimed {
		return outcomeAmbiguous
	}
	// B2: late confirmation — the write took too long.
	if clockNow().After(entry.writeClaimedAt.Add(coordinatorEntryTimeout)) {
		entry.state = stateTerminal
		entry.completion <- TerminalResult{Outcome: TerminalAmbiguous}
		delete(c.entries, claimToken)
		return outcomeAmbiguous
	}
	if writeOK {
		entry.state = stateDecisionWritten
		entry.decisionWrittenAt = clockNow()
		// R3-A: ConfirmWrite is intermediate, not terminal.
		// Only MarkWitnessed publishes terminal success.
		return outcomeWritten
	}
	// Write failed: ambiguous because partial bytes may have been sent.
	entry.state = stateTerminal
	entry.completion <- TerminalResult{Outcome: TerminalAmbiguous}
	delete(c.entries, claimToken)
	return outcomeAmbiguous
}

// CancelEntry cancels an active entry. Handles all non-terminal states:
// reserved→cancelled, writeClaimed→ambiguous, decisionWritten→terminal.
func (c *claudeResumeCoordinator) CancelEntry(claimToken string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[claimToken]
	if !ok {
		return
	}
	switch entry.state {
	case stateDecisionReserved:
		entry.state = stateTerminal
		entry.completion <- TerminalResult{Outcome: TerminalCancelled}
		delete(c.entries, claimToken)
	case stateWriteClaimed:
		entry.state = stateTerminal
		entry.completion <- TerminalResult{Outcome: TerminalAmbiguous}
		delete(c.entries, claimToken)
	case stateDecisionWritten:
		entry.state = stateTerminal
		entry.completion <- TerminalResult{Outcome: TerminalCancelled}
		delete(c.entries, claimToken)
	default:
	}
}

// ClearForApproval removes the identity and cancels any active entry for
// the given approvalID.
func (c *claudeResumeCoordinator) ClearForApproval(approvalID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.identities, approvalID)
	for claimToken, entry := range c.entries {
		if entry.approvalID != approvalID {
			continue
		}
		switch entry.state {
		case stateDecisionReserved:
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalCancelled}
			delete(c.entries, claimToken)
		case stateWriteClaimed:
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalAmbiguous}
			delete(c.entries, claimToken)
		case stateDecisionWritten:
			entry.completion <- TerminalResult{Outcome: TerminalCancelled}
			delete(c.entries, claimToken)
		}
	}
}

// ClearRuntime removes all identities and cancels all active entries for
// a specific (pokitSessionID, launchGen) pair. Used on terminate and epoch
// replacement.
func (c *claudeResumeCoordinator) ClearRuntime(pokitSessionID string, launchGen int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for aid, id := range c.identities {
		if id.pokitSessionID == pokitSessionID && id.runtime.LaunchGen == launchGen {
			delete(c.identities, aid)
		}
	}
	for claimToken, entry := range c.entries {
		if entry.pokitSessionID == pokitSessionID && entry.runtime.LaunchGen == launchGen {
			switch entry.state {
			case stateDecisionReserved:
				entry.state = stateTerminal
				entry.completion <- TerminalResult{Outcome: TerminalStaleRuntime}
			case stateWriteClaimed:
				entry.state = stateTerminal
				entry.completion <- TerminalResult{Outcome: TerminalAmbiguous}
			case stateDecisionWritten:
				entry.completion <- TerminalResult{Outcome: TerminalStaleRuntime}
			}
			delete(c.entries, claimToken)
		}
	}
}

// Close cancels all active entries and marks the coordinator closed.
func (c *claudeResumeCoordinator) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	for _, entry := range c.entries {
		switch entry.state {
		case stateDecisionReserved:
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalCancelled}
		case stateWriteClaimed:
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalAmbiguous}
		case stateDecisionWritten:
			entry.completion <- TerminalResult{Outcome: TerminalCancelled}
		}
		delete(c.entries, entry.claimToken)
	}
}

// clearStaleEntries removes entries that have exceeded the entry timeout.
// Reserved entries time out. Write-claimed entries (write in flight past
// the deadline) are also cleaned up as ambiguous. Decision-written entries
// (witness never arrived) are cleaned up as ambiguous after a longer
// witness timeout.
func (c *claudeResumeCoordinator) clearStaleEntries(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entryCutoff := now.Add(-coordinatorEntryTimeout)
	// Witness timeout is longer: once written, the provider has more time
	// to produce the consumption witness.
	witnessCutoff := now.Add(-2 * coordinatorEntryTimeout)

	for claimToken, entry := range c.entries {
		switch entry.state {
		case stateDecisionReserved:
			if entry.createdAt.Before(entryCutoff) {
				entry.state = stateTerminal
				entry.completion <- TerminalResult{Outcome: TerminalTimeout}
				delete(c.entries, claimToken)
			}
		case stateWriteClaimed:
			ref := entry.writeClaimedAt
			if ref.IsZero() {
				ref = entry.createdAt
			}
			if ref.Before(entryCutoff) {
				entry.state = stateTerminal
				entry.completion <- TerminalResult{Outcome: TerminalAmbiguous}
				delete(c.entries, claimToken)
			}
		case stateDecisionWritten:
			ref := entry.decisionWrittenAt
			if ref.IsZero() {
				ref = entry.createdAt
			}
			if ref.Before(witnessCutoff) {
				entry.state = stateTerminal
				delete(c.entries, claimToken)
			}
		}
	}
}

// MarkWitnessed is called by C2D-C after a consumption witness is observed.
// It validates the witness identity against the stored entry and returns the
// full stored ApprovalExecutionBinding for receipt construction.
//
// Only accepted in stateDecisionWritten. The witness kind MUST match the
// stored decision: allow→PostToolUse, deny→PermissionDenials. Unknown
// kinds are rejected. Early, wrong, duplicate, mismatched, or stale
// witnesses leave the entry unchanged.
func (c *claudeResumeCoordinator) MarkWitnessed(claimToken string, kind WitnessKind, sessionID, toolUseID, toolName, inputDigest string, rt RuntimeRef) (ApprovalExecutionBinding, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[claimToken]
	if !ok || entry.state != stateDecisionWritten {
		return ApprovalExecutionBinding{}, "", false
	}
	switch entry.decision {
	case "allow":
		if kind != WitnessPostToolUse {
			return ApprovalExecutionBinding{}, "", false
		}
	case "deny":
		if kind != WitnessPermissionDenials {
			return ApprovalExecutionBinding{}, "", false
		}
	default:
		return ApprovalExecutionBinding{}, "", false
	}
	if clockNow().After(entry.decisionWrittenAt.Add(2 * coordinatorEntryTimeout)) {
		entry.state = stateTerminal
		delete(c.entries, claimToken)
		return ApprovalExecutionBinding{}, "", false
	}
	if entry.sessionID != sessionID || entry.toolUseID != toolUseID ||
		entry.toolName != toolName || entry.inputDigest != inputDigest {
		return ApprovalExecutionBinding{}, "", false
	}
	if !entry.runtime.equal(rt) {
		return ApprovalExecutionBinding{}, "", false
	}

	respDigest := payloadDigest(claudeHookResponseBytes(entry.decision))
	entry.state = stateTerminal
	result := TerminalResult{Outcome: TerminalWitnessed, Binding: entry.binding, ExactResponseDigest: respDigest}
	entry.completion <- result
	delete(c.entries, claimToken)
	delete(c.identities, entry.approvalID)
	return entry.binding, respDigest, true
}



// ── Test helpers ──

func (c *claudeResumeCoordinator) pendingCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.entries {
		if e.state == stateDecisionReserved || e.state == stateWriteClaimed {
			n++
		}
	}
	return n
}

// PendingCount is the exported accessor for tests.
func (c *claudeResumeCoordinator) PendingCount() int { return c.pendingCount() }

func (c *claudeResumeCoordinator) identityCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.identities)
}

// IdentityCount is the exported accessor for tests.
func (c *claudeResumeCoordinator) IdentityCount() int { return c.identityCount() }

func (c *claudeResumeCoordinator) entryCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// EntryCount is the exported accessor for tests.
func (c *claudeResumeCoordinator) EntryCount() int { return c.entryCount() }
