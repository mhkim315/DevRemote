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
	"syscall"
	"time"

	"golang.org/x/term"
)

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
		log.Printf("Cannot attach terminal (daemon socket not available): %v", err)
		log.Printf("Use mobile/web to interact with session %s", sessionID)
		return
	}
	defer conn.Close()

	// Send subscriber protocol header.
	fmt.Fprintf(conn, "sub:%s\n", sessionID)

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
	defer func() {
		term.Restore(int(os.Stdin.Fd()), oldState)
		os.Stdout.Write([]byte("[>4;0m[?1l"))
	}()

	// Handle Ctrl+C gracefully.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		term.Restore(int(os.Stdin.Fd()), oldState)
		os.Stdout.Write([]byte("[>4;0m[?1l"))
		conn.Close()
		os.Exit(0)
	}()

	// Relay: recorder broadcast → local stdout.
	// When conn closes (session ended), exit cleanly.
	stdoutDone := make(chan struct{})
	go func() {
		io.Copy(os.Stdout, conn)
		close(stdoutDone)
	}()

	// Relay: local stdin → recorder WriteInput (background, killed by os.Exit).
	go func() {
		io.Copy(conn, os.Stdin)
	}()

	// Wait for session end (stdout EOF) or Ctrl+C.
	select {
	case <-stdoutDone:
	case <-sigCh:
	}
	conn.Close()
	term.Restore(int(os.Stdin.Fd()), oldState)
	os.Stdout.Write([]byte("\x1b[>4;0m\x1b[?1l"))
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
