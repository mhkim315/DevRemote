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
	nativeMu.Lock()
	defer nativeMu.Unlock()
	
	var list []Session
	for _, s := range nativeSessions {
		list = append(list, s)
	}
	return list, nil
}

func (a *nativeAdapter) GetSession(id string) (Session, error) {
	nativeMu.Lock()
	defer nativeMu.Unlock()
	
	if s, ok := nativeSessions[id]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("session %s not found in native adapter", id)
}

func init() {
	RegisterAdapter(NewNativeAdapter())
}
