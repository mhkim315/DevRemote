package mux

import "fmt"

type nativeAdapter struct{}

// NewNativeAdapter returns a Native PTY Adapter
func NewNativeAdapter() Adapter {
	return &nativeAdapter{}
}

func (a *nativeAdapter) Name() string {
	return "native"
}

func (a *nativeAdapter) ListSessions() ([]Session, error) {
	// Native sessions are no longer tracked globally in Phase 4.
	return nil, nil
}

func (a *nativeAdapter) GetSession(id string) (Session, error) {
	// Not supported to fetch detached native sessions anymore.
	return nil, fmt.Errorf("session %s not found in native adapter", id)
}

func init() {
	RegisterAdapter(NewNativeAdapter())
}
