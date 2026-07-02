// DevRemote — Human-in-the-Loop mobile dashboard for AI CLI tools.
//
// Subcommands:
//
//	watch    Start the daemon: watch JSONL files, serve WebSocket + WebRTC
//	hook     Print shell script to intercept AI CLI commands (.bashrc/.zshrc)
//	wrap     Run a CLI inside a PTY proxy for full stdin/stdout control
//
// The daemon watches Claude Code's JSONL session files for tool_use events,
// broadcasts alerts to a mobile app via WebSocket (LAN) or WebRTC (remote),
// and relays mobile responses (approve/deny, stdin text, Ctrl-C) back to the AI CLI.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
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
	var claudeCmd *exec.Cmd // for sending signals

	// sendToWrap tries the wrap IPC HTTP server, falls back to pipe.
	sendToWrap := func(text string) {
		// Try piping through the wrap's localhost HTTP server.
		state, err := wrap.ReadIPC(os.TempDir())
		if err == nil && state.Port > 0 {
			body, _ := json.Marshal(map[string]string{"text": text})
			resp, err := http.Post(
				fmt.Sprintf("http://127.0.0.1:%d/stdin", state.Port),
				"application/json",
				bytes.NewReader(body),
			)
			if err == nil && resp.StatusCode == 200 {
				resp.Body.Close()
				return
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
		// Fallback: direct pipe (--exec mode).
		if stdinWriter != nil {
			if _, err := stdinWriter.Write([]byte(text)); err != nil {
				log.Printf("stdin write failed (claude may have exited): %v", err)
				stdinWriter = nil
			}
		}
	}

	onResponse := func(clientIP string, msg map[string]interface{}) {
		msgType, _ := msg["type"].(string)

		if msgType == "register" {
			if tok, ok := msg["pushToken"].(string); ok && tok != "" {
				pushTokens = append(pushTokens, tok)
				log.Printf("push token registered from %s (%d total)", clientIP, len(pushTokens))
			}
			return
		}

		// Raw stdin text from mobile.
		if msgType == "stdin" {
			if text, ok := msg["text"].(string); ok {
				sendToWrap(text)
				log.Printf("stdin from mobile: %q", text)
			}
			return
		}

		// Interrupt / Ctrl-C.
		if msgType == "interrupt" {
			if claudeCmd != nil && claudeCmd.Process != nil {
				if err := wrap.Interrupt(claudeCmd.Process.Pid); err != nil {
					log.Printf("interrupt failed: %v", err)
					wrap.KillProcess(claudeCmd.Process)
				} else {
					log.Printf("interrupt sent to claude pid=%d", claudeCmd.Process.Pid)
				}
			}
			return
		}

		if msgType != "response" {
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
				input = "1\n" // Claude numbered menu: first option = Yes
			} else {
				input = "3\n" // Claude numbered menu: last option = No
			}
		}
		log.Printf("relaying to claude stdin: %q", input)
		sendToWrap(input)
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

	// Raw event stream: forward every JSONL event to connected clients as type=raw.
	rawFeed := func(ev watcher.RawEvent) {
		desc := fmt.Sprintf("[%s] %s", ev.Type, ev.Timestamp)
		// Try to extract useful info from the message.
		var msg map[string]interface{}
		if json.Unmarshal(ev.Message, &msg) == nil {
			if role, ok := msg["role"].(string); ok {
				desc = fmt.Sprintf("[%s:%s]", ev.Type, role)
			}
			if content, ok := msg["content"].([]interface{}); ok && len(content) > 0 {
				if block, ok := content[0].(map[string]interface{}); ok {
					switch block["type"] {
					case "tool_use":
						desc = fmt.Sprintf("[%s:%s] %v", ev.Type, block["name"], block["input"])
					case "text":
						txt := block["text"].(string)
						if len(txt) > 60 {
							txt = txt[:60] + "..."
						}
						desc = fmt.Sprintf("[%s] %q", ev.Type, txt)
					case "thinking":
						desc = fmt.Sprintf("[%s:thinking]", ev.Type)
					}
				}
			}
		}
		a := detector.Alert{
			SessionID:   ev.SessionID,
			Type:        ev.Type,
			Description: desc,
			Timestamp:   ev.Timestamp,
		}
		hub.SendRaw(a)
	}
	t, err := watcher.New(absDir, func(ev watcher.RawEvent) {
		rawFeed(ev)
		state.Feed(ev)
	})
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
		claudeCmd = cmd
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
			hub.AddRawListener(webrtcSess.HandleRaw)

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
