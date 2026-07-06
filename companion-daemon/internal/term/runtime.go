package term

import (
	"context"

	"devremote/companion-daemon/internal/mux"
)

// Runtime holds the daemon's live dependencies.
// Created in main(), passed to all handlers and services.
// Phase 1 minimal form — will grow in Phase 2+.
type Runtime struct {
	Registry *mux.Registry
}

// R is the active Runtime, set by main() during startup.
// Phase 1 transitional — Phase 2 will inject explicitly.
var R *Runtime

// Sessions returns cached sessions using the active Runtime.
func Sessions(ctx context.Context) []mux.Session {
	return R.Registry.Sessions(ctx)
}

// FindSession looks up a session in the active Runtime's Registry.
func FindSession(id string) (mux.Session, error) {
	return R.Registry.FindSession(id)
}

// MigrateLegacyID delegates to the mux package function.
func MigrateLegacyID(id string) string {
	return mux.MigrateLegacyID(id)
}
