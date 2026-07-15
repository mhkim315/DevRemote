package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
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

// managedCodexRun reports whether the parsed `pokit run` args are EXACTLY the
// recognized Codex profile token (SP0.5 managed-only invocation).
func managedCodexRun(commandArgs []string) bool {
	return len(commandArgs) == 1 && commandArgs[0] == "codex"
}

// buildRunCreateRequest maps parsed `pokit run` arguments to the IPC create
// request body. SP0/SP0.5: the recognized Codex invocation (exactly one
// token) is ALWAYS sent as a STRUCTURED profile request — never a joined
// command string — detached or not. Every other form keeps the legacy
// command string.
func buildRunCreateRequest(commandArgs []string, cwd string, detach bool) map[string]interface{} {
	if managedCodexRun(commandArgs) {
		req := map[string]interface{}{
			"version":   1,
			"operation": "create",
			"profileId": "codex",
			"cwd":       cwd,
		}
		if detach {
			req["detach"] = true
		}
		return req
	}
	return map[string]interface{}{
		"version":   1,
		"operation": "create",
		"command":   strings.Join(commandArgs, " "),
		"cwd":       cwd,
	}
}

// createViaSocket creates a session over the daemon's 0600 Unix socket — the
// privileged local channel. Arbitrary command execution is only available
// here, never over the tunnel-reachable HTTP API. Returns the
// daemon-generated canonical session ID.
func createViaSocket(body map[string]interface{}) (string, error) {
	return createRequestViaSocketAt("/tmp/pokit.sock", body)
}

func createViaSocketAt(socketPath, command, cwd string) (string, error) {
	return createRequestViaSocketAt(socketPath, map[string]interface{}{
		"version":   1,
		"operation": "create",
		"command":   command,
		"cwd":       cwd,
	})
}

func createRequestViaSocketAt(socketPath string, body map[string]interface{}) (string, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	payload, _ := json.Marshal(body)
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return "", err
	}
	buf := make([]byte, 8192)
	n, _ := conn.Read(buf)
	var resp struct {
		ID    string `json:"id"`
		State string `json:"state"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return "", fmt.Errorf("unexpected daemon response: %s", strings.TrimSpace(string(buf[:n])))
	}
	if resp.Error != "" {
		return "", fmt.Errorf("%s", resp.Error)
	}
	if resp.ID == "" {
		return "", fmt.Errorf("daemon did not return a session id")
	}
	return resp.ID, nil
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

	daemonURL := os.Getenv("POKIT_URL")
	if daemonURL == "" {
		daemonURL = "http://localhost:9171"
	}

	// Create over the privileged 0600 Unix socket (not HTTP): the daemon runs
	// arbitrary commands only through this local channel. Recognized detached
	// profiles are sent structurally (SP0); everything else as a command string.
	sessionID, err := createViaSocket(buildRunCreateRequest(commandArgs, cwd, detach))
	if err != nil {
		log.Fatalf("Failed to create session via local socket: %v\nIs the daemon running? Try: pokit daemon --insecure-local-only", err)
	}

	fmt.Printf("Session created: %s\n", sessionID)
	fmt.Printf("Terminal: %s/term/?session=%s\n", daemonURL, sessionID)

	if detach {
		return
	}

	// SP0.5: the managed Codex session has no PTY — attach through the
	// bounded structured line client, not the recorder subscriber.
	if managedCodexRun(commandArgs) {
		attachManagedSession(sessionID)
		return
	}

	// E10b: attach local terminal as subscriber via Unix socket.
	attachLocalTerminal(sessionID)
}

// attachManagedSession is the thin line-oriented local client for a managed
// native session: projected bounded events print to stdout; each stdin line
// is one prompt; Ctrl-D detaches the viewer WITHOUT stopping the session. No
// raw mode, no keymap changes, no terminal emulation.
func attachManagedSession(sessionID string) {
	conn, err := net.Dial("unix", "/tmp/pokit.sock")
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nERROR: could not attach — daemon IPC socket unavailable: %v\n", err)
		fmt.Fprintf(os.Stderr, "Session %s keeps running; use mobile/web or 'pokit run --detach codex' next time.\n", sessionID)
		os.Exit(1)
	}
	defer conn.Close()

	req, _ := json.Marshal(map[string]interface{}{
		"version":   1,
		"operation": "managed-attach",
		"sessionId": sessionID,
	})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		log.Fatalf("attach write: %v", err)
	}

	fmt.Println("Attached to managed Codex session. Type a prompt and press Enter; Ctrl-D detaches (session keeps running).")

	// Daemon → stdout: projected bounded events.
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			var ev struct {
				Kind  string `json:"kind"`
				Text  string `json:"text"`
				Error string `json:"error"`
			}
			if json.Unmarshal(sc.Bytes(), &ev) != nil {
				continue
			}
			switch {
			case ev.Error != "":
				fmt.Fprintf(os.Stderr, "! %s\n", ev.Error)
			case ev.Kind == "assistant":
				fmt.Println(ev.Text)
			case ev.Kind == "working":
				fmt.Println("… working")
			case ev.Kind == "completed":
				fmt.Println("· idle")
			case ev.Kind == "exited":
				fmt.Println("× session exited")
			case ev.Kind == "gap":
				fmt.Println("~ some earlier output was dropped (bounded history)")
			}
		}
	}()

	// stdin lines → prompts. EOF (Ctrl-D) detaches the viewer only.
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 64*1024), 1024*1024)
	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		payload, _ := json.Marshal(map[string]string{"prompt": line})
		if _, err := conn.Write(append(payload, '\n')); err != nil {
			break
		}
	}
	conn.Close()
	<-done
	fmt.Println("Detached. The managed session keeps running.")
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
