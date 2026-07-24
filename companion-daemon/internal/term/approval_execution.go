package term

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"unicode/utf8"

	"devremote/companion-daemon/internal/devicetrust"
)

// A1 remediation 3 — execution-authority contract.
//
// The store is the sole authority: ClaimForExecution recomputes the canonical
// ActionDigest AND the canonical delivery payload from the stored immutable option,
// binds the exact server-derived requester authorization context, and issues a claim
// token owning one immutable ApprovalExecutionBinding (approval, session, runtime,
// action digest, payload digest, key). The idempotent-replay decision runs ONLY
// behind the full current-authority checks. Delivery and the receipt carry the same
// binding plus the delivered-payload digest, so substituted bytes cannot commit.

const ActionSchemaVersion = "a1.action.v1"

const (
	maxIdempotencyKeyLen = 128
	maxNormalizedInput   = 4096
	// SP1 P2A — bounds of the optional per-option delivery material.
	maxDeliveryMaterialBytes  = 4096
	maxDeliverySchemaVerBytes = 64
)

// ApprovalDeliveryMaterial is daemon-generated immutable delivery material
// for ONE certified option (SP1 §4): the selected action identity, its
// response schema version, and the EXACT bounded provider response bytes the
// delivery boundary must write verbatim. It is supplied at ingestion,
// defensively copied by the store, folded into the canonical action and
// payload digests, returned by ClaimForExecution, and NEVER projected into
// any public/mobile DTO.
type ApprovalDeliveryMaterial struct {
	OptionID      string
	SchemaVersion string
	ResponseBytes []byte
}

// RequesterContext is the SERVER-DERIVED authenticated requester handed to the
// store. The store immediately canonicalizes it; nothing here comes from the client
// body.
type RequesterContext struct {
	DeviceID        string
	DeviceEpoch     uint64
	HostID          string
	BearerSessionID string
	BootID          string
	Permissions     []string
}

// MutationAuthorization is the immutable device authority captured by an
// authenticated handler and carried to the exact approval mutation boundary.
// The authorizer is mandatory in production; keeping the identity beside the
// interface prevents a later lookup from accidentally authorizing a different
// device or epoch.
type MutationAuthorization struct {
	Authorizer  devicetrust.MutationAuthorizer
	DeviceID    string
	DeviceEpoch uint64
}

func (a MutationAuthorization) authorize(intent devicetrust.MutationIntent) error {
	if a.Authorizer == nil {
		return localMutationAuthorizer{}.AuthorizeCommit(a.DeviceID, a.DeviceEpoch, intent)
	}
	return a.Authorizer.AuthorizeCommit(a.DeviceID, a.DeviceEpoch, intent)
}

func (r RequesterContext) hasPermission(need string) bool {
	for _, p := range r.Permissions {
		if p == need {
			return true
		}
	}
	return false
}

// present reports whether the COMPLETE server-authenticated requester identity was
// derived. All four server-derived fields (DeviceID, HostID, BearerSessionID, BootID)
// must be non-empty; a missing field fails closed. This is validated inside the store
// (the deepest authority boundary), not left to a handler convention.
func (r RequesterContext) present() bool {
	return r.DeviceID != "" && r.HostID != "" && r.BearerSessionID != "" && r.BootID != ""
}

// requesterAuthorized is the SINGLE requester validator the store applies to EVERY
// path — initial claim, bounded manual retry, and already_accepted replay — so no
// path can obtain execution authority with an incomplete server-derived context or
// without the stored permission.
func requesterAuthorized(r RequesterContext, storedPerm string) bool {
	return r.present() && (storedPerm == "" || r.hasPermission(storedPerm))
}

// RequesterAuthContext is the IMMUTABLE canonical requester authorization identity
// bound into the claim and ledger. The permission set is captured as a sorted,
// domain-separated digest so a later request with a changed set (or a mutated caller
// slice) does not match. No mutable caller slice is retained.
type RequesterAuthContext struct {
	DeviceID        string
	DeviceEpoch     uint64
	HostID          string
	BearerSessionID string
	BootID          string
	PermDigest      string
}

func canonicalRequesterAuth(r RequesterContext) RequesterAuthContext {
	perms := append([]string(nil), r.Permissions...)
	sort.Strings(perms)
	h := sha256.New()
	h.Write([]byte("a1.perms.v1\x00"))
	for _, p := range perms {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(p)))
		h.Write(n[:])
		h.Write([]byte(p))
	}
	return RequesterAuthContext{
		DeviceID: r.DeviceID, DeviceEpoch: r.DeviceEpoch, HostID: r.HostID, BearerSessionID: r.BearerSessionID,
		BootID: r.BootID, PermDigest: hex.EncodeToString(h.Sum(nil)),
	}
}

func (a RequesterAuthContext) equal(o RequesterAuthContext) bool {
	return a.DeviceID == o.DeviceID && a.DeviceEpoch == o.DeviceEpoch && a.HostID == o.HostID &&
		a.BearerSessionID == o.BearerSessionID && a.BootID == o.BootID &&
		a.PermDigest == o.PermDigest
}

// RuntimeRef is the exact runtime identity an approval is bound to.
type RuntimeRef struct {
	Adapter   string
	Version   string
	LaunchGen int64
	StreamGen int
}

func (r RuntimeRef) equal(o RuntimeRef) bool {
	return r.Adapter == o.Adapter && r.Version == o.Version &&
		r.LaunchGen == o.LaunchGen && r.StreamGen == o.StreamGen
}

// CanonicalAction is the exact, delivery-semantic representation of ONE selected
// action. SP1 P2A: when the stored option carries delivery material, its schema
// version and exact-bytes digest are part of the action identity — a record
// differing only in material yields a different ActionDigest.
type CanonicalAction struct {
	OptionID        string
	Kind            string
	SchemaVersion   string
	InputType       string
	InputPlacement  string
	NormalizedInput string
	// DeliverySchemaVersion / DeliveryPayloadDigest are empty for options
	// without delivery material (frozen legacy behavior unchanged).
	DeliverySchemaVersion string
	DeliveryPayloadDigest string
}

func (c CanonicalAction) Digest() string {
	h := sha256.New()
	for _, f := range []string{
		c.SchemaVersion, c.OptionID, c.Kind, c.InputType, c.InputPlacement, c.NormalizedInput,
		c.DeliverySchemaVersion, c.DeliveryPayloadDigest,
	} {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(f)))
		h.Write(n[:])
		h.Write([]byte(f))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// validPlacement is the closed input-placement vocabulary. An unknown placement in
// a stored option fails the claim closed.
func validPlacement(p string) bool {
	switch p {
	case "", "as_payload", "after_payload", "metadata_only":
		return true
	default:
		return false
	}
}

// canonicalPayload is the ONLY delivery payload for a canonical action — computed by
// the store, never by the handler from a display snapshot. Decision/no-input actions
// carry no bytes (no blind Y/N). Only an explicit input-bearing placement forwards
// the user's literal normalized input.
func canonicalPayload(view *interactionOptionView, normalizedInput string) []byte {
	if !view.hasInput {
		return nil
	}
	switch view.placement {
	case "as_payload", "after_payload":
		if normalizedInput == "" {
			return nil
		}
		return []byte(normalizedInput + "\n")
	default:
		return nil
	}
}

// payloadDigest is a domain-separated digest of exact delivery bytes. A delivery
// boundary reports the digest of what it actually delivered; commit requires it to
// equal the claim's PayloadDigest, so substituted bytes cannot commit.
func payloadDigest(p []byte) string {
	h := sha256.New()
	h.Write([]byte("a1.payload.v1\x00"))
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(p)))
	h.Write(n[:])
	h.Write(p)
	return hex.EncodeToString(h.Sum(nil))
}

// ApprovalExecutionBinding is the ONE immutable identity a claim token owns, carried
// unchanged through claim → delivery → receipt → commit; every field is compared.
// SP1 P2A-R1: OptionID (the store-selected action identity) and DeliverySchema (the
// delivery material's schema identity, empty without material) are INTERNAL binding
// fields set by the store at claim time, compared everywhere, readable by the
// certified delivery boundary for exact action↔response semantic coupling, and never
// projected into any public/mobile DTO.
type ApprovalExecutionBinding struct {
	ApprovalID     string
	SessionID      string
	Runtime        RuntimeRef
	ActionDigest   string
	PayloadDigest  string
	IdempotencyKey string
	OptionID       string
	DeliverySchema string
}

func (b ApprovalExecutionBinding) equal(o ApprovalExecutionBinding) bool {
	return b.ApprovalID == o.ApprovalID && b.SessionID == o.SessionID &&
		b.Runtime.equal(o.Runtime) && b.ActionDigest == o.ActionDigest &&
		b.PayloadDigest == o.PayloadDigest && b.IdempotencyKey == o.IdempotencyKey &&
		b.OptionID == o.OptionID && b.DeliverySchema == o.DeliverySchema
}

// ClaimOutcome is the closed result vocabulary of ClaimForExecution.
type ClaimOutcome string

const (
	ClaimGranted         ClaimOutcome = "granted"
	ClaimAlreadyAccepted ClaimOutcome = "already_accepted"
	ClaimConflict        ClaimOutcome = "conflict"
	ClaimNotFound        ClaimOutcome = "not_found"
	ClaimNotActionable   ClaimOutcome = "not_actionable"
	ClaimUnknownAction   ClaimOutcome = "unknown_action"
	ClaimInvalidInput    ClaimOutcome = "invalid_input"
	ClaimInvalidKey      ClaimOutcome = "invalid_key"
	ClaimDigestMismatch  ClaimOutcome = "digest_mismatch"
	ClaimExpired         ClaimOutcome = "expired"
	ClaimAlreadyOwned    ClaimOutcome = "already_owned"
	ClaimStaleRuntime    ClaimOutcome = "stale_runtime"
	ClaimStaleEpoch      ClaimOutcome = "stale_epoch"
	ClaimRuntimeMismatch ClaimOutcome = "runtime_mismatch"
	ClaimUnauthorized    ClaimOutcome = "unauthorized"
	ClaimLedgerFull      ClaimOutcome = "ledger_full"
	ClaimRetryExhausted  ClaimOutcome = "retry_exhausted"
)

// ClaimRequest is one atomic execution-claim attempt. Runtime and Requester are
// SERVER-DERIVED; the store recomputes the digest and payload and uses the stored
// permission. AssertDigest is an optional consistency check only.
type ClaimRequest struct {
	SessionID      string
	ApprovalID     string
	OptionID       string
	Input          string
	Runtime        RuntimeRef
	Requester      RequesterContext
	IdempotencyKey string
	AssertDigest   string
}

// ClaimResult carries the outcome and, for a granted/already_accepted claim, the
// immutable binding and the canonical delivery payload the boundary must use.
type ClaimResult struct {
	Outcome ClaimOutcome
	Token   string
	Binding ApprovalExecutionBinding
	Payload []byte
}

func validCanonicalKey(k string) bool {
	if k == "" || len(k) > maxIdempotencyKeyLen {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '.' || c == '_' || c == ':' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

func canonicalActionFromOption(opt *interactionOptionView, normalizedInput string) CanonicalAction {
	inputType, placement := "", ""
	if opt.hasInput {
		inputType = "text"
		placement = opt.placement
	}
	return CanonicalAction{
		OptionID: opt.id, Kind: opt.kind, SchemaVersion: ActionSchemaVersion,
		InputType: inputType, InputPlacement: placement, NormalizedInput: normalizedInput,
	}
}

// interactionOptionView is the immutable projection of a stored option used to
// recompute the digest and payload.
type interactionOptionView struct {
	id        string
	kind      string
	hasInput  bool
	required  bool
	placement string
}

// validInput reports whether the normalized input is acceptable for the stored
// option schema. Invalid UTF-8, oversize, or a no-input option carrying input all
// fail closed.
func validInput(view *interactionOptionView, input string) bool {
	if len(input) > maxNormalizedInput || !utf8.ValidString(input) {
		return false
	}
	if !view.hasInput && input != "" {
		return false
	}
	if view.hasInput && view.required && input == "" {
		return false
	}
	return true
}
