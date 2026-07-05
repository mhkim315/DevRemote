package mux

import "io"

// Session represents an abstract terminal session that can be viewed and controlled remotely.
type Session interface {
	// ID returns the unique identifier for the session
	ID() string
	
	// AdapterName returns the name of the adapter managing this session
	AdapterName() string
	
	io.ReadWriteCloser
	
	// Resize informs the session that the client's terminal size has changed
	Resize(rows, cols int) error
}

// Adapter defines the interface for different session backends (Native, cmux, tmux)
type Adapter interface {
	// Name returns the identifier of this adapter (e.g., "native", "cmux", "tmux")
	Name() string
	
	// ListSessions returns all active sessions managed by this adapter
	ListSessions() ([]Session, error)
	
	// GetSession returns a specific session by ID
	GetSession(id string) (Session, error)
}
