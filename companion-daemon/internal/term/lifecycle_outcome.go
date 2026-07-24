package term

// PA2c-R1: closed typed lifecycle outcome vocabulary, produced at the
// decisive owner boundary. The dispatcher maps outcomes to HTTP-facing
// sentinels mechanically; no provider error string is ever parsed and no
// late federated-catalog reread is used to infer what an owner decided.

// LifecycleActionOutcome is the closed internal vocabulary a lifecycle owner
// returns for a dispatched {SessionID, generation} action.
type LifecycleActionOutcome string

const (
	// OutcomeAccepted — the owner performed the action for the exact
	// dispatched generation and recorded terminal acceptance.
	OutcomeAccepted LifecycleActionOutcome = "accepted"
	// OutcomeAlreadyTerminal — the exact generation is already terminal;
	// the action is idempotent success.
	OutcomeAlreadyTerminal LifecycleActionOutcome = "already_terminal"
	// OutcomeStaleGeneration — the dispatched generation is not the owner's
	// current generation (a replacement won); the current process was not
	// touched.
	OutcomeStaleGeneration LifecycleActionOutcome = "stale_generation"
	// OutcomeNotFound — the owner has no record for the session.
	OutcomeNotFound LifecycleActionOutcome = "not_found"
	// OutcomeNotTerminal — Delete on a non-terminal runtime.
	OutcomeNotTerminal LifecycleActionOutcome = "not_terminal"
	// OutcomeUnavailable — the owner cannot serve lifecycle actions in this
	// composition.
	OutcomeUnavailable LifecycleActionOutcome = "unavailable"
	// OutcomeTerminationFailed — same generation, owner attempted
	// termination, process death could not be confirmed.
	OutcomeTerminationFailed LifecycleActionOutcome = "termination_failed"
)

// mapOutcome converts a closed owner outcome to the handler-facing sentinel
// (nil for success outcomes). Purely mechanical: one arm per vocabulary
// entry, no inference.
func mapOutcome(oc LifecycleActionOutcome) error {
	switch oc {
	case OutcomeAccepted, OutcomeAlreadyTerminal:
		return nil
	case OutcomeStaleGeneration:
		return ErrLifecycleStaleGeneration
	case OutcomeNotFound:
		return ErrLifecycleNotFound
	case OutcomeNotTerminal:
		return ErrLifecycleNotTerminal
	case OutcomeUnavailable:
		return ErrLifecycleUnavailable
	case OutcomeTerminationFailed:
		return ErrLifecycleTerminateFailed
	default:
		// Not in the closed vocabulary — fail closed as unavailable rather
		// than inventing semantics.
		return ErrLifecycleUnavailable
	}
}

// managedProviderOwner adapts a frozen provider service (ManagedCodexService
// / ManagedClaudeService) to the closed typed outcome vocabulary at the
// owner boundary. The frozen services perform the DECISIVE generation
// comparison at their own lifecycle lock; this adapter classifies a refusal
// from the provider-OWNED ManagedSessionRegistry record — the exact typed
// store that decisive comparison used — read immediately at the boundary.
// It never parses provider error strings and never consults the federated
// ManagedRuntimeCatalog, so a stale rejection classifies correctly even
// BEFORE a replacement publishes anywhere else.
type managedProviderOwner struct {
	reg  *ManagedSessionRegistry
	stop func(sessionID string, epoch int64, deviceID string, deviceEpoch uint64) error
	kill func(sessionID string, epoch int64, deviceID string, deviceEpoch uint64) error
	del  func(sessionID string, epoch int64, deviceID string, deviceEpoch uint64) error
}

// NewManagedProviderOwner wraps a frozen provider service's generation-bound
// lifecycle surface in the typed outcome vocabulary.
func NewManagedProviderOwner(
	reg *ManagedSessionRegistry,
	stop func(string, int64, string, uint64) error,
	kill func(string, int64, string, uint64) error,
	del func(string, int64, string, uint64) error,
) ProviderLifecycleOwner {
	return &managedProviderOwner{reg: reg, stop: stop, kill: kill, del: del}
}

// act runs one generation-bound provider lifecycle call with typed
// classification (PA2c-R2).
//
// PRE-CALL: idempotency and state outcomes come from the authoritative
// provider-owned registry record read BEFORE the call — already_terminal and
// not_terminal can ONLY be produced here. This is the only place a refusal
// may be classified as success-shaped.
//
// POST-CALL: a non-nil provider error is NEVER success. It classifies from
// the typed post-call record as stale_generation / not_found when the
// record moved under the call, otherwise termination_failed (Stop/Kill) or
// not_terminal (Delete). In particular the frozen Codex/Claude timeout path
// — MarkExited(sessionID, epoch) followed by an error return — stays
// termination_failed: the record shows Exited at the SAME generation after
// the call, but termination was not confirmed, so it must not be reported
// as already_terminal. err is only checked for nil-ness — never parsed.
func (p *managedProviderOwner) act(id string, epoch int64, deviceID string, deviceEpoch uint64, call func(string, int64, string, uint64) error, isDelete bool) LifecycleActionOutcome {
	if call == nil || p.reg == nil {
		return OutcomeUnavailable
	}
	pre, ok := p.reg.Get(id)
	switch {
	case !ok:
		return OutcomeNotFound
	case pre.Epoch != epoch:
		return OutcomeStaleGeneration
	case !isDelete && pre.Exited:
		// Authoritative PRE-call state: the exact generation is already
		// terminal — idempotent success, no signal needed.
		return OutcomeAlreadyTerminal
	case isDelete && !pre.Exited:
		return OutcomeNotTerminal
	}
	err := call(id, epoch, deviceID, deviceEpoch)
	if err == nil {
		return OutcomeAccepted
	}
	post, ok := p.reg.Get(id)
	switch {
	case !ok:
		return OutcomeNotFound
	case post.Epoch != epoch:
		return OutcomeStaleGeneration
	case isDelete:
		return OutcomeNotTerminal
	default:
		return OutcomeTerminationFailed
	}
}

func (p *managedProviderOwner) Stop(id string, epoch int64, deviceID string, deviceEpoch uint64) LifecycleActionOutcome {
	return p.act(id, epoch, deviceID, deviceEpoch, p.stop, false)
}

func (p *managedProviderOwner) Kill(id string, epoch int64, deviceID string, deviceEpoch uint64) LifecycleActionOutcome {
	return p.act(id, epoch, deviceID, deviceEpoch, p.kill, false)
}

func (p *managedProviderOwner) Delete(id string, epoch int64, deviceID string, deviceEpoch uint64) LifecycleActionOutcome {
	return p.act(id, epoch, deviceID, deviceEpoch, p.del, true)
}
