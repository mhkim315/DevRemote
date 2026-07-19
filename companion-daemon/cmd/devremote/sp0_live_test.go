package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devremote/companion-daemon/internal/term"
)

// TestSP0_LiveProductionProof is the single bounded LIVE production proof for
// SP0 (handoff §8): the exact structured CLI request over a real 0600 IPC
// socket → the production managed launch of the pinned Codex app-server →
// the production event pump drives native working → completed, observed only
// through the authenticated REST read surface.
//
// It spends ONE real pinned-provider turn, so it is guarded:
//
//	POKIT_SP0_LIVE=1 go test ./cmd/devremote -run TestSP0_LiveProductionProof -v
func TestSP0_LiveProductionProof(t *testing.T) {
	if os.Getenv("POKIT_SP0_LIVE") != "1" {
		t.Skip("live pinned-provider proof; set POKIT_SP0_LIVE=1 to run once")
	}

	app, err := NewAppWithDeps(Config{InsecureLocalOnly: true, EnableManagedCodex: true}, testDeps())
	if err != nil {
		t.Fatalf("NewAppWithDeps: %v", err)
	}

	// Real IPC server (production StartIPCServer) on a short temp socket.
	dir, err := os.MkdirTemp("/tmp", "sp0")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "d.sock")
	ipc, err := term.StartIPCServer(sock, app.registry, nil, app.lifecycle, app.managed, nil)
	if err != nil {
		t.Fatalf("StartIPCServer: %v", err)
	}
	defer ipc.Close()

	// Authenticated production HTTP surface.
	srv := httptest.NewServer(app.server.Handler)
	defer srv.Close()

	// 1. The EXACT structured request `pokit run --detach codex` sends.
	body := buildRunCreateRequest([]string{"codex"}, dir, true)
	payload, _ := json.Marshal(body)
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial ipc: %v", err)
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		t.Fatalf("write create: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if err != nil && line == "" {
		t.Fatalf("read create response: %v", err)
	}
	var created map[string]string
	if err := json.Unmarshal([]byte(line), &created); err != nil {
		t.Fatalf("decode create response %q: %v", line, err)
	}
	if created["error"] != "" {
		t.Fatalf("managed create failed: %s", created["error"])
	}
	id := created["id"]
	t.Logf("SP0-LIVE created managed session %s at %s", id, time.Now().UTC().Format(time.RFC3339Nano))

	get := func(path string) (int, []byte) {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer dev-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		buf := make([]byte, 1<<20)
		n, _ := resp.Body.Read(buf)
		return resp.StatusCode, buf[:n]
	}

	// 2. Authenticated REST list carries the managed row.
	code, listBody := get("/api/sessions")
	if code != 200 {
		t.Fatalf("list code = %d", code)
	}
	if !json.Valid(listBody) || !containsID(listBody, id) {
		t.Fatalf("managed session missing from authenticated list: %s", listBody)
	}
	t.Logf("SP0-LIVE authenticated list contains %s", id)

	// 3. Observe native working → completed IN ORDER via native-status.
	statusPath := "/api/sessions/" + id + "/native-status"
	sawWorking := false
	var transitions []string
	last := ""
	deadline := time.Now().Add(180 * time.Second)
	for {
		code, body := get(statusPath)
		if code != 200 {
			t.Fatalf("native-status code = %d body=%s", code, body)
		}
		var dto struct {
			NativeStatus string `json:"nativeStatus"`
			LaunchGen    int64  `json:"launchGen"`
			Exited       bool   `json:"exited"`
		}
		if err := json.Unmarshal(body, &dto); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		if dto.NativeStatus != last {
			last = dto.NativeStatus
			transitions = append(transitions, fmt.Sprintf("%s@%s", dto.NativeStatus, time.Now().UTC().Format(time.RFC3339Nano)))
			t.Logf("SP0-LIVE native-status → %s (launchGen=%d exited=%v)", dto.NativeStatus, dto.LaunchGen, dto.Exited)
		}
		if dto.NativeStatus == "working" {
			sawWorking = true
		}
		if dto.NativeStatus == "completed" {
			if !sawWorking {
				t.Fatalf("completed observed without a prior working transition: %v", transitions)
			}
			break
		}
		if dto.NativeStatus == "exited" {
			t.Fatalf("child exited before completing the certification turn: %v", transitions)
		}
		if time.Now().After(deadline) {
			t.Fatalf("no completed within deadline; transitions=%v", transitions)
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Logf("SP0-LIVE ordered transitions: %v", transitions)

	// 4. Shutdown kills and reaps the owned child within the bounded deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.managed.Shutdown(ctx); err != nil {
		t.Fatalf("managed shutdown: %v", err)
	}
	t.Logf("SP0-LIVE managed shutdown clean at %s", time.Now().UTC().Format(time.RFC3339Nano))
}

func containsID(body []byte, id string) bool {
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		return false
	}
	for _, row := range rows {
		if row["id"] == id {
			return true
		}
	}
	return false
}
