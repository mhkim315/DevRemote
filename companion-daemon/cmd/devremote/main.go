package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"

	"devremote/companion-daemon/internal/detector"
	"devremote/companion-daemon/internal/server"
	"devremote/companion-daemon/internal/watcher"
)

func main() {
	projectDir := flag.String("project", "", "Path to Claude project directory to watch (required)")
	port := flag.String("port", "9171", "WebSocket server port")
	execClaude := flag.Bool("exec", false, "Run 'claude' as a child process and relay mobile responses to its stdin")
	workDir := flag.String("workdir", "", "Working directory for --exec (default: current directory)")
	flag.Parse()

	if *projectDir == "" {
		fmt.Fprintf(os.Stderr, "Usage: devremote watch --project <path> [--exec] [--workdir <path>]\n")
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  devremote watch --project \"C:\\Users\\user\\.claude\\projects\\C--Users-user-Documents-remote-control\" --exec\n")
		os.Exit(1)
	}

	absDir, err := filepath.Abs(*projectDir)
	if err != nil {
		log.Fatalf("Invalid project path: %v", err)
	}
	if _, err := os.Stat(absDir); os.IsNotExist(err) {
		log.Fatalf("Project directory not found: %s", absDir)
	}

	// Response relay writes to Claude Code's stdin.
	var stdinWriter io.Writer
	onResponse := func(clientIP string, msg map[string]interface{}) {
		if stdinWriter == nil {
			return
		}
		approved, _ := msg["approved"].(bool)
		answer, _ := msg["answer"].(string)
		toolName, _ := msg["toolName"].(string)

		var input string
		if toolName == "AskUserQuestion" {
			if approved && answer != "" {
				input = answer + "\n"
			} else {
				input = "\n" // empty response skips the question
			}
		} else {
			if approved {
				input = "y\n"
			} else {
				input = "n\n"
			}
		}
		log.Printf("ws: relaying to claude stdin: %q", input)
		stdinWriter.Write([]byte(input))
	}

	hub := server.NewHub(onResponse)
	state := detector.NewState(hub.HandleAlert)

	t, err := watcher.New(absDir, state.Feed)
	if err != nil {
		log.Fatalf("Failed to create watcher: %v", err)
	}
	if err := t.Start(); err != nil {
		log.Fatalf("Failed to start watcher: %v", err)
	}
	defer t.Close()

	log.Printf("Watching: %s", absDir)

	// Optionally start Claude Code as a child process.
	if *execClaude {
		cwd := *workDir
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		cmd := exec.Command("claude")
		cmd.Dir = cwd
		cmd.Env = os.Environ()

		stdinPipe, err := cmd.StdinPipe()
		if err != nil {
			log.Fatalf("Failed to create stdin pipe: %v", err)
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		stdinWriter = stdinPipe

		if err := cmd.Start(); err != nil {
			log.Fatalf("Failed to start claude: %v", err)
		}
		log.Printf("Started claude in %s (pid=%d)", cwd, cmd.Process.Pid)

		defer func() {
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		}()
	}

	fmt.Println("\n=== DevRemote Daemon ===")
	fmt.Println("Mobile app connect to one of:")
	for _, ip := range server.LocalIPs() {
		fmt.Printf("  ws://%s:%s/ws\n", ip, *port)
	}
	if *execClaude {
		fmt.Println("Claude Code relay: ENABLED")
	}
	fmt.Println("========================")

	go func() {
		if err := hub.Start(":" + *port); err != nil {
			log.Fatalf("WebSocket server: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	log.Println("Shutting down...")
}
