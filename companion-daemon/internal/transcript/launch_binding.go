package transcript

import (
	"sync"

	"devremote/companion-daemon/internal/agent/contract"
)

// LaunchBinding is an immutable session-owned launch authority.
// Only the session creation controller (pokit run) may create one.
// It establishes CorrelationManagedLaunch for controlled sessions.
type LaunchBinding struct {
	SessionID    string
	Provider     string // "codex" or "claude"
	Adapter      string // "controlled_pty"
	Generation   int64  // process generation, monotonic
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
func RegisterLaunch(sessionID, provider, adapter string, generation int64) {
	globalLaunchRegistry.mu.Lock()
	defer globalLaunchRegistry.mu.Unlock()
	if _, exists := globalLaunchRegistry.bindings[sessionID]; exists {
		return // immutable once set
	}
	globalLaunchRegistry.bindings[sessionID] = LaunchBinding{
		SessionID:  sessionID,
		Provider:   provider,
		Adapter:    adapter,
		Generation: generation,
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
func LaunchCorrelation(binding *LaunchBinding, discoveredProvider string) contract.Correlation {
	if binding == nil {
		return contract.CorrelationUnavailable
	}
	// Provider must match the launch binding exactly.
	if binding.Provider != discoveredProvider {
		return contract.CorrelationUnavailable
	}
	return contract.CorrelationManagedLaunch
}
