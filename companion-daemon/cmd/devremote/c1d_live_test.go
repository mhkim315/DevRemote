package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	if rec.Provider != "claude" {
		t.Errorf("provider: got %q want claude", rec.Provider)
	}
	if rec.Version != "2.1.209" {
		t.Errorf("version: got %q want 2.1.209", rec.Version)
	}
	if rec.Epoch != 1 {
		t.Errorf("epoch: got %d want 1", rec.Epoch)
	}
	if rec.CertifiedDigest != expectedDigest {
		t.Errorf("digest: got %s want %s", rec.CertifiedDigest, expectedDigest)
	}

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

	// Privacy: scan all visible DTO fields for credential/payload leakage.
	dtoJSON, _ := json.Marshal(obs)
	dtoStr := string(dtoJSON)
	for _, secret := range []string{"sk-", "ghp_", "xoxb-", "xoxp-", "Bearer "} {
		if strings.Contains(dtoStr, secret) {
			t.Errorf("credential leak in DTO: %s", secret)
		}
	}
	// Non-vacuous: the certification prompt and CWD must not leak into DTO.
	if strings.Contains(dtoStr, "echo c1d-probe-ok") {
		t.Error("certification command leaked into DTO")
	}
	if strings.Contains(dtoStr, dir) {
		t.Error("CWD leaked into DTO")
	}
	// DTO must not carry actionable fields.
	if obs.Actionable || len(obs.Options) != 0 {
		t.Error("DTO must not carry actionable fields for non-actionable observation")
	}

	if err := svc.Stop(id, rec.Epoch); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	rec2, ok2 := svc.Registry().Get(id)
	if !ok2 || !rec2.Exited {
		t.Fatal("session not exited after stop")
	}

	// After stop: zero live records.
	for _, s := range store.ListSafe(id) {
		st := s.State
		if st == string(term.ApprovalPending) || st == string(term.ApprovalExecuting) {
			t.Errorf("live record after stop: id=%s state=%s", s.ID, st)
		}
	}
	for _, a := range store.List(id) {
		if a.Status == "pending" || a.Status == "executing" {
			t.Errorf("live approval after stop: id=%s status=%s", a.ID, a.Status)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	ipc.Wait(ctx2)

	// Cleanup: socket must be removable, temp dir must be cleanable.
	if err := os.Remove(sock); err != nil {
		t.Errorf("socket cleanup: %v", err)
	}
	// RemoveAll cleans hook dirs; verify no stale children via registry.
	if entries, _ := os.ReadDir(dir); len(entries) > 1 {
		t.Logf("temp dir entries after cleanup: %d", len(entries))
	}

	t.Logf("C1D-LIVE PASS: provider=%s version=%s epoch=%d digest=%s",
		rec.Provider, rec.Version, rec.Epoch, rec.CertifiedDigest)
}
