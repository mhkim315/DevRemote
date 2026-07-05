package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"
)

func runClient(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: pokit run <command>")
		os.Exit(1)
	}

	command := strings.Join(args, " ")

	// Connect to local daemon via Unix Socket
	socketPath := "/tmp/pokit.sock"
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		log.Fatalf("Failed to connect to POKIT daemon (is it running?): %v", err)
	}
	defer conn.Close()

	// Send the command and terminal environment as headers
	fmt.Fprintf(conn, "cmd:%s\n", command)
	termEnv := os.Getenv("TERM")
	if termEnv == "" {
		termEnv = "xterm-256color"
	}
	fmt.Fprintf(conn, "term:%s\n", termEnv)

	// Put local terminal into raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	isRaw := err == nil
	if err != nil {
		log.Printf("WARN: Not a TTY, output may be garbled: %v", err)
	}
	defer func() {
		if isRaw {
			term.Restore(int(os.Stdin.Fd()), oldState)
		}
	}()

	// Send current terminal size to daemon and EOH
	if isRaw {
		w, h, err := term.GetSize(int(os.Stdin.Fd()))
		if err == nil && w > 0 && h > 0 {
			fmt.Fprintf(conn, "size:%dx%d\n", w, h)
		}
	}
	// End of Headers
	fmt.Fprintf(conn, "\n")

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		if isRaw {
			term.Restore(int(os.Stdin.Fd()), oldState)
		}
		conn.Close()
		os.Exit(0)
	}()

	// Read from Socket, write to local Stdout
	go func() {
		io.Copy(os.Stdout, conn)
		if isRaw {
			term.Restore(int(os.Stdin.Fd()), oldState)
		}
		os.Exit(0)
	}()

	// Read from local Stdin, write to Socket
	io.Copy(conn, os.Stdin)
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

	// Read response
	buf := make([]byte, 4096)
	n, _ := conn.Read(buf)
	fmt.Print(string(buf[:n]))
}
