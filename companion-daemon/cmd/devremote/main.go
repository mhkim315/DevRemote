package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/mdp/qrterminal/v3"

	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/term"
	"devremote/companion-daemon/internal/watcher"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "run" {
		runClient(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		printShellHook()
		return
	}

	ownerUUID := flag.String("owner-uuid", "", "Supabase user UUID that owns this daemon (required for auth)")
	supabaseRef := flag.String("supabase-ref", "", "Supabase project reference for JWKS (e.g. abcdefghijklmnop)")
	
	// If the user specifies "daemon" explicitly, parse flags starting from Args[2]
	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		flag.CommandLine.Parse(os.Args[2:])
	} else {
		flag.Parse()
	}

	term.OwnerUUID = *ownerUUID
	term.SupabaseProjectRef = *supabaseRef

	if *ownerUUID == "" {
		log.Println("WARN: --owner-uuid not set. All valid Supabase tokens will be accepted (INSECURE).")
	}
	if *supabaseRef == "" {
		log.Println("WARN: --supabase-ref not set. RS256 JWKS verification disabled. Falling back to HS256 dev mode.")
	}

	// 1. Core Endpoints
	term.StartTelemetryLoop()

	// Start JSONL watcher for Claude logs
	homeDir, _ := os.UserHomeDir()
	claudeLogDir := filepath.Join(homeDir, ".claude")
	if _, err := os.Stat(claudeLogDir); os.IsNotExist(err) {
		claudeLogDir = "."
	}
	t, err := watcher.New(claudeLogDir, func(ev watcher.RawEvent) {
		toolUse := watcher.ExtractToolUse(ev)
		if toolUse != nil && (toolUse.Name == "Replace" || toolUse.Name == "Edit" || toolUse.Name == "Write" || toolUse.Name == "StrReplace" || toolUse.Name == "GlobReplace" || toolUse.Name == "View" || toolUse.Name == "Bash") {
			file := "file"
			if f, ok := toolUse.Input["file_path"].(string); ok {
				file = f
			} else if f, ok := toolUse.Input["path"].(string); ok {
				file = f
			} else if f, ok := toolUse.Input["command"].(string); ok {
				file = f
			}
			session := ev.SessionID
			if session == "" {
				session = "devremote"
			}
			models.EmitEvent(session, "file_edit", toolUse.Name, file)
		}
	})
	if err == nil {
		t.Start()
	}

	http.HandleFunc("/api/sessions", term.HandleSessionsAPI)
	http.HandleFunc("/term/ws", term.HandleWS)
	http.HandleFunc("/term/", term.HandleHTML)
	http.HandleFunc("/term/size", term.HandleSize)
	var pushToken string

	// Start Unix Socket IPC Server for local 'pokit run' commands
	socketPath := "/tmp/pokit.sock"
	if err := term.StartIPCServer(socketPath); err != nil {
		log.Printf("Failed to start IPC server: %v", err)
	}
	defer os.Remove(socketPath)

	http.HandleFunc("/push/register", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token != "" {
			pushToken = token
			log.Printf("📱 Push token registered: %s", token)
		}
		w.WriteHeader(200)
	})

	// Wire approval detection → push notification
	term.OnApproval = func(msg string) {
		log.Printf("🚨 APPROVAL DETECTED: %s", msg)
		if pushToken != "" {
			go sendPushNotification(pushToken, msg)
		}
	}

	http.HandleFunc("/debug/dump", term.HandleDump)
	http.HandleFunc("/debug/cmd", term.HandleCmd)

	// 2. Start Cloudflared tunnel automatically
	go func() {
		cloudflaredPath := "cloudflared" // assume in PATH first
		
		// Search upwards from executable dir up to 4 levels
		exePath, err := os.Executable()
		if err == nil {
			dir := filepath.Dir(exePath)
			for i := 0; i < 5; i++ {
				p := filepath.Join(dir, "cloudflared")
				if stat, err := os.Stat(p); err == nil && !stat.IsDir() {
					cloudflaredPath = p
					break
				}
				dir = filepath.Dir(dir)
			}
		}

		// Use dynamic tunnel
		cmd := exec.Command(cloudflaredPath, "tunnel", "--url", "http://127.0.0.1:9171")
		cmd.Stdout = os.Stdout

		stderrPipe, err := cmd.StderrPipe()
		if err != nil {
			log.Printf("Failed to get cloudflared stderr: %v", err)
			return
		}

		if err := cmd.Start(); err != nil {
			log.Printf("Failed to start cloudflared: %v", err)
			return
		}

		urlRegex := regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)
		scanner := bufio.NewScanner(stderrPipe)
		urlFound := false

		for scanner.Scan() {
			line := scanner.Text()
			// Forward stderr to our stderr so we still see logs
			fmt.Fprintln(os.Stderr, line)

			if !urlFound {
				if match := urlRegex.FindString(line); match != "" {
					urlFound = true
					
					// Clear terminal a bit
					fmt.Print("\n\n\n\n\n")
					
					// Print the QR Code
					config := qrterminal.Config{
						Level:     qrterminal.L,
						Writer:    os.Stdout,
						BlackChar: qrterminal.BLACK,
						WhiteChar: qrterminal.WHITE,
						QuietZone: 2,
					}
					qrterminal.GenerateWithConfig(match, config)
					
					// Print the URL as text as well
					fmt.Printf("\n🚀 POKIT Daemon is live at: %s\n", match)
					fmt.Println("👉 Scan this QR code with the POKIT mobile app to connect instantly.")
					fmt.Println("\nWaiting for connections...")
				}
			}
		}

		if err := cmd.Wait(); err != nil {
			log.Printf("cloudflared tunnel exited: %v", err)
		}
	}()

	// 3. Start HTTP server
	log.Printf("POKIT daemon :9171 (owner=%s)", *ownerUUID)
	log.Fatal(http.ListenAndServe(":9171", nil))
}

func sendPushNotification(token, message string) {
	payloadMap := map[string]string{
		"to":    token,
		"title": "DevRemote",
		"body":  message,
	}
	payloadBytes, _ := json.Marshal(payloadMap)

	resp, err := http.Post("https://exp.host/--/api/v2/push/send", "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		log.Printf("Failed to send push: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("Push sent to %s (Status: %s)", token, resp.Status)
}
