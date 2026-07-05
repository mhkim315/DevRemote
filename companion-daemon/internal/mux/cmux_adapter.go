package mux

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type cmuxAdapter struct {
	mu       sync.Mutex
	sessions map[string]*CmuxSession
}

func NewCmuxAdapter() Adapter {
	return &cmuxAdapter{
		sessions: make(map[string]*CmuxSession),
	}
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
	a.mu.Lock()
	defer a.mu.Unlock()

	panels, err := TrackCmuxPanels()
	if err == nil {
		// Update internal sessions map
		for _, p := range panels {
			if _, exists := a.sessions[p.ID]; !exists {
				a.sessions[p.ID] = &CmuxSession{
					id:  p.ID,
					tty: p.TTY,
					pid: p.PID,
				}
			}
		}
	}

	var list []Session
	for _, s := range a.sessions {
		list = append(list, s)
	}
	return list, nil
}

func (a *cmuxAdapter) GetSession(id string) (Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if s, ok := a.sessions[id]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("session %s not found in cmux adapter", id)
}

// CmuxSession implements the Session interface for a cmux panel
type CmuxSession struct {
	id        string
	tty       string
	pid       int
	listeners []chan []byte
	mu        sync.Mutex
}

func (s *CmuxSession) ID() string {
	return s.id
}

func (s *CmuxSession) AdapterName() string {
	return "cmux"
}

func (s *CmuxSession) Write(p []byte) (n int, err error) {
	// Try cmux send-panel first, but for arbitrary bytes (ANSI sequences, arrow keys),
	// tmux load-buffer + paste-buffer is much safer and handles raw bytes perfectly.
	
	// We'll use the tmux fallback since we are using tmux for listing anyway in TrackCmuxPanels
	cmd := exec.Command("tmux", "load-buffer", "-b", "devremote_"+s.id, "-")
	cmd.Stdin = bytes.NewReader(p)
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("failed to load buffer: %w", err)
	}

	cmd2 := exec.Command("tmux", "paste-buffer", "-d", "-b", "devremote_"+s.id, "-t", s.id)
	if err := cmd2.Run(); err != nil {
		return 0, fmt.Errorf("failed to paste buffer: %w", err)
	}
	return len(p), nil
}

func (s *CmuxSession) CaptureScreen() string {
	cmd := exec.Command("cmux", "read-screen", "--surface", s.id)
	out, err := cmd.Output()
	if err != nil {
		// Fallback to tmux
		cmd = exec.Command("tmux", "capture-pane", "-p", "-e", "-t", s.id)
		out, _ = cmd.Output()
	}
	return string(out)
}

func (s *CmuxSession) Resize(rows, cols int) error {
	// cmux manages its own layout, so resizing from mobile isn't strictly allowed or might break the desktop.
	// We do nothing and return nil to avoid breaking mobile clients.
	return nil
}

func (s *CmuxSession) AddListener(ch chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, ch)
	
	// Start a simple polling loop to send screen updates (Live Stream approximation)
	go func() {
		lastScreen := ""
		for {
			// Check if listener is still attached
			s.mu.Lock()
			active := false
			for _, l := range s.listeners {
				if l == ch {
					active = true
					break
				}
			}
			s.mu.Unlock()
			if !active {
				break
			}

			screen := s.CaptureScreen()
			if screen != "" && screen != lastScreen {
				ch <- []byte("\033[2J\033[H")
				ch <- []byte(strings.ReplaceAll(screen, "\n", "\r\n"))
				lastScreen = screen
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

func (s *CmuxSession) RemoveListener(ch chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, listener := range s.listeners {
		if listener == ch {
			s.listeners = append(s.listeners[:i], s.listeners[i+1:]...)
			break
		}
	}
}

func (s *CmuxSession) Close() error {
	// Closing a cmux session from mobile might not be desired, but if it is:
	// cmux send-key-panel --panel s.id <C-d> or similar, or just leave it.
	return fmt.Errorf("closing cmux panel remotely not supported yet")
}

func init() {
	RegisterAdapter(NewCmuxAdapter())
}
