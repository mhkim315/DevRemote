package mux

// NewSession connects to or creates a tmux session.
// SpawnPTY/SpawnPTYWithDir/NativeSession are in pty_spawn.go (RETAINED).
func NewSession(id string, termEnv string, command string, args ...string) (Session, error) {
	// If the client asks to create/connect, we just run `tmux new-session -A -s id`
	// This will attach if it exists, or create if it doesn't.
	return SpawnPTY(id, termEnv, "tmux", "new-session", "-A", "-s", id)
}
