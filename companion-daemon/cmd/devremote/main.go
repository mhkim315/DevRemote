package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"devremote/companion-daemon/internal/term"
)

func main() {
	ownerUUID := flag.String("owner-uuid", "", "Supabase user UUID that owns this daemon (required for auth)")
	supabaseRef := flag.String("supabase-ref", "", "Supabase project reference for JWKS (e.g. abcdefghijklmnop)")
	flag.Parse()

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
	http.HandleFunc("/api/sessions", term.HandleSessionsV2)
	http.HandleFunc("/term/ws", term.HandleWS)
	http.HandleFunc("/term/", term.HandleHTML)
	http.HandleFunc("/term/size", term.HandleSize)
	var pushToken string

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
		cloudflaredPath := filepath.Join(filepath.Dir(os.Args[0]), "..", "cloudflared")
		if _, err := os.Stat(cloudflaredPath); os.IsNotExist(err) {
			cloudflaredPath = "./cloudflared"
		}
		cmd := exec.Command(cloudflaredPath, "tunnel", "run", "devremote")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		log.Printf("Starting cloudflared tunnel 'devremote'...")
		if err := cmd.Run(); err != nil {
			log.Printf("cloudflared tunnel err: %v", err)
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
