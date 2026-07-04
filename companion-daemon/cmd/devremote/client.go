package main

import (
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

// runClient acts as a thin wrapper that sends local input to the background daemon
// and receives the daemon's PTY output over a Unix Domain Socket.
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

	// Send the command as the first line
	fmt.Fprintf(conn, "cmd:%s\n", command)

	// Put local terminal into raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	isRaw := err == nil
	if err != nil {
		log.Printf("WARN: Failed to set raw mode (not a TTY?): %v", err)
	}
	defer func() {
		if isRaw {
			term.Restore(int(os.Stdin.Fd()), oldState)
		}
	}()

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
