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

	"devremote/companion-daemon/internal/devicetrust"
)

const (
	maxCoordinatorIdentities = 16
	maxCoordinatorEntries    = 16
	coordinatorEntryTimeout  = 120 * time.Second

	// Input bounds.
	maxCoordinatorSessionID  = 256
	maxCoordinatorToolID     = 256
	maxCoordinatorToolName   = 256
	maxCoordinatorClaimToken = 256
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
	WitnessPermissionDenials                        // deny path
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
	catalogActionID string     // P2A: empty for non-catalog observations
	runtime         RuntimeRef // {Adapter, Version, LaunchGen, StreamGen}
	pokitSessionID  string     // POKIT compound session ID (e.g. "claude_headless:claude-abc123")
}

// ── Resume state machine ──

// resumeState is the closed state vocabulary for a coordinator entry.
type resumeState int

const (
	stateDecisionReserved resumeState = iota
	stateWriteClaimed
	stateWitnessPending // R4: early witness stored before ConfirmWrite
	stateDecisionWritten
	stateTerminal
)

// ── R4: Witness outcome ──

// WitnessOutcome is the closed result vocabulary for MarkWitnessed and
// MarkDenialWitness. Only Witnessed is terminal success; Pending means
// the witness is stored and awaiting ConfirmWrite (binding/digest are
// zero). All other outcomes are non-success.
type WitnessOutcome int

const (
	WitnessPending   WitnessOutcome = iota + 1 // early witness stored, not terminal
	Witnessed                                  // terminal success (binding+digest returned)
	WitnessMismatch                            // identity mismatch
	WitnessDuplicate                           // same identity already witnessed
	WitnessStale                               // entry not found or wrong state
)

// WitnessResult packs the witness outcome with optional binding/digest
// (populated only for Witnessed).
type WitnessResult struct {
	Outcome WitnessOutcome
	Binding ApprovalExecutionBinding
	Digest  string
}

// ── R4: Resume attempt identity ──

// ResumeAttemptIdentity is the real invocation identity observed in the
// resume PreToolUse hook body. It is bound exactly once per claim at
// ClaimWrite time, under the coordinator lock. Witness validation
// (MarkWitnessed, MarkDenialWitness) compares against this bound identity,
// not the original deferred observation.
type ResumeAttemptIdentity struct {
	sessionID       string
	toolUseID       string
	toolName        string
	inputDigest     string
	resumeLaunchGen int64
	registeredAt    time.Time
}

// earlyWitnessArgs stores a fully-validated early witness when
// MarkWitnessed or MarkDenialWitness arrives before ConfirmWrite(true).
type earlyWitnessArgs struct {
	kind            WitnessKind
	sessionID       string
	toolUseID       string
	toolName        string
	inputDigest     string
	runtime         RuntimeRef
	exactRespDigest string // precomputed response digest for deny deny witness
}

// ── Claim-owned terminal result (R3-A) ──

// TerminalOutcome is the closed result vocabulary for a single claim.
type TerminalOutcome int

const (
	TerminalWitnessed    TerminalOutcome = iota + 1 // MarkWitnessed succeeded
	TerminalCancelled                               // CancelEntry or ClearForApproval
	TerminalStaleRuntime                            // ClearRuntime invalidated
	TerminalAmbiguous                               // response write failed or ambiguous
	TerminalTimeout                                 // deadline exceeded
	TerminalRejected                                // claim/identity mismatch
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
	outcomeWritten resumeOutcome = iota + 1
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
	claimToken        string
	resumeNonce       string
	approvalID        string
	sessionID         string // Claude session_id (from original identity; replaced by attempt.sessionID at ClaimWrite)
	toolUseID         string // original tool_use_id (replaced by attempt.toolUseID at ClaimWrite)
	toolName          string
	inputDigest       string
	decision          string
	runtime           RuntimeRef
	pokitSessionID    string                   // POKIT compound session ID from binding
	binding           ApprovalExecutionBinding // full defensive copy for witness→receipt binding
	state             resumeState
	createdAt         time.Time           // reservation time
	writeClaimedAt    time.Time           // ClaimWrite called
	decisionWrittenAt time.Time           // ConfirmWrite(true) called
	completion        chan TerminalResult // buffered 1; claim-owned terminal signal
	deviceID          string
	deviceEpoch       uint64

	// R4: resume attempt identity fields
	attempt           *ResumeAttemptIdentity // nil until ClaimWrite binds the real resume identity
	expectedLaunchGen int64                  // set by BindResumeProcess; independently validated by ClaimWrite
	earlyWitness      *earlyWitnessArgs      // non-nil only in stateWitnessPending
}

// ── Coordinator ──

// claudeResumeCoordinator is the C2D-B private one-shot resume coordinator.
type claudeResumeCoordinator struct {
	mu         sync.Mutex
	authorizer devicetrust.MutationAuthorizer
	identities map[string]*claudePrivateIdentity // ApprovalID → identity
	entries    map[string]*resumeEntry           // claimToken → entry
	closed     bool
}

func (c *claudeResumeCoordinator) authorizeEntryLocked(entry *resumeEntry, intent devicetrust.MutationIntent, commit func() error) bool {
	if entry == nil || c.authorizer == nil {
		return false
	}
	return c.authorizer.AuthorizeAndCommit(entry.deviceID, entry.deviceEpoch, intent, commit) == nil
}

// NewClaudeResumeCoordinator creates an empty coordinator.
func NewClaudeResumeCoordinator(authorizer devicetrust.MutationAuthorizer) (*claudeResumeCoordinator, error) {
	if authorizer == nil {
		return nil, fmt.Errorf("claude coordinator mutation authorizer is required")
	}
	return &claudeResumeCoordinator{authorizer: authorizer,
		identities: make(map[string]*claudePrivateIdentity),
		entries:    make(map[string]*resumeEntry),
	}, nil
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
		entry, found := lookupCatalogEntry(catalogActionID)
		if !found {
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

// HasIdentityForRuntime reports whether any private identity is bound to the
// exact (pokitSessionID, launchGen) pair. Read-only; used by RuntimeOf for
// the joined-deferred-exit claim window (C3D contract §6).
func (c *claudeResumeCoordinator) HasIdentityForRuntime(pokitSessionID string, launchGen int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range c.identities {
		if id.pokitSessionID == pokitSessionID && id.runtime.LaunchGen == launchGen {
			return true
		}
	}
	return false
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

// BindResumeProcess stores the coordinator-owned expected resume process
// generation BEFORE the process is spawned or its hook bridge is published.
// ResumeForApproval allocates the epoch, calls this transition, then places
// the same value in the immutable resume context. ClaimWrite independently
// compares the bridge-supplied generation against this stored value.
//
// Preconditions: entry exists, nonce matches, state == stateDecisionReserved,
// generator non-zero. Returns false on any violation.
func (c *claudeResumeCoordinator) BindResumeProcess(claimToken, resumeNonce string, launchGen int64) bool {
	if launchGen <= 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[claimToken]
	if !ok || entry.resumeNonce != resumeNonce || entry.state != stateDecisionReserved {
		return false
	}
	// R4: exactly-once. Reject if already bound.
	if entry.expectedLaunchGen != 0 {
		return false
	}
	if c.authorizer.AuthorizeAndCommit(entry.deviceID, entry.deviceEpoch, devicetrust.IntentApprovalResume, func() error {
		entry.expectedLaunchGen = launchGen
		return nil
	}) != nil {
		return false
	}
	return true
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
func (c *claudeResumeCoordinator) ReserveEntry(claimToken string, binding ApprovalExecutionBinding, deviceID string, deviceEpoch uint64) (ResumeHandle, bool) {
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
	var handle ResumeHandle
	err := c.authorizer.AuthorizeAndCommit(deviceID, deviceEpoch, devicetrust.IntentApprovalDeliver, func() error {
		if len(c.entries) >= maxCoordinatorEntries {
			return fmt.Errorf("resume coordinator capacity exhausted")
		}
		ch := make(chan TerminalResult, 1)
		entry := &resumeEntry{
			claimToken: claimToken, resumeNonce: nonce, approvalID: binding.ApprovalID,
			sessionID: id.sessionID, toolUseID: id.toolUseID, toolName: id.toolName,
			inputDigest: id.inputDigest, decision: decision, runtime: id.runtime,
			pokitSessionID: binding.SessionID, binding: cloneBindingCopy(binding),
			state: stateDecisionReserved, createdAt: clockNow(), completion: ch,
			deviceID: deviceID, deviceEpoch: deviceEpoch,
		}
		c.entries[claimToken] = entry
		handle = ResumeHandle{ClaimToken: claimToken, ResumeNonce: nonce, Completion: ch}
		return nil
	})
	if err != nil {
		return ResumeHandle{}, false
	}
	return handle, true
}

// ClaimWrite transitions a reserved entry to write-claimed and returns a
// WriteHandle with the decision string. The caller must write the HTTP
// response OUTSIDE the coordinator lock, then call ConfirmWrite.
//
// R4: ClaimWrite now accepts the REAL resume invocation identity and
// independently validates it against the stored OriginalApprovalIdentity
// AND the coordinator-owned expected launch generation. The first successful
// ClaimWrite creates the ResumeAttemptIdentity (one-time bind). Replay
// with the same identity is a duplicate; a different identity is rejected.
//
// Returns (WriteHandle, outcomeWritten) on success.
// Returns (WriteHandle{}, outcomeMismatch) on identity/generation mismatch.
// Returns (WriteHandle{}, outcomeDuplicate) on same-identity replay.
// Returns (WriteHandle{}, outcomeStale) if not found, wrong nonce, wrong state, or expired.
func (c *claudeResumeCoordinator) ClaimWrite(claimToken, resumeNonce, sessionID, toolUseID, toolName, inputDigest string, resumeLaunchGen int64) (WriteHandle, resumeOutcome) {
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
	switch entry.state {
	case stateDecisionReserved:
		// R4: BindResumeProcess MUST have been called before ClaimWrite.
		// The coordinator-owned expected generation must match exactly.
		if entry.expectedLaunchGen == 0 || entry.expectedLaunchGen != resumeLaunchGen {
			return WriteHandle{}, outcomeMismatch
		}
		// R4: validate every new provider identity field at the
		// deepest authority boundary. sessionID and toolUseID must
		// be non-empty bounded tokens. toolName and inputDigest
		// are independently compared against the stored
		// OriginalApprovalIdentity.
		if !validCoordinatorToken(sessionID, maxCoordinatorSessionID) ||
			!validCoordinatorToken(toolUseID, maxCoordinatorToolID) {
			return WriteHandle{}, outcomeMismatch
		}
		if entry.toolName != toolName || entry.inputDigest != inputDigest {
			return WriteHandle{}, outcomeMismatch
		}
		// B2: reject late hooks — the entry must not be expired.
		if clockNow().After(entry.createdAt.Add(coordinatorEntryTimeout)) {
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalTimeout}
			delete(c.entries, claimToken)
			return WriteHandle{}, outcomeStale
		}

		// R4: one-time bind — create the ResumeAttemptIdentity.
		attempt := &ResumeAttemptIdentity{
			sessionID:       sessionID,
			toolUseID:       toolUseID,
			toolName:        toolName,
			inputDigest:     inputDigest,
			resumeLaunchGen: resumeLaunchGen,
			registeredAt:    clockNow(),
		}
		// Atomic: authorize + commit state change.
		if !c.authorizeEntryLocked(entry, devicetrust.IntentApprovalDeliver, func() error {
			entry.attempt = attempt
			entry.state = stateWriteClaimed
			entry.writeClaimedAt = clockNow()
			return nil
		}) {
			return WriteHandle{}, outcomeStale
		}
		return WriteHandle{claimToken: claimToken, decision: entry.decision}, outcomeWritten

	case stateWriteClaimed, stateWitnessPending, stateDecisionWritten:
		// R4: attempt already bound. Replay with the same identity
		// fields is a duplicate (no re-write). Different identity is
		// a mismatch.
		if entry.attempt == nil {
			return WriteHandle{}, outcomeMismatch
		}
		a := entry.attempt
		if a.sessionID == sessionID && a.toolUseID == toolUseID &&
			a.toolName == toolName && a.inputDigest == inputDigest {
			return WriteHandle{}, outcomeDuplicate
		}
		return WriteHandle{}, outcomeMismatch

	default:
		// stateTerminal
		return WriteHandle{}, outcomeStale
	}
}

// ConfirmWrite confirms the outcome of an external HTTP write. It MUST be
// called after ClaimWrite's returned decision has been written (or the
// write has failed).
//
// If writeOK is true and state is writeClaimed: transitions → decisionWritten.
// If writeOK is true and state is witnessPending: commits the stored early
// witness as TerminalWitnessed (terminal).
// If writeOK is false: transitions any non-terminal state → terminal with
// outcomeAmbiguous, wiping any stored early witness.
//
// Returns outcomeWritten on successful confirmation, outcomeAmbiguous
// if the entry was invalidated concurrently or the write was reported
// as failed (non-retryable in both cases).
func (c *claudeResumeCoordinator) ConfirmWrite(claimToken string, writeOK bool) resumeOutcome {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[claimToken]
	if !ok {
		return outcomeAmbiguous
	}
	if entry.state != stateWriteClaimed && entry.state != stateWitnessPending {
		return outcomeAmbiguous
	}
	// B2: late confirmation — the write took too long.
	if clockNow().After(entry.writeClaimedAt.Add(coordinatorEntryTimeout)) {
		entry.state = stateTerminal
		entry.earlyWitness = nil
		entry.completion <- TerminalResult{Outcome: TerminalAmbiguous}
		delete(c.entries, claimToken)
		return outcomeAmbiguous
	}
	if !writeOK {
		// Write failed: ambiguous because partial bytes may have been sent.
		// Wipe any stored early witness.
		entry.earlyWitness = nil
		entry.state = stateTerminal
		entry.completion <- TerminalResult{Outcome: TerminalAmbiguous}
		delete(c.entries, claimToken)
		return outcomeAmbiguous
	}
	// writeOK == true
	switch entry.state {
	case stateWriteClaimed:
		if !c.authorizeEntryLocked(entry, devicetrust.IntentApprovalCommit, func() error {
			entry.state = stateDecisionWritten
			entry.decisionWrittenAt = clockNow()
			return nil
		}) {
			return outcomeAmbiguous
		}
		return outcomeWritten
	case stateWitnessPending:
		// R4: commit the stored early witness.
		if entry.earlyWitness == nil || entry.attempt == nil {
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalAmbiguous}
			delete(c.entries, claimToken)
			return outcomeAmbiguous
		}
		respDigest := payloadDigest(claudeHookResponseBytes(entry.decision))
		if !c.authorizeEntryLocked(entry, devicetrust.IntentApprovalCommit, func() error {
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalWitnessed, Binding: entry.binding, ExactResponseDigest: respDigest}
			delete(c.entries, claimToken)
			delete(c.identities, entry.approvalID)
			return nil
		}) {
			return outcomeAmbiguous
		}
		return outcomeWritten
	default:
		return outcomeAmbiguous
	}
}

// CancelEntry cancels an active entry. Handles all non-terminal states:
// reserved→cancelled, writeClaimed→ambiguous, witnessPending→cancelled,
// decisionWritten→terminal.
func (c *claudeResumeCoordinator) CancelEntry(claimToken string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[claimToken]
	if !ok {
		return
	}
	switch entry.state {
	case stateDecisionReserved:
		c.authorizeEntryLocked(entry, devicetrust.IntentApprovalCancel, func() error {
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalCancelled}
			delete(c.entries, claimToken)
			return nil
		})
	case stateWriteClaimed:
		c.authorizeEntryLocked(entry, devicetrust.IntentApprovalCancel, func() error {
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalAmbiguous}
			delete(c.entries, claimToken)
			return nil
		})
	case stateWitnessPending:
		c.authorizeEntryLocked(entry, devicetrust.IntentApprovalCancel, func() error {
			entry.earlyWitness = nil
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalCancelled}
			delete(c.entries, claimToken)
			return nil
		})
	case stateDecisionWritten:
		c.authorizeEntryLocked(entry, devicetrust.IntentApprovalCancel, func() error {
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalCancelled}
			delete(c.entries, claimToken)
			return nil
		})
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
		case stateWitnessPending:
			entry.earlyWitness = nil
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalCancelled}
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
			case stateWitnessPending:
				entry.earlyWitness = nil
				entry.state = stateTerminal
				entry.completion <- TerminalResult{Outcome: TerminalStaleRuntime}
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
		case stateWitnessPending:
			entry.earlyWitness = nil
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalCancelled}
		case stateDecisionWritten:
			entry.completion <- TerminalResult{Outcome: TerminalCancelled}
		}
		delete(c.entries, entry.claimToken)
	}
}

// clearStaleEntries removes entries that have exceeded the entry timeout.
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
				entry.earlyWitness = nil
				entry.completion <- TerminalResult{Outcome: TerminalAmbiguous}
				delete(c.entries, claimToken)
			}
		case stateWitnessPending:
			ref := entry.writeClaimedAt
			if ref.IsZero() {
				ref = entry.createdAt
			}
			if ref.Before(entryCutoff) {
				entry.earlyWitness = nil
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
// R4: it validates against the bound ResumeAttemptIdentity, not the original
// deferred identity. Returns a closed WitnessOutcome; only Witnessed carries
// binding+digest. Pending means the witness is stored but not yet terminal
// (awaiting ConfirmWrite).
func (c *claudeResumeCoordinator) MarkWitnessed(claimToken string, kind WitnessKind, sessionID, toolUseID, toolName, inputDigest string, rt RuntimeRef) WitnessResult {
	fail := func(o WitnessOutcome) WitnessResult { return WitnessResult{Outcome: o} }
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[claimToken]
	if !ok {
		return fail(WitnessStale)
	}
	if entry.attempt == nil {
		return fail(WitnessStale)
	}

	switch entry.decision {
	case "allow":
		if kind != WitnessPostToolUse {
			return fail(WitnessMismatch)
		}
	case "deny":
		if kind != WitnessPermissionDenials {
			return fail(WitnessMismatch)
		}
	default:
		return fail(WitnessMismatch)
	}

	a := entry.attempt
	if a.sessionID != sessionID || a.toolUseID != toolUseID ||
		a.toolName != toolName || a.inputDigest != inputDigest {
		return fail(WitnessMismatch)
	}
	if !entry.runtime.equal(rt) {
		return fail(WitnessMismatch)
	}

	switch entry.state {
	case stateWriteClaimed:
		if clockNow().After(entry.writeClaimedAt.Add(2 * coordinatorEntryTimeout)) {
			return fail(WitnessStale)
		}
		respDigest := payloadDigest(claudeHookResponseBytes(entry.decision))
		var result WitnessResult
		if !c.authorizeEntryLocked(entry, devicetrust.IntentApprovalCommit, func() error {
			entry.earlyWitness = &earlyWitnessArgs{
				kind: kind, sessionID: sessionID, toolUseID: toolUseID,
				toolName: toolName, inputDigest: inputDigest,
				runtime: rt, exactRespDigest: respDigest,
			}
			entry.state = stateWitnessPending
			return nil
		}) {
			return fail(WitnessStale)
		}
		_ = result
		return WitnessResult{Outcome: WitnessPending}

	case stateWitnessPending:
		if entry.earlyWitness == nil {
			return fail(WitnessStale)
		}
		ew := entry.earlyWitness
		if ew.sessionID == sessionID && ew.toolUseID == toolUseID &&
			ew.toolName == toolName && ew.inputDigest == inputDigest &&
			ew.kind == kind && ew.runtime.equal(rt) {
			return fail(WitnessDuplicate)
		}
		return fail(WitnessMismatch)

	case stateDecisionWritten:
		if clockNow().After(entry.decisionWrittenAt.Add(2 * coordinatorEntryTimeout)) {
			entry.state = stateTerminal
			delete(c.entries, claimToken)
			return fail(WitnessStale)
		}
		respDigest := payloadDigest(claudeHookResponseBytes(entry.decision))
		if !c.authorizeEntryLocked(entry, devicetrust.IntentApprovalCommit, func() error {
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalWitnessed, Binding: entry.binding, ExactResponseDigest: respDigest}
			delete(c.entries, claimToken)
			delete(c.identities, entry.approvalID)
			return nil
		}) {
			return fail(WitnessStale)
		}
		return WitnessResult{Outcome: Witnessed, Binding: entry.binding, Digest: respDigest}

	default:
		return fail(WitnessStale)
	}
}

// MarkDenialWitness is the single-lock coordinator operation for deny
// witness. Returns a closed WitnessOutcome.
func (c *claudeResumeCoordinator) MarkDenialWitness(claimToken, denialSessionID string, denialEntries []streamDenialEntry, rt RuntimeRef) WitnessResult {
	fail := func(o WitnessOutcome) WitnessResult { return WitnessResult{Outcome: o} }
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[claimToken]
	if !ok || entry.attempt == nil {
		return fail(WitnessStale)
	}
	if entry.decision != "deny" {
		return fail(WitnessMismatch)
	}

	a := entry.attempt
	var matches []streamDenialEntry
	for _, e := range denialEntries {
		if denialSessionID == a.sessionID &&
			e.ToolUseID == a.toolUseID &&
			e.ToolName == a.toolName &&
			e.InputDigest == a.inputDigest {
			matches = append(matches, e)
		}
	}

	if len(matches) != 1 {
		if len(matches) > 1 {
			entry.earlyWitness = nil
			entry.state = stateTerminal
			select {
			case entry.completion <- TerminalResult{Outcome: TerminalAmbiguous}:
			default:
			}
			delete(c.entries, claimToken)
			delete(c.identities, entry.approvalID)
		}
		return fail(WitnessMismatch)
	}

	match := matches[0]

	switch entry.state {
	case stateWriteClaimed:
		if clockNow().After(entry.writeClaimedAt.Add(2 * coordinatorEntryTimeout)) {
			return fail(WitnessStale)
		}
		respDigest := payloadDigest(claudeHookResponseBytes(entry.decision))
		if !c.authorizeEntryLocked(entry, devicetrust.IntentApprovalCommit, func() error {
			entry.earlyWitness = &earlyWitnessArgs{
				kind:      WitnessPermissionDenials,
				sessionID: denialSessionID, toolUseID: match.ToolUseID,
				toolName: match.ToolName, inputDigest: match.InputDigest,
				runtime: rt, exactRespDigest: respDigest,
			}
			entry.state = stateWitnessPending
			return nil
		}) {
			return fail(WitnessStale)
		}
		return WitnessResult{Outcome: WitnessPending}

	case stateWitnessPending:
		if entry.earlyWitness == nil {
			return fail(WitnessStale)
		}
		ew := entry.earlyWitness
		if ew.kind != WitnessPermissionDenials {
			return fail(WitnessMismatch)
		}
		if ew.sessionID == denialSessionID && ew.toolUseID == match.ToolUseID {
			return fail(WitnessDuplicate)
		}
		return fail(WitnessMismatch)

	case stateDecisionWritten:
		if clockNow().After(entry.decisionWrittenAt.Add(2 * coordinatorEntryTimeout)) {
			entry.state = stateTerminal
			delete(c.entries, claimToken)
			return fail(WitnessStale)
		}
		respDigest := payloadDigest(claudeHookResponseBytes(entry.decision))
		if !c.authorizeEntryLocked(entry, devicetrust.IntentApprovalCommit, func() error {
			entry.state = stateTerminal
			entry.completion <- TerminalResult{Outcome: TerminalWitnessed, Binding: entry.binding, ExactResponseDigest: respDigest}
			delete(c.entries, claimToken)
			delete(c.identities, entry.approvalID)
			return nil
		}) {
			return fail(WitnessStale)
		}
		return WitnessResult{Outcome: Witnessed, Binding: entry.binding, Digest: respDigest}

	default:
		return fail(WitnessStale)
	}
}

// ── Test helpers ──

func (c *claudeResumeCoordinator) pendingCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.entries {
		if e.state == stateDecisionReserved || e.state == stateWriteClaimed || e.state == stateWitnessPending {
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
