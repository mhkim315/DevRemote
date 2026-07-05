package mux

// Session represents an abstract terminal session that can be viewed and controlled remotely.
type Session interface {
	// ID returns the unique identifier for the session
	ID() string
	
	// AdapterName returns the name of the adapter managing this session
	AdapterName() string
	
	// Write sends input (keystrokes) to the session's PTY
	Write(p []byte) (n int, err error)
	
	// CaptureScreen returns the current ANSI text representation of the screen buffer
	CaptureScreen() string
	
	// Resize informs the session that the client's terminal size has changed
	Resize(rows, cols int) error
	
	// AddListener registers a channel to receive live PTY output bytes
	AddListener(ch chan []byte)
	
	// RemoveListener unregisters a channel from receiving PTY output bytes
	RemoveListener(ch chan []byte)
	
	// Close terminates the session
	Close() error
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
