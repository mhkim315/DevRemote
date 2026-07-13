package transcript

import (
	"sync"

	"devremote/companion-daemon/internal/agent/contract"
)

// LaunchBinding is an immutable session-owned launch authority.
// Only the session creation controller (pokit run) may create one.
type LaunchBinding struct {
	SessionID          string
	Provider           string // "codex" or "claude"
	Adapter            string // "controlled_pty"
	AcceptedVersion    string // "0.144.1" or "2.1.202"
	ProcessName        string // executable name (codex/claude)
	PID                int    // process ID at launch time
	Generation         int64  // monotonic launch counter per session
}

// LaunchRegistry stores active launch bindings. Only the session creation
// path may register bindings. TelemetryService reads them to establish
// correlation.
type LaunchRegistry struct {
	mu       sync.Mutex
	bindings map[string]LaunchBinding // sessionID → binding
}

var globalLaunchRegistry = &LaunchRegistry{
	bindings: make(map[string]LaunchBinding),
}

// RegisterLaunch records a managed launch binding. Only the session
// creation controller may call this. It establishes that the named
// session was launched under Pokit's control with the given provider.
func RegisterLaunch(sessionID, provider, adapter, version string, pid int, generation int64) {
	globalLaunchRegistry.mu.Lock()
	defer globalLaunchRegistry.mu.Unlock()
	if _, exists := globalLaunchRegistry.bindings[sessionID]; exists {
		return
	}
	globalLaunchRegistry.bindings[sessionID] = LaunchBinding{
		SessionID:       sessionID,
		Provider:        provider,
		Adapter:         adapter,
		AcceptedVersion: version,
		ProcessName:     provider,
		PID:             pid,
		Generation:      generation,
	}
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

// RemoveLaunch clears a launch binding (called on session delete).
func RemoveLaunch(sessionID string) {
	globalLaunchRegistry.mu.Lock()
	defer globalLaunchRegistry.mu.Unlock()
	delete(globalLaunchRegistry.bindings, sessionID)
}

// LaunchCorrelation returns the correlation state for a managed session.
// Validates provider, version, and process identity match.
func LaunchCorrelation(binding *LaunchBinding, discoveredProvider, discoveredVersion string, discoveredPID int) contract.Correlation {
	if binding == nil {
		return contract.CorrelationUnavailable
	}
	if binding.Provider != discoveredProvider {
		return contract.CorrelationUnavailable
	}
	if binding.AcceptedVersion != "" && discoveredVersion != "" && binding.AcceptedVersion != discoveredVersion {
		return contract.CorrelationUnavailable
	}
	if binding.PID != 0 && discoveredPID != 0 && binding.PID != discoveredPID {
		return contract.CorrelationUnavailable
	}
	return contract.CorrelationManagedLaunch
}
