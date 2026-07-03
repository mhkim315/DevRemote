package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"devremote/companion-daemon/internal/term"
)

func main() {
	// 1. Core Endpoints
	http.HandleFunc("/api/sessions", term.HandleSessions)
	http.HandleFunc("/term/ws", term.HandleWS)
	http.HandleFunc("/term/", term.HandleHTML)
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
			cloudflaredPath = "./cloudflared" // Fallback to current directory
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
	log.Printf("DevRemote :9171")
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
