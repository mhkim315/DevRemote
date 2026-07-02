package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"devremote/companion-daemon/internal/signal"
	"devremote/companion-daemon/internal/term"
	"devremote/companion-daemon/internal/webrtc"
)

func main() {
	home, _ := os.UserHomeDir()
	keyDir := filepath.Join(home, ".devremote")

	// 1. Local signaling store (no Oracle dependency)
	sig := signal.NewStore()
	http.HandleFunc("/join", sig.Join)
	http.HandleFunc("/poll", sig.Poll)
	http.HandleFunc("/send", sig.Send)
	http.HandleFunc("/term/ws", term.HandleWS)
	http.HandleFunc("/term/", term.HandleHTML)
	http.HandleFunc("/debug/dump", term.HandleDump)
	http.HandleFunc("/debug/cmd", term.HandleCmd)

	// 2. Start HTTP server first
	go func() {
		log.Printf("DevRemote :9171")
		log.Fatal(http.ListenAndServe(":9171", nil))
	}()

	// 3. WebRTC — use local signaling
	sess := webrtc.New("http://127.0.0.1:9171", []string{"stun:stun.l.google.com:19302"}, keyDir)

	// 4. PTY → WebRTC
	tty, err := term.StartPTY(func(data []byte) {
		sess.SendRawBytes(data)
	})
	if err != nil {
		log.Fatalf("PTY: %v", err)
	}

	// 5. WebRTC → PTY
	err = sess.Start(func(data []byte) {
		tty.Write(data)
	}, func() {
		log.Printf("Data channel opened!")
	})
	if err != nil {
		log.Fatalf("WebRTC: %v", err)
	}

	log.Printf("CODE: %s", sess.Code())

	select {} // keep running
}
