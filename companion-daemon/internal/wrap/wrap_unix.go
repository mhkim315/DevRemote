//go:build !windows

package wrap

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// Command runs the given command inside a PTY, relaying local I/O and
// writing IPC state for daemon injection.
func Command(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()

	// Create PTY.
	tty, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}
	defer tty.Close()

	ptyName, err := ptyName(tty)
	if err != nil {
		return fmt.Errorf("pty name: %w", err)
	}

	// Write IPC state so daemon can inject into this PTY.
	ipcPath, err := WriteIPC(os.TempDir(), IPCState{
		PID: cmd.Process.Pid,
		PTY: ptyName,
	})
	if err != nil {
		log.Printf("wrap: ipc write failed: %v", err)
	} else {
		defer os.Remove(ipcPath)
	}

	// Put local terminal into raw mode.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("make raw: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Handle window size changes.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			if err := pty.InheritSize(os.Stdin, tty); err != nil {
				log.Printf("wrap: resize: %v", err)
			}
		}
	}()
	ch <- syscall.SIGWINCH // initial resize
	defer signal.Stop(ch)
	close(ch)

	// Relay: stdin → PTY.
	go func() {
		io.Copy(tty, os.Stdin)
	}()

	// Relay: PTY → stdout.
	// Also watch for IPC injection file being written by daemon.
	go func() {
		io.Copy(os.Stdout, tty)
	}()

	// Wait for command to exit.
	err = cmd.Wait()
	return err
}

// ptyName returns the PTY slave path for the given master fd.
func ptyName(master *os.File) (string, error) {
	// On Linux/macOS, the slave name for an fd opened via /dev/ptmx
	// can be obtained from /proc/self/fd/<n> on Linux,
	// or via fcntl on macOS. We use a straightforward approach:
	// readlink on /proc/self/fd/<n> gives the pts path.
	return os.Readlink(fmt.Sprintf("/proc/self/fd/%d", master.Fd()))
}
