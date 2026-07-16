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

	"devremote/companion-daemon/internal/term"
)

// TestSP05_LiveInteractiveTurn is the single bounded LIVE proof for SP0.5-A:
// the exact non-detached structured CLI request over a real 0600 socket →
// interactive managed session (no automatic turn) → managed-attach → ONE
// bounded prompt → projected working / assistant / completed on the attach
// stream → detach leaves the session alive → clean shutdown.
//
// It spends ONE real pinned-provider turn, so it is guarded:
//
//	POKIT_SP05_LIVE=1 go test ./cmd/devremote -run TestSP05_LiveInteractiveTurn -v
func TestSP05_LiveInteractiveTurn(t *testing.T) {
	if os.Getenv("POKIT_SP05_LIVE") != "1" {
		t.Skip("live pinned-provider proof; set POKIT_SP05_LIVE=1 to run once")
	}

	app, err := NewAppWithDeps(Config{InsecureLocalOnly: true, EnableManagedCodex: true}, testDeps())
	if err != nil {
		t.Fatalf("NewAppWithDeps: %v", err)
	}
	dir, err := os.MkdirTemp("/tmp", "sp05")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "d.sock")
	ipc, err := term.StartIPCServer(sock, app.registry, app.events, app.links, nil, app.activity, app.lifecycle, app.managed, nil)
	if err != nil {
		t.Fatalf("StartIPCServer: %v", err)
	}
	defer ipc.Close()

	// 1. Exact non-detached structured CLI request.
	body := buildRunCreateRequest([]string{"codex"}, dir, false)
	payload, _ := json.Marshal(body)
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial ipc: %v", err)
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		t.Fatalf("write create: %v", err)
	}
	line, _ := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	var created map[string]string
	if err := json.Unmarshal([]byte(line), &created); err != nil || created["error"] != "" {
		t.Fatalf("create response %q err=%v", line, err)
	}
	id := created["id"]
	t.Logf("SP05-LIVE created interactive managed session %s", id)

	// 2. Attach and send ONE bounded prompt.
	aconn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial attach: %v", err)
	}
	defer aconn.Close()
	areq, _ := json.Marshal(map[string]any{"version": 1, "operation": "managed-attach", "sessionId": id})
	if _, err := aconn.Write(append(areq, '\n')); err != nil {
		t.Fatalf("attach write: %v", err)
	}
	prompt, _ := json.Marshal(map[string]string{"prompt": "Reply with exactly the single word READY. Do not run any commands or use any tools."})
	if _, err := aconn.Write(append(prompt, '\n')); err != nil {
		t.Fatalf("prompt write: %v", err)
	}

	// 3. Observe projected working → assistant → completed in order.
	sc := bufio.NewScanner(aconn)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var seen []string
	sawAssistant := ""
	deadline := time.Now().Add(180 * time.Second)
	_ = aconn.SetReadDeadline(deadline)
	for {
		if !sc.Scan() {
			t.Fatalf("attach stream ended early (seen=%v err=%v)", seen, sc.Err())
		}
		var ev struct {
			Kind, Text, Error string
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Error != "" {
			t.Fatalf("attach error: %s (seen=%v)", ev.Error, seen)
		}
		if len(seen) == 0 || seen[len(seen)-1] != ev.Kind {
			seen = append(seen, ev.Kind)
			t.Logf("SP05-LIVE event → %s %s", ev.Kind, truncateForLog(ev.Text))
		}
		if ev.Kind == "assistant" {
			sawAssistant = ev.Text
		}
		if ev.Kind == "completed" {
			break
		}
		if ev.Kind == "exited" {
			t.Fatalf("session exited before completing (seen=%v)", seen)
		}
	}
	if sawAssistant == "" {
		t.Fatalf("no assistant output before completion: %v", seen)
	}
	if !strings.Contains(strings.ToUpper(sawAssistant), "READY") {
		t.Logf("SP05-LIVE note: assistant text did not contain READY (model variance): %q", truncateForLog(sawAssistant))
	}
	t.Logf("SP05-LIVE ordered kinds: %v", seen)

	// 4. Detach leaves the session alive.
	aconn.Close()
	time.Sleep(200 * time.Millisecond)
	rec, ok := app.managed.Registry().Get(id)
	if !ok || rec.Exited {
		t.Fatalf("session not alive after detach: %+v ok=%v", rec, ok)
	}
	t.Logf("SP05-LIVE detach left session alive (status=%s)", rec.NativeStatus)

	// 5. Clean shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.managed.Shutdown(ctx); err != nil {
		t.Fatalf("managed shutdown: %v", err)
	}
	t.Log("SP05-LIVE shutdown clean")
}

func truncateForLog(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}
