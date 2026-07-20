package term

import (
	"context"
	"syscall"
)

// PB.5b DESIGN: Platform-neutral PTY boundary. Zero mux imports.
// Implementation deferred — ManagedPTYLauncher in pty_launcher.go serves during transition.

type SpawnConfig struct {
	Name, Command string
	Args          []string
	Executable    string
	CWD           string
	Env           []string
	Rows, Cols    int
}

type PTYHandle interface {
	Resize(rows, cols int) error
	Write(p []byte) (int, error)
	Wait() error
	Signal(sig syscall.Signal) error
	Close() error
}

type ManagedPTYLauncherV1 interface {
	Spawn(ctx context.Context, cfg SpawnConfig) (PTYHandle, error)
}
