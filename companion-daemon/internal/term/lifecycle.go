package term

// M0 — Session lifecycle contract.
//
// Lifecycle state is authoritative in the daemon and is NOT derived from the
// Transcript. This file defines the public contract; the Stop/Kill/Delete
// runtime transitions are implemented in M2. M1 only needs `starting` →
// `running` (and `failed` on spawn error) for the create path.
//
// Process-group ownership audit (needed by M2 Stop/Kill):
//
//	controlled_pty children are spawned via creack/pty's pty.Start, which sets
//	SysProcAttr.Setsid = true. The child therefore becomes a session leader and
//	its own process-group leader (pgid == child pid). Any process the child
//	spawns (e.g. `claude` launched inside the controlled bash) inherits that
//	process group. So a future Stop can terminate the whole daemon-owned
//	controlled PTY tree with a single signal to the negative pgid
//	(syscall.Kill(-pid, SIGTERM), then SIGKILL) — it does not need to walk
//	descendants. MVP Stop terminates the whole group, not a nested agent alone.

// LifecycleState is the public session lifecycle. Clients must be able to
// represent every terminal/lifecycle action without overloading DELETE or
// terminal text input.
type LifecycleState string

const (
	// LifecycleStarting: create accepted, runtime/Recorder not yet ready.
	LifecycleStarting LifecycleState = "starting"
	// LifecycleRunning: process up, Recorder reading, viewers may subscribe.
	LifecycleRunning LifecycleState = "running"
	// LifecycleStopping: graceful termination requested (M2), not yet exited.
	LifecycleStopping LifecycleState = "stopping"
	// LifecycleExited: process ended (natural exit or completed Stop).
	LifecycleExited LifecycleState = "exited"
	// LifecycleKilled: process force-terminated by an explicit Kill.
	LifecycleKilled LifecycleState = "killed"
	// LifecycleFailed: spawn or startup failed.
	LifecycleFailed LifecycleState = "failed"
)

// Valid reports whether s is a known public lifecycle state.
func (s LifecycleState) Valid() bool {
	switch s {
	case LifecycleStarting, LifecycleRunning, LifecycleStopping, LifecycleExited, LifecycleKilled, LifecycleFailed:
		return true
	}
	return false
}

// Terminal reports whether s is an end state (no further lifecycle action
// except Delete). exited/killed/failed are terminal.
func (s LifecycleState) Terminal() bool {
	switch s {
	case LifecycleExited, LifecycleKilled, LifecycleFailed:
		return true
	}
	return false
}

// SessionLifecycle is the create/lookup response DTO. Clients never construct
// canonical IDs — the daemon generates and returns them here. Timestamps and
// exit codes are added in M2 with the Session Catalog; M1 exposes the identity
// and current state so mobile can open the Live Terminal after creation.
type SessionLifecycle struct {
	ID        string         `json:"id"`                  // daemon-generated canonical <adapter>:<local-id>
	Adapter   string         `json:"adapter"`             // controlled_pty for the MVP
	ProfileID string         `json:"profileId,omitempty"` // resolved profile, if created from one
	Name      string         `json:"name,omitempty"`      // display name
	State     LifecycleState `json:"state"`
}
