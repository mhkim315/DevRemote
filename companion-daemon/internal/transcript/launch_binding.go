package transcript

import (
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent/contract"
)

// LaunchBinding is an immutable session-owned launch authority.
// Only the session creation controller (pokit run) may create one.
//
// S1.1-B: Generation is a REAL monotonic launch-instance identity assigned by the
// registry (never supplied by the caller). PID+StartedAt are optional supporting
// process identity — when present they STRENGTHEN correlation (PID reuse with a
// different start time fails); when absent correlation is weaker/unavailable, never
// a wildcard. Process identity is NEVER agent-status authority.
type LaunchBinding struct {
	SessionID       string
	Provider        string    // "codex" or "claude"
	Adapter         string    // "controlled_pty"
	AcceptedVersion string    // "0.144.1" or "2.1.202"
	ProcessName     string    // executable name (codex/claude)
	PID             int       // process ID at launch time (0 = unknown)
	StartedAt       time.Time // process start time at launch (zero = unknown)
	Generation      int64     // monotonic launch instance identity (registry-assigned)
}

// LaunchSpec is the caller-supplied description of a managed launch. The registry
// assigns Generation; callers never set it. PID/StartedAt are optional and should
// be sourced from the session's own ProcessInfo so a later discovery of the same
// process reports identical values.
type LaunchSpec struct {
	SessionID   string
	Provider    string
	Adapter     string
	Version     string
	ProcessName string // defaults to Provider when empty
	PID         int
	StartedAt   time.Time
}

// LaunchRegistry stores active launch bindings and a monotonic launch counter.
// Only the session creation path may register bindings. TelemetryService reads
// them to establish correlation.
type LaunchRegistry struct {
	mu       sync.Mutex
	bindings map[string]LaunchBinding // sessionID → binding
	// nextGen is a process-wide strictly-increasing launch counter. Using one
	// counter (rather than a per-session map) keeps the registry bounded to the
	// active bindings while guaranteeing that any later launch — including a
	// same-ID replacement or a delete→recreate — receives a strictly higher
	// generation than any earlier launch. Generations are opaque monotonic
	// tokens; they need not be dense per session.
	nextGen int64
}

var globalLaunchRegistry = &LaunchRegistry{
	bindings: make(map[string]LaunchBinding),
}

// RegisterLaunch records or REPLACES a managed launch binding, assigning a
// strictly-increasing launch Generation. It returns the assigned generation and
// whether an existing binding for the same session ID was replaced.
//
// Unlike the prior implementation it NEVER silently ignores a second registration
// for the same session ID: a re-registration replaces the stale binding at a
// higher generation so a recreated/relaunched session can never inherit stale
// provider/version/PID/start identity. The caller inspects `replaced` to install
// a status high-water for the new generation before any new positive evidence.
func RegisterLaunch(spec LaunchSpec) (generation int64, replaced bool) {
	globalLaunchRegistry.mu.Lock()
	defer globalLaunchRegistry.mu.Unlock()
	_, replaced = globalLaunchRegistry.bindings[spec.SessionID]
	globalLaunchRegistry.nextGen++
	gen := globalLaunchRegistry.nextGen
	name := spec.ProcessName
	if name == "" {
		name = spec.Provider
	}
	globalLaunchRegistry.bindings[spec.SessionID] = LaunchBinding{
		SessionID:       spec.SessionID,
		Provider:        spec.Provider,
		Adapter:         spec.Adapter,
		AcceptedVersion: spec.Version,
		ProcessName:     name,
		PID:             spec.PID,
		StartedAt:       spec.StartedAt,
		Generation:      gen,
	}
	return gen, replaced
}

// LookupLaunch returns the launch binding for a session, or nil if not
// a managed launch.
func LookupLaunch(sessionID string) *LaunchBinding {
	globalLaunchRegistry.mu.Lock()
	defer globalLaunchRegistry.mu.Unlock()
	b, ok := globalLaunchRegistry.bindings[sessionID]
	if !ok {
		return nil
	}
	return &b
}

// RemoveLaunch clears a launch binding (called on session delete). The monotonic
// counter is NOT reset, so a later same-ID launch still receives a strictly
// higher generation.
func RemoveLaunch(sessionID string) {
	globalLaunchRegistry.mu.Lock()
	defer globalLaunchRegistry.mu.Unlock()
	delete(globalLaunchRegistry.bindings, sessionID)
}

// LaunchCorrelation returns the correlation state for a managed session.
// Validates provider, version, and process identity. Empty/missing values
// fail closed — they are not wildcards.
//
// S1.1-B process-identity rule: when the binding claims a PID, discovery must
// report the SAME nonzero PID; and when the binding ALSO claims a start time,
// discovery must report the SAME nonzero start time. A matching PID with a
// different (or missing) start time fails closed — this rejects PID reuse by a
// replacement process. A binding that claims no PID does not gate on process
// identity (weaker correlation), but never treats a missing value as a wildcard
// match against a claimed one.
func LaunchCorrelation(binding *LaunchBinding, discoveredProvider, discoveredVersion string, discoveredPID int, discoveredStart time.Time) contract.Correlation {
	if binding == nil {
		return contract.CorrelationUnavailable
	}
	if binding.Provider != discoveredProvider {
		return contract.CorrelationUnavailable
	}
	// Require version match when binding specifies a version.
	if binding.AcceptedVersion != "" {
		if discoveredVersion == "" || binding.AcceptedVersion != discoveredVersion {
			return contract.CorrelationUnavailable
		}
	}
	// Require PID (and, when claimed, start-time) match when binding specifies a PID.
	if binding.PID != 0 {
		if discoveredPID == 0 || binding.PID != discoveredPID {
			return contract.CorrelationUnavailable
		}
		if !binding.StartedAt.IsZero() {
			if discoveredStart.IsZero() || !binding.StartedAt.Equal(discoveredStart) {
				return contract.CorrelationUnavailable // PID reuse with a different start
			}
		}
	}
	return contract.CorrelationManagedLaunch
}
