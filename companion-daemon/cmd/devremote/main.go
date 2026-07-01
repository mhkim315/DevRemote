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
	"devremote/companion-daemon/internal/push"
	"devremote/companion-daemon/internal/server"
	"devremote/companion-daemon/internal/watcher"
	devwebrtc "devremote/companion-daemon/internal/webrtc"
	"devremote/companion-daemon/internal/wrap"
)

// printHookScript outputs shell script that intercepts AI CLI commands.
// The user adds `eval "$(devremote hook)"` to .bashrc/.zshrc once.
func printHookScript() {
	fmt.Println(`# DevRemote Hook — intercepts AI CLI commands for mobile monitoring.
# Add this to your .bashrc or .zshrc:
#   eval "$(devremote hook)"

_devremote_wrap() {
  # Run the command inside DevRemote's PTY proxy for mobile control.
  devremote wrap "$@"
}

# Intercept common AI CLI tools — transparent PTY proxy.
alias claude='_devremote_wrap claude'
alias codex='_devremote_wrap codex'
alias gemini='_devremote_wrap gemini'
alias aider='_devremote_wrap aider'

if [ -z "$DEVSETUP_PROJECT" ]; then
  echo "DevRemote: Set DEVSETUP_PROJECT to your Claude project path."
  echo "  export DEVSETUP_PROJECT=\"$HOME/.claude/projects/...\""
fi`)
}

func main() {
	// Subcommand: devremote hook
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		printHookScript()
		return
	}

	// Subcommand: devremote wrap <command> [args...]
	if len(os.Args) > 1 && os.Args[1] == "wrap" {
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: devremote wrap <command> [args...]\n")
			os.Exit(1)
		}
		name := os.Args[2]
		args := os.Args[3:]
		if err := wrap.Command(".", name, args...); err != nil {
			log.Fatalf("wrap: %v", err)
		}
		return
	}

	// Default: devremote watch
	projectDir := flag.String("project", "", "Path to Claude project directory to watch (required)")
	port := flag.String("port", "9171", "WebSocket server port")
	execClaude := flag.Bool("exec", false, "Run 'claude' as a child process and relay mobile responses to its stdin")
	workDir := flag.String("workdir", "", "Working directory for --exec (default: current directory)")
	signalingURL := flag.String("signaling", "", "Signaling server URL for remote WebRTC access (e.g., ws://168.107.59.177:9173)")
	flag.Parse()

	if *projectDir == "" {
		fmt.Fprintf(os.Stderr, "Usage: devremote watch --project <path> [--exec] [--signaling <url>]\n")
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  devremote watch --project \"C:\\Users\\user\\.claude\\projects\\...\" --signaling ws://168.107.59.177:9173\n")
		os.Exit(1)
	}

	absDir, err := filepath.Abs(*projectDir)
	if err != nil {
		log.Fatalf("Invalid project path: %v", err)
	}
	if _, err := os.Stat(absDir); os.IsNotExist(err) {
		log.Fatalf("Project directory not found: %s", absDir)
	}

	var pushTokens []string
	var stdinWriter io.Writer
	onResponse := func(clientIP string, msg map[string]interface{}) {
		msgType, _ := msg["type"].(string)

		if msgType == "register" {
			if tok, ok := msg["pushToken"].(string); ok && tok != "" {
				pushTokens = append(pushTokens, tok)
				log.Printf("push token registered from %s (%d total)", clientIP, len(pushTokens))
			}
			return
		}

		if msgType != "response" || stdinWriter == nil {
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
				input = "\n"
			}
		} else {
			if approved {
				input = "y\n"
			} else {
				input = "n\n"
			}
		}
		log.Printf("relaying to claude stdin: %q", input)
		stdinWriter.Write([]byte(input))
	}

	hub := server.NewHub(onResponse)

	// WebRTC session, initialized later if --signaling is set.
	// Must be declared before onAlert so the closure can reference it.
	var webrtcSess *devwebrtc.Session

	// Wrap OnAlert with "Push on Idle" policy.
	onAlert := func(a detector.Alert) {
		hub.HandleAlert(a)

		activeConns := hub.ActiveClientCount()
		if webrtcSess != nil && webrtcSess.IsConnected() {
			activeConns++
		}
		if activeConns == 0 && len(pushTokens) > 0 {
			push.Send(
				pushTokens,
				"Claude 승인 필요",
				push.DescriptionFor(a.ToolName, a.Description),
				a.SessionID,
				a.ToolName,
			)
		}
	}
	state := detector.NewState(onAlert)

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

	// Optionally start WebRTC for remote access.
	if *signalingURL != "" {
		webrtcSess = devwebrtc.New(*signalingURL,
			[]string{"stun:stun.l.google.com:19302"},
			absDir,
		)
		hub.AddListener(webrtcSess.HandleAlert)

		go func() {
			if err := webrtcSess.Start(onResponse); err != nil {
				log.Printf("webrtc: start failed: %v", err)
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
	if webrtcSess != nil {
		fmt.Printf("\nRemote access code: %s\n", webrtcSess.Code())
		fmt.Printf("Signaling: %s\n", *signalingURL)
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
	if webrtcSess != nil {
		webrtcSess.Close()
	}
}
