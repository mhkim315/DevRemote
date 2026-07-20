package term

import (
	"devremote/companion-daemon/internal/mux"
)

// launcherWrapper wraps an mux.Adapter as a ManagedPTYLauncher for tests.
// Returns nil if adapter is nil or doesn't implement the required interfaces.
func launcherWrapper(adapter mux.Adapter) ManagedPTYLauncher {
	if adapter == nil {
		return nil
	}
	l, err := NewControlledPTYLauncher(adapter)
	if err != nil {
		return nil // adapter doesn't support managed PTY launch
	}
	return l
}
