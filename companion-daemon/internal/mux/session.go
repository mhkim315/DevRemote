package mux

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/creack/pty"
	"github.com/charmbracelet/x/vt"
)

// NativeSession represents a running background PTY process with an in-memory screen buffer.
type NativeSession struct {
	id      string
	Cmd     *exec.Cmd
	PTY     *os.File
	Term    vt.Terminal

	mu        sync.Mutex
	running   bool
	listeners []chan []byte
}

var (
	nativeSessions = make(map[string]*NativeSession)
	nativeMu       sync.Mutex
)

// NewSession spawns a new background process with a PTY and a VT100 emulator attached.
func NewSession(id string, termEnv string, command string, args ...string) (*NativeSession, error) {
	nativeMu.Lock()
	defer nativeMu.Unlock()

	if _, exists := nativeSessions[id]; exists {
		return nil, fmt.Errorf("session %s already exists", id)
	}

	cmd := exec.Command(command, args...)
	
	// Inherit shell environment (API keys, PATH, etc.)
	env := os.Environ()
	// Replace or append TERM
	termFound := false
	for i, e := range env {
		if strings.HasPrefix(e, "TERM=") {
			if termEnv != "" {
				env[i] = "TERM=" + termEnv
			} else {
				env[i] = "TERM=xterm-256color"
			}
			termFound = true
			break
		}
	}
	if !termFound {
		if termEnv != "" {
			env = append(env, "TERM="+termEnv)
		} else {
			env = append(env, "TERM=xterm-256color")
		}
	}
	cmd.Env = env

	// Start the command with a pty.
	ptm, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start pty: %w", err)
	}

	// Create a safe, concurrent virtual terminal emulator
	term := vt.NewSafeEmulator(80, 24)
	term.SetScrollbackSize(10000)

	// MUST drain the terminal's response pipe so it doesn't deadlock on OSC queries
	go io.Copy(io.Discard, term)


	s := &NativeSession{
		id:      id,
		Cmd:     cmd,
		PTY:     ptm,
		Term:    term,
		running: true,
	}

	nativeSessions[id] = s

	// Wait for the command to finish in the background
	go func() {
		defer ptm.Close()

		_ = cmd.Wait()

		s.mu.Lock()
		s.running = false
		s.mu.Unlock()

		nativeMu.Lock()
		delete(nativeSessions, id)
		nativeMu.Unlock()
	}()

	// Feed PTY output into the VT100 emulator AND our JSON scanner
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := ptm.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				term.Write(chunk)

				s.mu.Lock()
				for _, ch := range s.listeners {
					// non-blocking send
					select {
					case ch <- chunk:
					default:
					}
				}
				s.mu.Unlock()
				// For now, we do a basic string match to detect tool usage in the raw stream
				// which allows us to broadcast an event even before the screen updates.
				chunkStr := string(buf[:n])
				if strings.Contains(chunkStr, "\"type\": \"tool_use\"") || strings.Contains(chunkStr, "```") {
					// We would emit to WebSocket here, e.g. term.EmitEvent(id, "tool_detected", "")
				}
			}
			if err != nil {
				break
			}
		}
	}()

	return s, nil
}

// GetSession retrieves an active session by ID.
func GetSession(id string) (*NativeSession, bool) {
	nativeMu.Lock()
	defer nativeMu.Unlock()
	s, ok := nativeSessions[id]
	return s, ok
}

// ListSessions returns a list of all active session IDs.
func ListSessions() []string {
	nativeMu.Lock()
	defer nativeMu.Unlock()
	var list []string
	for id := range nativeSessions {
		list = append(list, id)
	}
	return list
}

func (s *NativeSession) ID() string {
	return s.id
}

// Write writes data to the PTY (injects keystrokes).
func (s *NativeSession) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return 0, fmt.Errorf("session is not running")
	}
	return s.PTY.Write(p)
}

// CaptureScreen returns the current text representation of the screen buffer.
func (s *NativeSession) CaptureScreen() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return ""
	}
	return s.Term.String()
}

// Resize resizes the PTY and the virtual terminal.
func (s *NativeSession) Resize(rows, cols int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return fmt.Errorf("session is not running")
	}

	// Resize the physical PTY
	err := pty.Setsize(s.PTY, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
	if err != nil {
		return fmt.Errorf("failed to resize pty: %w", err)
	}

	// Resize the virtual terminal
	s.Term.Resize(cols, rows)
	return nil
}

// AddListener registers a channel to receive PTY output bytes.
// It also returns the current screen buffer as the first message so the client syncs state.
func (s *NativeSession) AddListener(ch chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, ch)
	// Send current screen state to the new listener
	if s.running {
		// Sending empty string causes issues, we just send a clear screen then the buffer
		screen := s.Term.String()
		if screen != "" {
			go func() {
				// ANSI sequence to clear screen and move cursor to top-left
				ch <- []byte("\033[2J\033[H")
				ch <- []byte(strings.ReplaceAll(screen, "\n", "\r\n"))
			}()
		}
	}
}

// RemoveListener unregisters a channel from receiving PTY output bytes.
func (s *NativeSession) RemoveListener(ch chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, listener := range s.listeners {
		if listener == ch {
			s.listeners = append(s.listeners[:i], s.listeners[i+1:]...)
			break
		}
	}
}

// Close terminates the native session
func (s *NativeSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return fmt.Errorf("session already closed")
	}
	return s.PTY.Close()
}
