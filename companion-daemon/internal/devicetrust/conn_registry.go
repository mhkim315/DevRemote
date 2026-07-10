package devicetrust

import (
	"io"
	"sync"
)

// AuthenticatedConnRegistry tracks active authenticated connections per
// device. On device revoke or session replacement, every connection for
// that device is closed promptly. Thread-safe.
type AuthenticatedConnRegistry struct {
	mu    sync.Mutex
	conns map[string][]io.Closer // deviceID → closers
}

func NewAuthenticatedConnRegistry() *AuthenticatedConnRegistry {
	return &AuthenticatedConnRegistry{conns: make(map[string][]io.Closer)}
}

// Register adds a connection for a device. Safe to call concurrently.
func (r *AuthenticatedConnRegistry) Register(deviceID string, closer io.Closer) {
	r.mu.Lock()
	r.conns[deviceID] = append(r.conns[deviceID], closer)
	r.mu.Unlock()
}

// Unregister removes a specific connection. Idempotent.
func (r *AuthenticatedConnRegistry) Unregister(deviceID string, closer io.Closer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.conns[deviceID]
	for i, c := range list {
		if c == closer {
			r.conns[deviceID] = append(list[:i], list[i+1:]...)
			return
		}
	}
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
