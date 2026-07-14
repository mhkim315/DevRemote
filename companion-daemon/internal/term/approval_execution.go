package term

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// A1 remediation (R-A / B1-B2) — execution-authority contract.
//
// Execution authority for an approval is acquired ONLY through one atomic
// ClaimForExecution store transition (pending → executing) that validates the
// entire binding in a single critical section and returns an unforgeable claim
// token. The canonical ActionDigest below is the execution-integrity value that is
// bound identically through claim, delivery, receipt, idempotency, and commit —
// substituting the selected option, its normalized input, placement, or schema
// version changes the digest and fails the match.

// ActionSchemaVersion is the frozen version of the canonical-action encoding. Any
// change to how an action's delivered bytes are derived MUST bump this so a digest
// computed under a different schema can never collide with an older one.
const ActionSchemaVersion = "a1.action.v1"

// RequesterContext is the SERVER-DERIVED authenticated requester bound into a
// claim. Every field originates from the authenticated principal
// (devicetrust.PrincipalFromContext) — never from the client request body. A claim
// records it so an execution owner is attributable; a client may not assert any of
// these fields, and unknown/conflicting client identity fields are rejected upstream.
type RequesterContext struct {
	DeviceID        string
	HostID          string
	BearerSessionID string
	BootID          string
	Permissions     []string
}

// hasPermission reports whether the requester carries an exact permission.
func (r RequesterContext) hasPermission(need string) bool {
	for _, p := range r.Permissions {
		if p == need {
			return true
		}
	}
	return false
}

// present reports whether a requester was actually derived (a non-empty
// server-authenticated identity). A zero RequesterContext fails closed.
func (r RequesterContext) present() bool {
	return r.DeviceID != "" && r.BearerSessionID != ""
}

// RuntimeRef is the exact runtime identity an approval is bound to: the accepted
// adapter/provider version plus the current launch and stream generation. A claim
// and its delivery both require the CURRENT runtime to equal the record's bound
// runtime; any drift (launch replacement, stream-generation change, version
// mismatch) fails closed. It is derived server-side, never asserted by the client.
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
// action. Only fields that change the bytes/keys actually delivered are included —
// display labels, prompts, and raw provider payloads are NEVER part of it.
type CanonicalAction struct {
	OptionID        string
	Kind            string
	SchemaVersion   string
	InputType       string // "" (none) | "text"
	InputPlacement  string // "" | as_payload | after_payload | metadata_only
	NormalizedInput string
}

// Digest is the canonical ActionDigest: SHA-256 over a length-framed encoding of
// exactly the delivery-semantic fields (framing avoids delimiter-injection
// ambiguity). Two actions deliver identical bytes iff their digests match.
func (c CanonicalAction) Digest() string {
	h := sha256.New()
	for _, f := range []string{
		c.SchemaVersion, c.OptionID, c.Kind, c.InputType, c.InputPlacement, c.NormalizedInput,
	} {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(f)))
		h.Write(n[:])
		h.Write([]byte(f))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ClaimOutcome is the closed result vocabulary of ClaimForExecution.
type ClaimOutcome string

const (
	ClaimGranted         ClaimOutcome = "granted"          // exclusive ownership acquired; token issued
	ClaimAlreadyAccepted ClaimOutcome = "already_accepted" // same key+digest already accepted (idempotent)
	ClaimConflict        ClaimOutcome = "conflict"         // same key, different digest
	ClaimNotFound        ClaimOutcome = "not_found"        // no such approval for the exact session
	ClaimNotActionable   ClaimOutcome = "not_actionable"   // approval has no proven action mapping (B5)
	ClaimUnknownAction   ClaimOutcome = "unknown_action"   // option ID not in the record's allowed set
	ClaimExpired         ClaimOutcome = "expired"          // TTL elapsed
	ClaimAlreadyOwned    ClaimOutcome = "already_owned"    // already executing/terminal (no second owner)
	ClaimStaleRuntime    ClaimOutcome = "stale_runtime"    // current launch/stream generation moved
	ClaimRuntimeMismatch ClaimOutcome = "runtime_mismatch" // adapter/provider/version mismatch
	ClaimUnauthorized    ClaimOutcome = "unauthorized"     // requester absent or lacks the required permission
)

// ClaimRequest is one atomic execution-claim attempt. Runtime and Requester are
// SERVER-DERIVED (never client-asserted); the store validates the full binding in
// a single critical section.
type ClaimRequest struct {
	SessionID      string
	ApprovalID     string
	OptionID       string
	Runtime        RuntimeRef
	ActionDigest   string
	Requester      RequesterContext
	RequiredPerm   string
	IdempotencyKey string
}

// ClaimResult is the outcome of a claim. Token is non-empty only for
// ClaimGranted; it is an opaque, unforgeable, internal handle that never appears in
// a public DTO and is not derivable from the ApprovalID.
type ClaimResult struct {
	Outcome ClaimOutcome
	Token   string
	Digest  string
}
