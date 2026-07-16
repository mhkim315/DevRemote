package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
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
		t.Fatalf("digest mismatch")
	}

	// Capture the production daemon log sink (Go log package → stderr).
	var logBuf bytes.Buffer
	origWriter := log.Writer()
	log.SetOutput(io.MultiWriter(origWriter, &logBuf))
	defer log.SetOutput(origWriter)

	cfg := term.PinnedClaudeConfigWithDigest(digest)
	cfg.Bin = cfg.PinnedPath

	svc := term.NewManagedClaudeService(cfg, nil, nil)
	store := term.NewApprovalStore()
	if err := svc.SetApprovalStore(store); err != nil {
		t.Fatalf("SetApprovalStore: %v", err)
	}

	ipcDir, err := os.MkdirTemp("/tmp", "c1d-ipc")
	if err != nil {
		t.Fatalf("mkdtemp ipc: %v", err)
	}
	defer os.RemoveAll(ipcDir)
	sock := filepath.Join(ipcDir, "d.sock")

	cwd, err := os.MkdirTemp("/tmp", "c1d-cwd")
	if err != nil {
		t.Fatalf("mkdtemp cwd: %v", err)
	}
	defer os.RemoveAll(cwd)

	reg, _ := mux.NewRegistry()
	ipc, err := term.StartIPCServer(sock, reg, nil, nil, nil, nil, nil, nil, svc)
	if err != nil {
		t.Fatalf("StartIPCServer: %v", err)
	}
	defer ipc.Close()
	defer os.Remove(sock)

	body := buildRunCreateRequest([]string{"claude"}, cwd, true)
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
		t.Fatalf("decode response: %v", err)
	}
	if created["error"] != "" {
		t.Fatalf("create error: %s", created["error"])
	}
	id := created["id"]
	if !strings.HasPrefix(id, "claude_headless:") {
		t.Fatalf("unexpected session ID: %s", id)
	}
	t.Logf("created: %s", id)

	rec, ok := svc.Registry().Get(id)
	if !ok {
		t.Fatal("not in registry")
	}
	if rec.Provider != "claude" || rec.Version != "2.1.209" || rec.Epoch != 1 {
		t.Errorf("binding: provider=%s version=%s epoch=%d", rec.Provider, rec.Version, rec.Epoch)
	}
	if rec.CertifiedDigest != expectedDigest {
		t.Errorf("digest mismatch")
	}

	hookDir := rec.HookDir
	pid := rec.PID
	t.Logf("artifacts: hookDir=%s pid=%d", hookDir, pid)
	if hookDir == "" || pid <= 0 {
		t.Fatal("observability seam: hookDir or PID missing")
	}
	if _, err := os.Stat(hookDir); err != nil {
		t.Errorf("hook dir missing during runtime: %v", err)
	}

	// ── Unique per-run privacy markers ──
	cmdMarker := "echo c1d-probe-ok" // raw command + tool input
	cwdMarker := cwd                 // CWD
	// Hook token: extract the bridge URL from hook.sh.
	// Format: #!/bin/sh\ncurl -s -X POST -d @- '<URL>'\n
	hookScript, _ := os.ReadFile(filepath.Join(hookDir, "hook.sh"))
	hookToken := extractHookURL(string(hookScript))
	t.Logf("hook token: %.40s...", hookToken)

	// Provider payload marker: appears in stream-json deferred_tool_use.input.
	payloadMarker := "c1d-probe-ok"

	allMarkers := []string{cmdMarker, cwdMarker}
	if len(hookToken) > 0 {
		allMarkers = append(allMarkers, hookToken)
	}
	// Note: payloadMarker is same as cmdMarker (both contain c1d-probe-ok).

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
		t.Fatal("timed out waiting for observation")
	}
	t.Logf("observation: id=%s options=%d state=%s", obs.ID, len(obs.Options), obs.State)

	if len(obs.Options) != 0 {
		t.Errorf("non-actionable: got %d options", len(obs.Options))
	}

	// ── Privacy: markers must not appear in any public surface ──

	// IPC create response.
	if strings.Contains(line, cmdMarker) || strings.Contains(line, cwdMarker) {
		t.Error("marker in IPC create response")
	}

	// ListSafe DTO.
	dtoJSON, _ := json.Marshal(obs)
	dtoStr := string(dtoJSON)
	for _, m := range allMarkers {
		if strings.Contains(dtoStr, m) {
			t.Errorf("marker in ListSafe DTO: %.40s...", m)
		}
	}
	// CTA, claim, delivery must be absent.
	if strings.Contains(dtoStr, "claim_token") || strings.Contains(dtoStr, "delivery") {
		t.Error("CTA/claim/delivery material in DTO")
	}

	// List DTO.
	for _, a := range store.List(id) {
		j, _ := json.Marshal(a)
		s := string(j)
		for _, m := range allMarkers {
			if strings.Contains(s, m) {
				t.Errorf("marker in List DTO: %.40s...", m)
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
		t.Errorf("hook dir not removed: %s", hookDir)
	}

	// Direct PID: must not be alive.
	proc, findErr := os.FindProcess(pid)
	if findErr == nil {
		if proc.Signal(syscall.Signal(0)) == nil {
			t.Errorf("PID %d still alive", pid)
		}
	}

	// Process group: only ESRCH proves the group is gone.
	if err := syscall.Kill(-pid, syscall.Signal(0)); err != syscall.ESRCH {
		t.Errorf("process group %d not cleanly reaped: err=%v (want ESRCH)", pid, err)
	}

	// Production wait/reap: registry confirms exited.
	if r, ok := svc.Registry().Get(id); !ok || !r.Exited {
		t.Error("child not marked exited — wait/reap incomplete")
	}

	// Socket.
	if err := os.Remove(sock); err != nil {
		t.Errorf("socket not removable: %v", err)
	}

	// No live records.
	for _, s := range store.ListSafe(id) {
		if s.State == string(term.ApprovalPending) || s.State == string(term.ApprovalExecuting) {
			t.Errorf("live record after stop: %s", s.ID)
		}
	}

	// ── Daemon log privacy ──
	logStr := logBuf.String()
	// Non-vacuous: prove the buffer actually captured daemon log output.
	if !strings.Contains(logStr, "IPC Server listening") {
		t.Error("log capture empty — daemon log sink not observed")
	}
	for _, m := range allMarkers {
		if strings.Contains(logStr, m) {
			t.Errorf("marker in daemon log: %.40s...", m)
		}
	}
	for _, m := range []string{cmdMarker, payloadMarker, cwdMarker} {
		if strings.Contains(logStr, m) {
			t.Errorf("marker in daemon log: %s", m)
		}
	}

	// Also check captured log for credential material.
	for _, secret := range []string{"sk-", "ghp_", "xoxb-", "xoxp-", "Bearer "} {
		if strings.Contains(logStr, secret) {
			t.Errorf("credential in daemon log: %s", secret)
		}
	}

	t.Logf("PASS: provider=%s version=%s epoch=%d", rec.Provider, rec.Version, rec.Epoch)
}

// extractHookURL extracts the bridge URL from a hook script of the form:
// #!/bin/sh\ncurl -s -X POST -d @- '<URL>'\n
func extractHookURL(script string) string {
	// Find the last single-quoted string on the curl line.
	start := strings.LastIndex(script, "'")
	if start < 0 {
		return ""
	}
	end := strings.LastIndex(script[:start], "'")
	if end < 0 {
		return ""
	}
	return script[end+1 : start]
}
