package term

import (
	"sync"

	"devremote/companion-daemon/internal/devicetrust"
)

// CommandBroker stores pending commands keyed by session ID.
// Tests can inject a fake implementation.
type CommandBroker interface {
	PutAuthorized(sessionID string, command []byte, deviceID string, deviceEpoch uint64) error
	// Take retrieves and removes the pending command for a session.
	// Returns nil if no command is pending.
	Take(sessionID string) []byte
}

// NewCommandBroker creates an in-memory CommandBroker.
func NewCommandBroker(authorizer devicetrust.MutationAuthorizer) CommandBroker {
	if authorizer == nil {
		panic("command broker mutation authorizer is required")
	}
	return &commandBroker{
		cmds: make(map[string][]byte), authorizer: authorizer,
	}
}

type commandBroker struct {
	mu         sync.Mutex
	cmds       map[string][]byte
	authorizer devicetrust.MutationAuthorizer
}

func (b *commandBroker) PutAuthorized(sessionID string, command []byte, deviceID string, deviceEpoch uint64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.authorizer.AuthorizeCommit(deviceID, deviceEpoch, devicetrust.IntentCmd); err != nil {
		return err
	}
	// Copy to avoid aliasing the caller's buffer.
	cp := make([]byte, len(command))
	copy(cp, command)
	b.cmds[sessionID] = cp
	return nil
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
