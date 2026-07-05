package mux

import (
	"fmt"
	"sync"
)

var (
	adapters   = make(map[string]Adapter)
	adaptersMu sync.RWMutex
)

// RegisterAdapter adds a new multiplexer adapter.
func RegisterAdapter(a Adapter) {
	adaptersMu.Lock()
	defer adaptersMu.Unlock()
	adapters[a.Name()] = a
}

// GetAdapters returns a list of all registered adapters.
func GetAdapters() []Adapter {
	adaptersMu.RLock()
	defer adaptersMu.RUnlock()
	var list []Adapter
	for _, a := range adapters {
		list = append(list, a)
	}
	return list
}

// FindSession looks across all adapters to find a session by ID.
func FindSession(id string) (Session, error) {
	adaptersMu.RLock()
	defer adaptersMu.RUnlock()
	
	for _, a := range adapters {
		s, err := a.GetSession(id)
		if err == nil && s != nil {
			return s, nil
		}
	}
	return nil, fmt.Errorf("session %s not found in any adapter", id)
}

// GetAllSessions returns all sessions from all registered adapters.
func GetAllSessions() []Session {
	adaptersMu.RLock()
	defer adaptersMu.RUnlock()
	
	var all []Session
	for _, a := range adapters {
		sessions, err := a.ListSessions()
		if err == nil {
			all = append(all, sessions...)
		}
	}
	return all
}
