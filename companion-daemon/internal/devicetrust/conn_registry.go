package devicetrust

import (
	"io"
	"sync"
)

// AuthenticatedConnRegistry tracks active authenticated connections per
// device. On device revoke or session replacement, every connection for
// that device is closed promptly. Thread-safe.
type AuthenticatedConnRegistry struct {
	mu         sync.Mutex
	conns      map[string][]io.Closer // deviceID → closers
	authorizer MutationAuthorizer
}

func NewAuthenticatedConnRegistry(authorizer MutationAuthorizer) *AuthenticatedConnRegistry {
	if authorizer == nil {
		panic("authenticated connection mutation authorizer is required")
	}
	return &AuthenticatedConnRegistry{conns: make(map[string][]io.Closer), authorizer: authorizer}
}

// Register adds a connection for a device. Safe to call concurrently.
func (r *AuthenticatedConnRegistry) Register(deviceID string, deviceEpoch uint64, closer io.Closer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.authorizer == nil {
		return ErrNoAuthority
	}
	if err := r.authorizer.AuthorizeCommit(deviceID, deviceEpoch, IntentReconnect); err != nil {
		return err
	}
	if err := r.authorizer.AuthorizeCommit(deviceID, deviceEpoch, IntentReconnect); err != nil {
		return err
	}
	r.conns[deviceID] = append(r.conns[deviceID], closer)
	return nil
}

// Unregister removes a specific connection. Idempotent.
func (r *AuthenticatedConnRegistry) Unregister(deviceID string, closer io.Closer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.conns[deviceID]
	for i, c := range list {
		if c == closer {
			list = append(list[:i], list[i+1:]...)
			if len(list) == 0 {
				delete(r.conns, deviceID)
			} else {
				r.conns[deviceID] = list
			}
			return
		}
	}
}

// Count returns the number of active authenticated connections for a device.
// It is used by lifecycle diagnostics and production-boundary tests.
func (r *AuthenticatedConnRegistry) Count(deviceID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.conns[deviceID])
}

// CloseDevice closes every connection for a device.
func (r *AuthenticatedConnRegistry) CloseDevice(deviceID string) {
	r.mu.Lock()
	list := r.conns[deviceID]
	delete(r.conns, deviceID)
	r.mu.Unlock()
	for _, c := range list {
		c.Close()
	}
}

// CloseAll closes every registered connection (e.g. on daemon shutdown).
func (r *AuthenticatedConnRegistry) CloseAll() {
	r.mu.Lock()
	all := r.conns
	r.conns = make(map[string][]io.Closer)
	r.mu.Unlock()
	for _, list := range all {
		for _, c := range list {
			c.Close()
		}
	}
}
