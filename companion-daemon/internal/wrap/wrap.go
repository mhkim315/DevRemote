package wrap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// IPCState is written to a temp file so the daemon can discover
// this PTY and inject approved responses.
type IPCState struct {
	PID  int    `json:"pid"`
	PTY  string `json:"pty,omitempty"`  // PTY device path (Unix)
	Port int    `json:"port,omitempty"` // localhost port (Windows fallback)
}

// WriteIPC writes the IPC state file so the daemon can discover this wrapper.
func WriteIPC(dir string, state IPCState) (string, error) {
	path := filepath.Join(dir, "devremote_wrap.json")
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("write ipc: %w", err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(state); err != nil {
		return "", err
	}
	return path, nil
}
