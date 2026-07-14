package term

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// A1 remediation 2 (R2-A/R2-B/R2-C) — execution-authority contract.
//
// Execution authority is acquired ONLY through the store's atomic
// ClaimForExecution, which is the COMPLETE authority boundary: it receives the
// selected option ID and raw user input, loads the immutable stored approval,
// recomputes the canonical ActionDigest itself, uses only the permission stored
// with the approval, derives the requester from the authenticated server principal,
// validates the full runtime and expiry, and transitions pending→executing in one
// critical section. A caller-supplied digest is at most an optional consistency
// assertion — never authority. The claim token owns exactly one immutable
// ApprovalExecutionBinding that is carried, unchanged, through delivery, receipt,
// and commit, where every field is compared.

const ActionSchemaVersion = "a1.action.v1"

const (
	maxIdempotencyKeyLen = 128
	maxNormalizedInput   = 4096
)

// RequesterContext is the SERVER-DERIVED authenticated requester bound into a
// claim. Every field originates from devicetrust.PrincipalFromContext — never the
// client body.
type RequesterContext struct {
	DeviceID        string
	HostID          string
	BearerSessionID string
	BootID          string
	Permissions     []string
}

func (r RequesterContext) hasPermission(need string) bool {
	for _, p := range r.Permissions {
		if p == need {
			return true
		}
	}
	return false
}

func (r RequesterContext) present() bool {
	return r.DeviceID != "" && r.BearerSessionID != ""
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
// action. Only fields that change the delivered bytes/keys are included.
type CanonicalAction struct {
	OptionID        string
	Kind            string
	SchemaVersion   string
	InputType       string // "" (none) | "text"
	InputPlacement  string // "" | as_payload | after_payload | metadata_only
	NormalizedInput string
}

// Digest is the canonical ActionDigest: SHA-256 over a length-framed encoding of
// exactly the delivery-semantic fields.
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

// ApprovalExecutionBinding is the ONE immutable identity a claim token owns. It is
// carried unchanged through the claim, the delivery request, the receipt, and the
// commit; every field is compared before a success may commit. It contains the
// ApprovalID, SessionID, adapter/provider/version + launch/stream generation
// (Runtime), the recomputed ActionDigest, and the idempotency key.
type ApprovalExecutionBinding struct {
	ApprovalID     string
	SessionID      string
	Runtime        RuntimeRef
	ActionDigest   string
	IdempotencyKey string
}

func (b ApprovalExecutionBinding) equal(o ApprovalExecutionBinding) bool {
	return b.ApprovalID == o.ApprovalID && b.SessionID == o.SessionID &&
		b.Runtime.equal(o.Runtime) && b.ActionDigest == o.ActionDigest &&
		b.IdempotencyKey == o.IdempotencyKey
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
	ClaimDigestMismatch  ClaimOutcome = "digest_mismatch" // optional caller assertion failed
	ClaimExpired         ClaimOutcome = "expired"
	ClaimAlreadyOwned    ClaimOutcome = "already_owned"
	ClaimStaleRuntime    ClaimOutcome = "stale_runtime"
	ClaimRuntimeMismatch ClaimOutcome = "runtime_mismatch"
	ClaimUnauthorized    ClaimOutcome = "unauthorized"
	ClaimLedgerFull      ClaimOutcome = "ledger_full" // capacity fail-closed
)

// ClaimRequest is one atomic execution-claim attempt. Runtime and Requester are
// SERVER-DERIVED. The store recomputes the digest from OptionID + Input; AssertDigest
// is an OPTIONAL consistency assertion only. There is intentionally no RequiredPerm:
// the store always uses the permission stored with the approval, so a caller can
// never weaken it.
type ClaimRequest struct {
	SessionID      string
	ApprovalID     string
	OptionID       string
	Input          string
	Runtime        RuntimeRef
	Requester      RequesterContext
	IdempotencyKey string
	AssertDigest   string // optional; if non-empty must equal the store-computed digest
}

// ClaimResult carries the outcome and, for ClaimGranted/ClaimAlreadyAccepted, the
// immutable binding the token owns. Token is non-empty only for ClaimGranted and is
// opaque, unforgeable, and never exposed in a public DTO.
type ClaimResult struct {
	Outcome ClaimOutcome
	Token   string
	Binding ApprovalExecutionBinding
}

// validCanonicalKey enforces a bounded, non-empty, closed-grammar idempotency key
// (ASCII alphanumerics and `._:-`). This keeps keys log-safe and unambiguous.
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

// canonicalActionFromOption builds the canonical action from the STORED option plus
// the normalized input — the store's authoritative digest source.
func canonicalActionFromOption(opt *interactionOptionView, normalizedInput string) CanonicalAction {
	inputType, placement := "", ""
	if opt.hasInput {
		inputType = "text"
		placement = opt.placement
	}
	return CanonicalAction{
		OptionID:        opt.id,
		Kind:            opt.kind,
		SchemaVersion:   ActionSchemaVersion,
		InputType:       inputType,
		InputPlacement:  placement,
		NormalizedInput: normalizedInput,
	}
}

// interactionOptionView is the immutable projection of a stored option the claim
// uses to recompute the digest (id/kind/input schema only).
type interactionOptionView struct {
	id        string
	kind      string
	hasInput  bool
	required  bool
	placement string
}
