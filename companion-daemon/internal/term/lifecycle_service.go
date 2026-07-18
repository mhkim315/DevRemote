package term

import (
	"context"
	"errors"

	"devremote/companion-daemon/internal/sessionid"
	"devremote/companion-daemon/internal/transcript"
)

// Lifecycle errors map to structured, non-500 handler responses.
var (
	ErrLifecycleNotFound    = errors.New("session not found")
	ErrLifecycleUnsupported = errors.New("session does not support managed lifecycle")
	ErrLifecycleNotTerminal = errors.New("session must be stopped before it can be deleted")
	// ErrLifecycleStaleGeneration: the addressed runtime generation was replaced
	// between server-side derivation and the owner's decisive comparison. The
	// replacement process is unaffected → 409.
	ErrLifecycleStaleGeneration = errors.New("session runtime generation was replaced")
	// ErrLifecycleUnavailable: the selected lifecycle owner is not wired in this
	// composition (e.g. managed provider disabled) → 500.
	ErrLifecycleUnavailable = errors.New("lifecycle owner unavailable")
	// ErrLifecycleTerminateFailed: the process could not be confirmed dead even
	// after SIGKILL. The runtime is left intact (not marked terminal) → 500.
	ErrLifecycleTerminateFailed = errors.New("could not confirm session termination")
)

// LifecycleResult is the structured result of a lifecycle action.
type LifecycleResult struct {
	SessionID string         `json:"sessionId"`
	Action    string         `json:"action"`
	State     LifecycleState `json:"state"`
}

// StatusClearer drops S1 agent-activity status for a session. The production
// Delete path calls it so a deleted session immediately loses its activity record
// (and a recreated id cannot inherit it). TelemetryService satisfies this.
type StatusClearer interface{ Clear(sessionID string) }

// ProviderLifecycleOwner is the generation-bound lifecycle surface of the
// frozen provider services (ManagedCodexService / ManagedClaudeService both
// satisfy it). The provider performs the DECISIVE generation comparison at
// its own lifecycle lock — that is the linearization point; this dispatcher
// only derives {SessionID, generation} server-side and routes.
type ProviderLifecycleOwner interface {
	Stop(sessionID string, epoch int64) error
	Kill(sessionID string, epoch int64) error
	Delete(sessionID string, epoch int64) error
}

// LifecycleService is the PA2c lifecycle DISPATCHER. It owns no runtime,
// registry, or catalog state: every action is routed by canonical adapter
// prefix to exactly one managed lifecycle owner, with the current generation
// derived server-side. It has no *mux.Registry dependency; byte transport
// and terminal adapters cannot terminate, delete, or restore a runtime.
//
//	codex_app_server → ManagedCodexService   (provider-owned, generation-bound)
//	claude_headless  → ManagedClaudeService  (provider-owned, generation-bound)
//	controlled_pty   → OwnedPTYRuntime       (owned store, generation-bound)
//	unknown / legacy → fail closed
type LifecycleService struct {
	catalog    ManagedRuntimeCatalog // read path: server-side generation derivation for providers
	codex      ProviderLifecycleOwner
	claude     ProviderLifecycleOwner
	ownedPTY   *OwnedPTYRuntime
	activity   *ActivityBuffer     // provider Delete: daemon-owned history cleanup
	transcript *transcript.Service // provider Delete: daemon-owned projection cleanup
	status     StatusClearer       // provider Delete: S1 activity record cleanup
}

// NewLifecycleService constructs the dispatcher with the controlled-PTY owner.
// Provider owners and the read catalog are wired after construction (they are
// built later in the composition root) via WireManagedOwners.
func NewLifecycleService(ownedPTY *OwnedPTYRuntime, activity *ActivityBuffer, transcriptSvc *transcript.Service) *LifecycleService {
	return &LifecycleService{
		ownedPTY:   ownedPTY,
		activity:   activity,
		transcript: transcriptSvc,
	}
}

// WireManagedOwners wires the structured-provider lifecycle owners and the
// read-only ManagedRuntimeCatalog used for server-side generation derivation.
func (s *LifecycleService) WireManagedOwners(catalog ManagedRuntimeCatalog, codex, claude ProviderLifecycleOwner) {
	s.catalog = catalog
	s.codex = codex
	s.claude = claude
}

// SetStatusClearer wires the S1 agent-activity store so Delete clears it. Wired
// in the composition root after the TelemetryService is constructed.
func (s *LifecycleService) SetStatusClearer(c StatusClearer) {
	s.status = c
	if s.ownedPTY != nil {
		s.ownedPTY.SetStatusClearer(c)
	}
}

// OwnedPTY exposes the controlled-PTY owner (read path for /api/sessions
// lifecycle merge and tests).
func (s *LifecycleService) OwnedPTY() *OwnedPTYRuntime { return s.ownedPTY }

// ownerFor selects the exact lifecycle owner for a canonical session ID.
// Unknown and legacy adapters fail closed: the dispatch table deliberately
// has no Registry, TerminalTransport, or default row.
func (s *LifecycleService) ownerFor(id string) (ProviderLifecycleOwner, string, error) {
	ref := sessionid.ParseSessionID(id)
	switch ref.Adapter {
	case codexAppServerAdapter:
		if s.codex == nil {
			return nil, "", ErrLifecycleUnavailable
		}
		return s.codex, ref.Adapter, nil
	case claudeHeadlessAdapter:
		if s.claude == nil {
			return nil, "", ErrLifecycleUnavailable
		}
		return s.claude, ref.Adapter, nil
	case "controlled_pty":
		return nil, ref.Adapter, nil // handled by ownedPTY, not a ProviderLifecycleOwner
	default:
		return nil, ref.Adapter, ErrLifecycleUnsupported
	}
}

// deriveEpoch resolves the provider record and its exact current generation
// from the read-only ManagedRuntimeCatalog. Unknown sessions fail closed.
func (s *LifecycleService) deriveEpoch(id string) (ManagedSessionRecord, error) {
	if s.catalog == nil {
		return ManagedSessionRecord{}, ErrLifecycleUnavailable
	}
	rec, ok := s.catalog.Get(id)
	if !ok {
		return ManagedSessionRecord{}, ErrLifecycleNotFound
	}
	return rec, nil
}

// classifyProviderErr maps a failed provider lifecycle call to a TYPED
// outcome by re-reading the typed catalog state — never by parsing provider
// error strings. dispatchedEpoch is the generation the dispatcher derived.
func (s *LifecycleService) classifyProviderErr(id string, dispatchedEpoch int64, err error) error {
	rec, ok := s.catalog.Get(id)
	switch {
	case !ok:
		// The record disappeared (deleted concurrently) → not found.
		return ErrLifecycleNotFound
	case rec.Epoch != dispatchedEpoch:
		// Replaced between derivation and the owner's decisive comparison.
		// The stale request was rejected; the replacement is unaffected.
		return ErrLifecycleStaleGeneration
	case !rec.Exited:
		// Same generation, still live, owner refused → could not confirm
		// termination (Stop/Kill) or not terminal (Delete). The caller maps
		// per-action below.
		return err
	default:
		return err
	}
}

// Stop gracefully terminates a managed session through its exact owner.
func (s *LifecycleService) Stop(ctx context.Context, id string) (LifecycleResult, error) {
	owner, adapter, err := s.ownerFor(id)
	if err != nil {
		return LifecycleResult{}, err
	}
	if adapter == "controlled_pty" {
		if s.ownedPTY == nil {
			return LifecycleResult{}, ErrLifecycleUnavailable
		}
		return s.ownedPTY.Stop(ctx, id)
	}
	rec, derr := s.deriveEpoch(id)
	if derr != nil {
		return LifecycleResult{}, derr
	}
	if rec.Exited {
		return LifecycleResult{SessionID: id, Action: "stop", State: LifecycleExited}, nil
	}
	if perr := owner.Stop(id, rec.Epoch); perr != nil {
		cerr := s.classifyProviderErr(id, rec.Epoch, perr)
		if cerr == perr {
			cerr = ErrLifecycleTerminateFailed
		}
		return LifecycleResult{}, cerr
	}
	return LifecycleResult{SessionID: id, Action: "stop", State: LifecycleExited}, nil
}

// Kill force-terminates a managed session through its exact owner.
func (s *LifecycleService) Kill(ctx context.Context, id string) (LifecycleResult, error) {
	owner, adapter, err := s.ownerFor(id)
	if err != nil {
		return LifecycleResult{}, err
	}
	if adapter == "controlled_pty" {
		if s.ownedPTY == nil {
			return LifecycleResult{}, ErrLifecycleUnavailable
		}
		return s.ownedPTY.Kill(ctx, id)
	}
	rec, derr := s.deriveEpoch(id)
	if derr != nil {
		return LifecycleResult{}, derr
	}
	if rec.Exited {
		return LifecycleResult{SessionID: id, Action: "kill", State: LifecycleKilled}, nil
	}
	if perr := owner.Kill(id, rec.Epoch); perr != nil {
		cerr := s.classifyProviderErr(id, rec.Epoch, perr)
		if cerr == perr {
			cerr = ErrLifecycleTerminateFailed
		}
		return LifecycleResult{}, cerr
	}
	return LifecycleResult{SessionID: id, Action: "kill", State: LifecycleKilled}, nil
}

// Delete removes an ENDED managed session through its exact owner, then
// clears the daemon-owned history projections for exactly that canonical id.
func (s *LifecycleService) Delete(ctx context.Context, id string) (LifecycleResult, error) {
	owner, adapter, err := s.ownerFor(id)
	if err != nil {
		return LifecycleResult{}, err
	}
	if adapter == "controlled_pty" {
		if s.ownedPTY == nil {
			return LifecycleResult{}, ErrLifecycleUnavailable
		}
		return s.ownedPTY.Delete(ctx, id)
	}
	rec, derr := s.deriveEpoch(id)
	if derr != nil {
		return LifecycleResult{}, derr
	}
	if !rec.Exited {
		return LifecycleResult{}, ErrLifecycleNotTerminal
	}
	if perr := owner.Delete(id, rec.Epoch); perr != nil {
		cerr := s.classifyProviderErr(id, rec.Epoch, perr)
		if cerr == perr {
			cerr = ErrLifecycleNotTerminal
		}
		return LifecycleResult{}, cerr
	}
	// Daemon-owned projections for the deleted id (provider authority already
	// recorded terminal acceptance above).
	if s.activity != nil {
		s.activity.Clear(id)
	}
	if s.transcript != nil {
		s.transcript.ClearTranscript(id)
	}
	if s.status != nil {
		s.status.Clear(id)
	}
	DeleteRecorder(id)
	return LifecycleResult{SessionID: id, Action: "delete", State: LifecycleExited}, nil
}
