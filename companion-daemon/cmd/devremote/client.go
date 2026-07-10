package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"
)

// terminalResetSeq sanitizes the host terminal after a controlled_pty
// session ends. TUIs like claude/codex enable a range of private modes and
// (kitty) keyboard/mouse/focus reporting. If we don't disable them on exit,
// the host shell inherits a broken terminal and stray query responses
// (e.g. cursor-position reports ESC[?<r>;<c>R) leak into the shell as input.
// All sequences are ignored by terminals that don't support them.
const terminalResetSeq = "" +
	"\x1b[?1049l" + // leave alternate screen buffer
	"\x1b[?2004l" + // bracketed paste off
	"\x1b[?1004l" + // focus reporting off
	"\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1005l\x1b[?1006l\x1b[?1015l" + // all mouse tracking off
	"\x1b[<u" + // pop kitty keyboard protocol flags
	"\x1b[>4;0m" + // reset modifyOtherKeys (xterm)
	"\x1b[?1l" + // cursor keys: normal (DECCKM off)
	"\x1b>" + // keypad: numeric/normal (DECKPNM)
	"\x1b[?25h" + // show cursor
	"\x1b[0m" + // reset SGR attributes
	"\r" // return to column 0

// drainStdin reads and discards any pending stdin bytes for up to d. It is
// called during teardown (still in raw mode) to absorb in-flight terminal
// query responses the host emits as the TUI tears down, so they never reach
// the parent shell. Discarding is safe here — the session has already ended.
func drainStdin(d time.Duration) {
	deadline := time.After(d)
	buf := make([]byte, 256)
	for {
		read := make(chan struct{}, 1)
		go func() {
			os.Stdin.Read(buf)
			read <- struct{}{}
		}()
		select {
		case <-read:
			// discard and keep draining until the window elapses
		case <-deadline:
			return
		}
	}
}

// buildRunPayload constructs the JSON body for a POST /api/sessions request.
// Returns the encoded bytes and the generated session ID.
func buildRunPayload(command, cwd string) ([]byte, string) {
	sessionID := fmt.Sprintf("controlled_pty:run-%d", time.Now().UnixNano())
	payload := struct {
		ID          string `json:"id"`
		Runner      string `json:"runner"`
		RunnerColor string `json:"runnerColor"`
		Command     string `json:"command"`
		CWD         string `json:"cwd,omitempty"`
	}{
		ID:          sessionID,
		Runner:      command,
		RunnerColor: "#45EBE9",
		Command:     command,
		CWD:         cwd,
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		log.Fatalf("Failed to encode request: %v", err)
	}
	return buf.Bytes(), sessionID
}

// buildRunRequest creates an HTTP request for the pokit run command.
func buildRunRequest(daemonURL, token string, body []byte) (*http.Request, error) {
	req, err := http.NewRequest("POST", daemonURL+"/api/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

func runClient(args []string) {
	cwd := ""
	detach := false
	commandArgs := args

	// Parse flags.
	for len(commandArgs) > 0 && strings.HasPrefix(commandArgs[0], "--") {
		switch commandArgs[0] {
		case "--cwd":
			if len(commandArgs) < 2 {
				log.Fatal("--cwd requires a value")
			}
			cwd = commandArgs[1]
			commandArgs = commandArgs[2:]
		case "--detach":
			detach = true
			commandArgs = commandArgs[1:]
		default:
			log.Fatalf("unknown flag: %s", commandArgs[0])
		}
	}

	if len(commandArgs) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: pokit run [--cwd <dir>] <command>\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  pokit run claude\n")
		fmt.Fprintf(os.Stderr, "  pokit run --cwd ~/project codex\n")
		fmt.Fprintf(os.Stderr, "  pokit run bash\n")
		os.Exit(1)
	}

	command := strings.Join(commandArgs, " ")

	// Preflight: for an attached run, the daemon IPC socket must exist. The
	// HTTP API can be alive while the IPC listener is not (e.g. a duplicate
	// daemon start deleted the socket), and without this check we would
	// create a controlled_pty session that this terminal cannot attach to —
	// the user's later keystrokes would then run in the HOST shell instead.
	if !detach {
		if _, statErr := os.Stat("/tmp/pokit.sock"); statErr != nil {
			fmt.Fprintf(os.Stderr, "\nERROR: daemon IPC socket /tmp/pokit.sock is missing — local attach unavailable.\n")
			fmt.Fprintf(os.Stderr, "The daemon HTTP API may be alive but its IPC listener is not. Restart the daemon:\n")
			fmt.Fprintf(os.Stderr, "  pkill -f pokit-daemon; sleep 1; /tmp/pokit-daemon --insecure-local-only &\n")
			fmt.Fprintf(os.Stderr, "Then verify:  ls -l /tmp/pokit.sock   (expect srw-------)\n")
			fmt.Fprintf(os.Stderr, "Or re-run with --detach to create a session for mobile/web only.\n\n")
			os.Exit(1)
		}
	}

	payloadBytes, _ := buildRunPayload(command, cwd)

	daemonURL := os.Getenv("POKIT_URL")
	if daemonURL == "" {
		daemonURL = "http://localhost:9171"
	}

	req, err := buildRunRequest(daemonURL, os.Getenv("POKIT_TOKEN"), payloadBytes)
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Failed to reach daemon at %s: %v\nIs the daemon running? Try: pokit daemon --insecure-local-only", daemonURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Fatalf("Daemon returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct{ ID string }
	json.NewDecoder(resp.Body).Decode(&result)

	if result.ID == "" {
		fmt.Println("Session created (check daemon for details)")
		return
	}

	fmt.Printf("Session created: %s\n", result.ID)
	fmt.Printf("Terminal: %s/term/?session=%s\n", daemonURL, result.ID)

	if detach {
		return
	}

	// E10b: attach local terminal as subscriber via Unix socket.
	attachLocalTerminal(result.ID)
}

// attachLocalTerminal connects to the daemon Unix socket and bridges
// local stdin/stdout to the recorder broadcast. Local terminal is a
// subscriber — no second PTY reader is created.
func attachLocalTerminal(sessionID string) {
	socketPath := "/tmp/pokit.sock"
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nERROR: could not attach local terminal — daemon IPC socket unavailable: %v\n", err)
		fmt.Fprintf(os.Stderr, "Session %s was created but is NOT attached to this terminal.\n", sessionID)
		fmt.Fprintf(os.Stderr, "Restart the daemon (see above) and retry, or use mobile/web to interact.\n\n")
		os.Exit(1)
	}
	defer conn.Close()

	// Send subscriber protocol header. If stdout is a TTY, advertise the
	// local terminal size so the daemon resizes the PTY to match — TUIs like
	// claude assume a wider terminal than the 80x24 spawn default.
	if w, h, sErr := term.GetSize(int(os.Stdout.Fd())); sErr == nil && w > 0 && h > 0 {
		fmt.Fprintf(conn, "sub:%s %d %d\n", sessionID, w, h)
	} else {
		fmt.Fprintf(conn, "sub:%s\n", sessionID)
	}

	// Put terminal in raw mode.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Printf("Not a TTY — output only mode: %v", err)
		// Still relay stdout.
		go io.Copy(os.Stdout, conn)
		// Wait for connection to close.
		io.Copy(io.Discard, conn)
		return
	}

	// restore sanitizes the host terminal and restores cooked mode exactly
	// once, on whichever exit path fires first (EOF, error, Ctrl+C). The full
	// mode-reset lives in terminalResetSeq.
	var restoreOnce sync.Once
	restore := func() {
		restoreOnce.Do(func() {
			os.Stdout.WriteString(terminalResetSeq)
			term.Restore(int(os.Stdin.Fd()), oldState)
		})
	}
	defer restore()

	// Handle Ctrl+C gracefully.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Relay: recorder broadcast → local stdout. When conn closes (session
	// ended), stdoutDone fires.
	stdoutDone := make(chan struct{})
	go func() {
		io.Copy(os.Stdout, conn)
		close(stdoutDone)
	}()

	// Relay: local stdin → recorder WriteInput.
	go func() {
		io.Copy(conn, os.Stdin)
	}()

	// Wait for session end (stdout EOF) or Ctrl+C.
	select {
	case <-stdoutDone:
	case <-sigCh:
	}
	// Session ended. Close the socket, then drain any in-flight host-terminal
	// query responses (e.g. cursor-position reports emitted as the TUI tears
	// down) so they don't leak into the parent shell as input.
	conn.Close()
	drainStdin(150 * time.Millisecond)
	restore()
	os.Exit(0)
}

func runLinkerClient(cmd string, args []string) {
	socketPath := "/tmp/pokit.sock"
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		log.Fatalf("Failed to connect to POKIT daemon: %v", err)
	}
	defer conn.Close()

	var req map[string]interface{}

	if cmd == "link" {
		if len(args) < 3 {
			log.Fatalf("Usage: pokit link <sessionId> <provider> <externalSessionId>")
		}
		req = map[string]interface{}{
			"version":           1,
			"operation":         "link",
			"sessionId":         args[0],
			"provider":          args[1],
			"externalSessionId": args[2],
		}
	} else if cmd == "unlink" {
		if len(args) < 1 {
			log.Fatalf("Usage: pokit unlink <sessionId>")
		}
		req = map[string]interface{}{
			"version":   1,
			"operation": "unlink",
			"sessionId": args[0],
		}
	} else if cmd == "links" {
		req = map[string]interface{}{
			"version":   1,
			"operation": "links",
		}
	}

	importJSON, _ := json.Marshal(req)
	conn.Write(importJSON)
	conn.Write([]byte("\n"))

	buf := make([]byte, 4096)
	n, _ := conn.Read(buf)
	fmt.Print(string(buf[:n]))
}
