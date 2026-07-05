package mux

type cmuxAdapter struct{}

func NewCmuxAdapter() Adapter {
	return &cmuxAdapter{}
}

func (a *cmuxAdapter) Name() string {
	return "cmux"
}

// CmuxPanelInfo represents the data we get from cmux list-panels or similar
type CmuxPanelInfo struct {
	ID    string
	Title string
	TTY   string
	PID   int
}

func (a *cmuxAdapter) ListSessions() ([]Session, error) {
	return nil, nil
}

func (a *cmuxAdapter) GetSession(id string) (Session, error) {
	// Fallback for CmuxAdapter using the same Phase 4 approach
	// Assuming cmux panels are accessible via tmux
	return SpawnPTY(id, "xterm-256color", "tmux", "attach", "-t", id)
}

func init() {
	RegisterAdapter(NewCmuxAdapter())
}
