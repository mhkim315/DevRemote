package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
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
	t.Logf("C1D-LIVE created: %s", id)

	deadline := time.Now().Add(60 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		safe := store.ListSafe(id)
		if len(safe) == 1 {
			obs := safe[0]
			t.Logf("C1D-LIVE observation: id=%s options=%d state=%s", obs.ID, len(obs.Options), obs.State)
			if len(obs.Options) != 0 {
				t.Errorf("non-actionable must have zero options, got %d", len(obs.Options))
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("timed out waiting for exactly 1 observation")
	}

	rec, ok := svc.Registry().Get(id)
	if !ok {
		t.Fatal("session disappeared before stop")
	}
	if err := svc.Stop(id, rec.Epoch); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	rec2, ok2 := svc.Registry().Get(id)
	if !ok2 || !rec2.Exited {
		t.Fatal("session not exited after stop")
	}

	for _, s := range store.ListSafe(id) {
		if s.State == string(term.ApprovalPending) || s.State == string(term.ApprovalExecuting) {
			t.Errorf("live record after stop: id=%s state=%s", s.ID, s.State)
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

	if err := os.Remove(sock); err != nil {
		t.Errorf("socket cleanup: %v", err)
	}

	t.Logf("C1D-LIVE PASS")
}
