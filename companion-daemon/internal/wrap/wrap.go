package wrap

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// IPCState is written to a temp file so the daemon can discover
// this PTY and inject approved responses.
type IPCState struct {
	PID  int    `json:"pid"`
	PTY  string `json:"pty,omitempty"`  // PTY device path (Unix) or name (Windows)
	Port int    `json:"port,omitempty"` // reserved for future socket injection
}

// WriteIPC writes the IPC state file so the daemon can discover this wrapper.
func WriteIPC(dir string, state IPCState) (string, error) {
	path := dir + "/" + "devremote_wrap.json"
	if runtime.GOOS == "windows" {
		path = dir + "\\devremote_wrap.json"
	}
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

// Command runs the given command inside a PTY (ConPTY on Windows, Unix PTY elsewhere).
func Command(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()

	tty, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}
	defer tty.Close()

	ptyPath, err := ptyName(tty, cmd.Process.Pid)
	if err != nil {
		ptyPath = "unknown"
	}

	ipcPath, err := WriteIPC(os.TempDir(), IPCState{
		PID: cmd.Process.Pid,
		PTY: ptyPath,
	})
	if err != nil {
		log.Printf("wrap: ipc write failed: %v", err)
	} else {
		defer os.Remove(ipcPath)
	}

	// Raw terminal mode — works on all platforms.
	// Unix: term.MakeRaw disables line buffering/echo.
	// Windows: sets console to raw input mode for ConPTY passthrough.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("make raw: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Window resize handling.
	resizeLoop(tty)

	// Relay I/O.
	go io.Copy(tty, os.Stdin)
	go io.Copy(os.Stdout, tty)

	return cmd.Wait()
}

func ptyName(master *os.File, pid int) (string, error) {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("conpty-%d", pid), nil
	}
	return os.Readlink(fmt.Sprintf("/proc/self/fd/%d", master.Fd()))
}
