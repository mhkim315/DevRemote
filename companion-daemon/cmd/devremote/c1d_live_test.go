package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/term"
)

// captureLauncher tees the child's stdout into a buffer while piping it
// for the pump, so we can scan captured output for privacy leaks.
type captureLauncher struct {
	captured bytes.Buffer
}

func (l *captureLauncher) Launch(exe string, argv []string) (term.ManagedProcess, error) {
	cmd := exec.Command(exe, argv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &captureProcess{
		cmd:    cmd,
		stdin:  stdin,
		stdout: io.TeeReader(stdout, &l.captured),
	}, nil
}

func (l *captureLauncher) LaunchInDir(exe string, argv []string, cwd string) (term.ManagedProcess, error) {
	cmd := exec.Command(exe, argv...)
	cmd.Dir = cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &captureProcess{
		cmd:    cmd,
		stdin:  stdin,
		stdout: io.TeeReader(stdout, &l.captured),
	}, nil
}

type captureProcess struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.Reader
	termMu  sync.Mutex
	termErr error
}

func (p *captureProcess) Stdin() io.Writer  { return p.stdin }
func (p *captureProcess) Stdout() io.Reader { return p.stdout }
func (p *captureProcess) Term() error {
	p.termMu.Lock()
	defer p.termMu.Unlock()
	if p.termErr == nil {
		p.termErr = p.cmd.Process.Signal(syscall.SIGTERM)
	}
	return p.termErr
}
func (p *captureProcess) Kill() error {
	p.termMu.Lock()
	defer p.termMu.Unlock()
	if p.termErr == nil {
		p.termErr = p.cmd.Process.Kill()
	}
	return p.termErr
}
func (p *captureProcess) Wait() error      { return p.cmd.Wait() }
func (p *captureProcess) OpaqueID() string { return "captured" }

func TestC1D_LiveProductionProof(t *testing.T) {
	digest := os.Getenv("POKIT_CLAUDE_DIGEST")
	if digest == "" {
		t.Skip("POKIT_CLAUDE_DIGEST not set")
	}
	const expectedDigest = "59d2de7f49db2f75d5c33bbb46a6b8f288ad24d40b61e30602a502bb7ddc380c"
	if digest != expectedDigest {
		t.Fatalf("POKIT_CLAUDE_DIGEST mismatch: got %s, want %s", digest, expectedDigest)
	}

	cfg := term.PinnedClaudeConfigWithDigest(digest)
	cfg.Bin = cfg.PinnedPath

	launcher := &captureLauncher{}
	svc := term.NewManagedClaudeService(cfg, launcher, nil)
	store := term.NewApprovalStore()
	if err := svc.SetApprovalStore(store); err != nil {
		t.Fatalf("SetApprovalStore: %v", err)
	}

	dir, err := os.MkdirTemp("/tmp", "c1d-live")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "d.sock")

	reg, _ := mux.NewRegistry()
	ipc, err := term.StartIPCServer(sock, reg, nil, nil, nil, nil, nil, nil, svc)
	if err != nil {
		t.Fatalf("StartIPCServer: %v", err)
	}
	defer ipc.Close()
	defer os.Remove(sock)

	// Use the exact production CLI serializer.
	body := buildRunCreateRequest([]string{"claude"}, dir, true)
	payload, _ := json.Marshal(body)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial ipc: %v", err)
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		conn.Close()
		t.Fatalf("write create: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if err != nil && line == "" {
		t.Fatalf("read create response: %v", err)
	}

	var created map[string]string
	if err := json.Unmarshal([]byte(line), &created); err != nil {
		t.Fatalf("decode response %q: %v", line, err)
	}
	if created["error"] != "" {
		t.Fatalf("create error: %s", created["error"])
	}
	id := created["id"]
	if !strings.HasPrefix(id, "claude_headless:") {
		t.Fatalf("unexpected session ID: %s", id)
	}
	t.Logf("C1D-LIVE created: %s", id)

	rec, ok := svc.Registry().Get(id)
	if !ok {
		t.Fatal("session not in registry")
	}
	if rec.Provider != "claude" || rec.Version != "2.1.209" || rec.Epoch != 1 {
		t.Errorf("binding mismatch: provider=%s version=%s epoch=%d", rec.Provider, rec.Version, rec.Epoch)
	}
	if rec.CertifiedDigest != expectedDigest {
		t.Errorf("digest: got %s want %s", rec.CertifiedDigest, expectedDigest)
	}

	// Bounded poll for exactly one observation.
	deadline := time.Now().Add(60 * time.Second)
	var obs term.SafeApprovalDTO
	var found bool
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		safe := store.ListSafe(id)
		if len(safe) == 1 {
			obs = safe[0]
			found = true
			break
		}
	}
	if !found {
		t.Fatal("timed out waiting for exactly 1 observation")
	}
	t.Logf("C1D-LIVE observation: id=%s options=%d state=%s", obs.ID, len(obs.Options), obs.State)

	// Non-actionable proof.
	if len(obs.Options) != 0 {
		t.Errorf("non-actionable: got %d options, want 0", len(obs.Options))
	}

	// DTO privacy: scan all fields.
	dtoJSON, _ := json.Marshal(obs)
	dtoStr := string(dtoJSON)
	for _, secret := range []string{"sk-", "ghp_", "xoxb-", "xoxp-", "Bearer "} {
		if strings.Contains(dtoStr, secret) {
			t.Errorf("credential leak in DTO: %s", secret)
		}
	}
	if strings.Contains(dtoStr, "echo c1d-probe-ok") {
		t.Error("certification command leaked into DTO")
	}
	if strings.Contains(dtoStr, dir) {
		t.Error("CWD leaked into DTO")
	}

	// Stop.
	if err := svc.Stop(id, rec.Epoch); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	ipc.Wait(ctx2)

	// Cleanup: socket.
	if err := os.Remove(sock); err != nil {
		t.Errorf("socket not removable: %v", err)
	}

	// Cleanup: no leftover hook directories.
	hooks, _ := filepath.Glob("/tmp/pokit-claude-hooks-*")
	if len(hooks) > 0 {
		t.Errorf("hook directories not cleaned up: %v", hooks)
		for _, h := range hooks {
			os.RemoveAll(h)
		}
	}

	// Log privacy: captured Claude stream-json stdout must not contain
	// credential material. CWD and the certification prompt appear in
	// Claude's own output (stream-json cwd field, -p flag) — that is
	// provider-originated, not a POKIT DTO leak. The DTO privacy check
	// above already confirmed these are absent from the public surface.
	captured := launcher.captured.String()
	for _, secret := range []string{"sk-", "ghp_", "xoxb-", "xoxp-", "Bearer "} {
		if strings.Contains(captured, secret) {
			t.Errorf("credential found in captured stdout: %s", secret)
		}
	}
	_ = captured

	// No live records after stop+shutdown.
	for _, s := range store.ListSafe(id) {
		if s.State == string(term.ApprovalPending) || s.State == string(term.ApprovalExecuting) {
			t.Errorf("live record after stop: id=%s state=%s", s.ID, s.State)
		}
	}

	t.Logf("C1D-LIVE PASS: provider=%s version=%s epoch=%d digest=%s",
		rec.Provider, rec.Version, rec.Epoch, rec.CertifiedDigest)
}
