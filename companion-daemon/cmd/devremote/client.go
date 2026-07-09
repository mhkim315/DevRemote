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
	"strings"
	"time"
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

func runClient(args []string) {
	cwd := ""
	commandArgs := args

	if len(args) >= 2 && args[0] == "--cwd" {
		cwd = args[1]
		commandArgs = args[2:]
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

	req, err := http.NewRequest("POST", daemonURL+"/api/sessions", bytes.NewReader(payloadBytes))
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if token := os.Getenv("POKIT_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
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

	if result.ID != "" {
		fmt.Printf("Session created: %s\n", result.ID)
		fmt.Printf("Terminal: %s/term/?session=%s\n", daemonURL, result.ID)
	} else {
		fmt.Println("Session created (check daemon for details)")
	}
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
