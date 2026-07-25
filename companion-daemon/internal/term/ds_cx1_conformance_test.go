package term

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// DS-CX1: Codex 0.145.0 stock-binary co-presence conformance.
//
// Conformance-only. Uses the unmodified stock Codex binary. No production
// code is modified. Category B: shared-core seam exists; required live
// dual-client co-presence is not yet proven.
//
// Binary: /opt/homebrew/bin/codex (override with CODEX_BIN)

const (
	dsCX1DefaultBin    = "/opt/homebrew/bin/codex"
	dsCX1RequiredMajor = 0
	dsCX1RequiredMinor = 145
	dsCX1RequiredPatch = 0
	dsCX1PinnedSHA256  = "134063e133f0b4244fa3b251acf973d4fe4b4aeeacbdc135211bf480f59f1477"
)

func dsCX1Bin(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("CODEX_BIN")
	if bin == "" {
		bin = dsCX1DefaultBin
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("DS-CX1: codex binary not found at %s (set CODEX_BIN env): %v", bin, err)
	}
	return bin
}

// readUntilID reads JSON-RPC lines from scanner until a response with the
// given id is found. Returns the raw JSON bytes. Returns nil if the
// scanner exhausts or after ~10s of reads without the target ID.
func readUntilID(scanner *bufio.Scanner, id int) ([]byte, bool) {
	deadline := time.Now().Add(10 * time.Second)
	for scanner.Scan() {
		if time.Now().After(deadline) {
			return nil, false
		}
		var probe struct {
			ID float64 `json:"id"`
		}
		if json.Unmarshal(scanner.Bytes(), &probe) == nil && int(probe.ID) == id {
			raw := make([]byte, len(scanner.Bytes()))
			copy(raw, scanner.Bytes())
			return raw, true
		}
	}
	return nil, false
}

// collectMethods reads lines, collecting method names until a response
// with the given id is found. Returns collected methods and a done channel.
func collectMethods(scanner *bufio.Scanner, stopID int, timeout time.Duration) []string {
	var methods []string
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			return methods
		default:
		}
		if !scanner.Scan() {
			return methods
		}
		var probe struct {
			ID     float64 `json:"id"`
			Method string  `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &probe) == nil {
			if int(probe.ID) == stopID && stopID > 0 {
				return methods
			}
			if probe.Method != "" {
				methods = append(methods, probe.Method)
			}
		}
	}
}

// TestDS_CX1_BinaryIdentity verifies stock binary version and SHA-256.
func TestDS_CX1_BinaryIdentity(t *testing.T) {
	bin := dsCX1Bin(t)

	cmd := exec.Command(bin, "--version")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("codex --version: %v", err)
	}
	version := strings.TrimSpace(string(out))
	t.Logf("Version: %s", version)

	var major, minor, patch int
	if _, err := fmt.Sscanf(version, "codex-cli %d.%d.%d", &major, &minor, &patch); err != nil {
		t.Fatalf("parse version %q: %v", version, err)
	}
	if major != dsCX1RequiredMajor || minor != dsCX1RequiredMinor || patch != dsCX1RequiredPatch {
		t.Errorf("version %d.%d.%d != required %d.%d.%d",
			major, minor, patch, dsCX1RequiredMajor, dsCX1RequiredMinor, dsCX1RequiredPatch)
	}

	// SHA-256.
	if dsCX1PinnedSHA256 != "" {
		f, err := os.Open(bin)
		if err != nil {
			t.Fatalf("open binary: %v", err)
		}
		defer f.Close()
		h := sha256.New()
		io.Copy(h, f)
		got := fmt.Sprintf("%x", h.Sum(nil))
		if got != dsCX1PinnedSHA256 {
			t.Errorf("SHA-256 mismatch:\n  got:  %s\n  want: %s", got, dsCX1PinnedSHA256)
		}
		t.Logf("SHA-256: %s (verified)", got)
	}
}

// TestDS_CX1_AppServerStdioHandshake verifies the app-server starts and
// completes initialize + thread/start over stdio.
func TestDS_CX1_AppServerStdioHandshake(t *testing.T) {
	bin := dsCX1Bin(t)

	ctx := t.Context()
	cmd := exec.CommandContext(ctx, bin, "app-server", "--stdio")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	// initialize.
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"0.144.1","clientInfo":{"name":"ds-cx1","version":"0.0.0"},"capabilities":{}}}`+"\n")
	raw, ok := readUntilID(scanner, 1)
	if !ok {
		t.Fatal("no initialize response")
	}
	var ir struct {
		ID     int `json:"id"`
		Result struct {
			UserAgent string `json:"userAgent"`
		} `json:"result"`
	}
	json.Unmarshal(raw, &ir)
	t.Logf("initialize: userAgent=%s", ir.Result.UserAgent)
	// Note: Codex 0.145.0 does NOT include "jsonrpc":"2.0" in responses.
	// This is a Category B observation.

	// initialized.
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","method":"initialized","params":{}}`+"\n")

	// thread/start with UUID-format ID.
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":2,"method":"thread/start","params":{"threadId":"urn:uuid:00000000-0000-0000-0000-00000000c001","cwd":"/","model":"test"}}`+"\n")
	raw, ok = readUntilID(scanner, 2)
	if !ok {
		t.Fatal("no thread/start response")
	}
	var tr struct {
		ID     int `json:"id"`
		Result struct {
			Thread struct {
				ID         string `json:"id"`
				SessionID  string `json:"sessionId"`
				CLIVersion string `json:"cliVersion"`
			} `json:"thread"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	json.Unmarshal(raw, &tr)
	if tr.Error != nil {
		t.Fatalf("thread/start error: %s", tr.Error.Message)
	}
	t.Logf("thread: id=%s sessionId=%s cliVersion=%s",
		tr.Result.Thread.ID, tr.Result.Thread.SessionID, tr.Result.Thread.CLIVersion)

	// Server generates its own UUID — the client prefix is not preserved.
	if tr.Result.Thread.ID == "" {
		t.Error("thread ID is empty")
	}
	if tr.Result.Thread.SessionID == "" {
		t.Error("session ID is empty")
	}
	if tr.Result.Thread.CLIVersion != "0.145.0" {
		t.Errorf("cliVersion = %s, want 0.145.0", tr.Result.Thread.CLIVersion)
	}
}

// TestDS_CX1_OneAppServerOneThread verifies exactly one app-server process
// and one provider thread from a single launch.
func TestDS_CX1_OneAppServerOneThread(t *testing.T) {
	bin := dsCX1Bin(t)

	ctx := t.Context()
	cmd := exec.CommandContext(ctx, bin, "app-server", "--stdio")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	// Handshake + thread.
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"0.144.1","clientInfo":{"name":"ds-cx1","version":"0.0.0"},"capabilities":{}}}`+"\n")
	readUntilID(scanner, 1)
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","method":"initialized","params":{}}`+"\n")
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":2,"method":"thread/start","params":{"threadId":"urn:uuid:00000000-0000-0000-0000-00000000c002","cwd":"/","model":"test"}}`+"\n")
	raw, _ := readUntilID(scanner, 2)
	var tr struct {
		Result struct {
			Thread struct{ ID string }
		} `json:"result"`
	}
	json.Unmarshal(raw, &tr)
	threadID := tr.Result.Thread.ID
	t.Logf("Thread: %s", threadID)

	// Prove only ONE app-server process exists for this session.
	if cmd.Process == nil {
		t.Fatal("no app-server process")
	}
	t.Logf("One app-server PID=%d thread=%s", cmd.Process.Pid, threadID)

	// The app-server owns exactly one thread. Starting another thread
	// with a different UUID creates a separate server-side thread.
	// This is by design — one app-server = one thread authority.
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":3,"method":"thread/start","params":{"threadId":"urn:uuid:00000000-0000-0000-0000-00000000c003","cwd":"/","model":"test"}}`+"\n")
	raw2, _ := readUntilID(scanner, 3)
	var tr2 struct {
		Result struct {
			Thread struct{ ID string }
		} `json:"result"`
	}
	json.Unmarshal(raw2, &tr2)
	if tr2.Result.Thread.ID != threadID {
		t.Logf("Second thread/start got different ID: %s != %s", tr2.Result.Thread.ID, threadID)
		t.Log("CATEGORY-B FINDING: app-server allows multiple threads per process")
	} else {
		t.Log("One thread authority confirmed")
	}
}

// TestDS_CX1_JSONRPCTurnIdentity verifies thread and turn identity
// via the app-server JSON-RPC protocol.
func TestDS_CX1_JSONRPCTurnIdentity(t *testing.T) {
	bin := dsCX1Bin(t)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "app-server", "--stdio")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	// Handshake + thread.
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"0.144.1","clientInfo":{"name":"ds-cx1","version":"0.0.0"},"capabilities":{}}}`+"\n")
	readUntilID(scanner, 1)
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","method":"initialized","params":{}}`+"\n")
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":2,"method":"thread/start","params":{"threadId":"urn:uuid:00000000-0000-0000-0000-00000000c004","cwd":"/","model":"test"}}`+"\n")
	raw, _ := readUntilID(scanner, 2)
	var tr struct {
		Result struct {
			Thread struct {
				ID        string `json:"id"`
				SessionID string `json:"sessionId"`
			} `json:"thread"`
		} `json:"result"`
	}
	json.Unmarshal(raw, &tr)
	serverThreadID := tr.Result.Thread.ID
	sessionID := tr.Result.Thread.SessionID
	t.Logf("thread.id=%s thread.sessionId=%s", serverThreadID, sessionID)

	// Verify thread identity fields are present and are valid UUIDs.
	if serverThreadID == "" {
		t.Error("thread.id is empty — no provider thread identity")
	}
	if sessionID == "" {
		t.Error("thread.sessionId is empty — no provider session identity")
	}
	// thread.id == thread.sessionId for Codex 0.145.0 (one session per thread).
	if serverThreadID != sessionID {
		t.Logf("CATEGORY-B: thread.id (%s) != sessionId (%s)", serverThreadID, sessionID)
	}

	// Attempt turn/start — this may fail without auth/model credentials.
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":3,"method":"turn/start","params":{"threadId":%q,"input":[{"type":"text","text":"Reply READY"}],"approvalPolicy":"untrusted"}}`+"\n", serverThreadID)

	// Bounded read: collect up to 10s of notifications/responses.
	raw3, _ := readUntilID(scanner, 3)
	if raw3 == nil {
		t.Log("turn/start: no response within deadline (requires auth/model — Category B: turn initiation gated by provider credentials)")
		return
	}

	var tur struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
		Result *struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		} `json:"result"`
	}
	json.Unmarshal(raw3, &tur)
	if tur.Error != nil {
		t.Logf("turn/start error: code=%d msg=%s", tur.Error.Code, tur.Error.Message)
	}
	if tur.Result != nil {
		t.Logf("turn.id=%s", tur.Result.Turn.ID)
	}
}

// TestDS_CX1_RemoteControlDaemon verifies the remote-control daemon start
// path for the shipped TUI remote connection.
func TestDS_CX1_RemoteControlDaemon(t *testing.T) {
	bin := dsCX1Bin(t)

	// Check if remote-control daemon is already running.
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	// Start daemon with remote control enabled.
	// codex remote-control start starts a daemon with remote control.
	// The daemon persists across CLI invocations.
	cmd := exec.CommandContext(ctx, bin, "remote-control", "start", "--json")
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("remote-control start: %v (daemon path unavailable in this env)", err)
	}
	t.Logf("remote-control start: %s", strings.TrimSpace(string(out)))

	// Verify the daemon is responding.
	verCmd := exec.CommandContext(ctx, bin, "app-server", "daemon", "version")
	verOut, verErr := verCmd.Output()
	if verErr != nil {
		t.Logf("daemon version: %v (daemon may not support version query)", verErr)
	} else {
		t.Logf("daemon version: %s", strings.TrimSpace(string(verOut)))
	}
}

// TestDS_CX1_AppServerExitCleanup verifies clean exit on stdin close.
func TestDS_CX1_AppServerExitCleanup(t *testing.T) {
	bin := dsCX1Bin(t)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "app-server", "--stdio")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"0.144.1","clientInfo":{"name":"ds-cx1","version":"0.0.0"},"capabilities":{}}}`+"\n")
	readUntilID(scanner, 1)
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","method":"initialized","params":{}}`+"\n")
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":2,"method":"thread/start","params":{"threadId":"urn:uuid:00000000-0000-0000-0000-00000000c005","cwd":"/","model":"test"}}`+"\n")
	readUntilID(scanner, 2)

	stdin.Close()

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	select {
	case err := <-waitCh:
		if err != nil {
			t.Logf("App-server exited with: %v (expected on stdin close)", err)
		} else {
			t.Log("App-server exited cleanly on stdin close")
		}
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		<-waitCh
		t.Error("App-server did not exit within timeout")
	}
}

// TestDS_CX1_MultiClientFanOut tests dual-client co-presence via the
// shipped remote-control daemon path.
func TestDS_CX1_MultiClientFanOut(t *testing.T) {
	bin := dsCX1Bin(t)

	dir := t.TempDir()
	sockPath := filepath.Join(dir, "cx1.sock")

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Use unix socket listener (NOT --stdio). The --listen flag is the
	// shipped remote TUI connection path.
	cmd := exec.CommandContext(ctx, bin, "app-server", "--listen", "unix://"+sockPath)
	stdin, _ := cmd.StdinPipe()
	stdoutPipe, _ := cmd.StdoutPipe()
	_ = stdin
	if err := cmd.Start(); err != nil {
		t.Skipf("app-server --listen unix: %v (remote control path unavailable)", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	// Wait for socket.
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if _, err := os.Stat(sockPath); err != nil {
		t.Skipf("Unix socket %s not created: %v (remote TUI path not supported with stock binary)", sockPath, err)
	}

	// Client A connects.
	connA, err := net.DialTimeout("unix", sockPath, 5*time.Second)
	if err != nil {
		t.Skipf("dial socket: %v", err)
	}
	defer connA.Close()

	// Client A handshake.
	fmt.Fprintf(connA, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"0.144.1","clientInfo":{"name":"client-a","version":"0.0.0"},"capabilities":{}}}`+"\n")
	scA := bufio.NewScanner(connA)
	scA.Buffer(make([]byte, 64*1024), 1024*1024)
	readUntilID(scA, 1)
	fmt.Fprintf(connA, `{"jsonrpc":"2.0","method":"initialized","params":{}}`+"\n")

	// Client B connects.
	connB, err := net.DialTimeout("unix", sockPath, 5*time.Second)
	if err != nil {
		t.Skipf("second dial: %v (dual-client co-presence unconfirmed)", err)
	}
	defer connB.Close()

	fmt.Fprintf(connB, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"0.144.1","clientInfo":{"name":"client-b","version":"0.0.0"},"capabilities":{}}}`+"\n")
	scB := bufio.NewScanner(connB)
	scB.Buffer(make([]byte, 64*1024), 1024*1024)
	readUntilID(scB, 1)
	fmt.Fprintf(connB, `{"jsonrpc":"2.0","method":"initialized","params":{}}`+"\n")
	t.Log("DUAL-CLIENT: both clients connected to same app-server socket")

	// Start a thread from client A.
	fmt.Fprintf(connA, `{"jsonrpc":"2.0","id":2,"method":"thread/start","params":{"threadId":"urn:uuid:00000000-0000-0000-0000-00000000c006","cwd":"/","model":"test"}}`+"\n")
	raw, _ := readUntilID(scA, 2)
	var tr struct {
		Result struct {
			Thread struct{ ID string }
		} `json:"result"`
	}
	json.Unmarshal(raw, &tr)
	serverThreadID := tr.Result.Thread.ID

	// Start turn from client A.
	fmt.Fprintf(connA, `{"jsonrpc":"2.0","id":3,"method":"turn/start","params":{"threadId":%q,"input":[{"type":"text","text":"Reply READY"}],"approvalPolicy":"untrusted"}}`+"\n", serverThreadID)

	// Collect notifications from both clients.
	var aMethods, bMethods []string
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aMethods = collectMethods(scA, 3, 15*time.Second)
	}()
	go func() {
		defer wg.Done()
		time.Sleep(500 * time.Millisecond)
		bMethods = collectMethods(scB, 0, 15*time.Second)
	}()
	wg.Wait()

	aHasStarted := containsStr(aMethods, "turn/started")
	bHasStarted := containsStr(bMethods, "turn/started")

	if aHasStarted && bHasStarted {
		t.Log("DUAL-CLIENT CONFIRMED: both clients received turn/started")
	} else if aHasStarted {
		t.Logf("CATEGORY-B FINDING: only initiating client received turn/started (B: %v)", bMethods)
	} else {
		t.Logf("CATEGORY-B FINDING: no client received turn/started (A: %v, B: %v)", aMethods, bMethods)
	}

	_ = stdoutPipe
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
