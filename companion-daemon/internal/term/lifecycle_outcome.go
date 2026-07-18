package term

// PA2c-R1: closed typed lifecycle outcome vocabulary, produced at the
// decisive owner boundary. The dispatcher maps outcomes to HTTP-facing
// sentinels mechanically; no provider error string is ever parsed and no
// late federated-catalog reread is used to infer what an owner decided.

// LifecycleOutcome is the closed internal vocabulary a lifecycle owner
// returns for a dispatched {SessionID, generation} action.
type LifecycleOutcome string

const (
	// OutcomeAccepted — the owner performed the action for the exact
	// dispatched generation and recorded terminal acceptance.
	OutcomeAccepted LifecycleOutcome = "accepted"
	// OutcomeAlreadyTerminal — the exact generation is already terminal;
	// the action is idempotent success.
	OutcomeAlreadyTerminal LifecycleOutcome = "already_terminal"
	// OutcomeStaleGeneration — the dispatched generation is not the owner's
	// current generation (a replacement won); the current process was not
	// touched.
	OutcomeStaleGeneration LifecycleOutcome = "stale_generation"
	// OutcomeNotFound — the owner has no record for the session.
	OutcomeNotFound LifecycleOutcome = "not_found"
	// OutcomeNotTerminal — Delete on a non-terminal runtime.
	OutcomeNotTerminal LifecycleOutcome = "not_terminal"
	// OutcomeUnavailable — the owner cannot serve lifecycle actions in this
	// composition.
	OutcomeUnavailable LifecycleOutcome = "unavailable"
	// OutcomeTerminationFailed — same generation, owner attempted
	// termination, process death could not be confirmed.
	OutcomeTerminationFailed LifecycleOutcome = "termination_failed"
)

// mapOutcome converts a closed owner outcome to the handler-facing sentinel
// (nil for success outcomes). Purely mechanical: one arm per vocabulary
// entry, no inference.
func mapOutcome(oc LifecycleOutcome) error {
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
	stop func(sessionID string, epoch int64) error
	kill func(sessionID string, epoch int64) error
	del  func(sessionID string, epoch int64) error
}

// NewManagedProviderOwner wraps a frozen provider service's generation-bound
// lifecycle surface in the typed outcome vocabulary.
func NewManagedProviderOwner(
	reg *ManagedSessionRegistry,
	stop func(string, int64) error,
	kill func(string, int64) error,
	del func(string, int64) error,
) ProviderLifecycleOwner {
	return &managedProviderOwner{reg: reg, stop: stop, kill: kill, del: del}
}

// classify maps a provider refusal to a typed outcome from the provider-owned
// registry record. err is only checked for nil-ness — never parsed.
func (p *managedProviderOwner) classify(id string, epoch int64, err error, isDelete bool) LifecycleOutcome {
	if err == nil {
		return OutcomeAccepted
	}
	if p.reg == nil {
		return OutcomeUnavailable
	}
	rec, ok := p.reg.Get(id)
	switch {
	case !ok:
		return OutcomeNotFound
	case rec.Epoch != epoch:
		// The provider's current generation differs from the dispatched one:
		// the decisive comparison rejected a stale request. Classified from
		// the provider-owned record, independent of any later publication.
		return OutcomeStaleGeneration
	case isDelete && !rec.Exited:
		return OutcomeNotTerminal
	case !isDelete && rec.Exited:
		return OutcomeAlreadyTerminal
	default:
		return OutcomeTerminationFailed
	}
}

func (p *managedProviderOwner) Stop(id string, epoch int64) LifecycleOutcome {
	if p.stop == nil {
		return OutcomeUnavailable
	}
	return p.classify(id, epoch, p.stop(id, epoch), false)
}

func (p *managedProviderOwner) Kill(id string, epoch int64) LifecycleOutcome {
	if p.kill == nil {
		return OutcomeUnavailable
	}
	return p.classify(id, epoch, p.kill(id, epoch), false)
}

func (p *managedProviderOwner) Delete(id string, epoch int64) LifecycleOutcome {
	if p.del == nil {
		return OutcomeUnavailable
	}
	return p.classify(id, epoch, p.del(id, epoch), true)
}
