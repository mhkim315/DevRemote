package term

import (
	"sync"
)

// CommandBroker stores pending commands keyed by session ID.
// Tests can inject a fake implementation.
type CommandBroker interface {
	// Put stores a command for the given session (overwrites any existing).
	Put(sessionID string, command []byte)
	// Take retrieves and removes the pending command for a session.
	// Returns nil if no command is pending.
	Take(sessionID string) []byte
}

// NewCommandBroker creates an in-memory CommandBroker.
func NewCommandBroker() CommandBroker {
	return &commandBroker{
		cmds: make(map[string][]byte),
	}
}

type commandBroker struct {
	mu   sync.Mutex
	cmds map[string][]byte
}

func (b *commandBroker) Put(sessionID string, command []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Copy to avoid aliasing the caller's buffer.
	cp := make([]byte, len(command))
	copy(cp, command)
	b.cmds[sessionID] = cp
}

func (b *commandBroker) Take(sessionID string) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	cmd := b.cmds[sessionID]
	if cmd == nil {
		return nil
	}
	delete(b.cmds, sessionID)
	// Return a copy so the caller doesn't alias internal state.
	cp := make([]byte, len(cmd))
	copy(cp, cmd)
	return cp
}
