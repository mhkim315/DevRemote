package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"

	"devremote/companion-daemon/internal/detector"
	"devremote/companion-daemon/internal/server"
	"devremote/companion-daemon/internal/watcher"
)

func main() {
	projectDir := flag.String("project", "", "Path to Claude project directory to watch (required)")
	port := flag.String("port", "9171", "WebSocket server port")
	flag.Parse()

	if *projectDir == "" {
		fmt.Fprintf(os.Stderr, "Usage: devremote watch --project <path>\n")
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  devremote watch --project \"C:\\Users\\user\\.claude\\projects\\C--Users-user-Documents-remote-control\"\n")
		os.Exit(1)
	}

	absDir, err := filepath.Abs(*projectDir)
	if err != nil {
		log.Fatalf("Invalid project path: %v", err)
	}
	if _, err := os.Stat(absDir); os.IsNotExist(err) {
		log.Fatalf("Project directory not found: %s", absDir)
	}

	hub := server.NewHub()
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

	fmt.Println("\n=== DevRemote Daemon ===")
	fmt.Println("Mobile app connect to one of:")
	for _, ip := range server.LocalIPs() {
		fmt.Printf("  ws://%s:%s/ws\n", ip, *port)
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
