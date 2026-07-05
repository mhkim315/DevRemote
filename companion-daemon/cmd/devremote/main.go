package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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
	insecureLocalOnly := flag.Bool("insecure-local-only", false, "Disable authentication (DANGEROUS)")
	
	// If the user specifies "daemon" explicitly, parse flags starting from Args[2]
	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		flag.CommandLine.Parse(os.Args[2:])
	} else {
		flag.Parse()
	}

	term.OwnerUUID = *ownerUUID
	term.SupabaseProjectRef = *supabaseRef
	term.InsecureLocalOnly = *insecureLocalOnly

	if *ownerUUID == "" {
		log.Println("WARN: --owner-uuid not set. All valid Supabase tokens will be accepted (INSECURE).")
	}
	if *supabaseRef == "" {
		log.Println("WARN: --supabase-ref not set. RS256 JWKS verification disabled. Falling back to HS256 dev mode.")
	}

	// Implement Graceful Shutdown context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Received termination signal, shutting down gracefully...")
		cancel()
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()

	// 1. Core Endpoints
	term.StartTelemetryLoop(ctx)

	startWatcher()

	http.HandleFunc("/api/sessions", term.AuthMiddleware(term.HandleSessionsAPI))
	http.HandleFunc("/term/ws", term.AuthMiddleware(term.HandleWS))
	http.HandleFunc("/term/", term.AuthMiddleware(term.HandleHTML))
	var pushToken string

	// Start Unix Socket IPC Server for local 'pokit run' commands
	socketPath := "/tmp/pokit.sock"
	if err := term.StartIPCServer(socketPath); err != nil {
		log.Printf("Failed to start IPC server: %v", err)
	}
	defer os.Remove(socketPath)

	http.HandleFunc("/push/register", term.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token != "" {
			pushToken = token
			log.Printf("📱 Push token registered: %s", token)
		}
		w.WriteHeader(200)
	}))

	// Wire approval detection → push notification
	term.OnApproval = func(msg string) {
		log.Printf("🚨 APPROVAL DETECTED: %s", msg)
		if pushToken != "" {
			go sendPushNotification(pushToken, msg)
		}
	}

	http.HandleFunc("/debug/dump", term.AuthMiddleware(term.HandleDump))
	http.HandleFunc("/debug/cmd", term.AuthMiddleware(term.HandleCmd))

	go startTunnel()

	// 3. Start HTTP server
	log.Printf("POKIT daemon :9171 (owner=%s)", *ownerUUID)
	log.Fatal(http.ListenAndServe(":9171", nil))
}

func startWatcher() *watcher.Tailer {
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
	return t
}

func startTunnel() {
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

	// Use named tunnel
	cmd := exec.Command(cloudflaredPath, "tunnel", "run", "devremote")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start cloudflared: %v", err)
		return
	}

	fmt.Println("\n===========================================")
	fmt.Println("🚀 POKIT Daemon Started")
	fmt.Println("===========================================")

	// Note: URL fetching is removed for now, or you can restore the old logic
	// if needed, but since it's a named tunnel the URL is handled by cloudflare.
	
	if err := cmd.Wait(); err != nil {
		log.Printf("cloudflared tunnel exited: %v", err)
	}
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
