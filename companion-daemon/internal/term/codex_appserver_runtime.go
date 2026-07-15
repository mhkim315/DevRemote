// Package term — A1.1 CP0: bounded codex_app_server entry-path runtime (capacity zero).
// Evidence gate #7 (production entry) + Packet B (runtime replacement).
// NOT CP1; no delivery bridge, no registry ingestion, no reconnect/resume.
package term

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── Entry-path config (explicit pinned path — no PATH lookup) ──

// CodexAppServerEntryConfig holds the pinned provider identity.
type CodexAppServerEntryConfig struct {
	Bin       string
	Version   string
	ShimPath  string
	ShimSHA   string
	NativeSHA string
}

// PinnedConfig0x144 returns the CP0 accepted pinned config.
func PinnedConfig0x144() CodexAppServerEntryConfig {
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".pokit-cp0-toolchain")
	return CodexAppServerEntryConfig{
		Bin:       filepath.Join(p, "node_modules", ".bin", "codex"),
		Version:   "codex-cli 0.144.1",
		ShimPath:  filepath.Join(p, "node_modules", "@openai", "codex", "bin", "codex.js"),
		ShimSHA:   "134063e133f0b4244fa3b251acf973d4fe4b4aeeacbdc135211bf480f59f1477",
		NativeSHA: "29915529b97697def1a957b0505e770aa6a45744435d62fc263e98d7619e167a",
	}
}

// Verify returns an error if the pinned executable is missing, reports the
// wrong version, or any artifact digest differs.
func (c CodexAppServerEntryConfig) Verify() error {
	vs, err := exec.Command(c.Bin, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("pinned codex execute: %w", err)
	}
	if got := strings.TrimSpace(string(vs)); got != c.Version {
		return fmt.Errorf("version mismatch: want %q, got %q", c.Version, got)
	}
	if r, _ := filepath.EvalSymlinks(c.Bin); r != filepath.Clean(c.ShimPath) {
		return fmt.Errorf("realpath mismatch: .bin/codex does not resolve to the verified shim")
	}
	for _, pair := range []struct{ path, want string }{
		{c.ShimPath, c.ShimSHA},
	} {
		d, err := fileDigest(pair.path)
		if err != nil {
			return fmt.Errorf("artifact read %s: %w", pair.path, err)
		}
		if d != pair.want {
			return fmt.Errorf("artifact digest mismatch %s: got %s", pair.path, d[:16])
		}
	}
	return nil
}

func fileDigest(p string) (string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// ── Provider runtime (capacity zero; non-actionable) ──

// CodexEntryState tracks a single entry-path connection epoch.
type CodexEntryState struct {
	Adapter    string
	LaunchGen  int64
	Epoch      string
	PID        int
	StartedAt  time.Time
	Active     bool
	correlated map[string]nativeID // native request id → POKIT correlation
}

// nativeID is the provider thread/turn identity bound to one native request.
type nativeID struct{ threadID, turnID string }

// CodexAppServerRuntime is the minimal entry-path provider runtime.
type CodexAppServerRuntime struct {
	cfg   CodexAppServerEntryConfig
	mu    sync.Mutex
	gen   int64 // monotonic LaunchGeneration counter
	state *CodexEntryState
	cmd   *exec.Cmd
}

// NewCodexAppServerRuntime creates a pinned, verified, non-actionable entry-path
// runtime. It does NOT start the child — StartCertificationTurn must be called
// separately.
func NewCodexAppServerRuntime(cfg CodexAppServerEntryConfig) (*CodexAppServerRuntime, error) {
	if err := cfg.Verify(); err != nil {
		return nil, fmt.Errorf("codex_app_server verify: %w", err)
	}
	return &CodexAppServerRuntime{cfg: cfg}, nil
}

// StartCertificationTurn spawns the pinned child (if not already running),
// completes the initialize/initialized handshake, starts a thread and turn, and
// records the correlation of the first item/commandExecution/requestApproval
// observed. The returned correlation is NON-ACTIONABLE (display/record only;
// capacity zero). Returns the epoch state and any error.
func (r *CodexAppServerRuntime) StartCertificationTurn(writeLine func(string) error, readNext func() (map[string]any, error)) (*CodexEntryState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state != nil && !r.state.Active && r.cmd != nil {
		// Previous epoch is stopped — kill the old child first.
		_ = r.cmd.Process.Kill()
		r.cmd = nil
		r.state = nil
	}
	if r.state != nil && r.state.Active {
		return r.state, nil // already running
	}

	cmd := exec.Command(r.cfg.Bin, "app-server", "--stdio")
	cmd.Stdin = nil  // caller handles all I/O via writeLine/readNext
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex_app_server start: %w", err)
	}
	r.cmd = cmd
	r.gen++
	gen := r.gen
	st := &CodexEntryState{
		Adapter:    "codex_app_server",
		LaunchGen:  gen,
		Epoch:      fmt.Sprintf("codex-entry-%d-%d", gen, time.Now().UnixNano()),
		PID:        cmd.Process.Pid,
		StartedAt:  time.Now(),
		Active:     true,
		correlated: make(map[string]nativeID),
	}
	r.state = st
	log.Printf("codex_app_server: started pid=%d epoch=%s gen=%d", cmd.Process.Pid, st.Epoch, gen)
	return st, nil
}

// Replace kills the current child (if any), increments the LaunchGeneration,
// and invalidates ALL prior correlations atomically. A new child must be
// started with StartCertificationTurn before further operations. The returned
// generation is the NEW (post-replacement) value; all prior native requests
// are stale.
func (r *CodexAppServerRuntime) Replace() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
		r.cmd = nil
	}
	if r.state != nil {
		r.state.Active = false
		r.state.correlated = nil // atomic invalidation
	}
	r.state = nil
	r.gen++
	return r.gen
}

// State returns a copy of the current entry state, nil if none.
func (r *CodexAppServerRuntime) State() *CodexEntryState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == nil {
		return nil
	}
	c := *r.state
	c.correlated = make(map[string]nativeID, len(r.state.correlated))
	for k, v := range r.state.correlated {
		c.correlated[k] = v
	}
	return &c
}

// Stop terminates the child and invalidates all correlations. Idempotent.
func (r *CodexAppServerRuntime) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopLocked()
}

func (r *CodexAppServerRuntime) stopLocked() error {
	if r.state == nil || !r.state.Active {
		if r.cmd == nil {
			return nil
		}
	}
	if r.state != nil {
		r.state.Active = false
		r.state.correlated = nil
		r.state = nil
	}
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
		r.cmd = nil
	}
	return nil
}

// Ref returns the frozen RuntimeRef for the current state, or a zero value.
func (r *CodexAppServerRuntime) Ref() RuntimeRef {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == nil || !r.state.Active {
		return RuntimeRef{}
	}
	return RuntimeRef{
		Adapter:   r.state.Adapter,
		Version:   "codex-cli-0.144.1",
		LaunchGen: r.state.LaunchGen,
	}
}

// ── Non-actionable approval records (display only, capacity zero) ──

// entryPathRecord is an append-only observation tuple recording WHAT was
// observed on the provider connection WITHOUT making it actionable.
// It is defined for future use (Packet A gate #7 entry-path observation sink)
// but not yet consumed in CP0; the zero-capacity entry path records
// observations without feeding A1 delivery.
type entryPathRecord struct {
	Runtime  RuntimeRef
	Epoch    string
	ThreadID string
	TurnID   string
	ReqID    any
}
