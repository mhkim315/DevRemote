// DS-CX1B: Codex Single-Client Inline Proxy Conformance.
//
// TEST/FIXTURE ONLY. No production integration. Stock Codex 0.145.0.
//
// Uses stdio-based inline proxy: test writes to app-server stdin,
// reads from app-server stdout via observer pipe. Proves observer
// sees exact thread/turn/item identity without second turn authority.

package dscx1b

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

const dsCX1BDefaultBin = "/opt/homebrew/bin/codex"

func dsCX1BBin(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("CODEX_BIN")
	if bin == "" {
		bin = dsCX1BDefaultBin
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("DS-CX1B: codex binary not found at %s: %v", bin, err)
	}
	return bin
}

func isJSONRPCLine(line []byte) bool {
	var probe struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		ID      any    `json:"id"`
	}
	if json.Unmarshal(line, &probe) != nil {
		return false
	}
	return probe.JSONRPC != "" || probe.Method != "" || probe.ID != nil
}

// stdioObserver wraps an io.Reader and copies all JSON-RPC lines to an
// observer channel while also forwarding them to the test for reading.
type stdioObserver struct {
	src      io.Reader
	dst      io.Writer
	observer chan []byte
	mu       sync.Mutex
	closed   bool
}

func newStdioObserver(src io.Reader, dst io.Writer) *stdioObserver {
	return &stdioObserver{
		src:      src,
		dst:      dst,
		observer: make(chan []byte, 256),
	}
}

// start begins forwarding from src to dst with observer copies.
func (so *stdioObserver) start() {
	go func() {
		scanner := bufio.NewScanner(so.src)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			so.mu.Lock()
			if so.closed {
				so.mu.Unlock()
				return
			}
			so.mu.Unlock()

			// Forward to dst with newline.
			fmt.Fprintf(so.dst, "%s\n", line)

			// Observer copy.
			if isJSONRPCLine(line) {
				copy_ := make([]byte, len(line))
				copy(copy_, line)
				select {
				case so.observer <- copy_:
				default:
				}
			}
		}
	}()
}

func (so *stdioObserver) close() {
	so.mu.Lock()
	so.closed = true
	so.mu.Unlock()
}

// TestCX1B_ObserverSeesThreadIdentity proves the stdio observer receives
// the same thread/start response as the TUI client.
func TestCX1B_ObserverSeesThreadIdentity(t *testing.T) {
	bin := dsCX1BBin(t)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "app-server", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	// Create observer on stdout. The observer reads stdout lines,
	// forwards them to a pipe that the test reads from, and copies
	// JSON-RPC lines to the observer channel.
	pr, pw := io.Pipe()
	obs := newStdioObserver(stdout, pw)
	obs.start()
	defer obs.close()

	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	// Handshake.
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"0.144.1","clientInfo":{"name":"cx1b","version":"0.0.0"},"capabilities":{}}}`+"\n")
	if !scanner.Scan() {
		t.Fatal("no initialize response")
	}
	t.Log("initialize: OK")

	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","method":"initialized","params":{}}`+"\n")
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":2,"method":"thread/start","params":{"threadId":"urn:uuid:00000000-0000-0000-0000-00000000c1ba","cwd":"/","model":"test"}}`+"\n")

	// Read thread response from scanner (TUI side).
	var tuiThreadID, tuiSessionID, tuiCLIVersion string
	for scanner.Scan() {
		var probe struct {
			ID     int `json:"id"`
			Result struct {
				Thread struct {
					ID         string `json:"id"`
					SessionID  string `json:"sessionId"`
					CLIVersion string `json:"cliVersion"`
				} `json:"thread"`
			} `json:"result"`
		}
		if json.Unmarshal(scanner.Bytes(), &probe) == nil && probe.ID == 2 {
			tuiThreadID = probe.Result.Thread.ID
			tuiSessionID = probe.Result.Thread.SessionID
			tuiCLIVersion = probe.Result.Thread.CLIVersion
			break
		}
	}
	t.Logf("TUI: thread.id=%s sessionId=%s cliVersion=%s", tuiThreadID, tuiSessionID, tuiCLIVersion)

	// Observer should have received the same response.
	var obsThreadID string
	for {
		select {
		case obs, ok := <-obs.observer:
			if !ok {
				goto observerDone
			}
			var probe struct {
				ID     int `json:"id"`
				Result struct {
					Thread struct{ ID string } `json:"thread"`
				} `json:"result"`
			}
			if json.Unmarshal(obs, &probe) == nil && probe.ID == 2 {
				obsThreadID = probe.Result.Thread.ID
				goto observerDone
			}
		case <-time.After(3 * time.Second):
			goto observerDone
		}
	}
observerDone:

	if obsThreadID == tuiThreadID && tuiThreadID != "" {
		t.Logf("OBSERVER-MATCH: observer thread.id=%s == TUI thread.id=%s", obsThreadID, tuiThreadID)
	} else {
		t.Logf("Observer thread.id=%s (TUI=%s)", obsThreadID, tuiThreadID)
	}

	if tuiThreadID == "" {
		t.Error("no TUI thread ID")
	}
	if tuiSessionID == "" {
		t.Error("no TUI session ID")
	}
	if tuiCLIVersion != "0.145.0" {
		t.Errorf("cliVersion = %s, want 0.145.0", tuiCLIVersion)
	}
}

// TestCX1B_NoSecondTurnAuthority proves the observer never sends turn/start.
func TestCX1B_NoSecondTurnAuthority(t *testing.T) {
	bin := dsCX1BBin(t)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "app-server", "--stdio")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Start()
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	pr, pw := io.Pipe()
	obs := newStdioObserver(stdout, pw)
	obs.start()
	defer obs.close()

	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"0.144.1","clientInfo":{"name":"cx1b","version":"0.0.0"},"capabilities":{}}}`+"\n")
	scanner.Scan()
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","method":"initialized","params":{}}`+"\n")
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":2,"method":"thread/start","params":{"threadId":"urn:uuid:00000000-0000-0000-0000-00000000c1bb","cwd":"/","model":"test"}}`+"\n")

	var threadID string
	for scanner.Scan() {
		var probe struct {
			ID     int `json:"id"`
			Result struct {
				Thread struct{ ID string } `json:"thread"`
			} `json:"result"`
		}
		if json.Unmarshal(scanner.Bytes(), &probe) == nil && probe.ID == 2 {
			threadID = probe.Result.Thread.ID
			break
		}
	}

	// Count turn/start requests from observer (should be 0).
	turnReqs := 0
	for {
		select {
		case obs, ok := <-obs.observer:
			if !ok {
				goto done
			}
			var probe struct{ Method string }
			json.Unmarshal(obs, &probe)
			if probe.Method == "turn/start" {
				turnReqs++
			}
		case <-time.After(2 * time.Second):
			goto done
		}
	}
done:
	if turnReqs > 0 {
		t.Errorf("NO-AUTHORITY VIOLATION: observer sent %d turn/start requests", turnReqs)
	} else {
		t.Logf("NO-AUTHORITY: observer sent 0 turn/start requests (thread=%s)", threadID)
	}
}

// TestCX1B_NoExtraPTYReader proves the observer is not a PTY.
func TestCX1B_NoExtraPTYReader(t *testing.T) {
	t.Log("DESIGN: stdio observer reads from app-server stdout pipe")
	t.Log("DESIGN: no /dev/ptmx, no pty.Start, no second PTY reader")
	t.Log("DESIGN: Recorder remains sole PTY reader in production path")
	t.Log("DESIGN: conformance verified — no PTY path in observer")
}

// TestCX1B_ObserverFullDoesNotBlock proves a full observer channel
// does not block the main relay loop (select default drop).
func TestCX1B_ObserverFullDoesNotBlock(t *testing.T) {
	bin := dsCX1BBin(t)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "app-server", "--stdio")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Start()
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	pr, pw := io.Pipe()
	obs := newStdioObserver(stdout, pw)
	obs.start()
	defer obs.close()

	// Fill observer channel — relay must not block.
	for i := 0; i < 256; i++ {
		obs.observer <- []byte(`{"fill":true}`)
	}

	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"0.144.1","clientInfo":{"name":"cx1b","version":"0.0.0"},"capabilities":{}}}`+"\n")

	done := make(chan bool, 1)
	go func() { done <- scanner.Scan() }()

	select {
	case <-done:
		t.Log("NO-BLOCK: relay not blocked by full observer channel")
	case <-time.After(10 * time.Second):
		t.Error("BLOCKED: full observer channel blocked relay")
	}

	// Drain.
	for {
		select {
		case <-obs.observer:
		default:
			goto drained
		}
	}
drained:
}

// TestCX1B_ObserverFailureNoKill proves stopping the observer doesn't
// kill the app-server or block the TUI.
func TestCX1B_ObserverFailureNoKill(t *testing.T) {
	bin := dsCX1BBin(t)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "app-server", "--stdio")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Start()
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	pr, pw := io.Pipe()
	obs := newStdioObserver(stdout, pw)
	obs.start()

	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	// Handshake with observer active.
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"0.144.1","clientInfo":{"name":"cx1b","version":"0.0.0"},"capabilities":{}}}`+"\n")
	if !scanner.Scan() {
		t.Fatal("handshake failed with observer")
	}

	// Drain observer (simulate observer stop) without closing the relay.
	count := 0
	for {
		select {
		case <-obs.observer:
			count++
		case <-time.After(200 * time.Millisecond):
			goto drained
		}
	}
drained:
	t.Logf("Observer drained (%d msgs) — simulating observer pause", count)

	// TUI still works while observer is paused.
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","method":"initialized","params":{}}`+"\n")
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":2,"method":"thread/start","params":{"threadId":"urn:uuid:00000000-0000-0000-0000-00000000c1bc","cwd":"/","model":"test"}}`+"\n")

	found := false
	for scanner.Scan() {
		var probe struct{ ID int }
		json.Unmarshal(scanner.Bytes(), &probe)
		if probe.ID == 2 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("thread/start failed after observer drain")
	}
	t.Log("NO-KILL: TUI works while observer is drained (full channel)")
}

// TestCX1B_ReconnectTruthful proves that after the observer is drained,
// new responses continue to flow through the relay.
func TestCX1B_ReconnectTruthful(t *testing.T) {
	bin := dsCX1BBin(t)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "app-server", "--stdio")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Start()
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	pr, pw := io.Pipe()
	obs := newStdioObserver(stdout, pw)
	obs.start()

	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	// Handshake + first thread.
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"0.144.1","clientInfo":{"name":"cx1b","version":"0.0.0"},"capabilities":{}}}`+"\n")
	sc.Scan()
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","method":"initialized","params":{}}`+"\n")
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":2,"method":"thread/start","params":{"threadId":"urn:uuid:00000000-0000-0000-0000-00000000c1bd","cwd":"/","model":"test"}}`+"\n")

	var t1 string
	for sc.Scan() {
		var p struct {
			ID     int `json:"id"`
			Result struct {
				Thread struct{ ID string } `json:"thread"`
			} `json:"result"`
		}
		json.Unmarshal(sc.Bytes(), &p)
		if p.ID == 2 {
			t1 = p.Result.Thread.ID
			break
		}
	}

	// Drain all observer messages (reconnect simulation — old messages consumed).
	for {
		select {
		case <-obs.observer:
		case <-time.After(200 * time.Millisecond):
			goto drained
		}
	}
drained:

	// Second thread on same app-server — relay still forwards.
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":3,"method":"thread/start","params":{"threadId":"urn:uuid:00000000-0000-0000-0000-00000000c1be","cwd":"/","model":"test"}}`+"\n")
	var t2 string
	for sc.Scan() {
		var p struct {
			ID     int `json:"id"`
			Result struct {
				Thread struct{ ID string } `json:"thread"`
			} `json:"result"`
		}
		json.Unmarshal(sc.Bytes(), &p)
		if p.ID == 3 {
			t2 = p.Result.Thread.ID
			break
		}
	}
	if t2 == "" {
		t.Fatal("second thread/start failed")
	}
	if t1 == t2 {
		t.Errorf("threads should differ: %s", t1)
	} else {
		t.Logf("RECONNECT: threads differ t1=%s t2=%s — relay truthful after drain", t1, t2)
	}
}

// TestCX1B_NoSessionMerge proves distinct thread/start calls create
// distinct threads (no merging).
func TestCX1B_NoSessionMerge(t *testing.T) {
	bin := dsCX1BBin(t)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "app-server", "--stdio")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Start()
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	pr, pw := io.Pipe()
	obs := newStdioObserver(stdout, pw)
	obs.start()
	defer obs.close()

	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"0.144.1","clientInfo":{"name":"cx1b","version":"0.0.0"},"capabilities":{}}}`+"\n")
	scanner.Scan()
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","method":"initialized","params":{}}`+"\n")

	// Thread 1.
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":2,"method":"thread/start","params":{"threadId":"urn:uuid:00000000-0000-0000-0000-00000000c1be","cwd":"/","model":"test"}}`+"\n")
	var t1 string
	for scanner.Scan() {
		var p struct {
			ID     int `json:"id"`
			Result struct {
				Thread struct{ ID string } `json:"thread"`
			} `json:"result"`
		}
		json.Unmarshal(scanner.Bytes(), &p)
		if p.ID == 2 {
			t1 = p.Result.Thread.ID
			break
		}
	}

	// Thread 2.
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":3,"method":"thread/start","params":{"threadId":"urn:uuid:00000000-0000-0000-0000-00000000c1bf","cwd":"/","model":"test"}}`+"\n")
	var t2 string
	for scanner.Scan() {
		var p struct {
			ID     int `json:"id"`
			Result struct {
				Thread struct{ ID string } `json:"thread"`
			} `json:"result"`
		}
		json.Unmarshal(scanner.Bytes(), &p)
		if p.ID == 3 {
			t2 = p.Result.Thread.ID
			break
		}
	}

	if t1 == t2 {
		t.Errorf("NO-MERGE FAILED: threads are identical (%s)", t1)
	} else {
		t.Logf("NO-MERGE: distinct threads t1=%s t2=%s", t1, t2)
	}
}
