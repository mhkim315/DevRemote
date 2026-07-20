package term

import (
	"context"
	"syscall"

	"devremote/companion-daemon/internal/mux"
)

// PB.5b: Platform-neutral PTY boundary. Zero mux in the interface types.

type SpawnConfig struct {
	Name, Command string
	Args          []string
	Executable    string
	CWD           string
	Env           []string
	Rows, Cols    int
}

type PTYHandle interface {
	ID() string
	Resize(rows, cols int) error
	Write(p []byte) (int, error)
	Wait() error
	Signal(sig syscall.Signal) error
	Close() error
}

type ManagedPTYLauncherV1 interface {
	Spawn(ctx context.Context, cfg SpawnConfig) (PTYHandle, error)
}

// NewV1FromOld bridges the old ManagedPTYLauncher to V1.
// The V1 interface is Spawn-only; the old launcher provides the implementation.
func NewV1FromOld(old ManagedPTYLauncher) ManagedPTYLauncherV1 {
	return &v1Bridge{old: old}
}

type v1Bridge struct {
	old ManagedPTYLauncher
}

func (b *v1Bridge) Spawn(ctx context.Context, cfg SpawnConfig) (PTYHandle, error) {
	opts := mux.CreateOptions{
		Name: cfg.Name, Command: cfg.Command,
		Executable: cfg.Executable, Args: cfg.Args, CWD: cfg.CWD,
	}
	_, sess, err := b.old.CreateSessionAndCapture(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &v1Handle{sess: sess}, nil
}

// v1Handle wraps a mux.Session as a PTYHandle.
type v1Handle struct {
	sess mux.Session
}

func (h *v1Handle) ID() string { return h.sess.ID() }
func (h *v1Handle) Resize(rows, cols int) error {
	type r interface{ Resize(int, int) error }
	if rr, ok := h.sess.(r); ok {
		return rr.Resize(rows, cols)
	}
	return nil
}
func (h *v1Handle) Write(p []byte) (int, error) {
	type w interface{ Write([]byte) (int, error) }
	if ww, ok := h.sess.(w); ok {
		return ww.Write(p)
	}
	return len(p), nil
}
func (h *v1Handle) Wait() error {
	type w interface{ Wait() error }
	if ww, ok := h.sess.(w); ok {
		return ww.Wait()
	}
	return nil
}
func (h *v1Handle) Signal(sig syscall.Signal) error {
	type s interface{ Signal(syscall.Signal) error }
	if ss, ok := h.sess.(s); ok {
		return ss.Signal(sig)
	}
	return nil
}
func (h *v1Handle) Close() error {
	type c interface{ Close() error }
	if cc, ok := h.sess.(c); ok {
		return cc.Close()
	}
	return nil
}
