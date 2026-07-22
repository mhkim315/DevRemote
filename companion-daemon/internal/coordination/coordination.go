// Package coordination defines explicit, non-authoritative inter-session
// messages and their broker-owned delivery ledger. It never selects providers,
// dispatches work, replays delivery, or calls Timeline.
package coordination

import (
	"errors"
	"strings"
	"sync"
	"time"

	"devremote/companion-daemon/internal/workspace"
)

const MaxBytes = 512
const MaxReferenceBytes = 512
const MaxHandoffItems = 64

var (
	ErrInvalid    = errors.New("coordination: invalid envelope")
	ErrExists     = errors.New("coordination: message already exists")
	ErrTransition = errors.New("coordination: invalid delivery transition")
	ErrExpired    = errors.New("coordination: message expired")
)

type MessageType string

const (
	Instruction       MessageType = "instruction"
	Status            MessageType = "status"
	Result            MessageType = "result"
	ValidationRequest MessageType = "validation_request"
	Revision          MessageType = "revision"
	HandoffMessage    MessageType = "handoff"
	Cancellation      MessageType = "cancellation"
	FailureRecovery   MessageType = "failure_recovery"
	Question          MessageType = "question"
	Finding           MessageType = "finding"
	RevisionRequest   MessageType = "revision_request"
)

type DeliveryState string

const (
	Queued              DeliveryState = "queued"
	DeliveryAccepted    DeliveryState = "delivery_accepted"
	DeliveryFailed      DeliveryState = "delivery_failed"
	DeliveryUnknown     DeliveryState = "delivery_unknown"
	RuntimeAcknowledged DeliveryState = "runtime_acknowledged"
	ResponseReceived    DeliveryState = "response_received"
	Expired             DeliveryState = "expired"
	StaleTarget         DeliveryState = "stale_target"
)

type Endpoint struct {
	Provider, RuntimeID, SessionID string
	Generation                     int64
}
type Envelope struct {
	Version                                                                                                                                       uint16
	ID, TaskID                                                                                                                                    string
	Type                                                                                                                                          MessageType
	Source, Target                                                                                                                                Endpoint
	RepositoryID                                                                                                                                  string
	WorkspaceMode                                                                                                                                 workspace.Mode
	SnapshotID, BaseSHA, CurrentSHA, TreeHash, DiffDigest                                                                                         string
	HandoffReference, EvidenceReference, ContentDigest, RedactedSummary, RedactionPolicyVersion, ReplyToID, CausationID, RequiredTargetCapability string
	CreatedAt, ExpiresAt                                                                                                                          time.Time
	State                                                                                                                                         DeliveryState
}
type Handoff struct {
	Objective, AcceptanceCriteria, Constraints                                            string
	Workspace                                                                             workspace.Identity
	ChangedFiles, TestCommands, TestResults, UnresolvedFindings                           []string
	ApprovalState, EvidenceProvenance, ArtifactHash, ArtifactType, RedactionPolicyVersion string
	ArtifactBytes                                                                         int64
	ExpiresAt                                                                             time.Time
}

func (e Envelope) Validate() error {
	if e.Version != 1 || !text(e.ID) || !text(e.TaskID) || !endpoint(e.Source) || !endpoint(e.Target) || !text(e.RepositoryID) || !text(e.SnapshotID) || !text(e.BaseSHA) || !text(e.CurrentSHA) || !text(e.TreeHash) || !text(e.DiffDigest) || !text(e.ContentDigest) || !text(e.RedactedSummary) || !text(e.RedactionPolicyVersion) || !text(e.RequiredTargetCapability) || !reference(e.HandoffReference) || !reference(e.EvidenceReference) || !optionalReference(e.ReplyToID) || !optionalReference(e.CausationID) || e.CreatedAt.IsZero() || e.ExpiresAt.IsZero() || !e.ExpiresAt.After(e.CreatedAt) || !knownType(e.Type) || e.State != Queued {
		return ErrInvalid
	}
	if strings.Contains(strings.ToLower(e.RedactedSummary), "bearer ") || strings.Contains(strings.ToLower(e.RedactedSummary), "sk-") {
		return ErrInvalid
	}
	if e.WorkspaceMode != workspace.ModeSharedSequential && e.WorkspaceMode != workspace.ModeFrozenValidation {
		return ErrInvalid
	}
	return nil
}
func (h Handoff) Validate() error {
	if !text(h.Objective) || !text(h.AcceptanceCriteria) || !text(h.Constraints) || h.Workspace.Validate() != nil || !text(h.ApprovalState) || !text(h.EvidenceProvenance) || !text(h.ArtifactHash) || !text(h.ArtifactType) || !text(h.RedactionPolicyVersion) || h.ArtifactBytes < 0 || h.ExpiresAt.IsZero() || !items(h.ChangedFiles) || !items(h.TestCommands) || !items(h.TestResults) || !items(h.UnresolvedFindings) {
		return ErrInvalid
	}
	return nil
}
func text(s string) bool              { return s != "" && len(s) <= MaxBytes }
func reference(s string) bool         { return s != "" && len(s) <= MaxReferenceBytes }
func optionalReference(s string) bool { return s == "" || len(s) <= MaxReferenceBytes }
func items(v []string) bool {
	if len(v) > MaxHandoffItems {
		return false
	}
	for _, s := range v {
		if s == "" || len(s) > MaxBytes {
			return false
		}
	}
	return true
}
func endpoint(e Endpoint) bool {
	return text(e.Provider) && text(e.RuntimeID) && text(e.SessionID) && e.Generation >= 0
}
func knownType(t MessageType) bool {
	switch t {
	case Instruction, Status, Result, ValidationRequest, Revision, HandoffMessage, Cancellation, FailureRecovery, Question, Finding, RevisionRequest:
		return true
	}
	return false
}

type Broker struct {
	mu       sync.Mutex
	now      func() time.Time
	messages map[string]Envelope
}

func NewBroker(now func() time.Time) *Broker {
	if now == nil {
		now = time.Now
	}
	return &Broker{now: now, messages: map[string]Envelope{}}
}
func (b *Broker) Enqueue(e Envelope) error {
	if e.Validate() != nil {
		return ErrInvalid
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.messages[e.ID]; ok {
		return ErrExists
	}
	b.messages[e.ID] = e
	return nil
}
func (b *Broker) Transition(id string, state DeliveryState) (Envelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.messages[id]
	if !ok {
		return Envelope{}, ErrInvalid
	}
	if !b.now().Before(e.ExpiresAt) {
		e.State = Expired
		b.messages[id] = e
		return e, ErrExpired
	}
	if !allowed(e.State, state) {
		return Envelope{}, ErrTransition
	}
	e.State = state
	b.messages[id] = e
	return e, nil
}
func allowed(from, to DeliveryState) bool {
	switch from {
	case Queued:
		return to == DeliveryAccepted || to == DeliveryFailed || to == DeliveryUnknown || to == Expired || to == StaleTarget
	case DeliveryAccepted, DeliveryUnknown:
		return to == RuntimeAcknowledged || to == ResponseReceived || to == DeliveryFailed || to == Expired || to == StaleTarget
	case RuntimeAcknowledged:
		return to == ResponseReceived || to == Expired || to == StaleTarget
	}
	return false
}
