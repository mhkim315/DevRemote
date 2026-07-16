package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/term"
)

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

	svc := term.NewManagedClaudeService(cfg, nil, nil)
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
		t.Errorf("binding: provider=%s version=%s epoch=%d", rec.Provider, rec.Version, rec.Epoch)
	}
	if rec.CertifiedDigest != expectedDigest {
		t.Errorf("digest: got %s want %s", rec.CertifiedDigest, expectedDigest)
	}

	// Production artifacts recorded during launch.
	hookDir := rec.HookDir
	pid := rec.PID
	t.Logf("C1D-LIVE artifacts: hookDir=%s pid=%d", hookDir, pid)
	if hookDir == "" {
		t.Fatal("hookDir not recorded — observability seam missing")
	}
	if pid <= 0 {
		t.Fatal("PID not recorded — observability seam missing")
	}

	// Hook directory must exist during runtime.
	if _, err := os.Stat(hookDir); err != nil {
		t.Errorf("hook dir not found during runtime: %v", err)
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

	if len(obs.Options) != 0 {
		t.Errorf("non-actionable: got %d options, want 0", len(obs.Options))
	}

	// ── POKIT public log evidence privacy ──
	// ListSafe + List DTOs are the daemon's public API surface.
	// Unique markers: certification prompt "echo c1d-probe-ok" (command) and
	// temp dir path (CWD). Neither must appear in public DTOs.
	dtoJSON, _ := json.Marshal(obs)
	dtoStr := string(dtoJSON)
	for _, secret := range []string{"sk-", "ghp_", "xoxb-", "xoxp-", "Bearer "} {
		if strings.Contains(dtoStr, secret) {
			t.Errorf("credential in ListSafe DTO: %s", secret)
		}
	}
	if strings.Contains(dtoStr, "echo c1d-probe-ok") {
		t.Error("command marker in ListSafe DTO")
	}
	if strings.Contains(dtoStr, dir) {
		t.Error("CWD marker in ListSafe DTO")
	}
	for _, a := range store.List(id) {
		listJSON, _ := json.Marshal(a)
		listStr := string(listJSON)
		if strings.Contains(listStr, "echo c1d-probe-ok") {
			t.Error("command marker in List DTO")
		}
		if strings.Contains(listStr, dir) {
			t.Error("CWD marker in List DTO")
		}
		for _, secret := range []string{"sk-", "ghp_", "xoxb-", "xoxp-", "Bearer "} {
			if strings.Contains(listStr, secret) {
				t.Errorf("credential in List DTO: %s", secret)
			}
		}
	}

	// Stop + Shutdown.
	if err := svc.Stop(id, rec.Epoch); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	ipc.Wait(ctx2)

	// ── Cleanup assertions ──

	// Hook directory: must be removed by terminate().
	if _, err := os.Stat(hookDir); !os.IsNotExist(err) {
		t.Errorf("hook dir not removed after shutdown: %s", hookDir)
	}

	// Child process: must be reaped.
	if proc, err := os.FindProcess(pid); err == nil {
		if proc.Signal(syscall.Signal(0)) == nil {
			t.Errorf("child PID %d still alive after shutdown", pid)
		}
	}

	// Socket.
	if err := os.Remove(sock); err != nil {
		t.Errorf("socket not removable: %v", err)
	}

	// Registry: child must be marked exited.
	if r, ok := svc.Registry().Get(id); !ok || !r.Exited {
		t.Error("child not exited in registry after shutdown")
	}

	// No live records.
	for _, s := range store.ListSafe(id) {
		if s.State == string(term.ApprovalPending) || s.State == string(term.ApprovalExecuting) {
			t.Errorf("live record after stop: id=%s state=%s", s.ID, s.State)
		}
	}

	t.Logf("C1D-LIVE PASS: provider=%s version=%s epoch=%d digest=%s",
		rec.Provider, rec.Version, rec.Epoch, rec.CertifiedDigest)
}
